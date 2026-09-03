package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"golang.org/x/sync/errgroup"
)

// ChannelMonitorRepository 渠道监控数据访问接口。
// 入参/返回的指针类型均使用 service 包的 ChannelMonitor 模型，
// repository 实现负责与 ent 模型互转，并保持 api_key_encrypted 字段为密文。
type ChannelMonitorRepository interface {
	// CRUD
	Create(ctx context.Context, m *ChannelMonitor) error
	GetByID(ctx context.Context, id int64) (*ChannelMonitor, error)
	Update(ctx context.Context, m *ChannelMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error)
	FindByDuplicateOperationID(ctx context.Context, operationID string) (*ChannelMonitor, error)

	// 旧数据迁移：先解析全部旧密钥，再用单事务写入本站分组并清空旧凭据。
	ListPendingLocalTargets(ctx context.Context) ([]*ChannelMonitor, error)
	ResolveLegacyAPIKeyGroupID(ctx context.Context, apiKey string) (int64, error)
	MigrateLocalTargets(ctx context.Context, targets []ChannelMonitorLocalTargetMigration) error

	// 调度器辅助
	ListEnabled(ctx context.Context) ([]*ChannelMonitor, error)
	ListEnabledLocalByGroupIDs(ctx context.Context, groupIDs []int64) ([]*ChannelMonitor, error)
	MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error
	InsertHistoryBatch(ctx context.Context, rows []*ChannelMonitorHistoryRow) error
	DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error)

	// 历史记录
	ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error)

	// 用户视图聚合
	ListLatestPerModel(ctx context.Context, monitorID int64) ([]*ChannelMonitorLatest, error)
	ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*ChannelMonitorAvailability, error)

	// 批量聚合（admin/user list 用，避免 N+1）
	ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*ChannelMonitorLatest, error)
	ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*ChannelMonitorAvailability, error)
	// ListRecentHistoryForMonitors 批量取多个 monitor 各自主模型（primaryModels[monitorID]）最近 perMonitorLimit 条历史。
	// 返回的 entry 已按 checked_at DESC 排序（最新在前），不含 message 字段。
	ListRecentHistoryForMonitors(ctx context.Context, ids []int64, primaryModels map[int64]string, perMonitorLimit int) (map[int64][]*ChannelMonitorHistoryEntry, error)

	// ---------- 聚合维护（OpsCleanupService 调用） ----------

	// UpsertDailyRollupsFor 把 targetDate 当天的明细按 (monitor_id, model, bucket_date)
	// 聚合到 channel_monitor_daily_rollups。targetDate 会被截断到日期；
	// 用 ON CONFLICT DO UPDATE 实现幂等回填，返回 upsert 影响的行数。
	UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error)
	// DeleteRollupsBefore 软删 bucket_date < beforeDate 的聚合行，返回删除行数。
	DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error)
	// LoadAggregationWatermark 读 watermark（id=1）。
	// 返回 nil 表示从未聚合过；watermark 表本身预期已存在单行（migration 110 写入）。
	LoadAggregationWatermark(ctx context.Context) (*time.Time, error)
	// UpdateAggregationWatermark 写 watermark（UPSERT 到 id=1）。
	UpdateAggregationWatermark(ctx context.Context, date time.Time) error
}

// channelMonitorRuntimeReader is the optional settings view used to gate V1
// active probes by channel_monitor_enabled + channel_monitor_mode.
type channelMonitorRuntimeReader interface {
	GetChannelMonitorRuntime(ctx context.Context) ChannelMonitorRuntime
}

// ChannelMonitorService 渠道监控管理服务。
type ChannelMonitorService struct {
	repo      ChannelMonitorRepository
	encryptor SecretEncryptor
	groupRepo GroupRepository
	// settings is optional; when nil, RunCheck fails closed for active probes
	// (mode defaults to v2 / retired) so tests without settings never hit upstream.
	settings channelMonitorRuntimeReader

	localHandlerMu sync.RWMutex
	localHandler   http.Handler

	routingHealthCacheMu   sync.RWMutex
	routingHealthRefreshMu sync.Mutex
	routingHealthCache     map[int64]channelMonitorRoutingHealthCacheEntry
	// scheduler 由 wire 通过 SetScheduler 注入；CRUD 后调用对应钩子即时同步任务。
	// 测试或未注入场景下保持 nil，所有钩子调用变为 no-op。
	scheduler MonitorScheduler
	// quotaFetcher 由 wire 通过 SetQuotaFetcher 注入（accountUsage/CN 服务在本服务
	// 之后构造，构造参数注入会破坏既有依赖顺序）。nil 时 fail-closed：
	// 配额模式的检测产出「未配置」错误快照，Create/Update 关联账号直接报错。
	quotaFetcher *ChannelMonitorQuotaFetcher
}

const maxChannelMonitorNameRunes = 100

// ChannelMonitorDuplicateOperationIDMetadataKey is stored in the existing
// extra_headers JSON column to avoid a schema migration. The colon makes it an
// invalid HTTP header name, and repository adapters remove it before exposing
// ExtraHeaders to the service layer.
const ChannelMonitorDuplicateOperationIDMetadataKey = "sub2api:duplicate_operation_id"

// NewChannelMonitorService 创建渠道监控服务实例。
// groupRepo 使用可选参数以兼容只测试外站逻辑的轻量单元测试。
func NewChannelMonitorService(repo ChannelMonitorRepository, encryptor SecretEncryptor, groupRepos ...GroupRepository) *ChannelMonitorService {
	svc := &ChannelMonitorService{
		repo:               repo,
		encryptor:          encryptor,
		routingHealthCache: make(map[int64]channelMonitorRoutingHealthCacheEntry),
	}
	if len(groupRepos) > 0 {
		svc.groupRepo = groupRepos[0]
	}
	return svc
}

// SetRuntimeReader injects the settings reader used to gate active probes.
// Optional: when unset, active probes are treated as mode=v2 (retired).
func (s *ChannelMonitorService) SetRuntimeReader(r channelMonitorRuntimeReader) {
	if s == nil {
		return
	}
	s.settings = r
}

func (s *ChannelMonitorService) probeRuntime(ctx context.Context) ChannelMonitorRuntime {
	if s == nil || s.settings == nil {
		return ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}
	}
	return s.settings.GetChannelMonitorRuntime(ctx)
}

// ---------- CRUD ----------

// List 列表查询（支持 provider/enabled/search 过滤 + 分页）。
// 返回的 ChannelMonitor.APIKey 已解密为明文，handler 层负责脱敏。
func (s *ChannelMonitorService) List(ctx context.Context, params ChannelMonitorListParams) ([]*ChannelMonitor, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 200 {
		params.PageSize = 20
	}
	items, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("list channel monitors: %w", err)
	}
	for _, it := range items {
		s.decryptInPlace(it)
	}
	return items, total, nil
}

// Get 查询单个监控（解密 API Key）。
func (s *ChannelMonitorService) Get(ctx context.Context, id int64) (*ChannelMonitor, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s.decryptInPlace(m)
	return m, nil
}

// Create 创建监控。本站绑定分组且不保存凭据；外站加密保存 API Key。
func (s *ChannelMonitorService) Create(ctx context.Context, p ChannelMonitorCreateParams) (*ChannelMonitor, error) {
	if err := validateCreateParams(p); err != nil {
		return nil, err
	}
	if err := validateBodyModeForProtocol(p.Provider, p.APIMode, p.BodyOverrideMode, p.BodyOverride); err != nil {
		return nil, err
	}
	if err := validateExtraHeaders(p.ExtraHeaders); err != nil {
		return nil, err
	}
	checkMode := defaultCheckMode(p.CheckMode)
	if err := s.validateLinkedAccount(ctx, p.Provider, p.AccountID); err != nil {
		return nil, err
	}
	targetType := normalizeMonitorCreateTargetType(p, checkMode)
	group, endpoint, encryptedKey, groupName, err := s.prepareCreateTarget(ctx, p, targetType, checkMode)
	if err != nil {
		return nil, err
	}
	var groupID *int64
	if group != nil {
		id := group.ID
		groupID = &id
	}
	m := &ChannelMonitor{
		Name:             strings.TrimSpace(p.Name),
		Provider:         p.Provider,
		APIMode:          defaultAPIMode(p.APIMode),
		TargetType:       targetType,
		GroupID:          groupID,
		Group:            group,
		Endpoint:         endpoint,
		APIKey:           encryptedKey,
		PrimaryModel:     normalizeMonitorPrimaryModel(p.Provider, checkMode, p.PrimaryModel),
		ExtraModels:      normalizeModels(p.ExtraModels),
		GroupName:        groupName,
		Enabled:          p.Enabled,
		IntervalSeconds:  p.IntervalSeconds,
		JitterSeconds:    p.JitterSeconds,
		CreatedBy:        p.CreatedBy,
		TemplateID:       p.TemplateID,
		ExtraHeaders:     emptyHeadersIfNil(p.ExtraHeaders),
		BodyOverrideMode: defaultBodyMode(p.BodyOverrideMode),
		BodyOverride:     p.BodyOverride,
		CheckMode:        checkMode,
		AccountID:        cloneInt64Pointer(p.AccountID),
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create channel monitor: %w", err)
	}
	if m.GroupID != nil {
		s.invalidateAPIKeyRoutingHealth(*m.GroupID)
	}
	if targetType == ChannelMonitorTargetExternal {
		m.APIKey = strings.TrimSpace(p.APIKey)
	}
	if s.scheduler != nil {
		s.scheduler.Schedule(m)
	}
	return m, nil
}

func (s *ChannelMonitorService) prepareCreateTarget(
	ctx context.Context,
	p ChannelMonitorCreateParams,
	targetType, checkMode string,
) (*Group, string, string, string, error) {
	switch targetType {
	case ChannelMonitorTargetLocal:
		group, err := s.resolveLocalMonitorGroup(ctx, p.Provider, p.GroupID)
		if err != nil {
			return nil, "", "", "", err
		}
		return group, "", "", group.Name, nil
	case ChannelMonitorTargetExternal:
		endpoint := strings.TrimSpace(p.Endpoint)
		plain := strings.TrimSpace(p.APIKey)
		if checkMode != MonitorCheckModeQuota || endpoint != "" {
			if err := validateEndpointForProvider(p.Provider, endpoint); err != nil {
				return nil, "", "", "", err
			}
		}
		if checkMode != MonitorCheckModeQuota && plain == "" {
			return nil, "", "", "", ErrChannelMonitorMissingAPIKey
		}
		encrypted := ""
		if plain != "" {
			var err error
			encrypted, err = s.encryptor.Encrypt(plain)
			if err != nil {
				return nil, "", "", "", fmt.Errorf("encrypt api key: %w", err)
			}
		}
		return nil, normalizeEndpoint(endpoint), encrypted, strings.TrimSpace(p.GroupName), nil
	default:
		return nil, "", "", "", ErrChannelMonitorInvalidTargetType
	}
}

// Duplicate creates an independent, disabled copy of an existing monitor.
func (s *ChannelMonitorService) Duplicate(
	ctx context.Context,
	id, createdBy int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	existing, err := s.RecoverDuplicate(ctx, id, actorScope, operationKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	source, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	var plainAPIKey, encryptedAPIKey string
	if source.TargetType != ChannelMonitorTargetLocal && strings.TrimSpace(source.APIKey) != "" {
		plainAPIKey, err = s.decryptAPIKeyForDuplicate(source)
		if err != nil {
			return nil, err
		}
		encryptedAPIKey, err = s.encryptor.Encrypt(plainAPIKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt duplicate channel monitor api key: %w", err)
		}
	}
	bodyOverride, err := cloneChannelMonitorJSONMap(source.BodyOverride)
	if err != nil {
		return nil, fmt.Errorf("clone duplicate channel monitor body override: %w", err)
	}

	duplicate := &ChannelMonitor{
		Name:                 duplicateChannelMonitorName(source.Name),
		Provider:             source.Provider,
		APIMode:              source.APIMode,
		TargetType:           source.TargetType,
		GroupID:              cloneInt64Pointer(source.GroupID),
		Group:                source.Group,
		Endpoint:             source.Endpoint,
		APIKey:               encryptedAPIKey,
		PrimaryModel:         source.PrimaryModel,
		ExtraModels:          append([]string{}, source.ExtraModels...),
		GroupName:            source.GroupName,
		Enabled:              false,
		IntervalSeconds:      source.IntervalSeconds,
		JitterSeconds:        source.JitterSeconds,
		CreatedBy:            createdBy,
		TemplateID:           cloneInt64Pointer(source.TemplateID),
		ExtraHeaders:         cloneChannelMonitorHeaders(source.ExtraHeaders),
		BodyOverrideMode:     source.BodyOverrideMode,
		BodyOverride:         bodyOverride,
		CheckMode:            defaultCheckMode(source.CheckMode),
		AccountID:            cloneInt64Pointer(source.AccountID),
		DuplicateOperationID: operationID,
	}
	if source.TargetType == ChannelMonitorTargetLocal {
		// Local monitors use their group binding as the in-process credential.
		duplicate.APIKey = ""
	} else if defaultCheckMode(source.CheckMode) != MonitorCheckModeQuota && strings.TrimSpace(plainAPIKey) == "" {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	if err := s.repo.Create(ctx, duplicate); err != nil {
		return nil, fmt.Errorf("duplicate channel monitor: %w", err)
	}

	// Match Create/Update response semantics: repository receives ciphertext,
	// while handlers receive plaintext only so they can return the masked form.
	// Empty target_type is a legacy external monitor and follows the same path.
	if source.TargetType != ChannelMonitorTargetLocal {
		duplicate.APIKey = plainAPIKey
	}
	return duplicate, nil
}

// RecoverDuplicate performs a read-only lookup for a duplicate that was
// already committed for the same actor, source monitor, and idempotency key.
// It deliberately never repeats the create side effect.
func (s *ChannelMonitorService) RecoverDuplicate(
	ctx context.Context,
	id int64,
	actorScope, operationKey string,
) (*ChannelMonitor, error) {
	operationID := duplicateChannelMonitorOperationID(id, actorScope, operationKey)
	if operationID == "" {
		return nil, nil
	}
	monitor, err := s.repo.FindByDuplicateOperationID(ctx, operationID)
	if err != nil {
		return nil, fmt.Errorf("find duplicate channel monitor operation: %w", err)
	}
	if monitor == nil {
		return nil, nil
	}
	s.decryptInPlace(monitor)
	return monitor, nil
}

func duplicateChannelMonitorOperationID(sourceID int64, actorScope, operationKey string) string {
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" {
		return ""
	}
	actorScope = strings.TrimSpace(actorScope)
	if actorScope == "" {
		actorScope = "admin:0"
	}
	payload := "admin.channel_monitors.duplicate\x00" + actorScope + "\x00" + strconv.FormatInt(sourceID, 10) + "\x00" + operationKey
	digest := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", digest)
}

func (s *ChannelMonitorService) decryptAPIKeyForDuplicate(source *ChannelMonitor) (string, error) {
	if source == nil || strings.TrimSpace(source.APIKey) == "" {
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	plain, err := s.encryptor.Decrypt(source.APIKey)
	if err != nil {
		slog.Warn("channel_monitor: decrypt api key for duplicate failed",
			"monitor_id", source.ID, "error", err)
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	// quota 模式明文为空串是合法状态（api_key_encrypted 存的是加密空串）：
	// 重加密空串即可。若在此报错，克隆出的配额监控会被 runner 当作
	// 解密失败而 Unschedule，静默停摆。
	if strings.TrimSpace(plain) == "" {
		if monitorCheckModeUsesQuota(defaultCheckMode(source.CheckMode)) {
			return "", nil
		}
		slog.Warn("channel_monitor: decrypted api key for duplicate is empty",
			"monitor_id", source.ID)
		return "", ErrChannelMonitorAPIKeyDecryptFailed
	}
	return plain, nil
}

func duplicateChannelMonitorName(sourceName string) string {
	const suffix = " (Copy)"
	nameRunes := []rune(strings.TrimSpace(sourceName))
	maxBaseRunes := maxChannelMonitorNameRunes - len([]rune(suffix))
	if len(nameRunes) > maxBaseRunes {
		nameRunes = nameRunes[:maxBaseRunes]
	}
	return string(nameRunes) + suffix
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelMonitorHeaders(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneChannelMonitorJSONMap(source map[string]any) (map[string]any, error) {
	if source == nil {
		return nil, nil
	}
	payload, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	cloned := make(map[string]any, len(source))
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func normalizeMonitorCreateTargetType(p ChannelMonitorCreateParams, checkMode string) string {
	if defaultCheckMode(checkMode) == MonitorCheckModeQuota &&
		strings.TrimSpace(p.TargetType) == "" && p.GroupID == nil {
		return ChannelMonitorTargetExternal
	}
	return normalizeMonitorTargetType(p.TargetType, p.Endpoint, p.APIKey)
}

// validateCreateParams 把 Create 入参的所有校验聚拢为一个函数。
// probe/quota_probe 需要本地分组或外部 endpoint+api_key；quota 只需关联账号。
func validateCreateParams(p ChannelMonitorCreateParams) error {
	if err := validateProvider(p.Provider); err != nil {
		return err
	}
	checkMode := defaultCheckMode(p.CheckMode)
	if err := validateCheckMode(p.Provider, checkMode); err != nil {
		return err
	}
	if err := validateAPIMode(p.Provider, p.APIMode); err != nil {
		return err
	}
	if err := validateInterval(p.IntervalSeconds); err != nil {
		return err
	}
	if err := validateJitter(p.JitterSeconds, p.IntervalSeconds); err != nil {
		return err
	}
	usesQuota := monitorCheckModeUsesQuota(checkMode)
	if usesQuota && (p.AccountID == nil || *p.AccountID <= 0) {
		return ErrChannelMonitorAccountRequired
	}
	if normalizeMonitorPrimaryModel(p.Provider, checkMode, p.PrimaryModel) == "" {
		return ErrChannelMonitorMissingPrimaryModel
	}
	targetType := normalizeMonitorCreateTargetType(p, checkMode)
	if err := validateMonitorTargetType(targetType); err != nil {
		return err
	}
	if targetType == ChannelMonitorTargetLocal {
		if p.GroupID == nil || *p.GroupID <= 0 {
			return ErrChannelMonitorMissingGroup
		}
	} else {
		endpoint := strings.TrimSpace(p.Endpoint)
		if checkMode != MonitorCheckModeQuota || endpoint != "" {
			if err := validateEndpointForProvider(p.Provider, endpoint); err != nil {
				return err
			}
		}
		if checkMode != MonitorCheckModeQuota && strings.TrimSpace(p.APIKey) == "" {
			return ErrChannelMonitorMissingAPIKey
		}
	}
	return nil
}

// validateLinkedAccount 校验关联账号存在、平台与监控 provider 一致、且能充当
// 配额数据源（能力拦截，见 monitorAccountQuotaCapability）。
// fetcher 未注入时 fail-closed（拒绝创建配额监控，而不是创建后静默坏）。
func (s *ChannelMonitorService) validateLinkedAccount(ctx context.Context, provider string, accountID *int64) error {
	if accountID == nil || *accountID <= 0 {
		return nil
	}
	if s.quotaFetcher == nil {
		return ErrChannelMonitorAccountRequired
	}
	account, err := s.quotaFetcher.LoadAccount(ctx, *accountID)
	if err != nil || account == nil {
		return ErrChannelMonitorAccountRequired
	}
	if account.Platform != monitorProviderPlatform(provider) {
		return ErrChannelMonitorProviderIncompatible
	}
	return monitorAccountQuotaCapability(account)
}

// Update 更新监控，并按最终 target_type 与 check_mode 应用约束。
func (s *ChannelMonitorService) Update(ctx context.Context, id int64, p ChannelMonitorUpdateParams) (*ChannelMonitor, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	previousProvider := existing.Provider
	previousGroupID := int64(0)
	if existing.GroupID != nil {
		previousGroupID = *existing.GroupID
	}
	if err := applyMonitorUpdate(existing, p); err != nil {
		return nil, err
	}

	targetType := normalizeMonitorTargetType(existing.TargetType, existing.Endpoint, existing.APIKey)
	if p.TargetType != nil {
		targetType = strings.TrimSpace(*p.TargetType)
	}
	if err := validateMonitorTargetType(targetType); err != nil {
		return nil, err
	}
	newPlainAPIKey, apiKeyUpdated, err := s.applyMonitorTargetUpdate(
		ctx,
		existing,
		p,
		targetType,
		existing.Provider != previousProvider,
	)
	if err != nil {
		return nil, err
	}
	if err := validateMonitorModeFields(existing); err != nil {
		return nil, err
	}
	if err := s.validateProbeAPIKey(existing, newPlainAPIKey); err != nil {
		return nil, err
	}
	if p.Provider != nil || p.CheckMode != nil || p.AccountID != nil {
		if err := s.revalidateLinkedAccount(ctx, existing); err != nil {
			return nil, err
		}
	}

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("update channel monitor: %w", err)
	}
	currentGroupID := int64(0)
	if existing.GroupID != nil {
		currentGroupID = *existing.GroupID
	}
	s.invalidateAPIKeyRoutingHealth(previousGroupID, currentGroupID)

	if existing.TargetType == ChannelMonitorTargetExternal {
		if apiKeyUpdated {
			existing.APIKey = newPlainAPIKey
		} else {
			s.decryptInPlace(existing)
		}
	}
	if s.scheduler != nil {
		s.scheduler.Schedule(existing)
	}
	return existing, nil
}

func (s *ChannelMonitorService) applyMonitorTargetUpdate(
	ctx context.Context,
	existing *ChannelMonitor,
	p ChannelMonitorUpdateParams,
	targetType string,
	providerChanged bool,
) (plain string, apiKeyUpdated bool, err error) {
	switch targetType {
	case ChannelMonitorTargetLocal:
		groupID := existing.GroupID
		if p.GroupID != nil {
			id := *p.GroupID
			groupID = &id
		}
		group, groupErr := s.resolveLocalMonitorGroup(ctx, existing.Provider, groupID)
		if groupErr != nil {
			return "", false, groupErr
		}
		existing.TargetType = ChannelMonitorTargetLocal
		existing.GroupID = groupID
		existing.Group = group
		existing.GroupName = group.Name
		existing.Endpoint = ""
		existing.APIKey = ""
		existing.APIKeyDecryptFailed = false
		return "", false, nil
	case ChannelMonitorTargetExternal:
		requiresProbe := defaultCheckMode(existing.CheckMode) != MonitorCheckModeQuota
		if requiresProbe && providerChanged && (p.APIKey == nil || strings.TrimSpace(*p.APIKey) == "") {
			return "", false, ErrChannelMonitorMissingAPIKey
		}
		endpoint := existing.Endpoint
		if p.Endpoint != nil {
			endpoint = *p.Endpoint
		}
		if requiresProbe || strings.TrimSpace(endpoint) != "" {
			if endpointErr := validateEndpointForProvider(existing.Provider, endpoint); endpointErr != nil {
				return "", false, endpointErr
			}
		}
		existing.TargetType = ChannelMonitorTargetExternal
		existing.GroupID = nil
		existing.Group = nil
		existing.Endpoint = normalizeEndpoint(endpoint)
		plain, apiKeyUpdated, err = s.applyAPIKeyUpdate(existing, p.APIKey)
		if err != nil {
			return "", false, err
		}
		if requiresProbe && strings.TrimSpace(existing.APIKey) == "" {
			return "", false, ErrChannelMonitorMissingAPIKey
		}
		return plain, apiKeyUpdated, nil
	default:
		return "", false, ErrChannelMonitorInvalidTargetType
	}
}

// validateMonitorModeFields 校验 check_mode 与 target 字段的组合约束。
func validateMonitorModeFields(m *ChannelMonitor) error {
	checkMode := defaultCheckMode(m.CheckMode)
	if err := validateCheckMode(m.Provider, checkMode); err != nil {
		return err
	}
	if monitorCheckModeUsesQuota(checkMode) && m.AccountID == nil {
		return ErrChannelMonitorAccountRequired
	}
	if checkMode == MonitorCheckModeQuota {
		return nil
	}
	if m.TargetType == ChannelMonitorTargetLocal {
		if m.GroupID == nil {
			return ErrChannelMonitorMissingGroup
		}
		return nil
	}
	if strings.TrimSpace(m.Endpoint) == "" {
		return ErrChannelMonitorInvalidEndpoint
	}
	return nil
}

// validateProbeAPIKey 探活模式（probe / quota_probe）必须持有可用明文 key：
// 存量密文解密为空串（quota 监控切回探活但未重填 key）时拒绝。
// 密文损坏的情况交给既有 APIKeyDecryptFailed 链路（Get/RunCheck 会显式报错）。
func (s *ChannelMonitorService) validateProbeAPIKey(m *ChannelMonitor, newPlainKey string) error {
	if defaultCheckMode(m.CheckMode) == MonitorCheckModeQuota || m.TargetType == ChannelMonitorTargetLocal {
		return nil
	}
	if strings.TrimSpace(newPlainKey) != "" {
		return nil
	}
	if strings.TrimSpace(m.APIKey) == "" {
		return ErrChannelMonitorMissingAPIKey
	}
	plain, err := s.encryptor.Decrypt(m.APIKey)
	if err != nil {
		return nil
	}
	if strings.TrimSpace(plain) == "" {
		return ErrChannelMonitorMissingAPIKey
	}
	return nil
}

// revalidateLinkedAccount 在 provider/check_mode/account_id 任一变化后复核关联账号：
//   - 账号已被删除或平台失配：probe 模式自动解绑（静默修复），
//     quota 模式显式报错（配额监控必须有可用数据源）
func (s *ChannelMonitorService) revalidateLinkedAccount(ctx context.Context, m *ChannelMonitor) error {
	usesQuota := monitorCheckModeUsesQuota(defaultCheckMode(m.CheckMode))
	if m.AccountID == nil {
		if usesQuota {
			return ErrChannelMonitorAccountRequired
		}
		return nil
	}
	if s.quotaFetcher == nil {
		return ErrChannelMonitorAccountRequired
	}
	account, err := s.quotaFetcher.LoadAccount(ctx, *m.AccountID)
	if err != nil || account == nil {
		if usesQuota {
			return ErrChannelMonitorAccountRequired
		}
		m.AccountID = nil
		return nil
	}
	if account.Platform != monitorProviderPlatform(m.Provider) {
		if usesQuota {
			return ErrChannelMonitorProviderIncompatible
		}
		m.AccountID = nil
		return nil
	}
	// 能力失配（如 deepseek coding / zhipu payg / API-Key 型海外账号）：
	// quota 模式显式报错（有该类存量监控时编辑会被拦，出路是换账号或切 probe），
	// probe 模式账号无用途，静默解绑。
	if err := monitorAccountQuotaCapability(account); err != nil {
		if usesQuota {
			return err
		}
		m.AccountID = nil
	}
	return nil
}

// applyAPIKeyUpdate 处理 Update 中的 APIKey 字段：
//   - 入参 raw 为 nil 或空白：不修改 existing.APIKey（仍为密文），返回 updated=false
//   - 非空：加密后写入 existing.APIKey；同时把明文返回给调用方，
//     供写库成功后塞回 existing 避免把密文吐回客户端
func (s *ChannelMonitorService) applyAPIKeyUpdate(existing *ChannelMonitor, raw *string) (plain string, updated bool, err error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return "", false, nil
	}
	plain = strings.TrimSpace(*raw)
	encrypted, encErr := s.encryptor.Encrypt(plain)
	if encErr != nil {
		return "", false, fmt.Errorf("encrypt api key: %w", encErr)
	}
	existing.APIKey = encrypted
	return plain, true, nil
}

// Delete 删除监控（历史通过外键 CASCADE 自动清理）。
func (s *ChannelMonitorService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete channel monitor: %w", err)
	}
	s.invalidateAPIKeyRoutingHealth()
	if s.scheduler != nil {
		s.scheduler.Unschedule(id)
	}
	return nil
}

// ListHistory 列出某个监控最近的检测历史。
// model 为空表示返回所有模型；limit <= 0 时使用默认值，超过上限会被截断。
func (s *ChannelMonitorService) ListHistory(ctx context.Context, id int64, model string, limit int) ([]*ChannelMonitorHistoryEntry, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = MonitorHistoryDefaultLimit
	}
	if limit > MonitorHistoryMaxLimit {
		limit = MonitorHistoryMaxLimit
	}
	entries, err := s.repo.ListHistory(ctx, id, strings.TrimSpace(model), limit)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	return entries, nil
}

// ---------- 业务 ----------

// RunCheck 同步触发对一个监控的检测：并发跑 primary + extra 模型，
// 写历史记录并更新 last_checked_at。返回每个模型的检测结果。
// 仅当 channel_monitor_enabled=true 且 channel_monitor_mode=v1 时真正探测；
// mode=v2 时返回 ErrChannelMonitorActiveProbesRetired，不产生上游流量。
//
// 按 check_mode 分派：probe（默认，现状探活）/ quota（仅查关联账号配额，
// 零 LLM 成本）/ quota_probe（探活 + 配额快照挂主模型行）。
func (s *ChannelMonitorService) RunCheck(ctx context.Context, id int64) ([]*CheckResult, error) {
	rt := s.probeRuntime(ctx)
	if !rt.Enabled {
		return nil, ErrChannelMonitorDisabled
	}
	if !rt.ActiveProbesAllowed() {
		return nil, ErrChannelMonitorActiveProbesRetired
	}
	m, err := s.Get(ctx, id) // 已解密 APIKey
	if err != nil {
		return nil, err
	}
	checkMode := defaultCheckMode(m.CheckMode)
	if checkMode != MonitorCheckModeQuota && m.TargetType == ChannelMonitorTargetExternal && m.APIKeyDecryptFailed {
		return nil, ErrChannelMonitorAPIKeyDecryptFailed
	}
	if checkMode != MonitorCheckModeQuota && m.TargetType == ChannelMonitorTargetLocal {
		if m.GroupID == nil || m.Group == nil {
			return nil, ErrChannelMonitorMissingGroup
		}
		if m.Group.ID != *m.GroupID || !m.Group.IsActive() {
			return nil, ErrChannelMonitorGroupUnavailable
		}
		if m.Group.Platform != monitorProviderPlatform(m.Provider) {
			return nil, ErrChannelMonitorGroupPlatformMismatch
		}
	}

	var results []*CheckResult
	switch checkMode {
	case MonitorCheckModeQuota:
		results = s.runQuotaOnlyCheck(ctx, m)
	case MonitorCheckModeQuotaProbe:
		results = s.runChecksConcurrent(ctx, m)
		attachQuotaSnapshot(results, s.fetchQuotaSnapshot(ctx, m))
	default:
		results = s.runChecksConcurrent(ctx, m)
	}
	s.persistCheckResults(ctx, m, results)
	return results, nil
}

// runQuotaOnlyCheck quota 模式：一次配额抓取 → 单条 CheckResult
// （Model=PrimaryModel，默认 "quota"；无 ping/latency，状态由快照推导）。
func (s *ChannelMonitorService) runQuotaOnlyCheck(ctx context.Context, m *ChannelMonitor) []*CheckResult {
	snapshot := s.fetchQuotaSnapshot(ctx, m)
	res := deriveQuotaCheckResult(snapshot, m.PrimaryModel, time.Now())
	res.Quota = snapshot
	return []*CheckResult{res}
}

// fetchQuotaSnapshot 抓取关联账号配额。未关联账号 / fetcher 未注入时返回
// 显式错误快照（不返回 error，保证检测周期与历史时间线连续）。
func (s *ChannelMonitorService) fetchQuotaSnapshot(ctx context.Context, m *ChannelMonitor) *domain.MonitorQuotaSnapshot {
	if m.AccountID == nil {
		return quotaErrorSnapshot("usage", "linked account not found", time.Now())
	}
	if s.quotaFetcher == nil {
		return quotaErrorSnapshot("usage", "quota fetcher is not configured", time.Now())
	}
	return s.quotaFetcher.Fetch(ctx, *m.AccountID)
}

// attachQuotaSnapshot quota_probe：把配额快照挂到主模型行（results[0]）。
// 配额失败不改变探活状态，仅在探活 message 为空时附注失败原因。
func attachQuotaSnapshot(results []*CheckResult, snapshot *domain.MonitorQuotaSnapshot) {
	if len(results) == 0 || snapshot == nil {
		return
	}
	primary := results[0]
	primary.Quota = snapshot
	if !snapshot.Success && strings.TrimSpace(primary.Message) == "" {
		primary.Message = truncateMessage("quota fetch failed: " + snapshot.Error)
	}
}

// persistCheckResults 写入本次检测的历史记录并更新 last_checked_at。
// 任一写库失败都只记日志，不影响调用方拿到 results（与 MVP 期望一致：宁可漏记历史也要先返回结果）。
func (s *ChannelMonitorService) persistCheckResults(ctx context.Context, m *ChannelMonitor, results []*CheckResult) {
	rows := make([]*ChannelMonitorHistoryRow, 0, len(results))
	for _, r := range results {
		rows = append(rows, &ChannelMonitorHistoryRow{
			MonitorID:     m.ID,
			Model:         r.Model,
			Status:        r.Status,
			LatencyMs:     r.LatencyMs,
			PingLatencyMs: r.PingLatencyMs,
			Message:       r.Message,
			CheckedAt:     r.CheckedAt,
			Quota:         r.Quota,
		})
	}
	if err := s.repo.InsertHistoryBatch(ctx, rows); err != nil {
		slog.Error("channel_monitor: insert history failed",
			"monitor_id", m.ID, "name", m.Name, "error", err)
	}
	if err := s.repo.MarkChecked(ctx, m.ID, time.Now()); err != nil {
		slog.Error("channel_monitor: mark checked failed",
			"monitor_id", m.ID, "error", err)
	}
	if m.TargetType == ChannelMonitorTargetLocal && m.GroupID != nil {
		s.invalidateAPIKeyRoutingHealth(*m.GroupID)
	}
}

// runChecksConcurrent 对 primary + extra 模型并发执行检测。
// errgroup 仅用于等待，不传播错误（每个 model 失败都已打包进 CheckResult）。
func (s *ChannelMonitorService) runChecksConcurrent(ctx context.Context, m *ChannelMonitor) []*CheckResult {
	models := append([]string{m.PrimaryModel}, m.ExtraModels...)
	results := make([]*CheckResult, len(models))

	// 外站共享一次 endpoint ping；本站为进程内请求，不生成网络 ping 指标。
	var pingMs *int
	if m.TargetType == ChannelMonitorTargetExternal {
		pingMs = pingEndpointOrigin(ctx, m.Endpoint)
	}

	// 所有模型共用同一份 CheckOptions（来自监控的快照字段）。
	opts := &CheckOptions{
		APIMode:          m.APIMode,
		ExtraHeaders:     m.ExtraHeaders,
		BodyOverrideMode: m.BodyOverrideMode,
		BodyOverride:     m.BodyOverride,
	}

	var eg errgroup.Group
	var mu sync.Mutex
	for i, model := range models {
		i, model := i, model
		eg.Go(func() error {
			var r *CheckResult
			if m.TargetType == ChannelMonitorTargetLocal {
				r = s.runLocalCheckForModel(ctx, m, model, opts)
			} else {
				r = runCheckForModel(ctx, m.Provider, m.Endpoint, m.APIKey, model, opts)
			}
			r.PingLatencyMs = pingMs
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

// ---------- 调度器协作 ----------

// SetScheduler 由 wire 在 runner 构造后注入，用于在 CRUD 时即时同步任务表。
// 通过 setter 注入避免 service ↔ runner 的依赖环。
func (s *ChannelMonitorService) SetScheduler(sched MonitorScheduler) {
	s.scheduler = sched
}

// SetQuotaFetcher 由 wire 注入配额抓取器（账号侧用量服务聚合）。
func (s *ChannelMonitorService) SetQuotaFetcher(fetcher *ChannelMonitorQuotaFetcher) {
	if s == nil {
		return
	}
	s.quotaFetcher = fetcher
}

// ListEnabledMonitors 返回所有 enabled=true 的监控（解密后），供 runner 启动时建立任务表。
func (s *ChannelMonitorService) ListEnabledMonitors(ctx context.Context) ([]*ChannelMonitor, error) {
	all, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		s.decryptInPlace(m)
	}
	return all, nil
}

// cleanupOldHistory 删除 monitorHistoryRetentionDays 天之前的明细历史记录。
// 由 RunDailyMaintenance 调用；SoftDeleteMixin 自动把 DELETE 改为 UPDATE deleted_at。
func (s *ChannelMonitorService) cleanupOldHistory(ctx context.Context) error {
	before := time.Now().UTC().AddDate(0, 0, -monitorHistoryRetentionDays)
	deleted, err := s.repo.DeleteHistoryBefore(ctx, before)
	if err != nil {
		return fmt.Errorf("delete history before %s: %w", before.Format(time.RFC3339), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: history cleanup",
			"deleted_rows", deleted, "before", before.Format(time.RFC3339))
	}
	return nil
}

// RunDailyMaintenance 每日维护任务：聚合昨天之前未聚合的明细，软删过期明细和聚合。
// 由 OpsCleanupService 的 cron 调度触发（共享 schedule 和 leader lock）。
//
// 幂等性：
//   - watermark 保证已聚合的日期不会重复处理；
//   - UpsertDailyRollupsFor 内部使用 ON CONFLICT DO UPDATE，同一日重复跑结果一致。
//
// 每一步失败都只记 slog.Warn，整体函数始终返回 nil 让后续步骤能继续跑
// （与 OpsCleanupService.runCleanupOnce 风格一致）。
func (s *ChannelMonitorService) RunDailyMaintenance(ctx context.Context) error {
	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)

	if err := s.runDailyAggregation(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "aggregate", "error", err)
	}
	if err := s.cleanupOldHistory(ctx); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_history", "error", err)
	}
	if err := s.cleanupOldRollups(ctx, today); err != nil {
		slog.Warn("channel_monitor: maintenance step failed",
			"step", "prune_rollups", "error", err)
	}
	return nil
}

// runDailyAggregation 从 watermark+1 聚合到昨天（UTC）。
// 首次跑（watermark nil）：从 today-monitorRollupRetentionDays 开始回填。
// 每次最多聚合 monitorMaintenanceMaxDaysPerRun 天，避免长事务。
func (s *ChannelMonitorService) runDailyAggregation(ctx context.Context, today time.Time) error {
	watermark, err := s.repo.LoadAggregationWatermark(ctx)
	if err != nil {
		return fmt.Errorf("load watermark: %w", err)
	}

	start := s.resolveAggregationStart(watermark, today)
	if !start.Before(today) {
		return nil // 没有需要聚合的日期
	}

	iterations := 0
	for d := start; d.Before(today); d = d.Add(24 * time.Hour) {
		if iterations >= monitorMaintenanceMaxDaysPerRun {
			slog.Info("channel_monitor: maintenance aggregation capped",
				"max_days", monitorMaintenanceMaxDaysPerRun,
				"next_resume", d.Format("2006-01-02"))
			break
		}
		affected, upErr := s.repo.UpsertDailyRollupsFor(ctx, d)
		if upErr != nil {
			return fmt.Errorf("upsert rollups for %s: %w", d.Format("2006-01-02"), upErr)
		}
		if err := s.repo.UpdateAggregationWatermark(ctx, d); err != nil {
			return fmt.Errorf("update watermark to %s: %w", d.Format("2006-01-02"), err)
		}
		slog.Info("channel_monitor: rollups upserted",
			"date", d.Format("2006-01-02"), "affected_rows", affected)
		iterations++
	}
	return nil
}

// resolveAggregationStart 计算本次聚合起点：
//   - watermark == nil：today - monitorRollupRetentionDays（首次回填最多 30 天）
//   - watermark != nil：*watermark + 1 day
func (s *ChannelMonitorService) resolveAggregationStart(watermark *time.Time, today time.Time) time.Time {
	if watermark == nil {
		return today.AddDate(0, 0, -monitorRollupRetentionDays)
	}
	return watermark.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// cleanupOldRollups 软删 bucket_date < today - monitorRollupRetentionDays 的日聚合行。
func (s *ChannelMonitorService) cleanupOldRollups(ctx context.Context, today time.Time) error {
	cutoff := today.AddDate(0, 0, -monitorRollupRetentionDays)
	deleted, err := s.repo.DeleteRollupsBefore(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete rollups before %s: %w", cutoff.Format("2006-01-02"), err)
	}
	if deleted > 0 {
		slog.Info("channel_monitor: rollups cleanup",
			"deleted_rows", deleted, "before", cutoff.Format("2006-01-02"))
	}
	return nil
}

// ---------- helpers ----------

// decryptInPlace 把 ChannelMonitor.APIKey 从密文解密为明文。
// 解密失败时把字段清空 + 设置 APIKeyDecryptFailed=true（不返回错误，避免阻断列表渲染）。
// runner / RunCheck 必须读取该标志位并拒绝执行检测。
func (s *ChannelMonitorService) decryptInPlace(m *ChannelMonitor) {
	if m == nil || m.TargetType == ChannelMonitorTargetLocal || m.APIKey == "" {
		return
	}
	plain, err := s.encryptor.Decrypt(m.APIKey)
	if err != nil {
		slog.Warn("channel_monitor: decrypt api key failed",
			"monitor_id", m.ID, "error", err)
		m.APIKey = ""
		m.APIKeyDecryptFailed = true
		return
	}
	m.APIKey = plain
}

// applyMonitorUpdate 把 update params 中非 nil 的字段应用到 existing 上。
// APIKey 字段在调用方单独处理（涉及加密）。
//
// 行数稍超过 30：这是逐字段平铺的 dispatcher，每个 if 都是 1-3 行的"非 nil 则覆盖"模式，
// 拆分反而会增加跳转噪音、影响可读性，故保留为单函数。
func applyMonitorUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams) error {
	providerChanged := false
	if p.Name != nil {
		existing.Name = strings.TrimSpace(*p.Name)
	}
	if p.Provider != nil {
		if err := validateProvider(*p.Provider); err != nil {
			return err
		}
		providerChanged = existing.Provider != *p.Provider
		existing.Provider = *p.Provider
	}
	if p.CheckMode != nil {
		existing.CheckMode = defaultCheckMode(*p.CheckMode)
	}
	// provider 与 check_mode 任一变化后统一复核组合矩阵：provider-only 更新
	// （如把 probe 监控的 provider 改成 antigravity）也不得落库非法组合，否则
	// 运行期恒 error。条件限定避免把存量非法行的 name/enabled-only 更新也判死
	// （否则连改名/停用都无法操作）。
	if p.Provider != nil || p.CheckMode != nil {
		if err := validateCheckMode(existing.Provider, defaultCheckMode(existing.CheckMode)); err != nil {
			return err
		}
	}
	if p.AccountID != nil {
		if *p.AccountID > 0 {
			id := *p.AccountID
			existing.AccountID = &id
		} else {
			existing.AccountID = nil // 0 = 清空关联
		}
	}
	if p.PrimaryModel != nil {
		primaryModel := normalizeMonitorPrimaryModel(existing.Provider, defaultCheckMode(existing.CheckMode), *p.PrimaryModel)
		if primaryModel == "" {
			return ErrChannelMonitorMissingPrimaryModel
		}
		existing.PrimaryModel = primaryModel
	} else if providerChanged && existing.Provider == MonitorProviderGrok {
		existing.PrimaryModel = MonitorDefaultGrokModel
	}
	if p.ExtraModels != nil {
		existing.ExtraModels = normalizeModels(*p.ExtraModels)
	}
	if p.GroupName != nil {
		existing.GroupName = strings.TrimSpace(*p.GroupName)
	}
	if p.Enabled != nil {
		existing.Enabled = *p.Enabled
	}
	if p.IntervalSeconds != nil {
		if err := validateInterval(*p.IntervalSeconds); err != nil {
			return err
		}
		existing.IntervalSeconds = *p.IntervalSeconds
	}
	if p.JitterSeconds != nil {
		existing.JitterSeconds = *p.JitterSeconds
	}
	if p.IntervalSeconds != nil || p.JitterSeconds != nil {
		// interval 与 jitter 任一变化都需要重新校验组合约束（interval - jitter >= 下限）。
		if err := validateJitter(existing.JitterSeconds, existing.IntervalSeconds); err != nil {
			return err
		}
	}
	return applyMonitorAdvancedUpdate(existing, p, providerChanged)
}

// applyMonitorAdvancedUpdate 处理自定义请求快照相关字段，从 applyMonitorUpdate 拆出避免过长。
func applyMonitorAdvancedUpdate(existing *ChannelMonitor, p ChannelMonitorUpdateParams, providerChanged bool) error {
	if p.ClearTemplate {
		existing.TemplateID = nil
	} else if p.TemplateID != nil {
		id := *p.TemplateID
		existing.TemplateID = &id
	}
	if p.ExtraHeaders != nil {
		if err := validateExtraHeaders(*p.ExtraHeaders); err != nil {
			return err
		}
		existing.ExtraHeaders = emptyHeadersIfNil(*p.ExtraHeaders)
	}
	newAPIMode := defaultAPIModeForProvider(existing.Provider, existing.APIMode)
	if p.APIMode != nil {
		newAPIMode = defaultAPIMode(*p.APIMode)
	}
	if err := validateAPIMode(existing.Provider, newAPIMode); err != nil {
		return err
	}
	// BodyOverrideMode / BodyOverride 联合校验，和模板一致。
	newMode := existing.BodyOverrideMode
	newBody := existing.BodyOverride
	if p.BodyOverrideMode != nil {
		newMode = *p.BodyOverrideMode
	}
	if p.BodyOverride != nil {
		newBody = *p.BodyOverride
	}
	if providerChanged || p.APIMode != nil || p.BodyOverrideMode != nil || p.BodyOverride != nil {
		if err := validateBodyModeForProtocol(existing.Provider, newAPIMode, newMode, newBody); err != nil {
			return err
		}
		existing.BodyOverrideMode = defaultBodyMode(newMode)
		existing.BodyOverride = newBody
	}
	existing.APIMode = newAPIMode
	return nil
}
