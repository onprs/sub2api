package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/dgraph-io/ristretto"
	"golang.org/x/sync/singleflight"
)

// MaxExpiresAt is the maximum allowed expiration date (year 2099)
// This prevents time.Time JSON serialization errors (RFC 3339 requires year <= 9999)
var MaxExpiresAt = time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

// MaxValidityDays is the maximum allowed validity days for subscriptions (100 years)
const MaxValidityDays = 36500

var (
	ErrSubscriptionNotFound        = infraerrors.NotFound("SUBSCRIPTION_NOT_FOUND", "subscription not found")
	ErrSubscriptionExpired         = infraerrors.Forbidden("SUBSCRIPTION_EXPIRED", "subscription has expired")
	ErrSubscriptionSuspended       = infraerrors.Forbidden("SUBSCRIPTION_SUSPENDED", "subscription is suspended")
	ErrSubscriptionAlreadyExists   = infraerrors.Conflict("SUBSCRIPTION_ALREADY_EXISTS", "subscription already exists for this user and group")
	ErrSubscriptionAssignConflict  = infraerrors.Conflict("SUBSCRIPTION_ASSIGN_CONFLICT", "subscription exists but request conflicts with existing assignment semantics")
	ErrSubscriptionNotRevoked      = infraerrors.Conflict("SUBSCRIPTION_NOT_REVOKED", "subscription is not revoked")
	ErrSubscriptionRestoreConflict = infraerrors.Conflict("SUBSCRIPTION_RESTORE_CONFLICT", "subscription already exists for this user and group")
	ErrGroupNotSubscriptionType    = infraerrors.BadRequest("GROUP_NOT_SUBSCRIPTION_TYPE", "group is not a subscription type")
	ErrInvalidInput                = infraerrors.BadRequest("INVALID_INPUT", "at least one quota window must be selected")
	ErrDailyLimitExceeded          = infraerrors.TooManyRequests("DAILY_LIMIT_EXCEEDED", "daily usage limit exceeded")
	ErrWeeklyLimitExceeded         = infraerrors.TooManyRequests("WEEKLY_LIMIT_EXCEEDED", "weekly usage limit exceeded")
	ErrMonthlyLimitExceeded        = infraerrors.TooManyRequests("MONTHLY_LIMIT_EXCEEDED", "monthly usage limit exceeded")
	ErrFiveHourLimitExceeded       = infraerrors.TooManyRequests("FIVE_HOUR_LIMIT_EXCEEDED", "5h usage limit exceeded")
	ErrSevenDayLimitExceeded       = infraerrors.TooManyRequests("SEVEN_DAY_LIMIT_EXCEEDED", "7d usage limit exceeded")
	ErrThirtyDayLimitExceeded      = infraerrors.TooManyRequests("THIRTY_DAY_LIMIT_EXCEEDED", "30d usage limit exceeded")
	ErrSubscriptionNilInput        = infraerrors.BadRequest("SUBSCRIPTION_NIL_INPUT", "subscription input cannot be nil")
	ErrAdjustWouldExpire           = infraerrors.BadRequest("ADJUST_WOULD_EXPIRE", "adjustment would result in expired subscription (remaining days must be > 0)")
)

// SubscriptionService 订阅服务
type SubscriptionService struct {
	groupRepo           GroupRepository
	userSubRepo         UserSubscriptionRepository
	billingCacheService *BillingCacheService
	entClient           *dbent.Client

	// L1 缓存：加速中间件热路径的订阅查询
	subCacheL1     *ristretto.Cache
	subCacheGroup  singleflight.Group
	subCacheTTL    time.Duration
	subCacheJitter int // 抖动百分比

	maintenanceQueue *SubscriptionMaintenanceQueue
	now              func() time.Time
}

func (s *SubscriptionService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// NewSubscriptionService 创建订阅服务
func NewSubscriptionService(groupRepo GroupRepository, userSubRepo UserSubscriptionRepository, billingCacheService *BillingCacheService, entClient *dbent.Client, cfg *config.Config) *SubscriptionService {
	svc := &SubscriptionService{
		groupRepo:           groupRepo,
		userSubRepo:         userSubRepo,
		billingCacheService: billingCacheService,
		entClient:           entClient,
		now:                 time.Now,
	}
	svc.initSubCache(cfg)
	svc.initMaintenanceQueue(cfg)
	svc.StartSubCacheInvalidationSubscriber(context.Background())
	return svc
}

func (s *SubscriptionService) initMaintenanceQueue(cfg *config.Config) {
	if cfg == nil {
		return
	}
	mc := cfg.SubscriptionMaintenance
	if mc.WorkerCount <= 0 || mc.QueueSize <= 0 {
		return
	}
	s.maintenanceQueue = NewSubscriptionMaintenanceQueue(mc.WorkerCount, mc.QueueSize)
}

// Stop stops the maintenance worker pool.
func (s *SubscriptionService) Stop() {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		s.maintenanceQueue.Stop()
	}
}

// initSubCache 初始化订阅 L1 缓存
func (s *SubscriptionService) initSubCache(cfg *config.Config) {
	if cfg == nil {
		return
	}
	sc := cfg.SubscriptionCache
	if sc.L1Size <= 0 || sc.L1TTLSeconds <= 0 {
		return
	}
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(sc.L1Size) * 10,
		MaxCost:     int64(sc.L1Size),
		BufferItems: 64,
	})
	if err != nil {
		log.Printf("Warning: failed to init subscription L1 cache: %v", err)
		return
	}
	s.subCacheL1 = cache
	s.subCacheTTL = time.Duration(sc.L1TTLSeconds) * time.Second
	s.subCacheJitter = sc.JitterPercent
}

// subCacheKey 生成订阅缓存 key（热路径，避免 fmt.Sprintf 开销）
func subCacheKey(userID, groupID int64) string {
	return "sub:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(groupID, 10)
}

// jitteredTTL 为 TTL 添加抖动，避免集中过期
func (s *SubscriptionService) jitteredTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 || s.subCacheJitter <= 0 {
		return ttl
	}
	pct := s.subCacheJitter
	if pct > 100 {
		pct = 100
	}
	delta := float64(pct) / 100
	factor := 1 - delta + rand.Float64()*(2*delta)
	if factor <= 0 {
		return ttl
	}
	return time.Duration(float64(ttl) * factor)
}

// InvalidateSubCache 失效指定用户+分组的订阅 L1 缓存
func (s *SubscriptionService) InvalidateSubCache(userID, groupID int64) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(subCacheKey(userID, groupID))
}

// InvalidateSubCacheSync 失效订阅 L1 缓存并等待 Ristretto 删除操作生效。
func (s *SubscriptionService) InvalidateSubCacheSync(userID, groupID int64) {
	s.invalidateSubCacheKeySync(subCacheKey(userID, groupID))
}

func (s *SubscriptionService) invalidateSubCacheKeySync(key string) {
	if s.subCacheL1 == nil {
		return
	}
	s.subCacheL1.Del(key)
	s.subCacheL1.Wait()
}

// StartSubCacheInvalidationSubscriber 启动跨实例订阅 L1 缓存失效订阅。
func (s *SubscriptionService) StartSubCacheInvalidationSubscriber(ctx context.Context) {
	if s.billingCacheService == nil || s.subCacheL1 == nil {
		return
	}
	if err := s.billingCacheService.SubscribeSubscriptionCacheInvalidation(ctx, func(cacheKey string) {
		s.invalidateSubCacheKeySync(cacheKey)
	}); err != nil {
		log.Printf("Warning: failed to start subscription cache invalidation subscriber: %v", err)
	}
}

func (s *SubscriptionService) invalidateSubscriptionCaches(userID, groupID int64) error {
	s.InvalidateSubCacheSync(userID, groupID)
	if s.billingCacheService == nil {
		return nil
	}

	cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID); err != nil {
		return fmt.Errorf("invalidate billing subscription cache: %w", err)
	}
	if err := s.billingCacheService.PublishSubscriptionCacheInvalidation(cacheCtx, subCacheKey(userID, groupID)); err != nil {
		return fmt.Errorf("publish subscription cache invalidation: %w", err)
	}
	return nil
}

// AssignSubscriptionInput 分配订阅输入
type AssignSubscriptionInput struct {
	UserID                  int64
	GroupID                 int64
	PlanID                  *int64
	ValidityDays            int
	AssignedBy              int64
	Notes                   string
	FiveHourLimitUSD        *float64
	SevenDayLimitUSD        *float64
	ThirtyDayLimitUSD       *float64
	HasRollingQuotaSnapshot bool
}

// AssignSubscriptionPlanInput assigns a subscription from an existing plan.
type AssignSubscriptionPlanInput struct {
	UserID             int64
	SubscriptionPlanID int64
	AssignedBy         int64
	Notes              string
}

// AssignSubscription 分配订阅给用户（不允许重复分配）
func (s *SubscriptionService) AssignSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	sub, _, err := s.assignSubscriptionWithReuse(ctx, input)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// AssignSubscriptionPlan assigns a subscription using a plan snapshot.
func (s *SubscriptionService) AssignSubscriptionPlan(ctx context.Context, input *AssignSubscriptionPlanInput) (*UserSubscription, error) {
	if input == nil {
		return nil, ErrSubscriptionNilInput
	}
	assignInput, err := s.buildAssignInputFromPlan(ctx, input.SubscriptionPlanID, input.UserID, input.AssignedBy, input.Notes)
	if err != nil {
		return nil, err
	}
	return s.AssignSubscription(ctx, assignInput)
}

// AssignOrExtendSubscription 分配或续期订阅（用于兑换码等场景）
// 如果用户已有同分组的订阅：
//   - 未过期：从当前过期时间累加天数
//   - 已过期：从当前时间开始计算新的过期时间，并激活订阅
//
// 如果没有订阅：创建新订阅
func (s *SubscriptionService) AssignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	return s.assignOrExtendSubscription(ctx, input, false)
}

func (s *SubscriptionService) assignOrExtendSubscription(ctx context.Context, input *AssignSubscriptionInput, deferCacheInvalidation bool) (*UserSubscription, bool, error) {
	// 检查分组是否存在且为订阅类型
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, false, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsSubscriptionType() {
		return nil, false, ErrGroupNotSubscriptionType
	}

	// 查询是否已有同一套餐身份的订阅。PlanID 为 nil 时表示 legacy/manual 分组订阅。
	existingSub, err := s.userSubRepo.GetByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
	if err != nil {
		// 不存在记录是正常情况，其他错误需要返回
		existingSub = nil
	}

	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	// 已有订阅，执行续期（在事务中完成所有更新）。暂停只能由明确的管理操作解除。
	if existingSub != nil {
		sub, err := s.updateExistingSubscriptionTerm(ctx, existingSub.ID, input, validityDays, false)
		if err != nil {
			return nil, errors.Is(err, ErrSubscriptionSuspended), err
		}

		// 失效订阅缓存
		s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, deferCacheInvalidation)

		return sub, true, nil
	}

	// 没有订阅，创建新订阅
	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		if errors.Is(err, ErrSubscriptionAlreadyExists) {
			sub, err := s.renewSubscriptionAfterCreateConflict(ctx, input, validityDays)
			if err != nil {
				return nil, false, err
			}
			_ = s.invalidateSubscriptionCaches(input.UserID, input.GroupID)
			return sub, true, nil
		}
		return nil, false, err
	}

	// 失效订阅缓存
	s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, deferCacheInvalidation)

	return sub, false, nil // false 表示是新建
}

func (s *SubscriptionService) maybeInvalidateAssignmentCaches(userID, groupID int64, deferred bool) {
	// Payment fulfillment owns an outer transaction and performs a synchronous
	// invalidation after commit. Invalidating inside that transaction can reload
	// the pre-commit subscription into cache.
	if deferred {
		return
	}

	s.InvalidateSubCache(userID, groupID)
	if s.billingCacheService != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}
}

func (s *SubscriptionService) renewSubscriptionAfterCreateConflict(ctx context.Context, input *AssignSubscriptionInput, validityDays int) (*UserSubscription, error) {
	existingSub, err := s.userSubRepo.GetByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
	if err != nil {
		return nil, fmt.Errorf("load subscription after create conflict: %w", err)
	}
	return s.updateExistingSubscriptionTerm(ctx, existingSub.ID, input, validityDays, false)
}

func (s *SubscriptionService) updateExistingSubscriptionTerm(
	ctx context.Context,
	subscriptionID int64,
	input *AssignSubscriptionInput,
	validityDays int,
	assignmentSemantics bool,
) (*UserSubscription, error) {
	var renewed *UserSubscription
	err := s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		var err error
		startsAt := s.currentTime()
		renewed, err = s.userSubRepo.RenewTerm(txCtx, &RenewSubscriptionTermInput{
			SubscriptionID:          subscriptionID,
			ValidityDays:            validityDays,
			Now:                     startsAt,
			MaxExpiresAt:            MaxExpiresAt,
			FiveHourLimitUSD:        input.FiveHourLimitUSD,
			SevenDayLimitUSD:        input.SevenDayLimitUSD,
			ThirtyDayLimitUSD:       input.ThirtyDayLimitUSD,
			HasRollingQuotaSnapshot: input.HasRollingQuotaSnapshot,
			Notes:                   input.Notes,
			SkipDuplicateNotes:      assignmentSemantics,
		})
		if errors.Is(err, ErrSubscriptionSuspended) && assignmentSemantics {
			renewed, err = s.userSubRepo.GetByID(txCtx, subscriptionID)
		}
		if err != nil {
			return fmt.Errorf("renew subscription term: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return renewed, nil
}

func (s *SubscriptionService) withSubscriptionUpdateTx(ctx context.Context, fn func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	if s.entClient == nil {
		return fn(ctx)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	txCtx := dbent.NewTxContext(ctx, tx)

	if err := fn(txCtx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func applyRollingQuotaSnapshot(sub *UserSubscription, input *AssignSubscriptionInput) {
	if sub == nil || input == nil {
		return
	}
	sub.FiveHourLimitUSD = input.FiveHourLimitUSD
	sub.SevenDayLimitUSD = input.SevenDayLimitUSD
	sub.ThirtyDayLimitUSD = input.ThirtyDayLimitUSD
}

func appendSubscriptionNotes(existingNotes, newNotes string) string {
	if newNotes == "" {
		return existingNotes
	}
	if existingNotes == "" {
		return newNotes
	}
	return existingNotes + "\n" + newNotes
}

// createSubscription 创建新订阅（内部方法）
func (s *SubscriptionService) createSubscription(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, error) {
	validityDays := input.ValidityDays
	if validityDays <= 0 {
		validityDays = 30
	}
	if validityDays > MaxValidityDays {
		validityDays = MaxValidityDays
	}

	now := time.Now()
	expiresAt := now.AddDate(0, 0, validityDays)
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}

	sub := &UserSubscription{
		UserID:     input.UserID,
		GroupID:    input.GroupID,
		PlanID:     input.PlanID,
		StartsAt:   now,
		ExpiresAt:  expiresAt,
		Status:     SubscriptionStatusActive,
		AssignedAt: now,
		Notes:      input.Notes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	applyRollingQuotaSnapshot(sub, input)
	// 只有当 AssignedBy > 0 时才设置（0 表示系统分配，如兑换码）
	if input.AssignedBy > 0 {
		sub.AssignedBy = &input.AssignedBy
	}

	if err := s.userSubRepo.Create(ctx, sub); err != nil {
		return nil, err
	}

	// 重新获取完整订阅信息（包含关联）
	return s.userSubRepo.GetByID(ctx, sub.ID)
}

// BulkAssignSubscriptionInput 批量分配订阅输入
type BulkAssignSubscriptionInput struct {
	UserIDs      []int64
	GroupID      int64
	ValidityDays int
	AssignedBy   int64
	Notes        string
}

// BulkAssignSubscriptionPlanInput assigns a subscription plan to multiple users.
type BulkAssignSubscriptionPlanInput struct {
	UserIDs            []int64
	SubscriptionPlanID int64
	AssignedBy         int64
	Notes              string
}

// BulkAssignResult 批量分配结果
type BulkAssignResult struct {
	SuccessCount  int
	CreatedCount  int
	ReusedCount   int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Statuses      map[int64]string
}

// BulkAssignSubscription 批量分配订阅
func (s *SubscriptionService) BulkAssignSubscription(ctx context.Context, input *BulkAssignSubscriptionInput) (*BulkAssignResult, error) {
	result := &BulkAssignResult{
		Subscriptions: make([]UserSubscription, 0),
		Errors:        make([]string, 0),
		Statuses:      make(map[int64]string),
	}

	for _, userID := range input.UserIDs {
		sub, reused, err := s.assignSubscriptionWithReuse(ctx, &AssignSubscriptionInput{
			UserID:       userID,
			GroupID:      input.GroupID,
			ValidityDays: input.ValidityDays,
			AssignedBy:   input.AssignedBy,
			Notes:        input.Notes,
		})
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			result.Statuses[userID] = "failed"
		} else {
			result.SuccessCount++
			result.Subscriptions = append(result.Subscriptions, *sub)
			if reused {
				result.ReusedCount++
				result.Statuses[userID] = "reused"
			} else {
				result.CreatedCount++
				result.Statuses[userID] = "created"
			}
		}
	}

	return result, nil
}

// BulkAssignSubscriptionPlan assigns a plan snapshot to multiple users.
func (s *SubscriptionService) BulkAssignSubscriptionPlan(ctx context.Context, input *BulkAssignSubscriptionPlanInput) (*BulkAssignResult, error) {
	if input == nil {
		return nil, ErrSubscriptionNilInput
	}

	result := &BulkAssignResult{
		Subscriptions: make([]UserSubscription, 0),
		Errors:        make([]string, 0),
		Statuses:      make(map[int64]string),
	}

	planInput, err := s.buildAssignInputFromPlan(ctx, input.SubscriptionPlanID, 0, input.AssignedBy, input.Notes)
	if err != nil {
		return nil, err
	}

	for _, userID := range input.UserIDs {
		assignInput := *planInput
		assignInput.UserID = userID
		sub, reused, err := s.assignSubscriptionWithReuse(ctx, &assignInput)
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("user %d: %v", userID, err))
			result.Statuses[userID] = "failed"
			continue
		}
		result.SuccessCount++
		result.Subscriptions = append(result.Subscriptions, *sub)
		if reused {
			result.ReusedCount++
			result.Statuses[userID] = "reused"
		} else {
			result.CreatedCount++
			result.Statuses[userID] = "created"
		}
	}

	return result, nil
}

func (s *SubscriptionService) buildAssignInputFromPlan(ctx context.Context, planID, userID, assignedBy int64, notes string) (*AssignSubscriptionInput, error) {
	if planID <= 0 {
		return nil, infraerrors.BadRequest("SUBSCRIPTION_PLAN_REQUIRED", "subscription_plan_id is required")
	}
	if s.entClient == nil {
		return nil, errors.New("ent client is required for subscription plan assignment")
	}
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
		}
		return nil, fmt.Errorf("subscription plan not found: %w", err)
	}
	group, err := s.groupRepo.GetByID(ctx, plan.GroupID)
	if err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsSubscriptionType() {
		return nil, ErrGroupNotSubscriptionType
	}

	snapshotPlanID := plan.ID
	return &AssignSubscriptionInput{
		UserID:                  userID,
		GroupID:                 plan.GroupID,
		PlanID:                  &snapshotPlanID,
		ValidityDays:            psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit),
		AssignedBy:              assignedBy,
		Notes:                   notes,
		FiveHourLimitUSD:        plan.FiveHourLimitUsd,
		SevenDayLimitUSD:        plan.SevenDayLimitUsd,
		ThirtyDayLimitUSD:       plan.ThirtyDayLimitUsd,
		HasRollingQuotaSnapshot: true,
	}, nil
}

func (s *SubscriptionService) assignSubscriptionWithReuse(ctx context.Context, input *AssignSubscriptionInput) (*UserSubscription, bool, error) {
	// 检查分组是否存在且为订阅类型
	group, err := s.groupRepo.GetByID(ctx, input.GroupID)
	if err != nil {
		return nil, false, fmt.Errorf("group not found: %w", err)
	}
	if !group.IsSubscriptionType() {
		return nil, false, ErrGroupNotSubscriptionType
	}

	// 检查是否已存在同一套餐身份的订阅；若已存在，则按幂等成功返回现有订阅。
	exists, err := s.userSubRepo.ExistsByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		sub, getErr := s.userSubRepo.GetByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
		if getErr != nil {
			return nil, false, getErr
		}
		now := time.Now()
		if sub.Status == SubscriptionStatusExpired ||
			(sub.Status != SubscriptionStatusSuspended && !sub.ExpiresAt.After(now)) {
			validityDays := normalizeAssignValidityDays(input.ValidityDays)
			renewed, renewErr := s.updateExistingSubscriptionTerm(ctx, sub.ID, input, validityDays, true)
			if renewErr != nil {
				return nil, false, renewErr
			}
			s.maybeInvalidateAssignmentCaches(input.UserID, input.GroupID, false)
			return renewed, true, nil
		}
		if conflictReason, conflict := detectAssignSemanticConflict(sub, input); conflict {
			return nil, false, ErrSubscriptionAssignConflict.WithMetadata(map[string]string{
				"conflict_reason": conflictReason,
			})
		}
		return sub, true, nil
	}

	sub, err := s.createSubscription(ctx, input)
	if err != nil {
		return nil, false, err
	}

	// 失效订阅缓存
	s.InvalidateSubCache(input.UserID, input.GroupID)
	if s.billingCacheService != nil {
		userID, groupID := input.UserID, input.GroupID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}

	return sub, false, nil
}

func detectAssignSemanticConflict(existing *UserSubscription, input *AssignSubscriptionInput) (string, bool) {
	if existing == nil || input == nil {
		return "", false
	}

	normalizedDays := normalizeAssignValidityDays(input.ValidityDays)
	if !existing.StartsAt.IsZero() {
		expectedExpiresAt := existing.StartsAt.AddDate(0, 0, normalizedDays)
		if expectedExpiresAt.After(MaxExpiresAt) {
			expectedExpiresAt = MaxExpiresAt
		}
		if !existing.ExpiresAt.Equal(expectedExpiresAt) {
			return "validity_days_mismatch", true
		}
	}

	existingNotes := strings.TrimSpace(existing.Notes)
	inputNotes := strings.TrimSpace(input.Notes)
	if existingNotes != inputNotes {
		return "notes_mismatch", true
	}

	if input.HasRollingQuotaSnapshot {
		if !floatPtrEqual(existing.FiveHourLimitUSD, input.FiveHourLimitUSD) {
			return "five_hour_limit_mismatch", true
		}
		if !floatPtrEqual(existing.SevenDayLimitUSD, input.SevenDayLimitUSD) {
			return "seven_day_limit_mismatch", true
		}
		if !floatPtrEqual(existing.ThirtyDayLimitUSD, input.ThirtyDayLimitUSD) {
			return "thirty_day_limit_mismatch", true
		}
	}

	return "", false
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func normalizeAssignValidityDays(days int) int {
	if days <= 0 {
		days = 30
	}
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	return days
}

// RevokeSubscription 撤销订阅
func (s *SubscriptionService) RevokeSubscription(ctx context.Context, subscriptionID int64) error {
	// 先获取订阅信息用于失效缓存
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return err
	}

	if err := s.userSubRepo.Delete(ctx, subscriptionID); err != nil {
		return err
	}

	if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		return err
	}

	return nil
}

// RestoreSubscription 恢复已撤销订阅
func (s *SubscriptionService) RestoreSubscription(ctx context.Context, subscriptionID int64) (*UserSubscription, error) {
	sub, err := s.userSubRepo.GetByIDIncludeDeleted(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if sub.DeletedAt == nil {
		return nil, ErrSubscriptionNotRevoked
	}

	exists, err := s.userSubRepo.ExistsActiveByUserIDAndGroupID(ctx, sub.UserID, sub.GroupID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrSubscriptionRestoreConflict
	}

	restoredStatus := sub.Status
	now := time.Now()
	if restoredStatus == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
		restoredStatus = SubscriptionStatusExpired
	}

	restored, err := s.userSubRepo.Restore(ctx, subscriptionID, restoredStatus)
	if err != nil {
		return nil, err
	}

	if err := s.invalidateSubscriptionCaches(restored.UserID, restored.GroupID); err != nil {
		return nil, err
	}
	return restored, nil
}

// ExtendSubscription 调整订阅时长（正数延长，负数缩短）
func (s *SubscriptionService) ExtendSubscription(ctx context.Context, subscriptionID int64, days int) (*UserSubscription, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	// 限制调整天数范围
	if days > MaxValidityDays {
		days = MaxValidityDays
	}
	if days < -MaxValidityDays {
		days = -MaxValidityDays
	}

	now := time.Now()
	isExpired := !sub.ExpiresAt.After(now)

	// 如果订阅已过期，不允许负向调整
	if isExpired && days < 0 {
		return nil, infraerrors.BadRequest("CANNOT_SHORTEN_EXPIRED", "cannot shorten an expired subscription")
	}

	// 计算新的过期时间
	var newExpiresAt time.Time
	if isExpired {
		// 已过期：从当前时间开始增加天数
		newExpiresAt = now.AddDate(0, 0, days)
	} else {
		// 未过期：从原过期时间增加/减少天数
		newExpiresAt = sub.ExpiresAt.AddDate(0, 0, days)
	}

	if newExpiresAt.After(MaxExpiresAt) {
		newExpiresAt = MaxExpiresAt
	}

	// 检查新的过期时间必须大于当前时间
	if !newExpiresAt.After(now) {
		return nil, ErrAdjustWouldExpire
	}

	if err := s.userSubRepo.ExtendExpiry(ctx, subscriptionID, newExpiresAt); err != nil {
		return nil, err
	}

	// 如果订阅已过期，恢复为active状态
	if sub.Status == SubscriptionStatusExpired {
		if err := s.userSubRepo.UpdateStatus(ctx, subscriptionID, SubscriptionStatusActive); err != nil {
			return nil, err
		}
	}

	// 失效订阅缓存
	s.InvalidateSubCache(sub.UserID, sub.GroupID)
	if s.billingCacheService != nil {
		userID, groupID := sub.UserID, sub.GroupID
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.billingCacheService.InvalidateSubscription(cacheCtx, userID, groupID)
		}()
	}

	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

// GetByID 根据ID获取订阅
func (s *SubscriptionService) GetByID(ctx context.Context, id int64) (*UserSubscription, error) {
	return s.userSubRepo.GetByID(ctx, id)
}

func (s *SubscriptionService) GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error) {
	return s.userSubRepo.GetByUserIDGroupIDAndPlanID(ctx, userID, groupID, planID)
}

// GetActiveSubscription 获取用户对特定分组的有效订阅
// 使用 L1 缓存 + singleflight 加速中间件热路径。
// 返回缓存对象的浅拷贝，调用方可安全修改字段而不会污染缓存或触发 data race。
func (s *SubscriptionService) GetActiveSubscription(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	key := subCacheKey(userID, groupID)

	// L1 缓存命中：返回浅拷贝
	if s.subCacheL1 != nil {
		if v, ok := s.subCacheL1.Get(key); ok {
			if sub, ok := v.(*UserSubscription); ok {
				cp := *sub
				return &cp, nil
			}
		}
	}

	// singleflight 防止并发击穿
	value, err, _ := s.subCacheGroup.Do(key, func() (any, error) {
		sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
		if err != nil {
			return nil, err // 直接透传 repo 已翻译的错误（NotFound → ErrSubscriptionNotFound，其他错误原样返回）
		}
		// 写入 L1 缓存
		if s.subCacheL1 != nil {
			_ = s.subCacheL1.SetWithTTL(key, sub, 1, s.jitteredTTL(s.subCacheTTL))
		}
		return sub, nil
	})
	if err != nil {
		return nil, err
	}
	// singleflight 返回的也是缓存指针，需要浅拷贝
	sub, ok := value.(*UserSubscription)
	if !ok || sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

// GetUsableActiveSubscription selects the active subscription that should be
// used by runtime preflight checks. When a user has multiple plan-backed
// subscriptions for the same group, the earliest active subscription may have
// already exhausted a rolling quota while a later package is still usable.
//
// The selection mirrors the billing repository's policy as closely as a
// preflight can without knowing the final request cost: choose the earliest
// active candidate that currently passes subscription validation.
func (s *SubscriptionService) GetUsableActiveSubscription(ctx context.Context, userID, groupID int64, group *Group) (*UserSubscription, bool, error) {
	subs, err := s.userSubRepo.ListActiveByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil, false, err
	}
	if len(subs) == 0 {
		return nil, false, ErrSubscriptionNotFound
	}
	sort.SliceStable(subs, func(i, j int) bool {
		if !subs[i].ExpiresAt.Equal(subs[j].ExpiresAt) {
			return subs[i].ExpiresAt.Before(subs[j].ExpiresAt)
		}
		return subs[i].ID < subs[j].ID
	})

	var firstErr error
	var needsAnyMaintenance bool
	for i := range subs {
		candidate := subs[i]
		needsMaintenance, validateErr := s.ValidateAndCheckLimits(&candidate, group)
		if validateErr == nil {
			return &candidate, needsMaintenance, nil
		}
		if firstErr == nil {
			firstErr = validateErr
		}
		if needsMaintenance {
			needsAnyMaintenance = true
		}
	}
	if firstErr == nil {
		firstErr = ErrSubscriptionNotFound
	}
	return nil, needsAnyMaintenance, firstErr
}

// ListUserSubscriptions 获取用户的所有订阅
func (s *SubscriptionService) ListUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, nil
}

// ListActiveUserSubscriptions 获取用户的所有有效订阅
func (s *SubscriptionService) ListActiveUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	normalizeExpiredWindows(subs)
	return subs, nil
}

// ListGroupSubscriptions 获取分组的所有订阅
func (s *SubscriptionService) ListGroupSubscriptions(ctx context.Context, groupID int64, page, pageSize int) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.ListByGroupID(ctx, groupID, params)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// List 获取所有订阅（分页，支持筛选和排序）
func (s *SubscriptionService) List(ctx context.Context, page, pageSize int, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	subs, pag, err := s.userSubRepo.List(ctx, params, userID, groupID, status, platform, sortBy, sortOrder)
	if err != nil {
		return nil, nil, err
	}
	normalizeExpiredWindows(subs)
	normalizeSubscriptionStatus(subs)
	return subs, pag, nil
}

// normalizeExpiredWindows 将已过期的订阅滚动限额窗口数据清零（仅影响返回数据，不影响数据库）
// legacy 日/周/月窗口保持原有展示行为，由写侧重置逻辑维护。
func normalizeExpiredWindows(subs []UserSubscription) {
	normalizeExpiredWindowsAt(subs, time.Now())
}

func normalizeExpiredWindowsAt(subs []UserSubscription, now time.Time) {
	for i := range subs {
		sub := &subs[i]
		if sub.canAutomaticallyResetDailyAt(now) {
			sub.DailyWindowStart = nil
			sub.DailyUsageUSD = 0
		}
		if sub.canAutomaticallyResetWeeklyAt(now) {
			sub.WeeklyWindowStart = nil
			sub.WeeklyUsageUSD = 0
		}
		if sub.canAutomaticallyResetMonthlyAt(now) {
			sub.MonthlyWindowStart = nil
			sub.MonthlyUsageUSD = 0
		}
		if sub.NeedsFiveHourResetAt(now) {
			sub.FiveHourWindowStart = nil
			sub.FiveHourUsageUSD = 0
		}
		if sub.NeedsSevenDayResetAt(now) {
			sub.SevenDayWindowStart = nil
			sub.SevenDayUsageUSD = 0
		}
		if sub.NeedsThirtyDayResetAt(now) {
			sub.ThirtyDayWindowStart = nil
			sub.ThirtyDayUsageUSD = 0
		}
	}
}

// normalizeSubscriptionStatus 根据实际过期时间修正状态（仅影响返回数据，不影响数据库）
// 这确保前端显示正确的状态，即使定时任务尚未更新数据库
func normalizeSubscriptionStatus(subs []UserSubscription) {
	now := time.Now()
	for i := range subs {
		sub := &subs[i]
		if sub.Status == SubscriptionStatusActive && !sub.ExpiresAt.After(now) {
			sub.Status = SubscriptionStatusExpired
		}
	}
}

// startOfDay 返回给定时间所在日期的零点（保持原时区）
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// CheckAndActivateWindow 检查并激活窗口（首次使用时）
func (s *SubscriptionService) CheckAndActivateWindow(ctx context.Context, sub *UserSubscription) error {
	return s.checkAndActivateWindowAt(ctx, sub, s.currentTime())
}

func (s *SubscriptionService) checkAndActivateWindowAt(ctx context.Context, sub *UserSubscription, now time.Time) error {
	if sub.IsWindowActivated() {
		return nil
	}

	// 多日订阅日窗口锚定当天 0 点；一日一次性额度覆盖完整订阅周期。
	// 周/月窗口锚定首次使用时刻，避免最后一个不完整周期重复发放额度。
	dailyStart := timezone.StartOfDay(now)
	if sub.HasOneTimeDailyQuota() {
		dailyStart = sub.StartsAt
	}
	return s.userSubRepo.ActivateWindows(ctx, sub.ID, dailyStart, now)
}

type BulkResetSubscriptionQuotaInput struct {
	SubscriptionIDs []int64
	AllFiltered     bool
	Filter          BulkResetSubscriptionQuotaFilter
	ResetDaily      bool
	ResetWeekly     bool
	ResetMonthly    bool
	ResetFiveHour   bool
	ResetSevenDay   bool
	ResetThirtyDay  bool
}

type BulkResetSubscriptionQuotaFilter struct {
	UserID    *int64
	GroupID   *int64
	Status    string
	Platform  string
	SortBy    string
	SortOrder string
}

// BulkResetQuotaResult reports the outcome of resetting subscription quota windows.
type BulkResetQuotaResult struct {
	SuccessCount  int
	FailedCount   int
	Subscriptions []UserSubscription
	Errors        []string
	Warnings      []string
	Statuses      map[int64]string
}

// AdminResetQuota manually resets selected legacy and rolling usage windows.
// Daily windows stay aligned to midnight; weekly, monthly, and rolling windows
// restart from the reset instant.
func (s *SubscriptionService) AdminResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly, resetFiveHour, resetSevenDay, resetThirtyDay bool) (*UserSubscription, error) {
	if !resetDaily && !resetWeekly && !resetMonthly && !resetFiveHour && !resetSevenDay && !resetThirtyDay {
		return nil, ErrInvalidInput
	}
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	now := s.currentTime()
	if err := s.resetQuotaWindows(ctx, sub.ID, resetDaily, resetWeekly, resetMonthly, resetFiveHour, resetSevenDay, resetThirtyDay, timezone.StartOfDay(now), now, now); err != nil {
		return nil, err
	}
	if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
		return nil, err
	}
	// Return the refreshed subscription from DB
	return s.userSubRepo.GetByID(ctx, subscriptionID)
}

func (s *SubscriptionService) BulkAdminResetQuota(ctx context.Context, input BulkResetSubscriptionQuotaInput) (*BulkResetQuotaResult, error) {
	if !input.ResetDaily && !input.ResetWeekly && !input.ResetMonthly && !input.ResetFiveHour && !input.ResetSevenDay && !input.ResetThirtyDay {
		return nil, ErrInvalidInput
	}

	ids, err := s.resolveBulkResetSubscriptionIDs(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &BulkResetQuotaResult{
			Subscriptions: []UserSubscription{},
			Errors:        []string{},
			Warnings:      []string{},
			Statuses:      map[int64]string{},
		}, nil
	}

	result := &BulkResetQuotaResult{
		Subscriptions: make([]UserSubscription, 0, len(ids)),
		Errors:        make([]string, 0),
		Warnings:      make([]string, 0),
		Statuses:      make(map[int64]string, len(ids)),
	}
	now := s.currentTime()
	dailyWindowStart := timezone.StartOfDay(now)
	for _, id := range ids {
		sub, err := s.userSubRepo.GetByID(ctx, id)
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("subscription %d: %v", id, err))
			result.Statuses[id] = "failed"
			continue
		}
		if err := s.resetQuotaWindows(ctx, sub.ID, input.ResetDaily, input.ResetWeekly, input.ResetMonthly, input.ResetFiveHour, input.ResetSevenDay, input.ResetThirtyDay, dailyWindowStart, now, now); err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("subscription %d: %v", id, err))
			result.Statuses[id] = "failed"
			continue
		}
		cacheWarn := ""
		if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
			// The database reset has already committed. Report the subscription as
			// successfully reset and surface cache invalidation as a warning so the
			// API result does not claim the quota reset failed when only cache fanout
			// needs attention.
			cacheWarn = fmt.Sprintf("subscription %d reset but cache invalidation failed: %v", id, err)
			result.Warnings = append(result.Warnings, cacheWarn)
		}
		refreshed, err := s.userSubRepo.GetByID(ctx, id)
		if err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("subscription %d: %v", id, err))
			result.Statuses[id] = "failed"
			continue
		}
		result.SuccessCount++
		if cacheWarn != "" {
			result.Statuses[id] = "reset_cache_warning"
		} else {
			result.Statuses[id] = "reset"
		}
		result.Subscriptions = append(result.Subscriptions, *refreshed)
	}
	return result, nil
}

func (s *SubscriptionService) resetQuotaWindows(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly, resetFiveHour, resetSevenDay, resetThirtyDay bool, dailyWindowStart, periodicWindowStart, rollingWindowStart time.Time) error {
	return s.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		if resetDaily || resetWeekly || resetMonthly {
			if err := s.userSubRepo.ResetUsageWindows(txCtx, subscriptionID, resetDaily, resetWeekly, resetMonthly, dailyWindowStart, periodicWindowStart); err != nil {
				return err
			}
		}
		// Rolling windows are hierarchical: 5h is contained by 7d, and 7d is
		// contained by 30d. Apply only the largest selected rolling reset so a
		// parent reset also clears child windows without double-deducting usage.
		switch {
		case resetThirtyDay:
			if err := s.userSubRepo.ResetThirtyDayUsage(txCtx, subscriptionID, rollingWindowStart); err != nil {
				return err
			}
		case resetSevenDay:
			if err := s.userSubRepo.ResetSevenDayUsage(txCtx, subscriptionID, rollingWindowStart); err != nil {
				return err
			}
		case resetFiveHour:
			if err := s.userSubRepo.ResetFiveHourUsage(txCtx, subscriptionID, rollingWindowStart); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SubscriptionService) resolveBulkResetSubscriptionIDs(ctx context.Context, input BulkResetSubscriptionQuotaInput) ([]int64, error) {
	if !input.AllFiltered {
		if len(input.SubscriptionIDs) == 0 {
			return nil, ErrInvalidInput
		}
		seen := make(map[int64]struct{}, len(input.SubscriptionIDs))
		ids := make([]int64, 0, len(input.SubscriptionIDs))
		for _, id := range input.SubscriptionIDs {
			if id <= 0 {
				return nil, ErrInvalidInput
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		return ids, nil
	}

	const bulkResetMaxPageSize = 1000
	params := pagination.PaginationParams{Page: 1, PageSize: bulkResetMaxPageSize}
	ids := make([]int64, 0)
	for {
		batch, pag, err := s.userSubRepo.ListIDs(ctx, params, input.Filter.UserID, input.Filter.GroupID, input.Filter.Status, input.Filter.Platform, input.Filter.SortBy, input.Filter.SortOrder)
		if err != nil {
			return nil, err
		}
		ids = append(ids, batch...)
		if pag == nil || params.Page >= pag.Pages || len(batch) == 0 {
			break
		}
		params.Page++
	}
	return ids, nil
}

// CheckAndResetWindows checks and resets legacy usage windows.
// Rolling subscription quota windows are advanced atomically by the billing
// usage write path, so preflight maintenance must not clear them.
func (s *SubscriptionService) CheckAndResetWindows(ctx context.Context, sub *UserSubscription) error {
	now := s.currentTime()
	needsInvalidateCache := false

	// 日窗口重置（每天 0 点刷新，按日历日对齐）
	if windowStart, ok := sub.automaticDailyWindowStartAt(now); ok {
		expectedWindowStart := sub.DailyWindowStart
		if err := s.userSubRepo.ResetDailyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.DailyWindowStart = &windowStart
		sub.DailyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 周窗口重置（7天）
	if windowStart, ok := sub.automaticWindowStartAt(sub.WeeklyWindowStart, 7*24*time.Hour, now); ok {
		expectedWindowStart := sub.WeeklyWindowStart
		if err := s.userSubRepo.ResetWeeklyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.WeeklyWindowStart = &windowStart
		sub.WeeklyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 月窗口重置（30天）
	if windowStart, ok := sub.automaticWindowStartAt(sub.MonthlyWindowStart, 30*24*time.Hour, now); ok {
		expectedWindowStart := sub.MonthlyWindowStart
		if err := s.userSubRepo.ResetMonthlyUsage(ctx, sub.ID, expectedWindowStart, windowStart); err != nil {
			return err
		}
		sub.MonthlyWindowStart = &windowStart
		sub.MonthlyUsageUSD = 0
		needsInvalidateCache = true
	}

	// 如果有窗口被重置，失效缓存以保持一致性
	if needsInvalidateCache {
		s.InvalidateSubCache(sub.UserID, sub.GroupID)
		if s.billingCacheService != nil {
			_ = s.billingCacheService.InvalidateSubscription(ctx, sub.UserID, sub.GroupID)
		}
	}

	return nil
}

// EnsureWindowMaintenance advances expired usage windows before a request is
// allowed to proceed. It returns a fresh database snapshot because a competing
// request may have won one of the conditional resets.
func (s *SubscriptionService) EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription) (*UserSubscription, error) {
	if sub == nil {
		return nil, ErrSubscriptionNilInput
	}
	if !sub.IsWindowActivated() {
		if err := s.CheckAndActivateWindow(ctx, sub); err != nil {
			return nil, err
		}
	}
	if err := s.CheckAndResetWindows(ctx, sub); err != nil {
		return nil, err
	}

	// GetByID bypasses the service caches. This prevents a stale loser of the
	// CAS from validating limits against zeroed in-memory usage.
	refreshed, err := s.userSubRepo.GetByID(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	s.InvalidateSubCacheSync(sub.UserID, sub.GroupID)
	return refreshed, nil
}

// CheckUsageLimits 检查使用限额（返回错误如果超限）
// 用于中间件的快速预检查，additionalCost 通常为 0
func (s *SubscriptionService) CheckUsageLimits(ctx context.Context, sub *UserSubscription, group *Group, additionalCost float64) error {
	if !sub.CheckFiveHourLimit(additionalCost) {
		return ErrFiveHourLimitExceeded
	}
	if !sub.CheckSevenDayLimit(additionalCost) {
		return ErrSevenDayLimitExceeded
	}
	if !sub.CheckThirtyDayLimit(additionalCost) {
		return ErrThirtyDayLimitExceeded
	}
	return nil
}

// ValidateAndCheckLimits 合并验证+限额检查（中间件热路径专用）
// 仅做内存检查，不触发 DB 写入。调用方必须在放行请求前同步完成窗口维护。
// 返回 needsMaintenance 表示是否需要执行窗口维护并回读数据库快照。
func (s *SubscriptionService) ValidateAndCheckLimits(sub *UserSubscription, group *Group) (needsMaintenance bool, err error) {
	now := s.currentTime()
	// 1. 验证订阅状态
	if sub.Status == SubscriptionStatusExpired {
		return false, ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return false, ErrSubscriptionSuspended
	}
	if !sub.ExpiresAt.After(now) {
		return false, ErrSubscriptionExpired
	}

	// 2. 内存中修正过期窗口的用量，确保预检查不会误拒绝用户。
	//    调用方随后同步推进 DB 窗口，并用回读快照重新校验。
	if sub.canAutomaticallyResetDailyAt(now) {
		sub.DailyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.canAutomaticallyResetWeeklyAt(now) {
		sub.WeeklyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.canAutomaticallyResetMonthlyAt(now) {
		sub.MonthlyUsageUSD = 0
		needsMaintenance = true
	}
	if sub.NeedsFiveHourResetAt(now) {
		sub.FiveHourUsageUSD = 0
	}
	if sub.NeedsSevenDayResetAt(now) {
		sub.SevenDayUsageUSD = 0
	}
	if sub.NeedsThirtyDayResetAt(now) {
		sub.ThirtyDayUsageUSD = 0
	}
	if !sub.IsWindowActivated() {
		needsMaintenance = true
	}

	// 3. 检查用量限额
	if !sub.CheckFiveHourLimit(0) {
		return needsMaintenance, ErrFiveHourLimitExceeded
	}
	if !sub.CheckSevenDayLimit(0) {
		return needsMaintenance, ErrSevenDayLimitExceeded
	}
	if !sub.CheckThirtyDayLimit(0) {
		return needsMaintenance, ErrThirtyDayLimitExceeded
	}

	return needsMaintenance, nil
}

// DoWindowMaintenance 异步执行窗口维护（激活+重置）
// 使用独立 context，不受请求取消影响。
// 注意：此方法仅在 ValidateAndCheckLimits 返回 needsMaintenance=true 时调用，
// 而 IsExpired()=true 的订阅在 ValidateAndCheckLimits 中已被拦截返回错误，
// 因此进入此方法的订阅一定未过期，无需处理过期状态同步。
func (s *SubscriptionService) DoWindowMaintenance(sub *UserSubscription) {
	if s == nil {
		return
	}
	if s.maintenanceQueue != nil {
		err := s.maintenanceQueue.TryEnqueue(func() {
			s.doWindowMaintenance(sub)
		})
		if err != nil {
			log.Printf("Subscription maintenance enqueue failed: %v", err)
		}
		return
	}

	s.doWindowMaintenance(sub)
}

func (s *SubscriptionService) doWindowMaintenance(sub *UserSubscription) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 激活窗口（首次使用时）
	if !sub.IsWindowActivated() {
		if err := s.CheckAndActivateWindow(ctx, sub); err != nil {
			log.Printf("Failed to activate subscription windows: %v", err)
		}
	}

	// 重置过期窗口
	if err := s.CheckAndResetWindows(ctx, sub); err != nil {
		log.Printf("Failed to reset subscription windows: %v", err)
	}

	// 失效 L1 缓存，确保后续请求拿到更新后的数据
	s.InvalidateSubCache(sub.UserID, sub.GroupID)
}

// RecordUsage 记录使用量到订阅
func (s *SubscriptionService) RecordUsage(ctx context.Context, subscriptionID int64, costUSD float64) error {
	return s.userSubRepo.IncrementUsage(ctx, subscriptionID, costUSD)
}

// SubscriptionProgress 订阅进度
type SubscriptionProgress struct {
	ID            int64                `json:"id"`
	GroupName     string               `json:"group_name"`
	ExpiresAt     time.Time            `json:"expires_at"`
	ExpiresInDays int                  `json:"expires_in_days"`
	Daily         *UsageWindowProgress `json:"daily,omitempty"`
	Weekly        *UsageWindowProgress `json:"weekly,omitempty"`
	Monthly       *UsageWindowProgress `json:"monthly,omitempty"`
	FiveHour      *UsageWindowProgress `json:"five_hour,omitempty"`
	SevenDay      *UsageWindowProgress `json:"seven_day,omitempty"`
	ThirtyDay     *UsageWindowProgress `json:"thirty_day,omitempty"`
}

// UsageWindowProgress 使用窗口进度
type UsageWindowProgress struct {
	LimitUSD        float64   `json:"limit_usd"`
	UsedUSD         float64   `json:"used_usd"`
	RemainingUSD    float64   `json:"remaining_usd"`
	Percentage      float64   `json:"percentage"`
	WindowStart     time.Time `json:"window_start"`
	ResetsAt        time.Time `json:"resets_at"`
	ResetsInSeconds int64     `json:"resets_in_seconds"`
}

func buildUsageWindowProgress(limit *float64, used float64, start *time.Time, duration time.Duration, expiresAt time.Time) *UsageWindowProgress {
	if limit == nil || start == nil {
		return nil
	}
	resetsAt := start.Add(duration)
	if !expiresAt.IsZero() && expiresAt.Before(resetsAt) {
		resetsAt = expiresAt
	}
	progress := &UsageWindowProgress{
		LimitUSD:        *limit,
		UsedUSD:         used,
		RemainingUSD:    *limit - used,
		WindowStart:     *start,
		ResetsAt:        resetsAt,
		ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
	}
	if *limit <= 0 {
		progress.Percentage = 100
		progress.RemainingUSD = 0
	} else {
		progress.Percentage = (used / *limit) * 100
	}
	if progress.RemainingUSD < 0 {
		progress.RemainingUSD = 0
	}
	if progress.Percentage > 100 {
		progress.Percentage = 100
	}
	if progress.ResetsInSeconds < 0 {
		progress.ResetsInSeconds = 0
	}
	return progress
}

// GetSubscriptionProgress 获取订阅使用进度
func (s *SubscriptionService) GetSubscriptionProgress(ctx context.Context, subscriptionID int64) (*SubscriptionProgress, error) {
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}

	group := sub.Group
	if group == nil {
		group, err = s.groupRepo.GetByID(ctx, sub.GroupID)
		if err != nil {
			return nil, err
		}
	}

	return s.calculateProgress(sub, group), nil
}

// calculateProgress 根据已加载的订阅和分组数据计算使用进度（纯内存计算，无 DB 查询）
func (s *SubscriptionService) calculateProgress(sub *UserSubscription, group *Group) *SubscriptionProgress {
	displaySub := *sub
	NormalizeSubscriptionWindowsForDisplay(&displaySub)
	sub = &displaySub

	progress := &SubscriptionProgress{
		ID:            sub.ID,
		GroupName:     group.Name,
		ExpiresAt:     sub.ExpiresAt,
		ExpiresInDays: sub.DaysRemaining(),
	}

	// 日进度
	if group.HasDailyLimit() && sub.DailyWindowStart != nil {
		limit := *group.DailyLimitUSD
		resetsAt := sub.DailyWindowStart.Add(24 * time.Hour)
		if dailyResetTime := sub.DailyResetTime(); dailyResetTime != nil {
			resetsAt = *dailyResetTime
		}
		progress.Daily = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.DailyUsageUSD,
			RemainingUSD:    limit - sub.DailyUsageUSD,
			Percentage:      (sub.DailyUsageUSD / limit) * 100,
			WindowStart:     *sub.DailyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Daily.RemainingUSD < 0 {
			progress.Daily.RemainingUSD = 0
		}
		if progress.Daily.Percentage > 100 {
			progress.Daily.Percentage = 100
		}
		if progress.Daily.ResetsInSeconds < 0 {
			progress.Daily.ResetsInSeconds = 0
		}
	}

	// 周进度
	if group.HasWeeklyLimit() && sub.WeeklyWindowStart != nil {
		limit := *group.WeeklyLimitUSD
		resetsAt := sub.WeeklyWindowStart.Add(7 * 24 * time.Hour)
		if weeklyResetTime := sub.WeeklyResetTime(); weeklyResetTime != nil {
			resetsAt = *weeklyResetTime
		}
		progress.Weekly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.WeeklyUsageUSD,
			RemainingUSD:    limit - sub.WeeklyUsageUSD,
			Percentage:      (sub.WeeklyUsageUSD / limit) * 100,
			WindowStart:     *sub.WeeklyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Weekly.RemainingUSD < 0 {
			progress.Weekly.RemainingUSD = 0
		}
		if progress.Weekly.Percentage > 100 {
			progress.Weekly.Percentage = 100
		}
		if progress.Weekly.ResetsInSeconds < 0 {
			progress.Weekly.ResetsInSeconds = 0
		}
	}

	// 月进度
	if group.HasMonthlyLimit() && sub.MonthlyWindowStart != nil {
		limit := *group.MonthlyLimitUSD
		resetsAt := sub.MonthlyWindowStart.Add(30 * 24 * time.Hour)
		if monthlyResetTime := sub.MonthlyResetTime(); monthlyResetTime != nil {
			resetsAt = *monthlyResetTime
		}
		progress.Monthly = &UsageWindowProgress{
			LimitUSD:        limit,
			UsedUSD:         sub.MonthlyUsageUSD,
			RemainingUSD:    limit - sub.MonthlyUsageUSD,
			Percentage:      (sub.MonthlyUsageUSD / limit) * 100,
			WindowStart:     *sub.MonthlyWindowStart,
			ResetsAt:        resetsAt,
			ResetsInSeconds: int64(time.Until(resetsAt).Seconds()),
		}
		if progress.Monthly.RemainingUSD < 0 {
			progress.Monthly.RemainingUSD = 0
		}
		if progress.Monthly.Percentage > 100 {
			progress.Monthly.Percentage = 100
		}
		if progress.Monthly.ResetsInSeconds < 0 {
			progress.Monthly.ResetsInSeconds = 0
		}
	}

	progress.FiveHour = buildUsageWindowProgress(sub.FiveHourLimitUSD, sub.FiveHourUsageUSD, sub.FiveHourWindowStart, SubscriptionWindowFiveHour, sub.ExpiresAt)
	progress.SevenDay = buildUsageWindowProgress(sub.SevenDayLimitUSD, sub.SevenDayUsageUSD, sub.SevenDayWindowStart, SubscriptionWindowSevenDay, sub.ExpiresAt)
	progress.ThirtyDay = buildUsageWindowProgress(sub.ThirtyDayLimitUSD, sub.ThirtyDayUsageUSD, sub.ThirtyDayWindowStart, SubscriptionWindowThirtyDay, sub.ExpiresAt)

	return progress
}

// GetUserSubscriptionsWithProgress 获取用户所有订阅及进度
func (s *SubscriptionService) GetUserSubscriptionsWithProgress(ctx context.Context, userID int64) ([]SubscriptionProgress, error) {
	// ListActiveByUserID 已使用 .WithGroup() eager-load Group 关联，1 次查询获取所有数据
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	progresses := make([]SubscriptionProgress, 0, len(subs))
	for i := range subs {
		sub := &subs[i]
		group := sub.Group
		if group == nil {
			continue
		}
		progresses = append(progresses, *s.calculateProgress(sub, group))
	}

	return progresses, nil
}

// ValidateSubscription 验证订阅是否有效
func (s *SubscriptionService) ValidateSubscription(ctx context.Context, sub *UserSubscription) error {
	if sub.Status == SubscriptionStatusExpired {
		return ErrSubscriptionExpired
	}
	if sub.Status == SubscriptionStatusSuspended {
		return ErrSubscriptionSuspended
	}
	if sub.IsExpired() {
		// 更新状态
		_ = s.userSubRepo.UpdateStatus(ctx, sub.ID, SubscriptionStatusExpired)
		return ErrSubscriptionExpired
	}
	return nil
}
