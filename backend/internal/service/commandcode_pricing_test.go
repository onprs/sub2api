package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandCodeReferencePricingCoversOfficialRates(t *testing.T) {
	offPeak := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	pricing, ok := commandCodeReferencePricingAt("claude-sonnet-5", offPeak)
	require.True(t, ok)
	require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 10e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.2e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 2.5e-6, pricing.CacheCreationPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("deepseek/deepseek-v4-flash", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.22e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.66e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.007e-6, pricing.CacheReadPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("Qwen/Qwen3.7-Max", offPeak)
	require.True(t, ok)
	require.InDelta(t, 2.5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 3.13e-6, pricing.CacheCreationPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("Qwen/Qwen3.7-Flash", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.03e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.13e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.006e-6, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, 0.038e-6, pricing.CacheCreationPricePerToken, 1e-12)

	pricing, ok = commandCodeReferencePricingAt("xiaomi/mimo-v2.5-pro", offPeak)
	require.True(t, ok)
	require.InDelta(t, 0.435e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.87e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.0036e-6, pricing.CacheReadPricePerToken, 1e-12)
}

func TestCommandCodeReferencePricingResolvesAliasesAndCase(t *testing.T) {
	offPeak := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	for _, model := range []string{
		"deepseek-v4-flash",
		"DeepSeek/DeepSeek-V4-Flash",
		"kimi-k3",
		"moonshotai/Kimi-K3",
		"claude-haiku-4-5",
		"qwen-3.7-plus",
		"qwen3.7-plus",
		"grok-4.6",
		"ox-alpha",
	} {
		_, ok := commandCodeReferencePricingAt(model, offPeak)
		require.True(t, ok, "expected pricing for %q", model)
	}

	_, ok := commandCodeReferencePricingAt("totally-unknown-model", offPeak)
	require.False(t, ok)
}

func TestCommandCodeReferencePricingAppliesDeepSeekPeakWindow(t *testing.T) {
	base := time.Date(2026, 8, 26, 0, 30, 0, 0, time.UTC)

	offPeak, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-pro", base)
	require.True(t, ok)
	require.InDelta(t, 0.66e-6, offPeak.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.98e-6, offPeak.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.022e-6, offPeak.CacheReadPricePerToken, 1e-12)

	// UTC 01:00–04:00 为峰时（×2）。
	peak, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-pro", base.Add(45*time.Minute))
	require.True(t, ok)
	require.InDelta(t, 1.32e-6, peak.InputPricePerToken, 1e-12)
	require.InDelta(t, 3.96e-6, peak.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.044e-6, peak.CacheReadPricePerToken, 1e-12)

	// UTC 06:00–10:00 同样为峰时。
	peak2, ok := commandCodeReferencePricingAt("deepseek/deepseek-v4-flash-vision-exp", base.Add(6*time.Hour))
	require.True(t, ok)
	require.InDelta(t, 0.44e-6, peak2.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.32e-6, peak2.OutputPricePerToken, 1e-12)

	// 非 DeepSeek 模型不受峰时影响。
	other, ok := commandCodeReferencePricingAt("claude-sonnet-5", base.Add(90*time.Minute))
	require.True(t, ok)
	require.InDelta(t, 2e-6, other.InputPricePerToken, 1e-12)
}

func TestCommandCodeReferencePricingGemini37FlashDealExpiry(t *testing.T) {
	before := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	pricing, ok := commandCodeReferencePricingAt("google/gemini-3.7-flash", before)
	require.True(t, ok)
	require.InDelta(t, 0.75e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 3.75e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.04167e-6, pricing.CacheCreationPricePerToken, 1e-12)

	after := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	pricing, ok = commandCodeReferencePricingAt("google/gemini-3.7-flash", after)
	require.True(t, ok)
	require.InDelta(t, 1.5e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.08334e-6, pricing.CacheCreationPricePerToken, 1e-12)
}

func TestCommandCodeReferencePricingZeroRatesAndPromos(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

	free, ok := commandCodeReferencePricingAt("stealth/ox-alpha", now)
	require.True(t, ok)
	require.True(t, free.AllowZeroRate)
	require.InDelta(t, 0.0, free.InputPricePerToken, 1e-12)

	free, ok = commandCodeReferencePricingAt("minimax/minimax-m3-free", now)
	require.True(t, ok)
	require.True(t, free.AllowZeroRate)

	free, ok = commandCodeReferencePricingAt("ling/ling-3.0-flash", now)
	require.True(t, ok)
	require.True(t, free.AllowZeroRate)

	free, ok = commandCodeReferencePricingAt("poolside/laguna-s-2.1-free", now)
	require.True(t, ok)
	require.True(t, free.AllowZeroRate)

	// minimax 免费模型到期后恢复收费。
	afterExpiry := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	paid, ok := commandCodeReferencePricingAt("minimax/minimax-m3-free", afterExpiry)
	require.True(t, ok)
	require.False(t, paid.AllowZeroRate)
	require.InDelta(t, 0.3e-6, paid.InputPricePerToken, 1e-12)
	require.InDelta(t, 1.2e-6, paid.OutputPricePerToken, 1e-12)
}

func TestBillingServiceCommandCodePlatformPricingFailsClosed(t *testing.T) {
	svc := &BillingService{}
	pricing, err := svc.GetModelPricingForPlatform(PlatformCommandCode, "claude-sonnet-5")
	require.NoError(t, err)
	require.NotNil(t, pricing)
	require.InDelta(t, 2e-6, pricing.InputPricePerToken, 1e-12)

	_, err = svc.GetModelPricingForPlatform(PlatformCommandCode, "unknown-model")
	require.Error(t, err)

	// 通用平台解析不应命中 Command Code 平台隔离表。
	_, err = svc.GetModelPricing("unknown-model")
	require.Error(t, err)
}

func TestGatewayServiceValidateGatewayTokenPricingAvailable_CommandCode(t *testing.T) {
	gateway := &GatewayService{billingService: &BillingService{}}
	account := &Account{ID: 201, Platform: PlatformCommandCode, Type: AccountTypeAPIKey}

	// 1. Priced model succeeds
	err := gateway.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "claude-sonnet-5", ChannelMappingResult{})
	require.NoError(t, err)

	// 2. Unpriced model fails closed
	err = gateway.ValidateGatewayTokenPricingAvailable(context.Background(), &APIKey{ID: 1}, account, "totally-unknown-model", ChannelMappingResult{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrModelPricingUnavailable))
}
