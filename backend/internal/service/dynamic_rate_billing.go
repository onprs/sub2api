package service

import (
	"context"
	"time"
)

type groupBillingRatePlan struct {
	Multiplier       float64
	StaticMultiplier float64
	Dynamic          *DynamicRateUsageCommand
}

func resolveMinimumGroupBillingRate(ctx context.Context, resolver *userGroupRateResolver, group *Group, userID int64) float64 {
	if group == nil {
		return 1
	}
	resolved := resolvedUserGroupRate{Multiplier: group.RateMultiplier}
	if resolver != nil {
		resolved = resolver.ResolveDetail(ctx, userID, group.ID, group.RateMultiplier)
	}
	if resolved.HasOverride || resolved.LookupFailed || !group.DynamicRateEnabled {
		return resolved.Multiplier
	}
	return group.DynamicRateMultiplier(group.DynamicRateTargetTokens)
}

// resolveGroupBillingRatePlan 只准备计费快照，不写动态账本。
// 最终倍率解析、事件写入和扣费由仓库在同一事务中完成。
func resolveGroupBillingRatePlan(
	ctx context.Context,
	resolver *userGroupRateResolver,
	group *Group,
	userID int64,
	apiKeyID int64,
	requestID string,
	totalTokens int64,
	occurredAt time.Time,
) groupBillingRatePlan {
	if group == nil {
		return groupBillingRatePlan{Multiplier: 1, StaticMultiplier: 1}
	}

	resolved := resolvedUserGroupRate{Multiplier: group.RateMultiplier}
	if resolver != nil {
		resolved = resolver.ResolveDetail(ctx, userID, group.ID, group.RateMultiplier)
	}
	if resolved.HasOverride || resolved.LookupFailed || !group.DynamicRateEnabled || totalTokens <= 0 {
		return groupBillingRatePlan{Multiplier: resolved.Multiplier, StaticMultiplier: resolved.Multiplier}
	}

	fallback := group.DynamicRateMultiplier(0)
	if err := ValidateDynamicRateConfig(
		group.DynamicRateMaxMultiplier,
		group.DynamicRateMinMultiplier,
		group.DynamicRateTargetTokens,
		group.DynamicRateWindowMinutes,
	); err != nil {
		return groupBillingRatePlan{Multiplier: fallback, StaticMultiplier: resolved.Multiplier}
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	return groupBillingRatePlan{
		Multiplier:       fallback,
		StaticMultiplier: resolved.Multiplier,
		Dynamic: &DynamicRateUsageCommand{
			RequestID:     requestID,
			APIKeyID:      apiKeyID,
			UserID:        userID,
			GroupID:       group.ID,
			TotalTokens:   totalTokens,
			OccurredAt:    occurredAt,
			WindowMinutes: group.DynamicRateWindowMinutes,
			TargetTokens:  group.DynamicRateTargetTokens,
			MaxMultiplier: group.DynamicRateMaxMultiplier,
			MinMultiplier: group.DynamicRateMinMultiplier,
		},
	}
}

func dynamicRateForTokenUsage(log *UsageLog, plan groupBillingRatePlan) *DynamicRateUsageCommand {
	if plan.Dynamic == nil || log == nil || log.BillingMode == nil || *log.BillingMode != string(BillingModeToken) {
		return nil
	}
	plan.Dynamic.RequestID = log.RequestID
	plan.Dynamic.TotalTokens = int64(log.InputTokens) + int64(log.OutputTokens) +
		int64(log.CacheCreationTokens) + int64(log.CacheReadTokens)
	if plan.Dynamic.TotalTokens <= 0 {
		return nil
	}
	return plan.Dynamic
}
