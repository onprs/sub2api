//go:build integration

package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type dynamicBillingFixture struct {
	user    *service.User
	group   *service.Group
	apiKey  *service.APIKey
	account *service.Account
}

func createDynamicBillingFixture(t *testing.T) dynamicBillingFixture {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("dynamic-rate-user-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      100,
	})
	group := mustCreateGroup(t, client, &service.Group{
		Name:     "dynamic-rate-group-" + uuid.NewString(),
		Platform: service.PlatformAnthropic,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-dynamic-rate-" + uuid.NewString(),
		Name:    "dynamic-rate",
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name:     "dynamic-rate-account-" + uuid.NewString(),
		Platform: service.PlatformAnthropic,
	})
	return dynamicBillingFixture{user: user, group: group, apiKey: apiKey, account: account}
}

func dynamicBillingCommands(fixture dynamicBillingFixture, requestID string, totalTokens int, occurredAt time.Time, targetTokens int64) (*service.UsageBillingCommand, *service.DynamicRateUsageCommand) {
	groupID := fixture.group.ID
	billing := &service.UsageBillingCommand{
		RequestID:                  requestID,
		APIKeyID:                   fixture.apiKey.ID,
		UserID:                     fixture.user.ID,
		AccountID:                  fixture.account.ID,
		GroupID:                    &groupID,
		InputTokens:                totalTokens,
		BalanceCost:                1,
		DynamicRateBasisMultiplier: 1,
	}
	dynamic := &service.DynamicRateUsageCommand{
		RequestID:     requestID,
		APIKeyID:      fixture.apiKey.ID,
		UserID:        fixture.user.ID,
		GroupID:       fixture.group.ID,
		TotalTokens:   int64(totalTokens),
		OccurredAt:    occurredAt,
		WindowMinutes: 60,
		TargetTokens:  targetTokens,
		MaxMultiplier: 1,
		MinMultiplier: 0.2,
	}
	return billing, dynamic
}

func TestUsageBillingRepositoryDynamicRateSerializesAndDeduplicatesAtomically(t *testing.T) {
	ctx := context.Background()
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)

	occurredAt := time.Now().UTC()
	const requestCount = 4
	rates := make([]float64, requestCount)
	errs := make([]error, requestCount)
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			billing, dynamic := dynamicBillingCommands(fixture, uuid.NewString(), 250, occurredAt, 1_000)
			result, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
			errs[index] = err
			if result != nil && result.DynamicRateMultiplier != nil {
				rates[index] = *result.DynamicRateMultiplier
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	sort.Float64s(rates)
	require.Equal(t, []float64{0.2, 0.4, 0.6, 0.8}, rates)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, fixture.user.ID).Scan(&balance))
	require.InDelta(t, 98, balance, 1e-8)

	requestID := uuid.NewString()
	billing, dynamic := dynamicBillingCommands(fixture, requestID, 50, occurredAt.Add(time.Second), 2_000)
	first, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
	require.NoError(t, err)
	require.True(t, first.Applied)
	second, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
	require.NoError(t, err)
	require.False(t, second.Applied)
	require.NotNil(t, first.DynamicRateMultiplier)
	require.NotNil(t, second.DynamicRateMultiplier)
	require.Equal(t, *first.DynamicRateMultiplier, *second.DynamicRateMultiplier)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM dynamic_rate_usage_events
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, fixture.apiKey.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestUsageBillingRepositoryDynamicRateUsesSerializedSettlementOrder(t *testing.T) {
	ctx := context.Background()
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)
	now := time.Now().UTC()

	laterBilling, laterDynamic := dynamicBillingCommands(fixture, uuid.NewString(), 500, now.Add(time.Second), 1_000)
	laterResult, err := repo.ApplyWithDynamicRate(ctx, laterBilling, laterDynamic)
	require.NoError(t, err)
	require.NotNil(t, laterResult.DynamicRateMultiplier)
	require.Equal(t, 0.6, *laterResult.DynamicRateMultiplier)

	// 锁获取顺序可能与调用方采集时间相反；后结算请求仍必须看到已提交的同窗口事件。
	earlierBilling, earlierDynamic := dynamicBillingCommands(fixture, uuid.NewString(), 500, now, 1_000)
	earlierResult, err := repo.ApplyWithDynamicRate(ctx, earlierBilling, earlierDynamic)
	require.NoError(t, err)
	require.NotNil(t, earlierResult.DynamicRateMultiplier)
	require.Equal(t, 0.2, *earlierResult.DynamicRateMultiplier)
}

func TestUsageBillingRepositoryDynamicRateExcludesExpiredEvents(t *testing.T) {
	ctx := context.Background()
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)

	now := time.Now().UTC()
	oldBilling, oldDynamic := dynamicBillingCommands(fixture, uuid.NewString(), 500, now.Add(-2*time.Hour), 1_000)
	oldResult, err := repo.ApplyWithDynamicRate(ctx, oldBilling, oldDynamic)
	require.NoError(t, err)
	require.NotNil(t, oldResult.DynamicRateMultiplier)
	require.Equal(t, 0.6, *oldResult.DynamicRateMultiplier)

	currentBilling, currentDynamic := dynamicBillingCommands(fixture, uuid.NewString(), 500, now, 1_000)
	currentResult, err := repo.ApplyWithDynamicRate(ctx, currentBilling, currentDynamic)
	require.NoError(t, err)
	require.NotNil(t, currentResult.DynamicRateMultiplier)
	require.Equal(t, 0.6, *currentResult.DynamicRateMultiplier)
}

func TestUsageBillingRepositoryDynamicRateBackfillsOnlyUsageBeforeLedgerWatermark(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := NewUsageBillingRepository(client, integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)

	var watermark time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT ledger_started_at FROM dynamic_rate_usage_metadata WHERE singleton = TRUE
	`).Scan(&watermark))
	groupID := fixture.group.ID
	usageRepo := newUsageLogRepositoryWithSQL(client, integrationDB)
	historicalRequestID := "historical-" + uuid.NewString()
	unbilledHistoricalRequestID := "unbilled-historical-" + uuid.NewString()
	uncommittedRequestID := "uncommitted-" + uuid.NewString()
	for requestID, createdAt := range map[string]time.Time{
		historicalRequestID:         watermark.Add(-time.Minute),
		unbilledHistoricalRequestID: watermark.Add(-30 * time.Second),
		uncommittedRequestID:        watermark.Add(10 * time.Second),
	} {
		_, err := usageRepo.Create(ctx, &service.UsageLog{
			UserID:      fixture.user.ID,
			APIKeyID:    fixture.apiKey.ID,
			AccountID:   fixture.account.ID,
			GroupID:     &groupID,
			RequestID:   requestID,
			Model:       "claude-sonnet-4",
			InputTokens: 100,
			CreatedAt:   createdAt,
		})
		require.NoError(t, err)
	}
	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint, created_at)
		VALUES ($1, $2, $3, $4)
	`, historicalRequestID, fixture.apiKey.ID, strings.Repeat("a", 64), watermark.Add(-time.Minute))
	require.NoError(t, err)

	billing, dynamic := dynamicBillingCommands(
		fixture,
		uuid.NewString(),
		100,
		watermark.Add(time.Minute),
		1_000,
	)
	result, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
	require.NoError(t, err)
	require.NotNil(t, result.DynamicRateMultiplier)
	// 只有存在已提交扣费幂等键的水位前日志计入；水位前未扣费及水位后无账本日志均排除。
	require.Equal(t, 0.84, *result.DynamicRateMultiplier)
}

func TestUsageBillingRepositoryDynamicRateFallsBackToMaximumWhenLedgerAggregationFails(t *testing.T) {
	ctx := context.Background()
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)
	now := time.Now().UTC()

	_, err := integrationDB.ExecContext(ctx, `
		INSERT INTO dynamic_rate_usage_events (
			request_id, api_key_id, user_id, group_id, total_tokens, resolved_multiplier, occurred_at
		) VALUES ($1, $2, $3, $4, $5, 1, $6)
	`, uuid.NewString(), fixture.apiKey.ID, fixture.user.ID, fixture.group.ID, int64(9223372036854775807), now.Add(-time.Minute))
	require.NoError(t, err)

	requestID := uuid.NewString()
	billing, dynamic := dynamicBillingCommands(fixture, requestID, 1, now, 1_000)
	result, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.DynamicRateMultiplier)
	require.Equal(t, dynamic.MaxMultiplier, *result.DynamicRateMultiplier)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1`, fixture.user.ID).Scan(&balance))
	require.InDelta(t, 99, balance, 1e-8)

	var eventCount, dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dynamic_rate_usage_events WHERE request_id = $1 AND api_key_id = $2
	`, requestID, fixture.apiKey.ID).Scan(&eventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2
	`, requestID, fixture.apiKey.ID).Scan(&dedupCount))
	require.Zero(t, eventCount)
	require.Equal(t, 1, dedupCount)
}

func TestUsageBillingRepositoryDynamicRateRollsBackEventWhenBillingFails(t *testing.T) {
	ctx := context.Background()
	repo := NewUsageBillingRepository(testEntClient(t), integrationDB).(service.DynamicUsageBillingRepository)
	fixture := createDynamicBillingFixture(t)
	requestID := uuid.NewString()
	billing, dynamic := dynamicBillingCommands(fixture, requestID, 100, time.Now().UTC(), 1_000)
	billing.UserID = fixture.user.ID + 9_000_000_000
	dynamic.UserID = billing.UserID

	_, err := repo.ApplyWithDynamicRate(ctx, billing, dynamic)
	require.ErrorIs(t, err, service.ErrUserNotFound)

	var eventCount, dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dynamic_rate_usage_events WHERE request_id = $1 AND api_key_id = $2
	`, requestID, fixture.apiKey.ID).Scan(&eventCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2
	`, requestID, fixture.apiKey.ID).Scan(&dedupCount))
	require.Zero(t, eventCount)
	require.Zero(t, dedupCount)
}
