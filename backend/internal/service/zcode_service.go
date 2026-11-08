package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ZCodeService struct {
	repo         AccountRepository
	proxyRepo    ProxyRepository
	upstream     HTTPUpstream
	cipher       SecretEncryptor
	cfg          *config.Config
	store        zcode.Store
	rateLimit    *RateLimitService
	resolver     *zcode.Resolver
	signer       *zcode.Signer
	poolMu       sync.Mutex
	workerCursor int
	pools        map[string]*zcode.CaptchaPool
	solver       zcode.CaptchaSolver
	cancel       context.CancelFunc
	done         chan struct{}
	start        sync.Once
	stop         sync.Once
}

func NewZCodeService(repo AccountRepository, proxy ProxyRepository, httpClient HTTPUpstream, cipher SecretEncryptor, cfg *config.Config, store zcode.Store, rateLimit *RateLimitService) *ZCodeService {
	s := &ZCodeService{repo: repo, proxyRepo: proxy, upstream: httpClient, cipher: cipher, cfg: cfg, store: store, rateLimit: rateLimit, pools: map[string]*zcode.CaptchaPool{}, signer: zcode.NewSigner()}
	s.resolver = zcode.NewResolver(s.validateURL)
	browser := &zcode.BrowserSolver{}
	if cfg != nil {
		browser.Executable = cfg.Gateway.ZCode.ChromiumPath
		browser.NoSandbox = cfg.Gateway.ZCode.NoSandbox
	}
	s.solver = browser
	return s
}
func (s *ZCodeService) available() error {
	if s == nil || s.store == nil || s.cipher == nil || s.upstream == nil || s.cfg == nil || !s.cfg.Totp.EncryptionKeyConfigured {
		return infraerrors.New(http.StatusServiceUnavailable, "ZCODE_NOT_CONFIGURED", "ZCode OAuth 需要 Redis 和固定的凭据加密密钥")
	}
	return nil
}
func (s *ZCodeService) validateURL(raw string) error {
	allowed := []string{"zcode.z.ai", "api.z.ai", "open.bigmodel.cn", "bigmodel.cn"}
	if s.cfg != nil && s.cfg.Security.URLAllowlist.Enabled {
		allowed = s.cfg.Security.URLAllowlist.UpstreamHosts
	}
	_, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{AllowedHosts: allowed, RequireAllowlist: true, AllowPrivate: false})
	if err != nil {
		return &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return nil
}
func (s *ZCodeService) proxyURL(ctx context.Context, id *int64) (string, error) {
	if id == nil || *id == 0 {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	p, err := s.proxyRepo.GetByID(ctx, *id)
	if err != nil || p == nil {
		return "", &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return p.URL(), nil
}
func (s *ZCodeService) client(a *Account, proxy string) *zcode.Client {
	version, origin := "", ""
	if s.cfg != nil {
		version = s.cfg.Gateway.ZCode.AppVersion
		origin = s.cfg.Gateway.ZCode.Origin
	}
	device := ""
	if a != nil {
		device = a.GetCredential("zcode_device_id")
		if override := a.GetCredential("zcode_app_version"); override != "" {
			version = override
		}
	}
	identity := zcode.DefaultIdentity(version, device)
	if s.cfg != nil {
		if value := s.cfg.Gateway.ZCode.IdentityPlatform; value != "" {
			identity.Platform = value
		}
		identity.OSVersion = s.cfg.Gateway.ZCode.IdentityOSVersion
	}
	id, concurrency := int64(0), 1
	if a != nil {
		id = a.ID
		concurrency = a.Concurrency
	}
	return &zcode.Client{Origin: origin, Identity: identity, Send: func(req *http.Request) (*http.Response, error) {
		if err := s.validateURL(req.URL.String()); err != nil {
			return nil, err
		}
		ctx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamPublicHostsOnly(req.Context()))
		req = req.Clone(ctx)
		return s.upstream.Do(req, proxy, id, concurrency)
	}}
}
func (s *ZCodeService) tokens(a *Account) (zcode.Tokens, error) {
	if !a.IsZCodeOAuth() {
		return zcode.Tokens{}, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	plain, err := s.cipher.Decrypt(a.GetCredential(zcodeTokensKey))
	if err != nil {
		return zcode.Tokens{}, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	var tokens zcode.Tokens
	if json.Unmarshal([]byte(plain), &tokens) != nil {
		return tokens, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return tokens, nil
}
func (s *ZCodeService) pool(proxy string) *zcode.CaptchaPool {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	pool := s.pools[proxy]
	if pool == nil {
		pool = zcode.NewCaptchaPool(s.solver)
		s.pools[proxy] = pool
	}
	return pool
}
func decodeZCodeExtra(value any, out any) error {
	if value == nil {
		return errors.New("状态尚未获取")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func zcodeAdminError(err error) error {
	if infraerrors.Reason(err) != "" {
		return err
	}
	kind := zcode.KindOf(err)
	status := http.StatusBadGateway
	if kind == zcode.ErrLogin {
		status = http.StatusConflict
	}
	if kind == zcode.ErrConfiguration {
		status = http.StatusServiceUnavailable
	}
	return infraerrors.New(status, "ZCODE_"+strings.ToUpper(string(kind)), "ZCode "+string(kind))
}
func (s *ZCodeService) stateError(ctx context.Context, a *Account, e *zcode.Error) {
	// 429 是上游限流而非额度耗尽：只要额度快照未证明耗尽，就不写临时停调，
	// 保持账号可调度，由当前请求的正常换号与后续调度继续处理。
	if e.Kind == zcode.ErrRateLimit && zcodeRateLimitKeepsScheduling(a, time.Now()) {
		return
	}
	// 额度耗尽改为模型级冷却：只拦住耗尽余额对应的模型，其它有剩余额度的
	// 模型仍可命中本账号。快照不可信或无法定位模型时保留账号级停调（fail-closed）。
	if e.Kind == zcode.ErrQuota && s.applyZCodeQuotaModelCooldown(ctx, a, e, nil) {
		return
	}
	state := map[string]any{"last_error": string(e.Kind), "checked_at": time.Now().Unix(), "oauth_status": "authorized"}
	if e.Kind == zcode.ErrLogin {
		state["oauth_status"] = "login_required"
	}
	delay := time.Minute
	switch e.Kind {
	case zcode.ErrQuota:
		delay = 5 * time.Minute
	case zcode.ErrLogin, zcode.ErrConfiguration:
		delay = 10 * time.Minute
	case zcode.ErrCaptcha:
		delay = 2 * time.Minute
	}
	until := time.Now().Add(delay)
	if e.RetryAt > time.Now().Unix() {
		until = time.Unix(e.RetryAt, 0)
	}
	if until.After(time.Now().Add(24 * time.Hour)) {
		until = time.Now().Add(24 * time.Hour)
	}
	if _, err := s.updateState(ctx, a, map[string]any{zcodeStateKey: state}, &until, "zcode_"+string(e.Kind), false); err != nil {
		slog.Warn("zcode_cooldown_write_failed", "account_id", a.ID)
	}
	if s.rateLimit != nil {
		s.rateLimit.notifyAccountSchedulingBlocked(a, until, "zcode_"+string(e.Kind))
	}
}

// zcodeQuotaModelCooldownScope 返回额度耗尽时应冷却的模型名。
// onlyExhausted=true 时只取剩余为零/百分比满的余额；onlyExhausted=false 时
// 返回快照中全部余额模型（用于上游明确返回 quota 错误时，请求模型可能无余额记录）。
// Start Plan 余额按 show_name 对应模型；Coding Plan 余额名是窗口名（TOKENS_LIMIT），
// 无模型语义，返回空串表示无法模型化，调用方保持账号级停调。
// latest 非 nil 且更新时优先用它（绕开 a.Extra 可能滞后的克隆快照）。
func zcodeQuotaModelCooldownScopeFrom(latest *zcode.Quota, a *Account, onlyExhausted bool) string {
	var quota *zcode.Quota
	if latest != nil {
		copyVal := *latest
		quota = &copyVal
	} else {
		quota = zcodeQuotaFromAccount(a)
	}
	if quota == nil || quota.Plan != zcode.PlanStart || len(quota.Balances) == 0 {
		return ""
	}
	now := time.Now()
	models := make([]string, 0, len(quota.Balances))
	for _, balance := range quota.Balances {
		name := strings.TrimSpace(balance.Name)
		if name == "" {
			continue
		}
		if onlyExhausted {
			exhausted := false
			if balance.ResetAt > 0 && balance.ResetAt <= now.Unix() {
				exhausted = false
			} else if balance.ExpiresAt > 0 && balance.ExpiresAt <= now.Unix() {
				exhausted = false
			} else if balance.Remaining != nil {
				exhausted = *balance.Remaining <= 0
			} else if balance.Percent != nil {
				exhausted = *balance.Percent >= 100
			} else {
				// 无剩余数据时不能确认耗尽，不纳入模型冷却范围。
				exhausted = false
			}
			if !exhausted {
				continue
			}
		}
		models = append(models, name)
	}
	if len(models) != 1 {
		// 零个或多个耗尽模型时无法确定单一 scope，保持账号级停调。
		return ""
	}
	return models[0]
}

// applyZCodeQuotaModelCooldown 尝试把 ErrQuota 转为模型级冷却。
// latest 非 nil 时优先用它（QueryQuota 刚刷新的快照，传入的 a 的 Extra 可能滞后）；
// 否则读 a.Extra 里的快照。返回 true 表示已写入模型冷却，调用方无需再停调整号。
func (s *ZCodeService) applyZCodeQuotaModelCooldown(ctx context.Context, a *Account, e *zcode.Error, latest *zcode.Quota) bool {
	model := zcodeQuotaModelCooldownScopeFrom(latest, a, true)
	if model == "" {
		return false
	}
	quota := zcodeQuotaFromAccount(a)
	reset := time.Now().Add(5 * time.Minute)
	if e.RetryAt > time.Now().Unix() {
		reset = time.Unix(e.RetryAt, 0)
	} else if quota != nil {
		for _, balance := range quota.Balances {
			if !strings.EqualFold(strings.TrimSpace(balance.Name), model) {
				continue
			}
			if balance.ResetAt > time.Now().Unix() {
				reset = time.Unix(balance.ResetAt, 0)
			} else if balance.ExpiresAt > time.Now().Unix() {
				reset = time.Unix(balance.ExpiresAt, 0)
			}
		}
	}
	if reset.After(time.Now().Add(24 * time.Hour)) {
		reset = time.Now().Add(24 * time.Hour)
	}
	if err := s.repo.SetModelRateLimit(ctx, a.ID, model, reset, zcodeQuotaExhaustedReason); err != nil {
		slog.Warn("zcode_quota_model_cooldown_failed", "account_id", a.ID, "model", model)
		return false
	}
	// 顺带记录 last_error，便于管理端排查；不写冷却字段。
	_, _ = s.updateState(ctx, a, map[string]any{zcodeStateKey: map[string]any{"last_error": string(e.Kind), "checked_at": time.Now().Unix(), "oauth_status": "authorized"}}, nil, "", true)
	slog.Info("zcode_quota_model_cooldown", "account_id", a.ID, "model", model, "reset_at", reset.UTC())
	return true
}

// clearZCodeQuotaModelCooldowns 快照显示模型余额恢复时，移除对应模型的冷却记录。
// 走专用 SQL（删除 model_rate_limits 里指定键），避免整表 ClearModelRateLimits
// 误清其它平台的模型冷却。
func (s *ZCodeService) clearZCodeQuotaModelCooldown(ctx context.Context, a *Account, model string) {
	if model == "" || a == nil {
		return
	}
	if err := s.repo.ClearModelRateLimit(ctx, a.ID, model); err != nil {
		slog.Warn("zcode_quota_model_cooldown_clear_failed", "account_id", a.ID, "model", model)
		return
	}
	slog.Info("zcode_quota_model_cooldown_cleared", "account_id", a.ID, "model", model)
}

// clearZCodeQuotaModelCooldowns 在额度快照整体未耗尽时，清理所有仍带
// zcode_quota_exhausted 原因的模型冷却：新快照未标耗尽的模型已恢复可用。
// 用了兼容既有 ReadModelRateLimit 语义的账号快照；冷却原因缺失时保守跳过。
func (s *ZCodeService) clearZCodeQuotaModelCooldowns(ctx context.Context, a *Account, quota *zcode.Quota) {
	if a == nil {
		return
	}
	rawLimits, ok := a.Extra[modelRateLimitsKey].(map[string]any)
	if !ok {
		return
	}
	for scope, rawLimit := range rawLimits {
		limit, ok := rawLimit.(map[string]any)
		if !ok {
			continue
		}
		reason, _ := limit["reason"].(string)
		if reason != zcodeQuotaExhaustedReason {
			continue
		}
		// 快照里仍有同名余额且未恢复时不清理。
		if quota != nil {
			stillExhausted := false
			for _, balance := range quota.Balances {
				if strings.EqualFold(strings.TrimSpace(balance.Name), scope) {
					stillExhausted = balance.Remaining != nil && *balance.Remaining <= 0
					break
				}
			}
			if stillExhausted {
				continue
			}
		}
		s.clearZCodeQuotaModelCooldown(ctx, a, scope)
	}
}

// RoundTrip 由原网关在账号选中、权威复核及抢槽后调用。
func (s *ZCodeService) RoundTrip(req *http.Request, proxy string, a *Account) (*http.Response, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	tokens, err := s.tokens(a)
	if err != nil {
		return s.failureResponse(req, a, &zcode.Error{Kind: zcode.ErrConfiguration}, ""), nil
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, int64(257<<20)))
	if err != nil {
		return nil, &zcode.Error{Kind: zcode.ErrNetwork}
	}
	_ = req.Body.Close()
	if len(body) > 256<<20 {
		return nil, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	client := s.client(a, proxy)
	resp, upstreamErr, err := client.Forward(req.Context(), req, body, zcode.ForwardOptions{Provider: a.ZCodeProvider(), Plan: a.GetAccountMode(), Tokens: tokens, Resolver: s.resolver, Captcha: s.pool(proxy), Signer: s.signer, Proxy: proxy})
	if err != nil {
		return nil, err
	}
	if upstreamErr != nil {
		return s.failureResponse(req, a, upstreamErr, requestedModelFromForwardBody(body)), nil
	}
	return resp, nil
}

// requestedModelFromForwardBody 提取 Anthropic Messages 请求中的模型名，
// 供上游错误写入模型级冷却时定位 scope。解析失败返回空串。
func requestedModelFromForwardBody(body []byte) string {
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return ""
	}
	return strings.TrimSpace(probe.Model)
}
func (s *ZCodeService) failureResponse(req *http.Request, a *Account, e *zcode.Error, requestedModel string) *http.Response {
	s.stateErrorForRequest(req.Context(), a, e, requestedModel)
	status := http.StatusServiceUnavailable
	if e.Kind == zcode.ErrRateLimit || e.Kind == zcode.ErrQuota {
		status = http.StatusTooManyRequests
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "zcode_" + string(e.Kind), "message": "ZCode " + string(e.Kind)}})
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if e.RetryAt > time.Now().Unix() {
		headers.Set("Retry-After", strconv.FormatInt(e.RetryAt-time.Now().Unix(), 10))
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

// stateErrorWithQuota 在 stateError 基础上允许显式传入刚刷新的额度快照：
// 调用方可能持有更新前加载的账号克隆，Extra 里的 quota 已滞后。
func (s *ZCodeService) stateErrorWithQuota(ctx context.Context, a *Account, e *zcode.Error, quota *zcode.Quota) {
	if e.Kind == zcode.ErrRateLimit && zcodeRateLimitKeepsSchedulingWithQuota(a, quota, time.Now()) {
		return
	}
	if e.Kind == zcode.ErrQuota && s.applyZCodeQuotaModelCooldown(ctx, a, e, quota) {
		return
	}
	s.stateError(ctx, a, e)
}

// zcodeRateLimitKeepsSchedulingWithQuota 同 zcodeRateLimitKeepsScheduling，
// 但优先使用显式传入的新快照。
func zcodeRateLimitKeepsSchedulingWithQuota(a *Account, quota *zcode.Quota, now time.Time) bool {
	if quota != nil {
		return !quota.Exhausted(now)
	}
	return zcodeRateLimitKeepsScheduling(a, now)
}

// stateErrorForRequest 在 stateError 基础上支持请求模型：上游明确返回 quota
// 错误时，即使快照无法确认耗尽（余额记录缺失），也可按请求模型写模型级冷却；
// 其它错误行为与 stateError 一致。
func (s *ZCodeService) stateErrorForRequest(ctx context.Context, a *Account, e *zcode.Error, requestedModel string) {
	if e.Kind == zcode.ErrRateLimit && zcodeRateLimitKeepsScheduling(a, time.Now()) {
		return
	}
	if e.Kind == zcode.ErrQuota {
		if s.applyZCodeQuotaModelCooldown(ctx, a, e, nil) {
			return
		}
		// 快照无法定位单一耗尽模型：若有请求模型，冷却请求模型本身；
		// 无请求模型或模型为空才退回账号级停调。
		if model := strings.TrimSpace(requestedModel); model != "" && s.setZCodeRequestModelCooldown(ctx, a, e, model) {
			return
		}
	}
	s.stateError(ctx, a, e)
}

// setZCodeRequestModelCooldown 按请求模型写冷却，reset 优先取上游 RetryAt。
func (s *ZCodeService) setZCodeRequestModelCooldown(ctx context.Context, a *Account, e *zcode.Error, model string) bool {
	reset := time.Now().Add(5 * time.Minute)
	if e.RetryAt > time.Now().Unix() {
		reset = time.Unix(e.RetryAt, 0)
	}
	if reset.After(time.Now().Add(24 * time.Hour)) {
		reset = time.Now().Add(24 * time.Hour)
	}
	if err := s.repo.SetModelRateLimit(ctx, a.ID, model, reset, zcodeQuotaExhaustedReason); err != nil {
		slog.Warn("zcode_quota_request_model_cooldown_failed", "account_id", a.ID, "model", model)
		return false
	}
	_, _ = s.updateState(ctx, a, map[string]any{zcodeStateKey: map[string]any{"last_error": string(e.Kind), "checked_at": time.Now().Unix(), "oauth_status": "authorized"}}, nil, "", true)
	slog.Info("zcode_quota_request_model_cooldown", "account_id", a.ID, "model", model, "reset_at", reset.UTC())
	return true
}

func (s *ZCodeService) Status(ctx context.Context, id int64) (map[string]any, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.IsZCodeOAuth() {
		return nil, infraerrors.BadRequest("ZCODE_ACCOUNT_INVALID", "账号不是 ZCode OAuth 智谱账号")
	}
	status := map[string]any{"auth_mode": "zcode_oauth", "provider": a.ZCodeProvider(), "plan": a.GetAccountMode(), "auto_claim": a.Credentials["zcode_auto_claim"], "state": a.Extra[zcodeStateKey], "quota": a.Extra[zcodeQuotaKey], "claim": a.Extra[zcodeClaimKey]}
	return status, nil
}
func (s *ZCodeService) clearOwnCooldown(ctx context.Context, a *Account) {
	if strings.HasPrefix(a.TempUnschedulableReason, "zcode_") {
		_ = s.repo.ClearTempUnschedulable(ctx, a.ID)
	}
}
func zcodeAccountKey(id int64) string { return fmt.Sprintf("account:%d", id) }
