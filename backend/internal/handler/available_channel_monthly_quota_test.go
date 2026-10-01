package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestToUserSupportedModelsExposesIndependentMonthlyQuotaValidity(t *testing.T) {
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	policy := service.MonthlyQuotaPolicy{BaseCreditsUSD: 20, Periods: []service.MonthlyQuotaPeriod{{
		CreditsUSD: 60, StartsAt: &start, ExpiresAt: &end, Term: "through Oct 7th",
	}}}
	for _, at := range []time.Time{start.Add(-time.Second), start, end} {
		quota, ok := policy.ResolveAt(at, 70)
		require.True(t, ok)
		src := []service.SupportedModel{{
			Name: "moonshotai/Kimi-K3", Platform: service.PlatformCommandCode, MonthlyQuota: &quota,
			QuotaCost: &service.ModelQuotaCost{IncludedMonthlyUsageUSD: quota.CreditsUSD, CostMultiplier: quota.CostMultiplier},
		}}
		out := toUserSupportedModels(src, nil)
		require.Len(t, out, 1)
		require.NotNil(t, out[0].MonthlyQuota)
		require.Equal(t, *out[0].ModelSpecificMultiplier, out[0].MonthlyQuota.CostMultiplier)
		body, err := json.Marshal(out[0])
		require.NoError(t, err)
		var model map[string]any
		require.NoError(t, json.Unmarshal(body, &model))
		monthly, ok := model["monthly_quota"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, 20.0, monthly["base_credits_usd"])
		require.NotContains(t, model, "promotion")
		if at.Equal(start) {
			require.Equal(t, 60.0, monthly["credits_usd"])
			require.Equal(t, "2026-10-08T00:00:00Z", monthly["expires_at"])
			require.Equal(t, "2026-10-08T00:00:00Z", monthly["next_change_at"])
		} else {
			require.Equal(t, 20.0, monthly["credits_usd"])
			require.NotContains(t, monthly, "expires_at")
		}
	}
	out := toUserSupportedModels([]service.SupportedModel{{Name: "unpriced", Platform: service.PlatformCommandCode}}, nil)
	body, err := json.Marshal(out[0])
	require.NoError(t, err)
	require.NotContains(t, string(body), "monthly_quota")
}
