package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) ApplyWithDynamicRate(ctx context.Context, billing *service.UsageBillingCommand, dynamic *service.DynamicRateUsageCommand) (*service.UsageBillingApplyResult, error) {
	return r.apply(ctx, billing, dynamic)
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	return r.apply(ctx, cmd, nil)
}

func (r *usageBillingRepository) apply(ctx context.Context, cmd *service.UsageBillingCommand, dynamic *service.DynamicRateUsageCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	if dynamic != nil {
		dynamic.Normalize()
		if err := validateDynamicUsageBillingCommand(cmd, dynamic); err != nil {
			return nil, err
		}
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		result := &service.UsageBillingApplyResult{Applied: false}
		if dynamic != nil {
			multiplier, lookupErr := selectExistingDynamicRate(ctx, tx, dynamic)
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return nil, lookupErr
			}
			if errors.Is(lookupErr, sql.ErrNoRows) {
				multiplier = dynamic.MaxMultiplier
			}
			result.DynamicRateMultiplier = &multiplier
		}
		return result, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if dynamic != nil {
		multiplier, resolveErr := r.resolveDynamicRateWithFallback(ctx, tx, dynamic)
		if resolveErr != nil {
			return nil, resolveErr
		}
		result.DynamicRateMultiplier = &multiplier
		scaleDynamicUsageBillingCommand(cmd, multiplier)
	}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func validateDynamicUsageBillingCommand(billing *service.UsageBillingCommand, dynamic *service.DynamicRateUsageCommand) error {
	if billing == nil || dynamic == nil {
		return errors.New("dynamic usage billing command is nil")
	}
	if dynamic.RequestID == "" {
		return service.ErrUsageBillingRequestIDRequired
	}
	if dynamic.RequestID != billing.RequestID || dynamic.APIKeyID != billing.APIKeyID || dynamic.UserID != billing.UserID {
		return service.ErrUsageBillingRequestConflict
	}
	if billing.GroupID == nil || dynamic.GroupID != *billing.GroupID || dynamic.APIKeyID <= 0 || dynamic.UserID <= 0 || dynamic.GroupID <= 0 {
		return service.ErrUsageBillingRequestConflict
	}
	totalTokens := int64(billing.InputTokens) + int64(billing.OutputTokens) +
		int64(billing.CacheCreationTokens) + int64(billing.CacheReadTokens)
	if dynamic.TotalTokens <= 0 || dynamic.TotalTokens != totalTokens {
		return service.ErrUsageBillingRequestConflict
	}
	return service.ValidateDynamicRateConfig(
		dynamic.MaxMultiplier,
		dynamic.MinMultiplier,
		dynamic.TargetTokens,
		dynamic.WindowMinutes,
	)
}

func (r *usageBillingRepository) resolveDynamicRateWithFallback(ctx context.Context, tx *sql.Tx, cmd *service.DynamicRateUsageCommand) (float64, error) {
	const savepoint = "dynamic_rate_resolution"
	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		return 0, err
	}
	multiplier, err := resolveDynamicRateInTransaction(ctx, tx, cmd)
	if err == nil {
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); releaseErr != nil {
			return 0, releaseErr
		}
		return multiplier, nil
	}
	if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rollbackErr != nil {
		return 0, rollbackErr
	}
	if _, releaseErr := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); releaseErr != nil {
		return 0, releaseErr
	}
	if errors.Is(err, service.ErrUsageBillingRequestConflict) {
		return 0, err
	}
	logger.LegacyPrintf(
		"repository.usage_billing",
		"resolve dynamic group rate failed, fallback to maximum: user=%d group=%d request_id=%s err=%v",
		cmd.UserID,
		cmd.GroupID,
		cmd.RequestID,
		err,
	)
	return cmd.MaxMultiplier, nil
}

func resolveDynamicRateInTransaction(ctx context.Context, tx *sql.Tx, cmd *service.DynamicRateUsageCommand) (float64, error) {
	// 两参数 advisory lock 使用独立命名空间，并按用户与分组串行化动态倍率结算。
	// 这样同一窗口的并发请求不会都基于旧累计值降价，不同分组仍可并行，且不会与其它单键锁冲突。
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(
			hashtext('dynamic_rate_usage'),
			hashtext($1::text || ':' || $2::text)
		)
	`, cmd.UserID, cmd.GroupID); err != nil {
		return 0, err
	}

	existingMultiplier, err := selectExistingDynamicRate(ctx, tx, cmd)
	if err == nil {
		return existingMultiplier, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO dynamic_rate_usage_events (
			request_id, api_key_id, user_id, group_id, total_tokens,
			resolved_multiplier, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.GroupID, cmd.TotalTokens, cmd.MaxMultiplier, cmd.OccurredAt); err != nil {
		return 0, err
	}

	windowStart := cmd.OccurredAt.Add(-time.Duration(cmd.WindowMinutes) * time.Minute)
	var accumulatedTokens int64
	// 不设时间上界：advisory lock 规定结算顺序，已先提交的并发事件即使采集时间略晚也必须计入。
	if err := tx.QueryRowContext(ctx, `
		SELECT
			COALESCE((
				SELECT SUM(event.total_tokens)
				FROM dynamic_rate_usage_events event
				WHERE event.user_id = $1
				  AND event.group_id = $2
				  AND event.occurred_at >= $3
			), 0)
			+
			COALESCE((
				SELECT SUM(
					log.input_tokens::BIGINT + log.output_tokens::BIGINT +
					log.cache_creation_tokens::BIGINT + log.cache_read_tokens::BIGINT
				)
				FROM usage_logs log
				WHERE log.user_id = $1
				  AND log.group_id = $2
				  AND log.created_at >= $3
				  AND (log.billing_mode IS NULL OR log.billing_mode = 'token')
				  AND log.created_at < (
					SELECT metadata.ledger_started_at
					FROM dynamic_rate_usage_metadata metadata
					WHERE metadata.singleton = TRUE
				  )
				  AND (
					EXISTS (
						SELECT 1
						FROM usage_billing_dedup dedup
						WHERE dedup.request_id = log.request_id
						  AND dedup.api_key_id = log.api_key_id
					)
					OR EXISTS (
						SELECT 1
						FROM usage_billing_dedup_archive archive
						WHERE archive.request_id = log.request_id
						  AND archive.api_key_id = log.api_key_id
					)
				  )
				  AND NOT EXISTS (
					SELECT 1
					FROM dynamic_rate_usage_events event
					WHERE event.request_id = log.request_id
					  AND event.api_key_id = log.api_key_id
				  )
			), 0)
	`, cmd.UserID, cmd.GroupID, windowStart).Scan(&accumulatedTokens); err != nil {
		return 0, err
	}

	multiplier := cmd.Multiplier(accumulatedTokens)
	if _, err := tx.ExecContext(ctx, `
		UPDATE dynamic_rate_usage_events
		SET resolved_multiplier = $3
		WHERE request_id = $1 AND api_key_id = $2
	`, cmd.RequestID, cmd.APIKeyID, multiplier); err != nil {
		return 0, err
	}
	return multiplier, nil
}

func selectExistingDynamicRate(ctx context.Context, tx *sql.Tx, cmd *service.DynamicRateUsageCommand) (float64, error) {
	var existingUserID, existingGroupID, existingTokens int64
	var multiplier float64
	err := tx.QueryRowContext(ctx, `
		SELECT user_id, group_id, total_tokens, resolved_multiplier
		FROM dynamic_rate_usage_events
		WHERE request_id = $1 AND api_key_id = $2
	`, cmd.RequestID, cmd.APIKeyID).Scan(&existingUserID, &existingGroupID, &existingTokens, &multiplier)
	if err != nil {
		return 0, err
	}
	if existingUserID != cmd.UserID || existingGroupID != cmd.GroupID || existingTokens != cmd.TotalTokens {
		return 0, service.ErrUsageBillingRequestConflict
	}
	return multiplier, nil
}

func scaleDynamicUsageBillingCommand(cmd *service.UsageBillingCommand, multiplier float64) {
	if cmd == nil || cmd.DynamicRateBasisMultiplier <= 0 || multiplier == cmd.DynamicRateBasisMultiplier {
		return
	}
	ratio := multiplier / cmd.DynamicRateBasisMultiplier
	cmd.BalanceCost = scaleDynamicRateBillingCost(cmd.BalanceCost, cmd.DynamicRateExcludedCost, ratio)
	cmd.SubscriptionCost = scaleDynamicRateBillingCost(cmd.SubscriptionCost, cmd.DynamicRateExcludedCost, ratio)
	cmd.APIKeyQuotaCost = scaleDynamicRateBillingCost(cmd.APIKeyQuotaCost, cmd.DynamicRateExcludedCost, ratio)
	cmd.APIKeyRateLimitCost = scaleDynamicRateBillingCost(cmd.APIKeyRateLimitCost, cmd.DynamicRateExcludedCost, ratio)
	cmd.Normalize()
}

func scaleDynamicRateBillingCost(value, excluded, ratio float64) float64 {
	if value <= 0 {
		return value
	}
	adjustable := value - excluded
	if adjustable < 0 {
		adjustable = 0
	}
	return excluded + adjustable*ratio
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 {
		subscriptionID, err := resolveUsageBillingSubscriptionID(ctx, tx, cmd)
		if err != nil {
			return err
		}
		if err := incrementUsageBillingSubscription(ctx, tx, subscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
		result.SubscriptionID = &subscriptionID
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

func resolveUsageBillingSubscriptionID(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (int64, error) {
	if cmd.SubscriptionID != nil {
		return *cmd.SubscriptionID, nil
	}
	if cmd.GroupID == nil {
		return 0, service.ErrSubscriptionNotFound
	}
	subscriptionID, err := selectUsageBillingSubscription(ctx, tx, cmd.UserID, *cmd.GroupID, cmd.SubscriptionCost)
	if err == nil || !errors.Is(err, service.ErrSubscriptionNotFound) {
		return subscriptionID, err
	}
	return selectUsageBillingExhaustionSubscription(ctx, tx, cmd.UserID, *cmd.GroupID, cmd.SubscriptionCost)
}

func selectUsageBillingSubscription(ctx context.Context, tx *sql.Tx, userID, groupID int64, costUSD float64) (int64, error) {
	const query = `
		SELECT us.id
		FROM user_subscriptions us
		WHERE us.user_id = $1
			AND us.group_id = $2
			AND us.status = $4
			AND us.expires_at > NOW()
			AND us.deleted_at IS NULL
			AND (
				us.five_hour_limit_usd IS NULL
				OR (
					us.five_hour_limit_usd > 0
					AND CASE
						WHEN us.five_hour_window_start IS NULL
							OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
						THEN 0
						ELSE us.five_hour_usage_usd
					END + $3 <= us.five_hour_limit_usd
				)
			)
			AND (
				us.seven_day_limit_usd IS NULL
				OR (
					us.seven_day_limit_usd > 0
					AND CASE
						WHEN us.seven_day_window_start IS NULL
							OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
						THEN 0
						ELSE us.seven_day_usage_usd
					END + $3 <= us.seven_day_limit_usd
				)
			)
			AND (
				us.thirty_day_limit_usd IS NULL
				OR (
					us.thirty_day_limit_usd > 0
					AND CASE
						WHEN us.thirty_day_window_start IS NULL
							OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
						THEN 0
						ELSE us.thirty_day_usage_usd
					END + $3 <= us.thirty_day_limit_usd
				)
			)
		ORDER BY us.expires_at ASC, us.id ASC
		FOR UPDATE
		LIMIT 1
	`
	var subscriptionID int64
	err := tx.QueryRowContext(ctx, query, userID, groupID, costUSD, service.SubscriptionStatusActive).Scan(&subscriptionID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrSubscriptionNotFound
	}
	if err != nil {
		return 0, err
	}
	return subscriptionID, nil
}

// selectUsageBillingExhaustionSubscription records an unavoidable response-cost
// overrun against a subscription that still had headroom before the response.
// This closes the entitlement for subsequent preflight checks instead of
// repeatedly charging an older subscription that was already exhausted.
func selectUsageBillingExhaustionSubscription(ctx context.Context, tx *sql.Tx, userID, groupID int64, costUSD float64) (int64, error) {
	const query = `
		SELECT us.id
		FROM user_subscriptions us
		WHERE us.user_id = $1
			AND us.group_id = $2
			AND us.status = $4
			AND us.expires_at > NOW()
			AND us.deleted_at IS NULL
			AND (
				(
					us.five_hour_limit_usd IS NOT NULL
					AND us.five_hour_limit_usd > 0
					AND CASE
						WHEN us.five_hour_window_start IS NULL
							OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
						THEN 0
						ELSE us.five_hour_usage_usd
					END + $3 > us.five_hour_limit_usd
				)
				OR (
					us.seven_day_limit_usd IS NOT NULL
					AND us.seven_day_limit_usd > 0
					AND CASE
						WHEN us.seven_day_window_start IS NULL
							OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
						THEN 0
						ELSE us.seven_day_usage_usd
					END + $3 > us.seven_day_limit_usd
				)
				OR (
					us.thirty_day_limit_usd IS NOT NULL
					AND us.thirty_day_limit_usd > 0
					AND CASE
						WHEN us.thirty_day_window_start IS NULL
							OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
						THEN 0
						ELSE us.thirty_day_usage_usd
					END + $3 > us.thirty_day_limit_usd
				)
			)
		ORDER BY
			CASE
				WHEN (
					us.five_hour_limit_usd IS NOT NULL
					AND (
						us.five_hour_limit_usd <= 0
						OR CASE
							WHEN us.five_hour_window_start IS NULL
								OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
							THEN 0
							ELSE us.five_hour_usage_usd
						END >= us.five_hour_limit_usd
					)
				)
				OR (
					us.seven_day_limit_usd IS NOT NULL
					AND (
						us.seven_day_limit_usd <= 0
						OR CASE
							WHEN us.seven_day_window_start IS NULL
								OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
							THEN 0
							ELSE us.seven_day_usage_usd
						END >= us.seven_day_limit_usd
					)
				)
				OR (
					us.thirty_day_limit_usd IS NOT NULL
					AND (
						us.thirty_day_limit_usd <= 0
						OR CASE
							WHEN us.thirty_day_window_start IS NULL
								OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
							THEN 0
							ELSE us.thirty_day_usage_usd
						END >= us.thirty_day_limit_usd
					)
				)
				THEN 1
				ELSE 0
			END ASC,
			us.expires_at ASC,
			us.id ASC
		FOR UPDATE
		LIMIT 1
	`
	var subscriptionID int64
	err := tx.QueryRowContext(ctx, query, userID, groupID, costUSD, service.SubscriptionStatusActive).Scan(&subscriptionID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, service.ErrSubscriptionNotFound
	}
	if err != nil {
		return 0, err
	}
	return subscriptionID, nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			five_hour_usage_usd = CASE
				WHEN us.five_hour_window_start IS NULL
					OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
				THEN $1
				ELSE us.five_hour_usage_usd + $1
			END,
			seven_day_usage_usd = CASE
				WHEN us.seven_day_window_start IS NULL
					OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
				THEN $1
				ELSE us.seven_day_usage_usd + $1
			END,
			thirty_day_usage_usd = CASE
				WHEN us.thirty_day_window_start IS NULL
					OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
				THEN $1
				ELSE us.thirty_day_usage_usd + $1
			END,
			five_hour_window_start = CASE
				WHEN us.five_hour_window_start IS NULL
					OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
				THEN NOW()
				ELSE us.five_hour_window_start
			END,
			seven_day_window_start = CASE
				WHEN us.seven_day_window_start IS NULL
					OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
				THEN NOW()
				ELSE us.seven_day_window_start
			END,
			thirty_day_window_start = CASE
				WHEN us.thirty_day_window_start IS NULL
					OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
				THEN NOW()
				ELSE us.thirty_day_window_start
			END,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
