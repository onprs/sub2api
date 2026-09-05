package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	mathrand "math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocolmetadata "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/metadata"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const geminiStickySessionTTL = time.Hour

const (
	geminiMaxRetries     = 5
	geminiRetryBaseDelay = 1 * time.Second
	geminiRetryMaxDelay  = 16 * time.Second
)

// Gemini tool calling now requires `thoughtSignature` in parts that include `functionCall`.
// Many clients don't send it; we inject a known dummy signature to satisfy the validator.
// Ref: https://ai.google.dev/gemini-api/docs/thought-signatures
const geminiDummyThoughtSignature = "skip_thought_signature_validator"

type GeminiMessagesCompatService struct {
	accountRepo               AccountRepository
	groupRepo                 GroupRepository
	cache                     GatewayCache
	schedulerSnapshot         *SchedulerSnapshotService
	tokenProvider             *GeminiTokenProvider
	rateLimitService          *RateLimitService
	httpUpstream              HTTPUpstream
	antigravityGatewayService *AntigravityGatewayService
	cfg                       *config.Config
	responseHeaderFilter      *responseheaders.CompiledHeaderFilter
	providerMetadataStore     protocolconv.MetadataStore
}

func NewGeminiMessagesCompatService(
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	cache GatewayCache,
	schedulerSnapshot *SchedulerSnapshotService,
	tokenProvider *GeminiTokenProvider,
	rateLimitService *RateLimitService,
	httpUpstream HTTPUpstream,
	antigravityGatewayService *AntigravityGatewayService,
	cfg *config.Config,
) *GeminiMessagesCompatService {
	return &GeminiMessagesCompatService{
		accountRepo:               accountRepo,
		groupRepo:                 groupRepo,
		cache:                     cache,
		schedulerSnapshot:         schedulerSnapshot,
		tokenProvider:             tokenProvider,
		rateLimitService:          rateLimitService,
		httpUpstream:              httpUpstream,
		antigravityGatewayService: antigravityGatewayService,
		cfg:                       cfg,
		responseHeaderFilter:      compileResponseHeaderFilter(cfg),
		providerMetadataStore:     protocolmetadata.NewStore(protocolmetadata.DefaultTTL, protocolmetadata.DefaultMaxSize),
	}
}

func (s *GeminiMessagesCompatService) configureGoogleMetadataBridge(
	ctx context.Context,
	account *Account,
	pipelineConfig *protocolconv.PipelineConfig,
) {
	if s == nil || s.providerMetadataStore == nil || account == nil || pipelineConfig == nil ||
		pipelineConfig.Route.IntendedTarget != protocolconv.ProtocolGoogleGenAI {
		return
	}
	switch pipelineConfig.Route.Source {
	case protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolOpenAIResponses:
	default:
		return
	}
	identity, ok := ProtocolMetadataIdentityFromContext(ctx)
	if !ok {
		return
	}
	pipelineConfig.MetadataStore = s.providerMetadataStore
	pipelineConfig.MetadataScope = protocolconv.MetadataScope{
		TenantID: identity.TenantID, APIKeyID: identity.APIKeyID, GroupID: identity.GroupID,
		AccountID: account.ID, Protocol: protocolconv.ProtocolGoogleGenAI,
	}
}

// GetTokenProvider returns the token provider for OAuth accounts
func (s *GeminiMessagesCompatService) GetTokenProvider() *GeminiTokenProvider {
	return s.tokenProvider
}

func (s *GeminiMessagesCompatService) SelectAccountForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*Account, error) {
	return s.SelectAccountForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, nil)
}

func (s *GeminiMessagesCompatService) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	return selectWithSchedulerSnapshotRetry(ctx, isNoAvailableAccountSelectionError, func(attemptCtx context.Context) (*Account, error) {
		return s.selectAccountForModelWithExclusionsOnce(attemptCtx, groupID, sessionHash, requestedModel, excludedIDs)
	})
}

func (s *GeminiMessagesCompatService) selectAccountForModelWithExclusionsOnce(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	// 1. 确定目标平台和调度模式
	// Determine target platform and scheduling mode
	platform, useMixedScheduling, hasForcePlatform, err := s.resolvePlatformAndSchedulingMode(ctx, groupID)
	if err != nil {
		return nil, err
	}

	cacheKey := "gemini:" + sessionHash

	// 2. 尝试粘性会话命中
	// Try sticky session hit
	if account := s.tryStickySessionHit(ctx, groupID, sessionHash, cacheKey, requestedModel, excludedIDs, platform, useMixedScheduling); account != nil {
		return account, nil
	}

	// 3. 查询可调度账户（强制平台模式：优先按分组查找，找不到再查全部）
	// Query schedulable accounts (force platform mode: try group first, fallback to all)
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, platform, hasForcePlatform)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	// 强制平台模式下，分组中找不到账户时回退查询全部
	if len(accounts) == 0 && groupID != nil && hasForcePlatform {
		accounts, err = s.listSchedulableAccountsOnce(ctx, nil, platform, hasForcePlatform)
		if err != nil {
			return nil, fmt.Errorf("query accounts failed: %w", err)
		}
	}

	// 4. 按优先级 + LRU 选择最佳账号
	// Select best account by priority + LRU
	selected := s.selectBestGeminiAccount(ctx, accounts, requestedModel, excludedIDs, platform, useMixedScheduling)

	if selected == nil {
		if requestedModel != "" {
			return nil, fmt.Errorf("no available Gemini accounts supporting model: %s", requestedModel)
		}
		return nil, errors.New("no available Gemini accounts")
	}

	// 5. 设置粘性会话绑定
	// Set sticky session binding
	if sessionHash != "" {
		_ = s.cache.SetSessionAccountID(ctx, derefGroupID(groupID), cacheKey, selected.ID, geminiStickySessionTTL)
	}

	return s.hydrateSelectedAccount(ctx, selected)
}

// resolvePlatformAndSchedulingMode 解析目标平台和调度模式。
// 返回：平台名称、是否使用混合调度、是否强制平台、错误。
//
// resolvePlatformAndSchedulingMode resolves target platform and scheduling mode.
// Returns: platform name, whether to use mixed scheduling, whether force platform, error.
func (s *GeminiMessagesCompatService) resolvePlatformAndSchedulingMode(ctx context.Context, groupID *int64) (platform string, useMixedScheduling bool, hasForcePlatform bool, err error) {
	// 优先检查 context 中的强制平台（/antigravity 路由）
	forcePlatform, hasForcePlatform := ctx.Value(ctxkey.ForcePlatform).(string)
	if hasForcePlatform && forcePlatform != "" {
		return forcePlatform, false, true, nil
	}

	if groupID != nil {
		// 根据分组 platform 决定查询哪种账号
		var group *Group
		if ctxGroup, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(ctxGroup) && ctxGroup.ID == *groupID {
			group = ctxGroup
		} else {
			group, err = s.groupRepo.GetByIDLite(ctx, *groupID)
			if err != nil {
				return "", false, false, fmt.Errorf("get group failed: %w", err)
			}
		}
		// gemini 分组支持混合调度（包含启用了 mixed_scheduling 的 antigravity 账户）
		return group.Platform, group.Platform == PlatformGemini, false, nil
	}

	// 无分组时只使用原生 gemini 平台
	return PlatformGemini, true, false, nil
}

// tryStickySessionHit 尝试从粘性会话获取账号。
// 如果命中且账号可用则返回账号；如果账号不可用则清理会话并返回 nil。
//
// tryStickySessionHit attempts to get account from sticky session.
// Returns account if hit and usable; clears session and returns nil if account unavailable.
func (s *GeminiMessagesCompatService) tryStickySessionHit(
	ctx context.Context,
	groupID *int64,
	sessionHash, cacheKey, requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) *Account {
	if sessionHash == "" {
		return nil
	}

	accountID, err := s.cache.GetSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
	if err != nil || accountID <= 0 {
		return nil
	}

	if _, excluded := excludedIDs[accountID]; excluded {
		return nil
	}

	account, err := s.getSchedulableAccount(ctx, accountID)
	if err != nil {
		return nil
	}

	// 检查账号是否需要清理粘性会话
	// Check if sticky session should be cleared
	if shouldClearStickySession(account, requestedModel) {
		_ = s.cache.DeleteSessionAccountID(ctx, derefGroupID(groupID), cacheKey)
		return nil
	}

	// 验证账号是否可用于当前请求
	// Verify account is usable for current request
	if !s.isAccountUsableForRequest(ctx, account, requestedModel, platform, useMixedScheduling) {
		return nil
	}

	// 刷新会话 TTL 并返回账号
	// Refresh session TTL and return account
	_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), cacheKey, geminiStickySessionTTL)
	return account
}

// isAccountUsableForRequest 检查账号是否可用于当前请求。
// 验证：模型调度、模型支持、平台匹配、速率限制预检。
//
// isAccountUsableForRequest checks if account is usable for current request.
// Validates: model scheduling, model support, platform matching, rate limit precheck.
func (s *GeminiMessagesCompatService) isAccountUsableForRequest(
	ctx context.Context,
	account *Account,
	requestedModel, platform string,
	useMixedScheduling bool,
) bool {
	return s.isAccountUsableForRequestWithPrecheck(ctx, account, requestedModel, platform, useMixedScheduling, nil)
}

func (s *GeminiMessagesCompatService) isAccountUsableForRequestWithPrecheck(
	ctx context.Context,
	account *Account,
	requestedModel, platform string,
	useMixedScheduling bool,
	precheckResult map[int64]bool,
) bool {
	// 检查模型调度能力
	// Check model scheduling capability
	if !account.IsSchedulableForModelWithContext(ctx, requestedModel) {
		return false
	}

	// 检查模型支持
	// Check model support
	if requestedModel != "" && !s.isModelSupportedByAccount(account, requestedModel) {
		return false
	}

	// 检查平台匹配
	// Check platform matching
	if !s.isAccountValidForPlatform(account, platform, useMixedScheduling) {
		return false
	}

	// 速率限制预检
	// Rate limit precheck
	if !s.passesRateLimitPreCheckWithCache(ctx, account, requestedModel, precheckResult) {
		return false
	}

	return true
}

// isAccountValidForPlatform 检查账号是否匹配目标平台。
// 原生平台直接匹配；混合调度模式下 antigravity 需要启用 mixed_scheduling。
//
// isAccountValidForPlatform checks if account matches target platform.
// Native platform matches directly; mixed scheduling mode requires antigravity to enable mixed_scheduling.
func (s *GeminiMessagesCompatService) isAccountValidForPlatform(account *Account, platform string, useMixedScheduling bool) bool {
	if account.Platform == platform {
		return true
	}
	if useMixedScheduling && account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled() {
		return true
	}
	return false
}

func (s *GeminiMessagesCompatService) passesRateLimitPreCheckWithCache(ctx context.Context, account *Account, requestedModel string, precheckResult map[int64]bool) bool {
	if s.rateLimitService == nil || requestedModel == "" {
		return true
	}

	if precheckResult != nil {
		if ok, exists := precheckResult[account.ID]; exists {
			return ok
		}
	}

	ok, err := s.rateLimitService.PreCheckUsage(ctx, account, requestedModel)
	if err != nil {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheck] Account %d precheck error: %v", account.ID, err)
	}
	return ok
}

// selectBestGeminiAccount 从候选账号中选择最佳账号（优先级 + LRU + OAuth 优先）。
// 返回 nil 表示无可用账号。
//
// selectBestGeminiAccount selects best account from candidates (priority + LRU + OAuth preferred).
// Returns nil if no available account.
func (s *GeminiMessagesCompatService) selectBestGeminiAccount(
	ctx context.Context,
	accounts []Account,
	requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) *Account {
	var selected *Account
	precheckResult := s.buildPreCheckUsageResultMap(ctx, accounts, requestedModel)

	for i := range accounts {
		acc := &accounts[i]

		// 跳过被排除的账号
		if _, excluded := excludedIDs[acc.ID]; excluded {
			continue
		}

		// 检查账号是否可用于当前请求
		if !s.isAccountUsableForRequestWithPrecheck(ctx, acc, requestedModel, platform, useMixedScheduling, precheckResult) {
			continue
		}

		// 选择最佳账号
		if selected == nil {
			selected = acc
			continue
		}

		if s.isBetterGeminiAccount(acc, selected) {
			selected = acc
		}
	}

	return selected
}

func (s *GeminiMessagesCompatService) buildPreCheckUsageResultMap(ctx context.Context, accounts []Account, requestedModel string) map[int64]bool {
	if s.rateLimitService == nil || requestedModel == "" || len(accounts) == 0 {
		return nil
	}

	candidates := make([]*Account, 0, len(accounts))
	for i := range accounts {
		candidates = append(candidates, &accounts[i])
	}

	result, err := s.rateLimitService.PreCheckUsageBatch(ctx, candidates, requestedModel)
	if err != nil {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheckBatch] failed: %v", err)
	}
	return result
}

// isBetterGeminiAccount 判断 candidate 是否比 current 更优。
// 规则：优先级更高（数值更小）优先；同优先级时，未使用过的优先（OAuth > 非 OAuth），其次是最久未使用的。
//
// isBetterGeminiAccount checks if candidate is better than current.
// Rules: higher priority (lower value) wins; same priority: never used (OAuth > non-OAuth) > least recently used.
func (s *GeminiMessagesCompatService) isBetterGeminiAccount(candidate, current *Account) bool {
	// 优先级更高（数值更小）
	if candidate.Priority < current.Priority {
		return true
	}
	if candidate.Priority > current.Priority {
		return false
	}

	// 同优先级，比较最后使用时间
	switch {
	case candidate.LastUsedAt == nil && current.LastUsedAt != nil:
		// candidate 从未使用，优先
		return true
	case candidate.LastUsedAt != nil && current.LastUsedAt == nil:
		// current 从未使用，保持
		return false
	case candidate.LastUsedAt == nil && current.LastUsedAt == nil:
		// 都未使用，优先选择 OAuth 账号（更兼容 Code Assist 流程）
		return candidate.Type == AccountTypeOAuth && current.Type != AccountTypeOAuth
	default:
		// 都使用过，选择最久未使用的
		return candidate.LastUsedAt.Before(*current.LastUsedAt)
	}
}

// isModelSupportedByAccount 根据账户平台检查模型支持
func (s *GeminiMessagesCompatService) isModelSupportedByAccount(account *Account, requestedModel string) bool {
	if account.Platform == PlatformAntigravity {
		if strings.TrimSpace(requestedModel) == "" {
			return true
		}
		return mapAntigravityModel(account, requestedModel) != ""
	}
	return account.IsModelSupported(requestedModel)
}

// GetAntigravityGatewayService 返回 AntigravityGatewayService
func (s *GeminiMessagesCompatService) GetAntigravityGatewayService() *AntigravityGatewayService {
	return s.antigravityGatewayService
}

func (s *GeminiMessagesCompatService) getSchedulableAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s.schedulerSnapshot != nil {
		return s.schedulerSnapshot.GetAccount(ctx, accountID)
	}
	return s.accountRepo.GetByID(ctx, accountID)
}

func (s *GeminiMessagesCompatService) hydrateSelectedAccount(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || s.schedulerSnapshot == nil {
		return account, nil
	}
	hydrated, err := s.schedulerSnapshot.GetAccount(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if hydrated == nil {
		return nil, fmt.Errorf("selected gemini account %d not found during hydration", account.ID)
	}
	return hydrated, nil
}

func (s *GeminiMessagesCompatService) listSchedulableAccountsOnce(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]Account, error) {
	if s.schedulerSnapshot != nil {
		accounts, _, err := s.schedulerSnapshot.ListSchedulableAccounts(ctx, groupID, platform, hasForcePlatform)
		return accounts, err
	}

	useMixedScheduling := platform == PlatformGemini && !hasForcePlatform
	queryPlatforms := []string{platform}
	if useMixedScheduling {
		queryPlatforms = []string{platform, PlatformAntigravity}
	}

	if groupID != nil {
		return s.accountRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, queryPlatforms)
	}
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return s.accountRepo.ListSchedulableByPlatforms(ctx, queryPlatforms)
	}
	return s.accountRepo.ListSchedulableUngroupedByPlatforms(ctx, queryPlatforms)
}

func (s *GeminiMessagesCompatService) validateUpstreamBaseURL(raw string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	return normalized, nil
}

// HasAntigravityAccounts 检查是否有可用的 antigravity 账户
func (s *GeminiMessagesCompatService) HasAntigravityAccounts(ctx context.Context, groupID *int64) (bool, error) {
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, PlatformAntigravity, false)
	if err != nil {
		return false, err
	}
	return len(accounts) > 0, nil
}

// SelectAccountForAIStudioEndpoints selects an account that is likely to succeed against
// generativelanguage.googleapis.com (e.g. GET /v1beta/models).
//
// Preference order:
// 1) API key accounts (AI Studio)
// 2) OAuth accounts without project_id (AI Studio OAuth)
// 3) OAuth accounts explicitly marked as ai_studio
// 4) Any remaining Gemini accounts (fallback)
func (s *GeminiMessagesCompatService) SelectAccountForAIStudioEndpoints(ctx context.Context, groupID *int64) (*Account, error) {
	return selectWithSchedulerSnapshotRetry(ctx, isNoAvailableAccountSelectionError, func(attemptCtx context.Context) (*Account, error) {
		return s.selectAccountForAIStudioEndpointsOnce(attemptCtx, groupID)
	})
}

func (s *GeminiMessagesCompatService) selectAccountForAIStudioEndpointsOnce(ctx context.Context, groupID *int64) (*Account, error) {
	accounts, err := s.listSchedulableAccountsOnce(ctx, groupID, PlatformGemini, true)
	if err != nil {
		return nil, fmt.Errorf("query accounts failed: %w", err)
	}
	if len(accounts) == 0 {
		return nil, errors.New("no available Gemini accounts")
	}

	rank := func(a *Account) int {
		if a == nil {
			return 999
		}
		switch a.Type {
		case AccountTypeAPIKey:
			if strings.TrimSpace(a.GetCredential("api_key")) != "" {
				return 0
			}
			return 999
		case AccountTypeOAuth:
			if strings.TrimSpace(a.GetCredential("project_id")) == "" {
				return 1
			}
			if strings.TrimSpace(a.GetCredential("oauth_type")) == "ai_studio" {
				return 2
			}
			// Code Assist OAuth tokens often lack AI Studio scopes for models listing.
			return 3
		case AccountTypeServiceAccount:
			// Vertex service accounts use aiplatform.googleapis.com, not the AI Studio
			// endpoint (generativelanguage.googleapis.com), so they cannot serve these requests.
			return 999
		default:
			return 999
		}
	}

	var selected *Account
	for i := range accounts {
		acc := &accounts[i]
		if !acc.IsSchedulable() || rank(acc) >= 999 {
			continue
		}
		if selected == nil {
			selected = acc
			continue
		}

		r1, r2 := rank(acc), rank(selected)
		if r1 < r2 {
			selected = acc
			continue
		}
		if r1 > r2 {
			continue
		}

		if acc.Priority < selected.Priority {
			selected = acc
		} else if acc.Priority == selected.Priority {
			switch {
			case acc.LastUsedAt == nil && selected.LastUsedAt != nil:
				selected = acc
			case acc.LastUsedAt != nil && selected.LastUsedAt == nil:
				// keep selected
			case acc.LastUsedAt == nil && selected.LastUsedAt == nil:
				if acc.Type == AccountTypeOAuth && selected.Type != AccountTypeOAuth {
					selected = acc
				}
			default:
				if acc.LastUsedAt.Before(*selected.LastUsedAt) {
					selected = acc
				}
			}
		}
	}

	if selected == nil {
		return nil, errors.New("no available Gemini accounts")
	}
	hydrated, err := s.hydrateSelectedAccount(ctx, selected)
	if err != nil {
		return nil, err
	}
	if hydrated == nil || !hydrated.IsSchedulable() || rank(hydrated) >= 999 {
		return nil, errors.New("no available Gemini accounts")
	}
	return hydrated, nil
}

func (s *GeminiMessagesCompatService) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	beginGeminiImageOutputObservation(c)
	startTime := time.Now()

	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("missing model")
	}

	originalModel := req.Model
	mappedModel := req.Model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(req.Model)
	}

	pipeline, geminiReq, err := s.newClaudeMessagesGooglePipeline(ctx, account, body, originalModel, mappedModel)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	originalClaudeBody := body

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	var requestIDHeader string
	var buildReq func(ctx context.Context) (*http.Request, string, error)
	useUpstreamStream := req.Stream
	if account.Type == AccountTypeOAuth && !req.Stream && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
	}

	switch account.Type {
	case AccountTypeAPIKey:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			apiKey := account.GetCredential("api_key")
			if strings.TrimSpace(apiKey) == "" {
				return nil, "", errors.New("gemini api_key not configured")
			}

			baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
			normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if req.Stream {
				action = "streamGenerateContent"
			}
			fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, action, req.Stream)
			if err != nil {
				return nil, "", err
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("x-goog-api-key", apiKey)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	case AccountTypeOAuth:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			projectID := strings.TrimSpace(account.GetCredential("project_id"))

			action := "generateContent"
			if useUpstreamStream {
				action = "streamGenerateContent"
			}

			// Two modes for OAuth:
			// 1. With project_id -> Code Assist API (wrapped request)
			// 2. Without project_id -> AI Studio API (direct OAuth, like API key but with Bearer token)
			if projectID != "" {
				// Mode 1: Code Assist API
				baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
				if err != nil {
					return nil, "", err
				}
				fullURL := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), action)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				wrapped := map[string]any{
					"model":   mappedModel,
					"project": projectID,
				}
				var inner any
				if err := json.Unmarshal(geminiReq, &inner); err != nil {
					return nil, "", fmt.Errorf("failed to parse gemini request: %w", err)
				}
				wrapped["request"] = inner
				wrappedBytes, _ := json.Marshal(wrapped)

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(wrappedBytes))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				upstreamReq.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
				return upstreamReq, "x-request-id", nil
			} else {
				// Mode 2: AI Studio API with OAuth (like API key mode, but using Bearer token)
				baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
				normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
				if err != nil {
					return nil, "", err
				}

				fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, action, useUpstreamStream)
				if err != nil {
					return nil, "", err
				}

				restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				return upstreamReq, "x-request-id", nil
			}
		}
		requestIDHeader = "x-request-id"

	case AccountTypeServiceAccount:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			action := "generateContent"
			if req.Stream {
				action = "streamGenerateContent"
			}
			fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, action, req.Stream)
			if err != nil {
				return nil, "", err
			}

			restGeminiReq := normalizeGeminiRequestForAIStudio(geminiReq)
			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(restGeminiReq))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	default:
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}

	var resp *http.Response
	var lastError *protocoltransport.Response
	signatureRetryStage := 0
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			// Local build error: don't retry.
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeClaudeError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			}
			return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", err.Error())
		}
		requestIDHeader = idHeader

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:            opsUpstreamProxyID(account),
				ProxyName:          opsUpstreamProxyName(account),
				Platform:           account.Platform,
				AccountID:          account.ID,
				AccountName:        account.Name,
				UpstreamStatusCode: 0,
				Kind:               "request_error",
				Message:            safeErr,
			})
			if attempt < geminiMaxRetries {
				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream request failed, retry %d/%d: %v", account.ID, attempt, geminiMaxRetries, err)
				sleepGeminiBackoff(attempt)
				continue
			}
			setOpsUpstreamError(c, 0, safeErr, "")
			return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed after retries: "+safeErr)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			upstream, collectErr := s.collectGeminiStructuredUpstreamError(resp, useUpstreamStream, requestIDHeader)
			if collectErr != nil {
				return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream error")
			}
			lastError = &upstream

			// Signature validation errors may be fixed by conservatively
			// downgrading thinking and tool history in two stages.
			if upstream.StatusCode == http.StatusBadRequest && signatureRetryStage < 2 {
				if isGeminiSignatureRelatedError(upstream.Body) {
					upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
					upstreamDetail := ""
					if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
						maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
						if maxBytes <= 0 {
							maxBytes = 2048
						}
						upstreamDetail = truncateString(string(upstream.Body), maxBytes)
					}
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						ProxyID:   opsUpstreamProxyID(account),
						ProxyName: opsUpstreamProxyName(account),
						Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
						UpstreamStatusCode: upstream.StatusCode, UpstreamRequestID: upstream.RequestID,
						Kind: "signature_error", Message: upstreamMsg, Detail: upstreamDetail,
					})

					var strippedClaudeBody []byte
					stageName := ""
					switch signatureRetryStage {
					case 0:
						strippedClaudeBody = FilterThinkingBlocksForRetry(originalClaudeBody, originalModel)
						stageName = "thinking-only"
						signatureRetryStage = 1
					default:
						strippedClaudeBody = FilterSignatureSensitiveBlocksForRetry(originalClaudeBody, originalModel)
						stageName = "thinking+tools"
						signatureRetryStage = 2
					}
					retryPipeline, retryGeminiReq, txErr := s.newClaudeMessagesGooglePipeline(ctx, account, strippedClaudeBody, originalModel, mappedModel)
					if txErr == nil {
						logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: detected signature-related 400, retrying with downgraded Claude blocks (%s)", account.ID, stageName)
						pipeline = retryPipeline
						geminiReq = retryGeminiReq
						lastError = nil
						sleepGeminiBackoff(1)
						continue
					}

				}
			}
			// 可恢复的签名错误必须先完成降级重试；仅对未恢复错误应用账号策略。
			if s.checkStructuredErrorPolicyInLoop(ctx, account, upstream, mappedModel) {
				break
			}
			if s.shouldRetryGeminiUpstreamError(account, upstream.StatusCode) {
				if upstream.StatusCode == http.StatusForbidden && isGeminiInsufficientScope(upstream.Headers, upstream.Body) {
					break
				}
				if upstream.StatusCode == http.StatusTooManyRequests {
					s.handleGeminiUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body)
				}
				if attempt < geminiMaxRetries {
					upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
					upstreamDetail := ""
					if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
						maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
						if maxBytes <= 0 {
							maxBytes = 2048
						}
						upstreamDetail = truncateString(string(upstream.Body), maxBytes)
					}
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
						Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
						UpstreamStatusCode: upstream.StatusCode, UpstreamRequestID: upstream.RequestID,
						Kind: "retry", Message: upstreamMsg, Detail: upstreamDetail,
					})
					logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, upstream.StatusCode, attempt, geminiMaxRetries)
					lastError = nil
					sleepGeminiBackoff(attempt)
					continue
				}
			}
			break
		}

		lastError = nil
		break
	}

	if lastError != nil {
		respBody := lastError.Body
		// 统一错误策略：自定义错误码 + 临时不可调度
		if s.rateLimitService != nil {
			policy := s.rateLimitService.CheckErrorPolicy(ctx, account, lastError.StatusCode, respBody, mappedModel)
			switch policy {
			case ErrorPolicySkipped:
				if failoverErr := s.skippedErrorPolicyFailoverError(c, account, lastError.StatusCode, respBody, lastError.RequestID); failoverErr != nil {
					failoverErr.ResponseHeaders = protocoltransport.CloneHeaders(lastError.Headers)
					return nil, failoverErr
				}
				if account.IsCustomErrorCodesEnabled() {
					return nil, s.writeGeminiCustomCodeSkippedError(c, account, lastError.StatusCode, lastError.RequestID, respBody, func() {
						_ = s.writeClaudeError(c, http.StatusInternalServerError, "api_error", geminiCustomCodeSkippedClientMessage)
					})
				}
				// 池模式仅跳过账号状态标记，客户端仍收到按真实状态映射的错误。
				return nil, s.writeGeminiMappedError(c, account, lastError.StatusCode, lastError.RequestID, respBody)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				if policy == ErrorPolicyMatched {
					s.handleGeminiUpstreamError(ctx, account, lastError.StatusCode, lastError.Headers, respBody)
				}
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:   opsUpstreamProxyID(account),
					ProxyName: opsUpstreamProxyName(account),
					Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
					UpstreamStatusCode: lastError.StatusCode, UpstreamRequestID: lastError.RequestID,
					Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
				})
				return nil, &UpstreamFailoverError{
					StatusCode: lastError.StatusCode, ResponseBody: respBody,
					ResponseHeaders: protocoltransport.CloneHeaders(lastError.Headers),
				}
			}
		}

		s.handleGeminiUpstreamError(ctx, account, lastError.StatusCode, lastError.Headers, respBody)
		if lastError.StatusCode == http.StatusBadRequest {
			msg400 := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			if isGoogleProjectConfigError(msg400) {
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(respBody), maxBytes)
				}
				log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:   opsUpstreamProxyID(account),
					ProxyName: opsUpstreamProxyName(account),
					Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
					UpstreamStatusCode: lastError.StatusCode, UpstreamRequestID: lastError.RequestID,
					Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
				})
				return nil, &UpstreamFailoverError{
					StatusCode: lastError.StatusCode, ResponseBody: respBody,
					ResponseHeaders: protocoltransport.CloneHeaders(lastError.Headers), RetryableOnSameAccount: true,
				}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(lastError.StatusCode) {
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(respBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:   opsUpstreamProxyID(account),
				ProxyName: opsUpstreamProxyName(account),
				Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: lastError.StatusCode, UpstreamRequestID: lastError.RequestID,
				Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: lastError.StatusCode, ResponseBody: respBody,
				ResponseHeaders: protocoltransport.CloneHeaders(lastError.Headers),
			}
		}
		return nil, s.writeGeminiMappedError(c, account, lastError.StatusCode, lastError.RequestID, respBody)
	}

	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = resp.Header.Get("x-goog-request-id")
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	clientDisconnected := false
	if req.Stream {
		streamRes, err := s.handleStreamingResponse(c, resp, pipeline, startTime)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
		clientDisconnected = streamRes.clientDisconnected
	} else {
		if useUpstreamStream {
			collected, usageObj, rawStreamBody, err := s.collectGeminiSSEWithRaw(resp.Body, true)
			if err != nil {
				return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream stream")
			}
			collectedBytes, _ := json.Marshal(collected)
			upstreamResponseModelObserverFromContext(c).ObserveGemini(collectedBytes)
			observeGeminiImageOutputs(c, collectedBytes)
			usage, err = s.renderGoogleAnthropicResponse(c, resp, pipeline, collectedBytes, usageObj, startTime, rawStreamBody)
			if err != nil {
				return nil, err
			}
		} else {
			defer func() { _ = resp.Body.Close() }()
			usage, err = s.handleNonStreamingResponse(c, resp, pipeline, startTime)
			if err != nil {
				return nil, err
			}
		}
	}

	// 图片生成计费
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	imageCount := resolveGeminiImageCount(c, originalModel, mappedModel)

	return &ForwardResult{
		RequestID:                     requestID,
		ActualProtocol:                protocolconv.ProtocolGoogleGenAI,
		UpstreamHeaders:               resp.Header,
		Usage:                         *usage,
		Model:                         originalModel,
		UpstreamModel:                 mappedModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		Stream:                        req.Stream,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ImageCount:                    imageCount,
		ImageSize:                     imageSize,
		ImageInputSize:                imageInputSize,
		ClientDisconnect:              clientDisconnected,
	}, nil
}

func isGeminiSignatureRelatedError(respBody []byte) bool {
	msg := strings.ToLower(strings.TrimSpace(extractAntigravityErrorMessage(respBody)))
	if msg == "" {
		msg = strings.ToLower(string(respBody))
	}
	return strings.Contains(msg, "thought_signature") || strings.Contains(msg, "signature")
}

func (s *GeminiMessagesCompatService) ForwardNative(ctx context.Context, c *gin.Context, account *Account, originalModel string, action string, stream bool, body []byte) (*ForwardResult, error) {
	beginUpstreamResponseModelObservation(c)
	beginGeminiImageOutputObservation(c)
	startTime := time.Now()

	if strings.TrimSpace(originalModel) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing model in URL")
	}
	if strings.TrimSpace(action) == "" {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Missing action in URL")
	}
	if len(body) == 0 {
		return nil, s.writeGoogleError(c, http.StatusBadRequest, "Request body is empty")
	}

	// 过滤掉 parts 为空的消息（Gemini API 不接受空 parts）
	if filteredBody, err := filterEmptyPartsFromGeminiRequest(body); err == nil {
		body = filteredBody
	}

	switch action {
	case "generateContent", "streamGenerateContent", "countTokens":
		// ok
	default:
		return nil, s.writeGoogleError(c, http.StatusNotFound, "Unsupported action: "+action)
	}

	// Some Gemini upstreams validate tool call parts strictly; ensure any `functionCall` part includes a
	// `thoughtSignature` to avoid frequent INVALID_ARGUMENT 400s.
	body = ensureGeminiFunctionCallThoughtSignatures(body)

	mappedModel := originalModel
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(originalModel)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	useUpstreamStream := stream
	upstreamAction := action
	if account.Type == AccountTypeOAuth && !stream && action == "generateContent" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
		// Code Assist's non-streaming generateContent may return no content; use streaming upstream and aggregate.
		useUpstreamStream = true
		upstreamAction = "streamGenerateContent"
	}
	forceAIStudio := action == "countTokens"

	var requestIDHeader string
	var buildReq func(ctx context.Context) (*http.Request, string, error)

	switch account.Type {
	case AccountTypeAPIKey:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			apiKey := account.GetCredential("api_key")
			if strings.TrimSpace(apiKey) == "" {
				return nil, "", errors.New("gemini api_key not configured")
			}

			baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
			normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
			if err != nil {
				return nil, "", err
			}

			fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, upstreamAction, useUpstreamStream)
			if err != nil {
				return nil, "", err
			}

			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("x-goog-api-key", apiKey)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	case AccountTypeOAuth:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			projectID := strings.TrimSpace(account.GetCredential("project_id"))

			// Two modes for OAuth:
			// 1. With project_id -> Code Assist API (wrapped request)
			// 2. Without project_id -> AI Studio API (direct OAuth, like API key but with Bearer token)
			if projectID != "" && !forceAIStudio {
				// Mode 1: Code Assist API
				baseURL, err := s.validateUpstreamBaseURL(geminicli.GeminiCliBaseURL)
				if err != nil {
					return nil, "", err
				}
				fullURL := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), upstreamAction)
				if useUpstreamStream {
					fullURL += "?alt=sse"
				}

				wrapped := map[string]any{
					"model":   mappedModel,
					"project": projectID,
				}
				var inner any
				if err := json.Unmarshal(body, &inner); err != nil {
					return nil, "", fmt.Errorf("failed to parse gemini request: %w", err)
				}
				wrapped["request"] = inner
				wrappedBytes, _ := json.Marshal(wrapped)

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(wrappedBytes))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				upstreamReq.Header.Set("User-Agent", geminicli.GeminiCLIUserAgent)
				return upstreamReq, "x-request-id", nil
			} else {
				// Mode 2: AI Studio API with OAuth (like API key mode, but using Bearer token)
				baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
				normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
				if err != nil {
					return nil, "", err
				}

				fullURL, err := buildGeminiAIStudioModelActionURL(normalizedBaseURL, mappedModel, upstreamAction, useUpstreamStream)
				if err != nil {
					return nil, "", err
				}

				upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
				if err != nil {
					return nil, "", err
				}
				upstreamReq.Header.Set("Content-Type", "application/json")
				upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
				return upstreamReq, "x-request-id", nil
			}
		}
		requestIDHeader = "x-request-id"

	case AccountTypeServiceAccount:
		buildReq = func(ctx context.Context) (*http.Request, string, error) {
			if s.tokenProvider == nil {
				return nil, "", errors.New("gemini token provider not configured")
			}
			accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
			if err != nil {
				return nil, "", err
			}

			fullURL, err := buildVertexGeminiURL(account.VertexProjectID(), account.VertexLocation(mappedModel), mappedModel, upstreamAction, useUpstreamStream)
			if err != nil {
				return nil, "", err
			}

			upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
			if err != nil {
				return nil, "", err
			}
			upstreamReq.Header.Set("Content-Type", "application/json")
			upstreamReq.Header.Set("Authorization", "Bearer "+accessToken)
			return upstreamReq, "x-request-id", nil
		}
		requestIDHeader = "x-request-id"

	default:
		return nil, s.writeGoogleError(c, http.StatusBadGateway, "Unsupported account type: "+account.Type)
	}

	var resp *http.Response
	var requestPipeline *protocolconv.Pipeline
	var lastError *protocoltransport.Response
	for attempt := 1; attempt <= geminiMaxRetries; attempt++ {
		upstreamReq, idHeader, err := buildReq(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if strings.Contains(err.Error(), "missing project_id") {
				return nil, s.writeGoogleError(c, http.StatusBadRequest, err.Error())
			}
			return nil, s.writeGoogleError(c, http.StatusBadGateway, err.Error())
		}
		requestIDHeader = idHeader
		if action != "countTokens" {
			attemptPipeline, pipelineErr := newGoogleIdentityPipeline(account, originalModel, mappedModel)
			if pipelineErr != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, pipelineErr.Error())
			}
			if _, pipelineErr = attemptPipeline.ConvertRequest(body); pipelineErr != nil {
				return nil, s.writeGoogleError(c, http.StatusBadRequest, pipelineErr.Error())
			}
			requestPipeline = attemptPipeline
		}

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:   opsUpstreamProxyID(account),
				ProxyName: opsUpstreamProxyName(account),
				Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: 0,
				Kind:               "request_error", Message: safeErr,
			})
			if attempt < geminiMaxRetries {
				logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream request failed, retry %d/%d: %v", account.ID, attempt, geminiMaxRetries, err)
				sleepGeminiBackoff(attempt)
				continue
			}
			if action == "countTokens" {
				estimated := estimateGeminiCountTokens(body)
				c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
				return &ForwardResult{RequestID: "", Usage: ClaudeUsage{}, Model: originalModel, UpstreamModel: mappedModel, Duration: time.Since(startTime)}, nil
			}
			setOpsUpstreamError(c, 0, safeErr, "")
			return nil, s.writeGoogleError(c, http.StatusBadGateway, "Upstream request failed after retries: "+safeErr)
		}

		if resp.StatusCode >= http.StatusBadRequest {
			upstream, collectErr := s.collectGeminiStructuredUpstreamError(resp, useUpstreamStream, requestIDHeader)
			if collectErr != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to read upstream error")
			}
			lastError = &upstream
			if s.checkStructuredErrorPolicyInLoop(ctx, account, upstream, mappedModel) {
				break
			}
			if s.shouldRetryGeminiUpstreamError(account, upstream.StatusCode) {
				if upstream.StatusCode == http.StatusForbidden && isGeminiInsufficientScope(upstream.Headers, upstream.Body) {
					break
				}
				if upstream.StatusCode == http.StatusTooManyRequests {
					s.handleGeminiUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body)
				}
				if attempt < geminiMaxRetries {
					upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
					upstreamDetail := ""
					if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
						maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
						if maxBytes <= 0 {
							maxBytes = 2048
						}
						upstreamDetail = truncateString(string(upstream.Body), maxBytes)
					}
					appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
						ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
						Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
						UpstreamStatusCode: upstream.StatusCode, UpstreamRequestID: upstream.RequestID,
						Kind: "retry", Message: upstreamMsg, Detail: upstreamDetail,
					})
					logger.LegacyPrintf("service.gemini_messages_compat", "Gemini account %d: upstream status %d, retry %d/%d", account.ID, upstream.StatusCode, attempt, geminiMaxRetries)
					lastError = nil
					sleepGeminiBackoff(attempt)
					continue
				}
				if action == "countTokens" {
					estimated := estimateGeminiCountTokens(body)
					c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
					return &ForwardResult{RequestID: "", Usage: ClaudeUsage{}, Model: originalModel, UpstreamModel: mappedModel, Duration: time.Since(startTime)}, nil
				}
			}
			break
		}

		lastError = nil
		break
	}
	responseBodyOwned := lastError == nil && resp != nil
	defer func() {
		if responseBodyOwned && resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	requestID := ""
	if lastError != nil {
		requestID = lastError.RequestID
	} else if resp != nil {
		requestID = resp.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = resp.Header.Get("x-goog-request-id")
		}
	}
	if requestID != "" {
		c.Header("x-request-id", requestID)
	}

	isOAuth := account.Type == AccountTypeOAuth
	hasVendorEnvelope := isOAuth && strings.TrimSpace(account.GetCredential("project_id")) != "" && !forceAIStudio

	if lastError != nil {
		statusCode := lastError.StatusCode
		headers := lastError.Headers
		respBody := lastError.Body
		// Best-effort fallback for OAuth tokens missing AI Studio scopes when calling countTokens.
		if action == "countTokens" && isOAuth && isGeminiInsufficientScope(headers, respBody) {
			estimated := estimateGeminiCountTokens(body)
			c.JSON(http.StatusOK, map[string]any{"totalTokens": estimated})
			return &ForwardResult{
				RequestID:       requestID,
				UpstreamHeaders: resp.Header,
				Usage:           ClaudeUsage{},
				Model:           originalModel,
				UpstreamModel:   mappedModel,
				Stream:          false,
				Duration:        time.Since(startTime),
				FirstTokenMs:    nil,
			}, nil
		}

		if s.rateLimitService != nil {
			policy := s.rateLimitService.CheckErrorPolicy(ctx, account, statusCode, respBody, mappedModel)
			switch policy {
			case ErrorPolicySkipped:
				if failoverErr := s.skippedErrorPolicyFailoverError(c, account, statusCode, respBody, requestID); failoverErr != nil {
					failoverErr.ResponseHeaders = protocoltransport.CloneHeaders(headers)
					return nil, failoverErr
				}
				if account.IsCustomErrorCodesEnabled() {
					return nil, s.writeGeminiCustomCodeSkippedError(c, account, statusCode, requestID, respBody, func() {
						_ = s.writeGoogleError(c, http.StatusInternalServerError, geminiCustomCodeSkippedClientMessage)
					})
				}
				// 池模式仅跳过账号状态标记，原始状态码与响应体保持不变。
				return nil, s.writeGeminiNativeUpstreamError(c, account, statusCode, headers, respBody, requestID, isOAuth)
			case ErrorPolicyMatched, ErrorPolicyTempUnscheduled:
				if policy == ErrorPolicyMatched {
					s.handleGeminiUpstreamError(ctx, account, statusCode, headers, respBody)
				}
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:   opsUpstreamProxyID(account),
					ProxyName: opsUpstreamProxyName(account),
					Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
					UpstreamStatusCode: statusCode, UpstreamRequestID: requestID,
					Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
				})
				return nil, &UpstreamFailoverError{
					StatusCode: statusCode, ResponseBody: respBody,
					ResponseHeaders: protocoltransport.CloneHeaders(headers),
				}
			}
		}

		s.handleGeminiUpstreamError(ctx, account, statusCode, headers, respBody)
		if statusCode == http.StatusBadRequest {
			msg400 := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
			if isGoogleProjectConfigError(msg400) {
				evBody := unwrapIfNeeded(isOAuth, respBody)
				upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
				upstreamDetail := ""
				if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
					maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
					if maxBytes <= 0 {
						maxBytes = 2048
					}
					upstreamDetail = truncateString(string(evBody), maxBytes)
				}
				log.Printf("[Gemini] status=400 google_config_error failover=true upstream_message=%q account=%d", upstreamMsg, account.ID)
				appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
					ProxyID:   opsUpstreamProxyID(account),
					ProxyName: opsUpstreamProxyName(account),
					Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
					UpstreamStatusCode: statusCode, UpstreamRequestID: requestID,
					Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
				})
				return nil, &UpstreamFailoverError{
					StatusCode: statusCode, ResponseBody: evBody,
					ResponseHeaders: protocoltransport.CloneHeaders(headers), RetryableOnSameAccount: true,
				}
			}
		}
		if s.shouldFailoverGeminiUpstreamError(statusCode) {
			evBody := unwrapIfNeeded(isOAuth, respBody)
			upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(evBody)))
			upstreamDetail := ""
			if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
				maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
				if maxBytes <= 0 {
					maxBytes = 2048
				}
				upstreamDetail = truncateString(string(evBody), maxBytes)
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				ProxyID:   opsUpstreamProxyID(account),
				ProxyName: opsUpstreamProxyName(account),
				Platform:  account.Platform, AccountID: account.ID, AccountName: account.Name,
				UpstreamStatusCode: statusCode, UpstreamRequestID: requestID,
				Kind: "failover", Message: upstreamMsg, Detail: upstreamDetail,
			})
			return nil, &UpstreamFailoverError{
				StatusCode: statusCode, ResponseBody: evBody,
				ResponseHeaders: protocoltransport.CloneHeaders(headers),
			}
		}

		return nil, s.writeGeminiNativeUpstreamError(c, account, statusCode, headers, respBody, requestID, isOAuth)
	}

	var usage *ClaudeUsage
	var firstTokenMs *int
	clientDisconnected := false
	if action == "countTokens" {
		responseBody, readErr := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
		if readErr != nil {
			return nil, readErr
		}
		responseBody = unwrapIfNeeded(isOAuth, responseBody)
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		c.Data(resp.StatusCode, contentType, responseBody)
		usage = &ClaudeUsage{}
	} else if requestPipeline == nil {
		return nil, errors.New("gemini upstream response missing request pipeline")
	} else if stream {
		responseBodyOwned = false
		streamRes, err := s.handleNativeStreamingResponse(c, resp, requestPipeline, startTime, hasVendorEnvelope)
		if err != nil {
			return nil, err
		}
		usage = streamRes.usage
		firstTokenMs = streamRes.firstTokenMs
		clientDisconnected = streamRes.clientDisconnected
	} else {
		if useUpstreamStream {
			responseBodyOwned = false
			collected, usageObj, rawStreamBody, err := s.collectGeminiSSEWithRaw(resp.Body, isOAuth)
			if err != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to read upstream stream")
			}
			b, marshalErr := json.Marshal(collected)
			if marshalErr != nil {
				return nil, s.writeGoogleError(c, http.StatusBadGateway, "Failed to aggregate upstream stream")
			}
			upstreamResponseModelObserverFromContext(c).ObserveGemini(b)
			observeGeminiImageOutputs(c, b)
			usage, err = s.renderNativeGoogleResponse(c, resp, requestPipeline, b, usageObj, startTime, rawStreamBody)
			if err != nil {
				return nil, err
			}
		} else {
			usageResp, err := s.handleNativeNonStreamingResponse(c, resp, requestPipeline, startTime, isOAuth)
			if err != nil {
				return nil, err
			}
			usage = usageResp
		}
	}

	if usage == nil {
		usage = &ClaudeUsage{}
	}

	// 图片生成计费
	imageInputSize := s.extractImageInputSize(body)
	imageSize := normalizeOpenAIImageSizeTier(imageInputSize)
	imageCount := resolveGeminiImageCount(c, originalModel, mappedModel)

	return &ForwardResult{
		RequestID:                     requestID,
		ActualProtocol:                protocolconv.ProtocolGoogleGenAI,
		UpstreamHeaders:               resp.Header,
		Usage:                         *usage,
		Model:                         originalModel,
		UpstreamModel:                 mappedModel,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		Stream:                        stream,
		Duration:                      time.Since(startTime),
		FirstTokenMs:                  firstTokenMs,
		ImageCount:                    imageCount,
		ImageSize:                     imageSize,
		ImageInputSize:                imageInputSize,
		ClientDisconnect:              clientDisconnected,
	}, nil
}

// collectGeminiStructuredUpstreamError captures one Google HTTP error attempt.
// Streaming request bodies transfer through transport.Stream.ErrorBody before
// they are read; every path closes the original response body exactly once.
func (s *GeminiMessagesCompatService) collectGeminiStructuredUpstreamError(
	resp *http.Response,
	streamRequested bool,
	requestIDHeader string,
) (protocoltransport.Response, error) {
	if resp == nil || resp.Body == nil {
		return protocoltransport.Response{}, errors.New("collect Gemini structured upstream error: empty response")
	}
	headers := protocoltransport.CloneHeaders(resp.Header)
	requestID := strings.TrimSpace(headers.Get(requestIDHeader))
	if requestID == "" {
		requestID = strings.TrimSpace(headers.Get("x-goog-request-id"))
	}
	statusCode := resp.StatusCode
	bodyReader := resp.Body
	closeBody := bodyReader.Close
	resp.Body = http.NoBody

	if streamRequested {
		stream := &protocoltransport.Stream{
			StatusCode: statusCode, Headers: headers, ActualProtocol: protocolconv.ProtocolGoogleGenAI,
			RequestID: requestID, ErrorBody: bodyReader,
		}
		if err := stream.Validate(); err != nil {
			_ = stream.Close()
			return protocoltransport.Response{}, fmt.Errorf("validate Gemini upstream error stream: %w", err)
		}
		bodyReader = stream.ErrorBody
		closeBody = stream.Close
	}

	body, _ := io.ReadAll(io.LimitReader(bodyReader, s.geminiUpstreamErrorBodyReadLimit()))
	_ = closeBody()
	upstream := protocoltransport.Response{
		StatusCode: statusCode, Headers: headers, Body: body,
		ActualProtocol: protocolconv.ProtocolGoogleGenAI, RequestID: requestID,
	}
	if err := upstream.Validate(); err != nil {
		return protocoltransport.Response{}, fmt.Errorf("validate Gemini structured upstream error response: %w", err)
	}
	if !upstream.IsError() {
		return protocoltransport.Response{}, fmt.Errorf("collect Gemini structured upstream error: status %d is not an error", statusCode)
	}
	return upstream, nil
}

func (s *GeminiMessagesCompatService) geminiUpstreamErrorBodyReadLimit() int64 {
	limit := gatewayUpstreamErrorBodyReadLimit
	if s != nil && s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody && s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes > int(limit) {
		limit = int64(s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes)
	}
	return limit
}

func (s *GeminiMessagesCompatService) checkStructuredErrorPolicyInLoop(
	ctx context.Context,
	account *Account,
	upstream protocoltransport.Response,
	mappedModel string,
) bool {
	if !upstream.IsError() || s.rateLimitService == nil {
		return false
	}
	return s.rateLimitService.CheckErrorPolicy(ctx, account, upstream.StatusCode, upstream.Body, mappedModel) != ErrorPolicyNone
}

func (s *GeminiMessagesCompatService) shouldRetryGeminiUpstreamError(account *Account, statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504, 529:
		return true
	case 403:
		// GeminiCli OAuth occasionally returns 403 transiently (activation/quota propagation); allow retry.
		if account == nil || account.Type != AccountTypeOAuth {
			return false
		}
		oauthType := strings.ToLower(strings.TrimSpace(account.GetCredential("oauth_type")))
		if oauthType == "" && strings.TrimSpace(account.GetCredential("project_id")) != "" {
			// Legacy/implicit Code Assist OAuth accounts.
			oauthType = "code_assist"
		}
		return oauthType == "code_assist"
	default:
		return false
	}
}

func (s *GeminiMessagesCompatService) shouldFailoverGeminiUpstreamError(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429, 529:
		return true
	default:
		return statusCode >= 500
	}
}

// skippedErrorPolicyFailoverError 命中 ErrorPolicySkipped（池模式、或自定义错误码未命中）
// 时构造 failover 错误：可 failover 的状态码返回 UpstreamFailoverError，交给 handler 层换号
// （池模式账号按 pool_mode_retry_count 先同账号重试）；返回 nil 表示状态码不可 failover，
// 由调用方决定客户端写出。Skipped 只豁免账号状态标记，不豁免换号，与 OpenAI 网关路径一致。
func (s *GeminiMessagesCompatService) skippedErrorPolicyFailoverError(c *gin.Context, account *Account, statusCode int, respBody []byte, upstreamRequestID string) *UpstreamFailoverError {
	if !s.shouldFailoverGeminiUpstreamError(statusCode) {
		return nil
	}
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	upstreamDetail := s.upstreamErrorDetail(respBody)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: statusCode,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "failover",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	return &UpstreamFailoverError{
		StatusCode:             statusCode,
		ResponseBody:           respBody,
		RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(statusCode),
	}
}

// geminiCustomCodeSkippedClientMessage 自定义错误码未命中时对客户端隐藏上游细节的固定文案，
// 与 OpenAI 网关路径同场景的文案一致。
const geminiCustomCodeSkippedClientMessage = "Upstream gateway error"

// upstreamErrorDetail 按配置截断上游错误响应体，用于 ops 错误日志的 Detail 字段；
// 未开启 LogUpstreamErrorBody 时返回空。
func (s *GeminiMessagesCompatService) upstreamErrorDetail(body []byte) string {
	if s.cfg == nil || !s.cfg.Gateway.LogUpstreamErrorBody {
		return ""
	}
	maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2048
	}
	return truncateString(string(body), maxBytes)
}

// writeGeminiCustomCodeSkippedError 处理自定义错误码未命中且不可 failover 的上游错误：
// 客户端统一收到 500 + 固定文案（由 write 按端点格式写出），不透传上游细节；
// 上游真实状态码与错误信息仅记录到 ops 错误日志。
func (s *GeminiMessagesCompatService) writeGeminiCustomCodeSkippedError(c *gin.Context, account *Account, upstreamStatus int, upstreamRequestID string, body []byte, write func()) error {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	upstreamDetail := s.upstreamErrorDetail(body)
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstreamStatus,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})
	write()
	if upstreamMsg == "" {
		return fmt.Errorf("gemini upstream error: %d (not in custom error codes)", upstreamStatus)
	}
	return fmt.Errorf("gemini upstream error: %d (not in custom error codes) message=%s", upstreamStatus, upstreamMsg)
}

// writeGeminiNativeUpstreamError 将不可 failover 的上游错误按原始状态码与响应体透传给客户端，
// 并记录 ops 错误事件。状态码保真：下游据此区分请求级错误与可重试的链路故障。
func (s *GeminiMessagesCompatService) writeGeminiNativeUpstreamError(c *gin.Context, account *Account, statusCode int, headers http.Header, respBody []byte, requestID string, isOAuth bool) error {
	respBody = unwrapIfNeeded(isOAuth, respBody)
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	upstreamDetail := s.upstreamErrorDetail(respBody)
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini] native upstream error %d: %s", statusCode, truncateForLog(respBody, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes))
	}
	setOpsUpstreamError(c, statusCode, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: statusCode,
		UpstreamRequestID:  requestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})

	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	MarkResponseCommitted(c)
	c.Data(statusCode, contentType, respBody)
	if upstreamMsg == "" {
		return fmt.Errorf("gemini upstream error: %d", statusCode)
	}
	return fmt.Errorf("gemini upstream error: %d message=%s", statusCode, upstreamMsg)
}

func sleepGeminiBackoff(attempt int) {
	delay := geminiRetryBaseDelay * time.Duration(1<<uint(attempt-1))
	if delay > geminiRetryMaxDelay {
		delay = geminiRetryMaxDelay
	}

	// +/- 20% jitter
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	jitter := time.Duration(float64(delay) * 0.2 * (r.Float64()*2 - 1))
	sleepFor := delay + jitter
	if sleepFor < 0 {
		sleepFor = 0
	}
	time.Sleep(sleepFor)
}

var (
	sensitiveQueryParamRegex = regexp.MustCompile(`(?i)([?&](?:key|client_secret|access_token|refresh_token)=)[^&"\s]+`)
	retryInRegex             = regexp.MustCompile(`Please retry in ([0-9.]+)s`)
)

func sanitizeUpstreamErrorMessage(msg string) string {
	if msg == "" {
		return msg
	}
	return sensitiveQueryParamRegex.ReplaceAllString(msg, `$1***`)
}

func (s *GeminiMessagesCompatService) writeGeminiMappedError(c *gin.Context, account *Account, upstreamStatus int, upstreamRequestID string, body []byte) error {
	MarkResponseCommitted(c)
	upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(body))
	upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
	upstreamDetail := ""
	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
		if maxBytes <= 0 {
			maxBytes = 2048
		}
		upstreamDetail = truncateString(string(body), maxBytes)
	}
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, upstreamDetail)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID:            opsUpstreamProxyID(account),
		ProxyName:          opsUpstreamProxyName(account),
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: upstreamStatus,
		UpstreamRequestID:  upstreamRequestID,
		Kind:               "http_error",
		Message:            upstreamMsg,
		Detail:             upstreamDetail,
	})

	if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
		logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini] upstream error %d: %s", upstreamStatus, truncateForLog(body, s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes))
	}

	if status, errType, errMsg, matched := applyErrorPassthroughRule(
		c,
		PlatformGemini,
		upstreamStatus,
		body,
		http.StatusBadGateway,
		"upstream_error",
		"Upstream request failed",
	); matched {
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": errType, "message": errMsg},
		})
		if upstreamMsg == "" {
			upstreamMsg = errMsg
		}
		if upstreamMsg == "" {
			return fmt.Errorf("upstream error: %d (passthrough rule matched)", upstreamStatus)
		}
		return fmt.Errorf("upstream error: %d (passthrough rule matched) message=%s", upstreamStatus, upstreamMsg)
	}

	var statusCode int
	var errType, errMsg string

	if mapped := mapGeminiErrorBodyToClaudeError(body); mapped != nil {
		errType = mapped.Type
		if mapped.Message != "" {
			errMsg = mapped.Message
		}
		if mapped.StatusCode > 0 {
			statusCode = mapped.StatusCode
		}
	}

	switch upstreamStatus {
	case 400:
		if statusCode == 0 {
			statusCode = http.StatusBadRequest
		}
		if errType == "" {
			errType = "invalid_request_error"
		}
		// 400 是确定性的请求错误：回传上游 message（已脱敏），客户端据此定位非法字段。
		if errMsg == "" {
			errMsg = upstreamMsg
		}
		if errMsg == "" {
			errMsg = "Invalid request"
		}
	case 401:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "authentication_error"
		}
		if errMsg == "" {
			errMsg = "Upstream authentication failed, please contact administrator"
		}
	case 403:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "permission_error"
		}
		if errMsg == "" {
			errMsg = "Upstream access forbidden, please contact administrator"
		}
	case 404:
		if statusCode == 0 {
			statusCode = http.StatusNotFound
		}
		if errType == "" {
			errType = "not_found_error"
		}
		if errMsg == "" {
			errMsg = "Resource not found"
		}
	case 429:
		if statusCode == 0 {
			statusCode = http.StatusTooManyRequests
		}
		if errType == "" {
			errType = "rate_limit_error"
		}
		if errMsg == "" {
			errMsg = "Upstream rate limit exceeded, please retry later"
		}
	case 529:
		if statusCode == 0 {
			statusCode = http.StatusServiceUnavailable
		}
		if errType == "" {
			errType = "overloaded_error"
		}
		if errMsg == "" {
			errMsg = "Upstream service overloaded, please retry later"
		}
	case 500, 502, 503, 504:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			switch upstreamStatus {
			case 504:
				errType = "timeout_error"
			case 503:
				errType = "overloaded_error"
			default:
				errType = "api_error"
			}
		}
		if errMsg == "" {
			errMsg = "Upstream service temporarily unavailable"
		}
	default:
		if statusCode == 0 {
			statusCode = http.StatusBadGateway
		}
		if errType == "" {
			errType = "upstream_error"
		}
		if errMsg == "" {
			errMsg = "Upstream request failed"
		}
	}

	c.JSON(statusCode, gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": errMsg},
	})
	if upstreamMsg == "" {
		return fmt.Errorf("upstream error: %d", upstreamStatus)
	}
	return fmt.Errorf("upstream error: %d message=%s", upstreamStatus, upstreamMsg)
}

type claudeErrorMapping struct {
	Type       string
	Message    string
	StatusCode int
}

func mapGeminiErrorBodyToClaudeError(body []byte) *claudeErrorMapping {
	if len(body) == 0 {
		return nil
	}

	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	if strings.TrimSpace(parsed.Error.Status) == "" && parsed.Error.Code == 0 && strings.TrimSpace(parsed.Error.Message) == "" {
		return nil
	}

	mapped := &claudeErrorMapping{
		Type:    mapGeminiStatusToClaudeErrorType(parsed.Error.Status),
		Message: "",
	}
	if mapped.Type == "" {
		mapped.Type = "upstream_error"
	}

	switch strings.ToUpper(strings.TrimSpace(parsed.Error.Status)) {
	case "INVALID_ARGUMENT":
		mapped.StatusCode = http.StatusBadRequest
	case "NOT_FOUND":
		mapped.StatusCode = http.StatusNotFound
	case "RESOURCE_EXHAUSTED":
		mapped.StatusCode = http.StatusTooManyRequests
	default:
		// Keep StatusCode unset and let HTTP status mapping decide.
	}

	// Keep messages generic by default; upstream error message can be long or include sensitive fragments.
	return mapped
}

func mapGeminiStatusToClaudeErrorType(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "INVALID_ARGUMENT":
		return "invalid_request_error"
	case "PERMISSION_DENIED":
		return "permission_error"
	case "NOT_FOUND":
		return "not_found_error"
	case "RESOURCE_EXHAUSTED":
		return "rate_limit_error"
	case "UNAUTHENTICATED":
		return "authentication_error"
	case "UNAVAILABLE":
		return "overloaded_error"
	case "INTERNAL":
		return "api_error"
	case "DEADLINE_EXCEEDED":
		return "timeout_error"
	default:
		return ""
	}
}

type geminiStreamResult struct {
	usage              *ClaudeUsage
	firstTokenMs       *int
	clientDisconnected bool
}

func (s *GeminiMessagesCompatService) handleNonStreamingResponse(c *gin.Context, resp *http.Response, pipeline *protocolconv.Pipeline, startTime time.Time) (*ClaudeUsage, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to read upstream response")
	}

	unwrappedBody, err := unwrapGeminiResponse(body)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to parse upstream response")
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveGemini(unwrappedBody)
	observeGeminiImageOutputs(c, unwrappedBody)

	return s.renderGoogleAnthropicResponse(c, resp, pipeline, unwrappedBody, nil, startTime, body)
}

func (s *GeminiMessagesCompatService) renderGoogleAnthropicResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	googleBody []byte,
	usageOverride *ClaudeUsage,
	startTime time.Time,
	rawUpstreamBody []byte,
) (*ClaudeUsage, error) {
	googleBody = withGoogleUsageOverride(googleBody, usageOverride)
	usage := extractGeminiUsage(googleBody)
	if usage == nil {
		usage = &ClaudeUsage{}
	}
	var envelope struct {
		ResponseID string `json:"responseId"`
	}
	_ = json.Unmarshal(googleBody, &envelope)
	structured := protocoltransport.Response{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body: googleBody, ActualProtocol: protocolconv.ProtocolGoogleGenAI,
		RequestID: resp.Header.Get("x-request-id"), ResponseID: envelope.ResponseID, Duration: time.Since(startTime),
	}
	if len(rawUpstreamBody) > 0 {
		structured.Metadata = map[string]any{"raw_upstream_body": append([]byte(nil), rawUpstreamBody...)}
	}
	if err := structured.Validate(); err != nil {
		return nil, fmt.Errorf("collect Google response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		return nil, s.writeClaudeError(c, http.StatusBadGateway, "upstream_error", "Failed to convert upstream response")
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body); err != nil {
		return nil, err
	}
	return usage, nil
}

func (s *GeminiMessagesCompatService) handleStreamingResponse(c *gin.Context, resp *http.Response, pipeline *protocolconv.Pipeline, startTime time.Time) (*geminiStreamResult, error) {
	if _, ok := c.Writer.(http.Flusher); !ok {
		return nil, errors.New("streaming not supported")
	}
	maxRecordSize := protocoltransport.DefaultMaxSSERecordBytes
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	inner := protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	events, err := protocoltransport.NewTransformEventStream(inner, unwrapGeminiResponse)
	if err != nil {
		return nil, err
	}
	stream := &protocoltransport.Stream{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolGoogleGenAI, RequestID: resp.Header.Get("x-request-id"),
		Duration: time.Since(startTime), Events: events,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolAnthropic)
	if err != nil {
		return nil, err
	}
	var usage ClaudeUsage
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	writePayloads := func(payloads [][]byte) (bool, error) {
		if clientDisconnected {
			return true, nil
		}
		if len(payloads) == 0 {
			return false, nil
		}
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return false, err
			}
			headersWritten = true
		}
		for _, payload := range payloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return false, err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				return true, nil
			}
		}
		c.Writer.Flush()
		return false, nil
	}

	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	for {
		record, err := stream.Events.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("stream read error: %w", err)
		}
		observer.ObserveGemini(record.Data)
		observeGeminiImageOutputs(c, record.Data)
		if current := extractGeminiUsage(record.Data); current != nil {
			usage = *current
		}
		converted, _, err := session.Convert(record.Data)
		if err != nil {
			return nil, fmt.Errorf("convert Google stream event: %w", err)
		}
		if firstTokenMs == nil && len(converted) > 0 {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		disconnected, err := writePayloads(converted)
		if err != nil {
			return nil, err
		}
		_ = disconnected
	}
	finalPayloads, _, err := session.Finalize()
	if err != nil {
		return nil, fmt.Errorf("finalize Google stream: %w", err)
	}
	disconnected, err := writePayloads(finalPayloads)
	if err != nil {
		return nil, err
	}
	_ = disconnected
	return &geminiStreamResult{usage: &usage, firstTokenMs: firstTokenMs, clientDisconnected: clientDisconnected}, nil
}

func (s *GeminiMessagesCompatService) writeClaudeError(c *gin.Context, status int, errType, message string) error {
	MarkResponseCommitted(c)
	c.JSON(status, gin.H{
		"type":  "error",
		"error": gin.H{"type": errType, "message": message},
	})
	return fmt.Errorf("%s", message)
}

func (s *GeminiMessagesCompatService) writeGoogleError(c *gin.Context, status int, message string) error {
	MarkResponseCommitted(c)
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
	return fmt.Errorf("%s", message)
}

func unwrapIfNeeded(isOAuth bool, raw []byte) []byte {
	if !isOAuth {
		return raw
	}
	inner, err := unwrapGeminiResponse(raw)
	if err != nil {
		return raw
	}
	return inner
}

func (s *GeminiMessagesCompatService) collectGeminiSSEWithRaw(body io.ReadCloser, isOAuth bool) (map[string]any, *ClaudeUsage, []byte, error) {
	if body == nil {
		return nil, nil, nil, errors.New("response body is nil for Gemini SSE")
	}
	maxTotalBytes := resolveUpstreamResponseReadLimit(s.cfg)
	limited := &io.LimitedReader{R: body, N: maxTotalBytes + 1}
	var rawStream bytes.Buffer
	recordedBody := &geminiSSERecordingReadCloser{Reader: io.TeeReader(limited, &rawStream), source: body}
	maxRecordSize := protocoltransport.DefaultMaxSSERecordBytes
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	parser := protocoltransport.NewSSEParser(recordedBody, maxRecordSize)
	defer func() { _ = parser.Close() }()

	var last map[string]any
	var lastWithParts map[string]any
	var collectedTextParts []string
	usage := &ClaudeUsage{}
	result := func() (map[string]any, *ClaudeUsage, []byte, error) {
		return mergeCollectedTextParts(pickGeminiCollectResult(last, lastWithParts), collectedTextParts), usage, append([]byte(nil), rawStream.Bytes()...), nil
	}
	tooLarge := func() error {
		return fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxTotalBytes)
	}

	for {
		record, err := parser.Next(context.Background())
		if int64(rawStream.Len()) > maxTotalBytes {
			return nil, nil, nil, tooLarge()
		}
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			return result()
		}
		if err != nil {
			return nil, nil, append([]byte(nil), rawStream.Bytes()...), err
		}
		payload := record.Data
		if isOAuth {
			payload, err = unwrapGeminiResponse(payload)
			if err != nil {
				return nil, nil, append([]byte(nil), rawStream.Bytes()...), err
			}
		}
		var parsed map[string]any
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, nil, append([]byte(nil), rawStream.Bytes()...), err
		}
		last = parsed
		if current := extractGeminiUsage(payload); current != nil {
			usage = current
		}
		if parts := extractGeminiParts(parsed); len(parts) > 0 {
			lastWithParts = parsed
			for _, part := range parts {
				if text, ok := part["text"].(string); ok && text != "" {
					collectedTextParts = append(collectedTextParts, text)
				}
			}
		}
	}
}

type geminiSSERecordingReadCloser struct {
	io.Reader
	source io.Closer
}

func (r *geminiSSERecordingReadCloser) Close() error {
	if r == nil || r.source == nil {
		return nil
	}
	return r.source.Close()
}

func pickGeminiCollectResult(last map[string]any, lastWithParts map[string]any) map[string]any {
	if lastWithParts != nil {
		return lastWithParts
	}
	if last != nil {
		return last
	}
	return map[string]any{}
}

// mergeCollectedTextParts merges all collected text chunks into the final response.
// This fixes the issue where non-streaming responses only returned the last chunk
// instead of the complete aggregated text.
func mergeCollectedTextParts(response map[string]any, textParts []string) map[string]any {
	if len(textParts) == 0 {
		return response
	}

	// Join all text parts
	mergedText := strings.Join(textParts, "")

	// Deep copy response
	result := make(map[string]any)
	for k, v := range response {
		result[k] = v
	}

	// Get or create candidates
	candidates, ok := result["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		candidates = []any{map[string]any{}}
	}

	// Get first candidate
	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		candidate = make(map[string]any)
		candidates[0] = candidate
	}

	// Get or create content
	content, ok := candidate["content"].(map[string]any)
	if !ok {
		content = map[string]any{"role": "model"}
		candidate["content"] = content
	}

	// Get existing parts
	existingParts, ok := content["parts"].([]any)
	if !ok {
		existingParts = []any{}
	}

	// Find and update first text part, or create new one
	newParts := make([]any, 0, len(existingParts)+1)
	textUpdated := false

	for _, p := range existingParts {
		pm, ok := p.(map[string]any)
		if !ok {
			newParts = append(newParts, p)
			continue
		}
		if _, hasText := pm["text"]; hasText && !textUpdated {
			// Replace with merged text
			newPart := make(map[string]any)
			for k, v := range pm {
				newPart[k] = v
			}
			newPart["text"] = mergedText
			newParts = append(newParts, newPart)
			textUpdated = true
		} else {
			newParts = append(newParts, pm)
		}
	}

	if !textUpdated {
		newParts = append([]any{map[string]any{"text": mergedText}}, newParts...)
	}

	content["parts"] = newParts
	result["candidates"] = candidates

	return result
}

type geminiNativeStreamResult struct {
	usage              *ClaudeUsage
	firstTokenMs       *int
	clientDisconnected bool
}

func isGeminiInsufficientScope(headers http.Header, body []byte) bool {
	if strings.Contains(strings.ToLower(headers.Get("Www-Authenticate")), "insufficient_scope") {
		return true
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "insufficient authentication scopes") || strings.Contains(lower, "access_token_scope_insufficient")
}

func estimateGeminiCountTokens(reqBody []byte) int {
	total := 0

	// systemInstruction.parts[].text
	gjson.GetBytes(reqBody, "systemInstruction.parts").ForEach(func(_, part gjson.Result) bool {
		if t := strings.TrimSpace(part.Get("text").String()); t != "" {
			total += estimateTokensForText(t)
		}
		return true
	})

	// contents[].parts[].text
	gjson.GetBytes(reqBody, "contents").ForEach(func(_, content gjson.Result) bool {
		content.Get("parts").ForEach(func(_, part gjson.Result) bool {
			if t := strings.TrimSpace(part.Get("text").String()); t != "" {
				total += estimateTokensForText(t)
			}
			return true
		})
		return true
	})

	if total < 0 {
		return 0
	}
	return total
}

func estimateTokensForText(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return 0
	}
	ascii := 0
	for _, r := range runes {
		if r <= 0x7f {
			ascii++
		}
	}
	asciiRatio := float64(ascii) / float64(len(runes))
	if asciiRatio >= 0.8 {
		// Roughly 4 chars per token for English-like text.
		return (len(runes) + 3) / 4
	}
	// For CJK-heavy text, approximate 1 rune per token.
	return len(runes)
}

type UpstreamHTTPResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func newGoogleIdentityPipeline(account *Account, clientModel, upstreamModel string) (*protocolconv.Pipeline, error) {
	route := protocolconv.Route{
		Source: protocolconv.ProtocolGoogleGenAI, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		ClientModel: clientModel, UpstreamModel: upstreamModel,
	}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	return protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: route, Options: protocolconv.Options{SourceModel: upstreamModel, LossPolicy: protocolconv.LossError},
	})
}

func (s *GeminiMessagesCompatService) handleNativeNonStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	startTime time.Time,
	isOAuth bool,
) (*ClaudeUsage, error) {
	if s.cfg != nil && s.cfg.Gateway.GeminiDebugResponseHeaders {
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========== Response Headers ==========")
		for key, values := range resp.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-ratelimit") {
				logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] %s: %v", key, values)
			}
		}
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========================================")
	}

	rawUpstreamBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	googleBody := rawUpstreamBody
	if isOAuth {
		unwrappedBody, uwErr := unwrapGeminiResponse(rawUpstreamBody)
		if uwErr == nil {
			googleBody = unwrappedBody
		}
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.ObserveGemini(googleBody)
	observeGeminiImageOutputs(c, googleBody)
	return s.renderNativeGoogleResponse(c, resp, pipeline, googleBody, nil, startTime, rawUpstreamBody)
}

func (s *GeminiMessagesCompatService) renderNativeGoogleResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	googleBody []byte,
	usageOverride *ClaudeUsage,
	startTime time.Time,
	rawUpstreamBody []byte,
) (*ClaudeUsage, error) {
	usage := usageOverride
	if usage == nil {
		usage = extractGeminiUsage(googleBody)
	}
	if usage == nil {
		usage = &ClaudeUsage{}
	}
	structured := protocoltransport.Response{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		Body: append([]byte(nil), googleBody...), ActualProtocol: protocolconv.ProtocolGoogleGenAI,
		RequestID: resp.Header.Get("x-request-id"), ResponseID: gjson.GetBytes(googleBody, "responseId").String(),
		Duration: time.Since(startTime),
	}
	if len(rawUpstreamBody) > 0 && !bytes.Equal(rawUpstreamBody, googleBody) {
		structured.Metadata = map[string]any{"raw_upstream_body": append([]byte(nil), rawUpstreamBody...)}
	}
	if err := structured.Validate(); err != nil {
		return nil, fmt.Errorf("collect native Google response: %w", err)
	}
	converted, err := pipeline.ConvertResponse(structured.Body, structured.ActualProtocol)
	if err != nil {
		return nil, &UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			ResponseBody:           append([]byte(nil), structured.Body...),
			ResponseHeaders:        structured.Headers,
			RetryableOnSameAccount: true,
		}
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, structured.StatusCode, structured.Headers, converted.Body); err != nil {
		return nil, fmt.Errorf("render native Google response: %w", err)
	}
	return usage, nil
}

func (s *GeminiMessagesCompatService) handleNativeStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	startTime time.Time,
	hasVendorEnvelope bool,
) (*geminiNativeStreamResult, error) {
	if s.cfg != nil && s.cfg.Gateway.GeminiDebugResponseHeaders {
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ========== Streaming Response Headers ==========")
		for key, values := range resp.Header {
			if strings.HasPrefix(strings.ToLower(key), "x-ratelimit") {
				logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] %s: %v", key, values)
			}
		}
		logger.LegacyPrintf("service.gemini_messages_compat", "[GeminiAPI] ====================================================")
	}

	maxRecordSize := protocoltransport.DefaultMaxSSERecordBytes
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxRecordSize = s.cfg.Gateway.MaxLineSize
	}
	var events protocoltransport.EventStream = protocoltransport.NewSSEParser(resp.Body, maxRecordSize)
	metadata := map[string]any(nil)
	if hasVendorEnvelope {
		transformed, err := protocoltransport.NewTransformEventStream(events, unwrapGeminiResponse)
		if err != nil {
			return nil, err
		}
		events = transformed
		metadata = map[string]any{"vendor_envelope": "gemini_code_assist"}
	}
	stream := &protocoltransport.Stream{
		StatusCode: resp.StatusCode, Headers: responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter),
		ActualProtocol: protocolconv.ProtocolGoogleGenAI, RequestID: resp.Header.Get("x-request-id"),
		Duration: time.Since(startTime), Metadata: metadata, Events: events,
	}
	if err := stream.Validate(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("collect native Google stream: %w", err)
	}
	defer func() { _ = stream.Close() }()
	if _, ok := c.Writer.(http.Flusher); !ok {
		return nil, errors.New("streaming not supported")
	}

	session, err := pipeline.NewStreamProcessor(stream.ActualProtocol)
	if err != nil {
		return nil, fmt.Errorf("create native Google stream processor: %w", err)
	}
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	usage := &ClaudeUsage{}
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	result := func() *geminiNativeStreamResult {
		return &geminiNativeStreamResult{
			usage: usage, firstTokenMs: firstTokenMs, clientDisconnected: clientDisconnected,
		}
	}
	writePayloads := func(payloads [][]byte) error {
		if clientDisconnected || len(payloads) == 0 {
			return nil
		}
		framedPayloads := make([][]byte, 0, len(payloads))
		for _, payload := range payloads {
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			framedPayloads = append(framedPayloads, framed)
		}
		if !headersWritten {
			if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
				return err
			}
			headersWritten = true
		}
		for _, framed := range framedPayloads {
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				logger.LegacyPrintf("service.gemini_messages_compat", "Client disconnected during native Gemini streaming, continuing to drain upstream for billing")
				return nil
			}
		}
		c.Writer.Flush()
		return nil
	}
	failBeforeOutput := func(cause error) error {
		if headersWritten || c.Writer.Written() || clientDisconnected {
			return cause
		}
		return &UpstreamFailoverError{
			StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":{"code":502,"message":"Invalid upstream Gemini stream","status":"INTERNAL"}}`),
			ResponseHeaders: stream.Headers, RetryableOnSameAccount: true,
		}
	}

	for {
		record, nextErr := stream.Events.Next(context.Background())
		if errors.Is(nextErr, io.EOF) || errors.Is(nextErr, protocoltransport.ErrSSEDone) {
			break
		}
		if nextErr != nil {
			return result(), failBeforeOutput(fmt.Errorf("read native Google stream: %w", nextErr))
		}
		observer.ObserveGemini(record.Data)
		observeGeminiImageOutputs(c, record.Data)
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if current := extractGeminiUsage(record.Data); current != nil {
			usage = current
		}
		payloads, _, err := session.Convert(record.Data)
		if err != nil {
			return result(), failBeforeOutput(fmt.Errorf("convert native Google stream event: %w", err))
		}
		if err := writePayloads(payloads); err != nil {
			return result(), fmt.Errorf("render native Google stream event: %w", err)
		}
	}
	payloads, _, err := session.Finalize()
	if err != nil {
		return result(), failBeforeOutput(fmt.Errorf("finalize native Google stream: %w", err))
	}
	if err := writePayloads(payloads); err != nil {
		return result(), fmt.Errorf("render finalized native Google stream: %w", err)
	}
	if !clientDisconnected && !headersWritten {
		if err := renderer.WriteStreamHeaders(c.Writer, stream.StatusCode, stream.Headers); err != nil {
			return result(), err
		}
		c.Writer.Flush()
	}
	return result(), nil
}

// ForwardAIStudioGET forwards a GET request to AI Studio (generativelanguage.googleapis.com) for
// endpoints like /v1beta/models and /v1beta/models/{model}.
//
// This is used to support Gemini SDKs that call models listing endpoints before generation.
func (s *GeminiMessagesCompatService) ForwardAIStudioGET(ctx context.Context, account *Account, path string) (*UpstreamHTTPResult, error) {
	if account == nil {
		return nil, errors.New("account is nil")
	}
	// path 会被直接拼到上游 base URL 后面，因此按路径护栏逐片段校验，
	// 见 upstream_path_guard.go。
	sanitizedPath, ok := sanitizedUpstreamPathSuffix(path)
	if !ok || sanitizedPath == "" {
		return nil, errors.New("invalid path")
	}
	path = sanitizedPath

	baseURL := account.GetGeminiBaseURL(geminicli.AIStudioBaseURL)
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	fullURL := strings.TrimRight(normalizedBaseURL, "/") + path

	var proxyURL string
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}

	switch account.Type {
	case AccountTypeAPIKey:
		apiKey := strings.TrimSpace(account.GetCredential("api_key"))
		if apiKey == "" {
			return nil, errors.New("gemini api_key not configured")
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case AccountTypeOAuth:
		if s.tokenProvider == nil {
			return nil, errors.New("gemini token provider not configured")
		}
		accessToken, err := s.tokenProvider.GetAccessToken(ctx, account)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
	default:
		return nil, fmt.Errorf("unsupported account type: %s", account.Type)
	}

	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	wwwAuthenticate := resp.Header.Get("Www-Authenticate")
	filteredHeaders := responseheaders.FilterHeaders(resp.Header, s.responseHeaderFilter)
	if wwwAuthenticate != "" {
		filteredHeaders.Set("Www-Authenticate", wwwAuthenticate)
	}
	return &UpstreamHTTPResult{
		StatusCode: resp.StatusCode,
		Headers:    filteredHeaders,
		Body:       body,
	}, nil
}

// unwrapGeminiResponse 解包 Gemini OAuth 响应中的 response 字段
// 使用 gjson 零拷贝提取，避免完整 Unmarshal+Marshal
func unwrapGeminiResponse(raw []byte) ([]byte, error) {
	result := gjson.GetBytes(raw, "response")
	if result.Exists() && result.Type == gjson.JSON {
		return []byte(result.Raw), nil
	}
	return raw, nil
}

func extractGeminiUsage(data []byte) *ClaudeUsage {
	usage := gjson.GetBytes(data, "usageMetadata")
	if !usage.Exists() {
		return nil
	}
	prompt := int(usage.Get("promptTokenCount").Int())
	cand := int(usage.Get("candidatesTokenCount").Int())
	cached := int(usage.Get("cachedContentTokenCount").Int())
	thoughts := int(usage.Get("thoughtsTokenCount").Int())

	// 从 candidatesTokensDetails 提取 IMAGE 模态 token 数
	imageTokens := 0
	candidateDetails := usage.Get("candidatesTokensDetails")
	if candidateDetails.Exists() {
		candidateDetails.ForEach(func(_, detail gjson.Result) bool {
			if detail.Get("modality").String() == "IMAGE" {
				imageTokens = int(detail.Get("tokenCount").Int())
				return false
			}
			return true
		})
	}

	// 注意：Gemini 的 promptTokenCount 包含 cachedContentTokenCount，
	// 但 Claude 的 input_tokens 不包含 cache_read_input_tokens，需要减去
	return &ClaudeUsage{
		InputTokens:          prompt - cached,
		OutputTokens:         cand + thoughts,
		CacheReadInputTokens: cached,
		ImageOutputTokens:    imageTokens,
	}
}

func (s *GeminiMessagesCompatService) handleGeminiUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, body []byte) {
	// 遵守自定义错误码策略：未命中则跳过所有限流处理
	if !account.ShouldHandleErrorCode(statusCode) {
		return
	}
	if s.rateLimitService != nil && (statusCode == 401 || statusCode == 403 || statusCode == 529) {
		s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
		return
	}
	if statusCode != 429 {
		return
	}
	// 池模式账号不写账号级限流：账号留在池内，由 failover / 同号重试消化 429。
	// 自定义错误码优先级高于池模式，开启后仍按其命中结果标记。
	if account.IsPoolMode() && !account.IsCustomErrorCodesEnabled() {
		return
	}

	oauthType := account.GeminiOAuthType()
	tierID := account.GeminiTierID()
	projectID := strings.TrimSpace(account.GetCredential("project_id"))
	isCodeAssist := account.IsGeminiCodeAssist()

	resetAt := ParseGeminiRateLimitResetTime(body)
	if resetAt == nil {
		// 根据账号类型使用不同的默认重置时间
		var ra time.Time
		if isCodeAssist || oauthType == "google_one" {
			// Gemini CLI / Google One: fallback cooldown by tier
			cooldown := geminiCooldownForTier(tierID)
			if s.rateLimitService != nil {
				cooldown = s.rateLimitService.GeminiCooldown(ctx, account)
			}
			ra = time.Now().Add(cooldown)
			if isCodeAssist {
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (Code Assist, tier=%s, project=%s) rate limited, cooldown=%v", account.ID, tierID, projectID, time.Until(ra).Truncate(time.Second))
			} else {
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (Google One OAuth, tier=%s, project=%s) rate limited, cooldown=%v", account.ID, tierID, projectID, time.Until(ra).Truncate(time.Second))
			}
		} else {
			// API Key / AI Studio OAuth: PST 午夜
			if ts := nextGeminiDailyResetUnix(); ts != nil {
				ra = time.Unix(*ts, 0)
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d (API Key/AI Studio, type=%s) rate limited, reset at PST midnight (%v)", account.ID, account.Type, ra)
			} else {
				// 兜底：5 分钟
				ra = time.Now().Add(5 * time.Minute)
				logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d rate limited, fallback to 5min", account.ID)
			}
		}
		_ = s.accountRepo.SetRateLimited(ctx, account.ID, ra)
		return
	}

	// 使用解析到的重置时间
	resetTime := time.Unix(*resetAt, 0)
	_ = s.accountRepo.SetRateLimited(ctx, account.ID, resetTime)
	logger.LegacyPrintf("service.gemini_messages_compat", "[Gemini 429] Account %d rate limited until %v (oauth_type=%s, tier=%s)",
		account.ID, resetTime, oauthType, tierID)
}

// ParseGeminiRateLimitResetTime 解析 Gemini 格式的 429 响应，返回重置时间的 Unix 时间戳
func ParseGeminiRateLimitResetTime(body []byte) *int64 {
	// 第一阶段：gjson 结构化提取
	errMsg := gjson.GetBytes(body, "error.message").String()
	if looksLikeGeminiDailyQuota(errMsg) {
		if ts := nextGeminiDailyResetUnix(); ts != nil {
			return ts
		}
	}

	// 遍历 error.details 查找 quotaResetDelay
	var found *int64
	gjson.GetBytes(body, "error.details").ForEach(func(_, detail gjson.Result) bool {
		v := detail.Get("metadata.quotaResetDelay").String()
		if v == "" {
			return true
		}
		if dur, err := time.ParseDuration(v); err == nil {
			// Use ceil to avoid undercounting fractional seconds (e.g. 10.1s should not become 10s),
			// which can affect scheduling decisions around thresholds (like 10s).
			ts := time.Now().Unix() + int64(math.Ceil(dur.Seconds()))
			found = &ts
			return false
		}
		return true
	})
	if found != nil {
		return found
	}

	// 第二阶段：regex 回退匹配 "Please retry in Xs"
	matches := retryInRegex.FindStringSubmatch(string(body))
	if len(matches) == 2 {
		if dur, err := time.ParseDuration(matches[1] + "s"); err == nil {
			ts := time.Now().Unix() + int64(math.Ceil(dur.Seconds()))
			return &ts
		}
	}

	return nil
}

func looksLikeGeminiDailyQuota(message string) bool {
	m := strings.ToLower(message)
	if strings.Contains(m, "per day") || strings.Contains(m, "requests per day") || strings.Contains(m, "quota") && strings.Contains(m, "per day") {
		return true
	}
	return false
}

func nextGeminiDailyResetUnix() *int64 {
	reset := geminiDailyResetTime(time.Now())
	ts := reset.Unix()
	return &ts
}

func ensureGeminiFunctionCallThoughtSignatures(body []byte) []byte {
	// Fast path: only run when functionCall is present.
	if !bytes.Contains(body, []byte(`"functionCall"`)) {
		return body
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	contentsAny, ok := payload["contents"].([]any)
	if !ok || len(contentsAny) == 0 {
		return body
	}

	modified := false
	for _, c := range contentsAny {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		partsAny, ok := cm["parts"].([]any)
		if !ok || len(partsAny) == 0 {
			continue
		}
		for _, p := range partsAny {
			pm, ok := p.(map[string]any)
			if !ok || pm == nil {
				continue
			}
			if fc, ok := pm["functionCall"].(map[string]any); !ok || fc == nil {
				continue
			}
			ts, _ := pm["thoughtSignature"].(string)
			if strings.TrimSpace(ts) == "" {
				pm["thoughtSignature"] = geminiDummyThoughtSignature
				modified = true
			}
		}
	}

	if !modified {
		return body
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return b
}

func extractGeminiFinishReason(geminiResp map[string]any) string {
	candidates, _ := geminiResp["candidates"].([]any)
	if len(candidates) == 0 {
		return ""
	}
	candidate, _ := candidates[0].(map[string]any)
	finishReason, _ := candidate["finishReason"].(string)
	return finishReason
}

func extractGeminiParts(geminiResp map[string]any) []map[string]any {
	if candidates, ok := geminiResp["candidates"].([]any); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]any); ok {
			if content, ok := cand["content"].(map[string]any); ok {
				if partsAny, ok := content["parts"].([]any); ok && len(partsAny) > 0 {
					out := make([]map[string]any, 0, len(partsAny))
					for _, p := range partsAny {
						pm, ok := p.(map[string]any)
						if !ok {
							continue
						}
						out = append(out, pm)
					}
					return out
				}
			}
		}
	}
	return nil
}

func convertClaudeMessagesToGeminiGenerateContent(body []byte) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	_, converted, err := newClaudeMessagesGooglePipeline(nil, body, model, model)
	return converted, err
}

func (s *GeminiMessagesCompatService) newClaudeMessagesGooglePipeline(
	ctx context.Context,
	account *Account,
	body []byte,
	originalModel, mappedModel string,
) (*protocolconv.Pipeline, []byte, error) {
	return newClaudeMessagesGooglePipelineConfigured(account, body, originalModel, mappedModel, func(config *protocolconv.PipelineConfig) {
		s.configureGoogleMetadataBridge(ctx, account, config)
	})
}

func newClaudeMessagesGooglePipeline(account *Account, body []byte, originalModel, mappedModel string) (*protocolconv.Pipeline, []byte, error) {
	return newClaudeMessagesGooglePipelineConfigured(account, body, originalModel, mappedModel, nil)
}

func newClaudeMessagesGooglePipelineConfigured(
	account *Account,
	body []byte,
	originalModel, mappedModel string,
	configure func(*protocolconv.PipelineConfig),
) (*protocolconv.Pipeline, []byte, error) {
	route := protocolconv.Route{
		Source: protocolconv.ProtocolAnthropic, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		ClientModel: originalModel, UpstreamModel: mappedModel,
	}
	if account != nil {
		route.Provider = account.Platform
		route.AccountID = account.ID
	}
	pipelineConfig := protocolconv.PipelineConfig{
		Route: route, Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	}
	if configure != nil {
		configure(&pipelineConfig)
	}
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, pipelineConfig)
	if err != nil {
		return nil, nil, err
	}
	converted, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, nil, err
	}
	var request map[string]any
	if err := json.Unmarshal(converted.Body, &request); err != nil {
		return nil, nil, err
	}
	// Google function call IDs are not portable across every Gemini upstream.
	// Existing Gemini transports correlate by function name and preserve their
	// request-scoped mapping outside the standard converter.
	stripGeminiFunctionIDs(request)
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	return pipeline, ensureGeminiFunctionCallThoughtSignatures(encoded), nil
}

func stripGeminiFunctionIDs(req map[string]any) {
	// Defensive cleanup: some upstreams reject unexpected `id` fields in functionCall/functionResponse.
	contents, ok := req["contents"].([]any)
	if !ok {
		return
	}
	for _, c := range contents {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		contentParts, ok := cm["parts"].([]any)
		if !ok {
			continue
		}
		for _, p := range contentParts {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if fc, ok := pm["functionCall"].(map[string]any); ok && fc != nil {
				delete(fc, "id")
			}
			if fr, ok := pm["functionResponse"].(map[string]any); ok && fr != nil {
				delete(fr, "id")
			}
		}
	}
}

func normalizeGeminiRequestForAIStudio(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body
	}

	modified := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		googleSearch, ok := tool["googleSearch"]
		if !ok {
			continue
		}
		if _, exists := tool["google_search"]; exists {
			continue
		}
		tool["google_search"] = googleSearch
		delete(tool, "googleSearch")
		modified = true
	}

	if !modified {
		return body
	}

	normalized, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return normalized
}

// cleanToolSchema 清理工具的 JSON Schema，移除 Gemini 不支持的字段
func cleanToolSchema(schema any) any {
	if schema == nil {
		return nil
	}

	switch v := schema.(type) {
	case map[string]any:
		cleaned := make(map[string]any)
		for key, value := range v {
			// 跳过不支持的字段
			if key == "$schema" || key == "$id" || key == "$ref" ||
				key == "$defs" || key == "definitions" ||
				key == "additionalProperties" || key == "patternProperties" || key == "minLength" ||
				key == "maxLength" || key == "minItems" || key == "maxItems" || key == "exclusiveMinimum" ||
				key == "deprecated" {
				continue
			}
			// 递归清理嵌套对象
			cleaned[key] = cleanToolSchema(value)
		}
		if enum, ok := cleaned["enum"].([]any); ok {
			if normalized, ok := normalizeGeminiEnum(enum); ok {
				cleaned["enum"] = normalized
			} else {
				delete(cleaned, "enum")
			}
		}
		// 规范化 type 字段为大写
		if typeVal, ok := cleaned["type"].(string); ok {
			cleaned["type"] = strings.ToUpper(typeVal)
		} else if typeValues, ok := cleaned["type"].([]any); ok {
			for _, typeValue := range typeValues {
				typeName, ok := typeValue.(string)
				if ok && !strings.EqualFold(typeName, "null") {
					cleaned["type"] = strings.ToUpper(typeName)
					break
				}
			}
			if _, ok := cleaned["type"].([]any); ok {
				delete(cleaned, "type")
			}
		}
		if cleaned["type"] == "INTEGER" {
			if minimum, ok := incrementIntegralSchemaBound(v["exclusiveMinimum"]); ok {
				if existing, exists := cleaned["minimum"]; !exists || schemaNumberLess(existing, minimum) {
					cleaned["minimum"] = minimum
				}
			}
		}
		return cleaned
	case []any:
		cleaned := make([]any, len(v))
		for i, item := range v {
			cleaned[i] = cleanToolSchema(item)
		}
		return cleaned
	default:
		return v
	}
}

func normalizeGeminiEnum(values []any) ([]any, bool) {
	normalized := make([]any, len(values))
	for i, value := range values {
		if stringValue, ok := value.(string); ok {
			normalized[i] = stringValue
			continue
		}

		switch value.(type) {
		case nil, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, false
			}
			normalized[i] = string(encoded)
		default:
			return nil, false
		}
	}
	return normalized, true
}

func incrementIntegralSchemaBound(value any) (any, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || v+1 <= v {
			return nil, false
		}
		return v + 1, true
	case int:
		if v == math.MaxInt {
			return nil, false
		}
		return v + 1, true
	case int64:
		if v == math.MaxInt64 {
			return nil, false
		}
		return v + 1, true
	case json.Number:
		i, err := v.Int64()
		if err != nil || i == math.MaxInt64 {
			return nil, false
		}
		return json.Number(fmt.Sprintf("%d", i+1)), true
	default:
		return nil, false
	}
}

func schemaNumberLess(left, right any) bool {
	leftNumber, leftOK := schemaNumberFloat64(left)
	rightNumber, rightOK := schemaNumberFloat64(right)
	return leftOK && rightOK && leftNumber < rightNumber
}

func schemaNumberFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil && !math.IsInf(n, 0)
	default:
		return 0, false
	}
}

func (s *GeminiMessagesCompatService) extractImageInputSize(body []byte) string {
	var req struct {
		GenerationConfig *struct {
			ImageConfig *struct {
				ImageSize string `json:"imageSize"`
			} `json:"imageConfig"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}

	if req.GenerationConfig != nil && req.GenerationConfig.ImageConfig != nil {
		return strings.TrimSpace(req.GenerationConfig.ImageConfig.ImageSize)
	}

	return ""
}
