package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dynamicRateUsageRepoStub struct {
	UsageBillingRepository
	multiplier float64
	err        error
	applyCalls int
	command    *DynamicRateUsageCommand
	billing    *UsageBillingCommand
}

func (s *dynamicRateUsageRepoStub) Apply(_ context.Context, billing *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.applyCalls++
	s.billing = billing
	if s.err != nil {
		return nil, s.err
	}
	return &UsageBillingApplyResult{Applied: true}, nil
}

func (s *dynamicRateUsageRepoStub) ApplyWithDynamicRate(_ context.Context, billing *UsageBillingCommand, dynamic *DynamicRateUsageCommand) (*UsageBillingApplyResult, error) {
	s.billing = billing
	s.command = dynamic
	if s.err != nil {
		return nil, s.err
	}
	multiplier := s.multiplier
	return &UsageBillingApplyResult{Applied: true, DynamicRateMultiplier: &multiplier}, nil
}

func testDynamicRateGroup() *Group {
	return &Group{
		ID:                       20,
		RateMultiplier:           0.9,
		DynamicRateEnabled:       true,
		DynamicRateMaxMultiplier: 0.15,
		DynamicRateMinMultiplier: 0.13,
		DynamicRateTargetTokens:  1_000,
		DynamicRateWindowMinutes: 60,
	}
}

func TestGroupDynamicRateMultiplier(t *testing.T) {
	group := testDynamicRateGroup()

	require.Equal(t, 0.15, group.DynamicRateMultiplier(0))
	require.Equal(t, 0.14, group.DynamicRateMultiplier(500))
	require.Equal(t, 0.13, group.DynamicRateMultiplier(1_000))
	require.Equal(t, 0.13, group.DynamicRateMultiplier(2_000))

	group.DynamicRateEnabled = false
	require.Equal(t, 0.9, group.DynamicRateMultiplier(500))
}

func TestValidateDynamicRateConfig(t *testing.T) {
	require.NoError(t, ValidateDynamicRateConfig(0.15, 0.13, 1_000, 60))
	require.Error(t, ValidateDynamicRateConfig(MaxDynamicRateMultiplier+0.0001, 0.13, 1_000, 60))
	require.Error(t, ValidateDynamicRateConfig(0.15, MaxDynamicRateMultiplier+0.0001, 1_000, 60))
	require.Error(t, ValidateDynamicRateConfig(0.12, 0.13, 1_000, 60))
	require.Error(t, ValidateDynamicRateConfig(0.15, 0.13, 0, 60))
	require.Error(t, ValidateDynamicRateConfig(0.15, 0.13, 1_000, 0))
	require.Error(t, ValidateDynamicRateConfig(0.15, 0.13, 1_000, MaxDynamicRateWindowMinutes+1))
}

func TestResolveGroupBillingRatePlanDefersDynamicResolution(t *testing.T) {
	group := testDynamicRateGroup()
	occurredAt := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	plan := resolveGroupBillingRatePlan(
		context.Background(), nil, group, 10, 30, "request-1", 375, occurredAt,
	)

	require.Equal(t, group.DynamicRateMaxMultiplier, plan.Multiplier)
	require.Equal(t, group.RateMultiplier, plan.StaticMultiplier)
	require.NotNil(t, plan.Dynamic)
	require.Equal(t, int64(375), plan.Dynamic.TotalTokens)
	require.Equal(t, occurredAt, plan.Dynamic.OccurredAt)
	require.Equal(t, int64(1_000), plan.Dynamic.TargetTokens)
}

func TestResolveGroupBillingRatePlanUserOverrideWinsEvenWhenEqualToStaticRate(t *testing.T) {
	group := testDynamicRateGroup()
	override := group.RateMultiplier
	resolver := newUserGroupRateResolver(
		&userGroupRateResolverRepoStub{rate: &override}, nil, time.Minute, nil, "service.test",
	)

	plan := resolveGroupBillingRatePlan(
		context.Background(), resolver, group, 10, 30, "request-2", 1_000, time.Now(),
	)

	require.Equal(t, override, plan.Multiplier)
	require.Nil(t, plan.Dynamic)
}

func TestResolveGroupBillingRatePlanLookupFailureKeepsStaticFallback(t *testing.T) {
	group := testDynamicRateGroup()
	resolver := newUserGroupRateResolver(
		&userGroupRateResolverRepoStub{err: errors.New("lookup failed")}, nil, time.Minute, nil, "service.test",
	)

	plan := resolveGroupBillingRatePlan(
		context.Background(), resolver, group, 10, 30, "request-lookup-failure", 1_000, time.Now(),
	)

	require.Equal(t, group.RateMultiplier, plan.Multiplier)
	require.Nil(t, plan.Dynamic)
	require.Equal(t, group.RateMultiplier, resolveMinimumGroupBillingRate(context.Background(), resolver, group, 10))
}

func TestResolveGroupBillingRatePlanFallsBackToMaximumAndSkipsZeroTokens(t *testing.T) {
	group := testDynamicRateGroup()
	group.DynamicRateMinMultiplier = group.DynamicRateMaxMultiplier + 0.01

	plan := resolveGroupBillingRatePlan(
		context.Background(), nil, group, 10, 30, "request-3", 100, time.Now(),
	)
	require.Equal(t, group.DynamicRateMaxMultiplier, plan.Multiplier)
	require.Nil(t, plan.Dynamic)

	group.DynamicRateMinMultiplier = 0.13
	plan = resolveGroupBillingRatePlan(
		context.Background(), nil, group, 10, 30, "request-4", 0, time.Now(),
	)
	require.Equal(t, group.RateMultiplier, plan.Multiplier)
	require.Nil(t, plan.Dynamic)
}

func TestDynamicRateForTokenUsageRejectsIndependentBillingModes(t *testing.T) {
	plan := resolveGroupBillingRatePlan(
		context.Background(), nil, testDynamicRateGroup(), 10, 30, "request-5", 100, time.Now(),
	)

	tokenMode := string(BillingModeToken)
	tokenLog := &UsageLog{
		RequestID:      "request-final",
		InputTokens:    60,
		OutputTokens:   40,
		BillingMode:    &tokenMode,
		RateMultiplier: plan.Multiplier,
	}
	dynamic := dynamicRateForTokenUsage(tokenLog, plan)
	require.NotNil(t, dynamic)
	require.Equal(t, tokenLog.RequestID, dynamic.RequestID)
	require.Equal(t, int64(100), dynamic.TotalTokens)

	imageMode := string(BillingModeImage)
	tokenLog.BillingMode = &imageMode
	require.Nil(t, dynamicRateForTokenUsage(tokenLog, plan))

	videoMode := string(BillingModeVideo)
	tokenLog.BillingMode = &videoMode
	require.Nil(t, dynamicRateForTokenUsage(tokenLog, plan))
}

func TestApplyDynamicRateBillingResultUpdatesCostAndUsageSnapshot(t *testing.T) {
	resolved := 0.14
	cost := &CostBreakdown{TotalCost: 10, ActualCost: 1.5, DynamicRateExcludedCost: 0.3}
	usageLog := &UsageLog{ActualCost: 1.5, RateMultiplier: 0.3}
	params := &postUsageBillingParams{Cost: cost, DynamicRateBasis: 0.15}

	applyDynamicRateBillingResult(usageLog, params, &UsageBillingApplyResult{
		Applied:               true,
		DynamicRateMultiplier: &resolved,
	})

	require.Equal(t, 1.42, cost.ActualCost)
	require.Equal(t, 1.42, usageLog.ActualCost)
	require.Equal(t, 0.28, usageLog.RateMultiplier)
}

func TestDynamicUsageBillingFingerprintIgnoresConfiguredMaximum(t *testing.T) {
	newCommand := func(basis, provisionalCost float64) *UsageBillingCommand {
		return &UsageBillingCommand{
			RequestID:                  "request-fingerprint",
			APIKeyID:                   30,
			UserID:                     10,
			AccountID:                  40,
			InputTokens:                100,
			BalanceCost:                provisionalCost,
			APIKeyQuotaCost:            provisionalCost,
			DynamicRateBasisMultiplier: basis,
		}
	}

	first := newCommand(0.15, 1.5)
	second := newCommand(0.30, 3)
	first.Normalize()
	second.Normalize()

	require.Equal(t, first.RequestFingerprint, second.RequestFingerprint)
}

func TestResolveMinimumGroupBillingRateUsesDynamicFloor(t *testing.T) {
	group := testDynamicRateGroup()
	require.Equal(t, group.DynamicRateMinMultiplier, resolveMinimumGroupBillingRate(context.Background(), nil, group, 0))
}
