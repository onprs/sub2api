package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandCodeMonthlyQuotaBillingAndDisplayUseRequestInstant(t *testing.T) {
	billing := &BillingService{}
	gateway := &GatewayService{billingService: billing}
	channel := &ChannelService{billingService: billing}
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		at      time.Time
		credits float64
		boosted bool
	}{
		{"开始前", start.Add(-time.Nanosecond), 20, false},
		{"开始时", start, 60, true},
		{"截止前", end.Add(-time.Nanosecond), 60, true},
		{"截止时", end, 20, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := &ForwardResult{Model: "moonshotai/Kimi-K3", Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 500}}
			cost, model, err := gateway.calculateRecordUsageCostFromCandidates(
				context.Background(), result, &APIKey{ID: 1}, []string{"kimi-k3"}, 2, 1, 1, tt.at,
				&recordUsageOpts{PricingPlatform: PlatformCommandCode},
			)
			require.NoError(t, err)
			require.Equal(t, "kimi-k3", model)
			// 延迟结算仍使用原请求时刻；Token 原价保持不变。
			rawCost := 1000*3e-6 + 500*15e-6
			require.InDelta(t, rawCost, cost.TotalCost, 1e-12)
			require.InDelta(t, 70/tt.credits, cost.ModelSpecificMultiplier, 1e-12)
			require.InDelta(t, rawCost*2*70/tt.credits, cost.ActualCost, 1e-12)
			for _, source := range []string{PricingSourceCatalog, PricingSourceChannel} {
				display := SupportedModel{Name: "moonshotai/Kimi-K3", Platform: PlatformCommandCode, PricingSource: source}
				require.True(t, channel.fillModelQuotaCostForNameAt(&display, "kimi-k3", tt.at))
				channel.fillCommandCodeMetadataForNameAt(&display, "kimi-k3", tt.at)
				require.NotNil(t, display.MonthlyQuota)
				require.Equal(t, tt.credits, display.MonthlyQuota.CreditsUSD)
				require.InDelta(t, cost.ModelSpecificMultiplier, display.QuotaCost.CostMultiplier, 1e-12)
				if tt.boosted {
					require.NotNil(t, display.UsageOffer)
					require.Equal(t, 3.0, display.UsageOffer.UsageMultiplier)
					require.Equal(t, end, *display.MonthlyQuota.ExpiresAt)
				} else {
					require.Nil(t, display.UsageOffer)
				}
			}
		})
	}
}

func TestCommandCodeMonthlyQuotaRefreshClearsExpiredOffer(t *testing.T) {
	channel := &ChannelService{billingService: &BillingService{}}
	model := SupportedModel{Name: "kimi-k3", Platform: PlatformCommandCode, PricingSource: PricingSourceCatalog}
	inside := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	require.True(t, channel.fillModelQuotaCostForNameAt(&model, model.Name, inside))
	channel.fillCommandCodeMetadataForNameAt(&model, model.Name, inside)
	require.NotNil(t, model.UsageOffer)
	after := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	require.True(t, channel.fillModelQuotaCostForNameAt(&model, model.Name, after))
	channel.fillCommandCodeMetadataForNameAt(&model, model.Name, after)
	require.Nil(t, model.UsageOffer)
	require.Nil(t, model.MonthlyQuota.ExpiresAt)
	require.Equal(t, 20.0, model.MonthlyQuota.CreditsUSD)
	// 可用渠道的批量展示也使用同一额度规则。
	models := []SupportedModel{{Name: "kimi-k3", Platform: PlatformCommandCode}}
	channel.fillModelQuotaCosts(models)
	require.NotNil(t, models[0].MonthlyQuota)
	current, ok := (&BillingService{}).GetCommandCodeQuotaCost("kimi-k3")
	require.True(t, ok)
	require.Equal(t, current.Multiplier, models[0].QuotaCost.CostMultiplier)
}

func TestCommandCodeOfficialDeepSeekWeekendsAndFastModelPricing(t *testing.T) {
	monday := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	for _, model := range []string{"deepseek-v4.1-flash", "deepseek-v4.1-flash-fast"} {
		entry, ok := defaultCommandCodeCatalog.entry(model)
		require.True(t, ok)
		weekday, ok := commandCodeCatalogPricingAt(entry, monday)
		require.True(t, ok)
		for _, day := range []time.Time{monday.AddDate(0, 0, -1), monday.AddDate(0, 0, -2)} {
			weekend, ok := commandCodeCatalogPricingAt(entry, day)
			require.True(t, ok)
			require.InDelta(t, weekday.InputPricePerToken/2, weekend.InputPricePerToken, 1e-12)
			bands := commandCodeReferencePricingTimeBandsAt(model, day)
			require.Len(t, bands, 1)
			require.Equal(t, []string{"00:00-24:00"}, bands[0].TimeRanges)
		}
	}
	pricing, ok := commandCodeReferencePricingAt("claude-sonnet-5-5", monday)
	require.True(t, ok)
	require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)
	quota, ok := (&BillingService{}).GetCommandCodeQuotaCostAt("claude-sonnet-5-5", monday)
	require.True(t, ok)
	require.Equal(t, 10.0, quota.IncludedMonthlyUsageUSD)
	require.Equal(t, 7.0, quota.Multiplier)
	quota, ok = (&BillingService{}).GetCommandCodeQuotaCostAt("glm-5.3-flash", monday)
	require.True(t, ok)
	require.Equal(t, 60.0, quota.IncludedMonthlyUsageUSD)
}
