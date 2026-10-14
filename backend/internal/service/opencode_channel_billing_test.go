//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

func openCodeBillingTestServices(pricing []ChannelModelPricing) (*ChannelService, *BillingService, *Channel) {
	billing := &BillingService{}
	channel := &Channel{ID: 1, Name: "OpenCode", Status: StatusActive, GroupIDs: []int64{1},
		BillingModelSource: BillingModelSourceUpstream, ModelPricing: pricing,
		AccountStatsPricingRules: []AccountStatsPricingRule{{PricingSource: AccountStatsPricingSourceChannel, Platform: PlatformOpenCodeGo}}}
	channels := &ChannelService{billingService: billing}
	channels.cache.Store(populateChannelCache([]Channel{*channel}, map[int64]string{1: PlatformOpenCodeGo}))
	return channels, billing, channel
}

func TestOpenCodeChannelSeedMatchesOfficialPricedCatalog(t *testing.T) {
	require.Len(t, DefaultOpenCodeGoModelIDs(), 31)
	require.Contains(t, DefaultOpenCodeGoModelIDs(), "cat-jam-free")
	require.NoError(t, validatePricingEntries(openCodeDefaultPricing))
	for _, removed := range []string{"omen-alpha", "deepseek-flash", "glm-5.1", "qwen3.7-max", "qwen3.6-plus", "minimax-m2.5", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "gemini-4-argon-contributor"} {
		require.NotContains(t, DefaultOpenCodeGoModelIDs(), removed)
	}
	migration, err := os.ReadFile("../../migrations/246_opencode_channel_billing.sql")
	require.NoError(t, err)
	parts := strings.Split(string(migration), "$seed$")
	require.Len(t, parts, 3)
	var seed []ChannelModelPricing
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &seed))
	update, err := os.ReadFile("../../migrations/247_sync_official_channel_catalogs.sql")
	require.NoError(t, err)
	parts = strings.Split(string(update), "$opencode_additions$")
	require.Len(t, parts, 3)
	var additions []ChannelModelPricing
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &additions))
	removal, err := os.ReadFile("../../migrations/249_refresh_opencode_priced_catalog.sql")
	require.NoError(t, err)
	parts = strings.Split(string(removal), "$retired_models$")
	require.Len(t, parts, 3)
	var retired []string
	require.NoError(t, json.Unmarshal([]byte(parts[1]), &retired))
	current := slices.DeleteFunc(applyFreeModelMigrationForTest(t, append(seed, additions...)), func(pricing ChannelModelPricing) bool {
		return slices.Contains(retired, pricing.Models[0])
	})
	require.ElementsMatch(t, openCodeDefaultPricing, current)
	var routes []struct {
		Models   []string `json:"models"`
		Protocol string   `json:"protocol"`
	}
	require.NoError(t, json.Unmarshal(openCodeChannelPricingJSON, &routes))
	for _, route := range routes {
		require.Equal(t, route.Protocol, matchProtocolRules(route.Models[0], DefaultOpenCodeGoProtocolRules()), route.Models[0])
	}
}

func TestOpenCodeZenCatalogExcludesRetiredAndUnpricedModels(t *testing.T) {
	account := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": "zen"}}
	require.Len(t, OpenCodeDefaultModelIDs(account), 72)
	require.True(t, IsOpenCodeOfficiallyPricedModel(account, "cat-jam-free"))
	for _, model := range []string{"test", "claude-opus-5-5-backup", "muse-spark-1.2-contributor-free", "exo-free", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "qwen3.7-max", "qwen3.7-plus", "ling-3.0-flash-fin-free", "mimo-v2.5-free", "nemotron-3-ultra-free"} {
		require.False(t, IsOpenCodeOfficiallyPricedModel(account, model), model)
	}
}

func TestOpenCodeWithdrawnPricesDoNotUseGenericFallback(t *testing.T) {
	billing := &BillingService{}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "gemini-4-argon-contributor"} {
		_, err := billing.GetModelPricingForPlatform(PlatformOpenCodeGo, model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
	}
}

func TestOpenCodeAccountRuleFollowsChannelChanges(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC)
	entry, ok := openCodeReferencePricing("glm-5.3")
	require.True(t, ok)
	channels, billing, channel := openCodeBillingTestServices([]ChannelModelPricing{*entry})
	groupID := int64(1)
	resolver := NewModelPricingResolver(channels, billing)
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 100_000}
	group := &Group{ID: groupID, Platform: PlatformOpenCodeGo, LongContextPricingEnabled: true,
		ModelPricing: []ChannelModelPricing{{Models: []string{"glm-5.3"}, InputPrice: floatPtr(99e-6)}}}
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "glm-5.3", PricingPlatform: PlatformOpenCodeGo,
		GroupID: &groupID, Group: group, Tokens: tokens, RateMultiplier: 3, PricingAt: at, Resolver: resolver})
	require.NoError(t, err)
	require.InDelta(t, 1.866, cost.TotalCost, 1e-12)
	require.InDelta(t, 4, cost.ModelSpecificMultiplier, 1e-12)
	require.InDelta(t, 1.866*4*3, cost.ActualCost, 1e-12)
	// 入口后处理不能重复应用模型倍率。
	require.NoError(t, billing.applyPlatformQuotaCostAt(cost, PlatformOpenCodeGo, "glm-5.3", at))
	require.InDelta(t, 1.866*4*3, cost.ActualCost, 1e-12)
	stats := resolveAccountStatsCostWithPlatform(ctx, channels, billing, 7, groupID, "glm-5.3", tokens, 1, 999, "", at, PlatformOpenCodeGo, true)
	require.NotNil(t, stats)
	require.InDelta(t, 1.866*4, *stats, 1e-12)
	display := channels.BuildSupportedModelForPricingGroup(ctx, groupID, "glm-5.3", PlatformOpenCodeGo, nil)
	require.NotNil(t, display.QuotaCost)
	require.InDelta(t, 4, display.QuotaCost.CostMultiplier, 1e-12)

	// 修改渠道模型、原价和月额度后，无需单独保存账号规则。
	channel.ModelPricing[0].Models = []string{"glm-custom-priced"}
	channel.ModelPricing[0].InputPrice = floatPtr(2e-6)
	channel.ModelPricing[0].MonthlyQuota.BaseCreditsUSD = 30
	channels.cache.Store(populateChannelCache([]Channel{*channel}, map[int64]string{1: PlatformOpenCodeGo}))
	models, found := channels.OpenCodeChannelModelIDs(ctx, groupID)
	require.True(t, found)
	require.Equal(t, []string{"glm-custom-priced"}, models)
	stats = resolveAccountStatsCostWithPlatform(ctx, channels, billing, 7, groupID, "glm-custom-priced", tokens, 1, 999, "", at, PlatformOpenCodeGo, true)
	require.NotNil(t, stats)
	require.InDelta(t, (2+0.44+0.026)*2, *stats, 1e-12)
	preview := channel.AccountStatsRulePricingAt(channel.AccountStatsPricingRules[0], at)
	require.Equal(t, []string{"glm-custom-priced"}, preview[0].Models)
	require.InDelta(t, 4e-6, *preview[0].InputPrice, 1e-15)
	require.InDelta(t, 2e-6, *channel.ModelPricing[0].InputPrice, 1e-15, "展示不能污染渠道原价")
	channel.ModelPricing = nil
	channels.cache.Store(populateChannelCache([]Channel{*channel}, map[int64]string{1: PlatformOpenCodeGo}))
	models, found = channels.OpenCodeChannelModelIDs(ctx, groupID)
	require.True(t, found)
	require.Empty(t, models)
	require.NotNil(t, models, "空渠道必须返回空列表，不能回退到旧模型")
	require.Empty(t, channel.AccountStatsRulePricingAt(channel.AccountStatsPricingRules[0], at))
	resolved := resolver.Resolve(ctx, PricingInput{Model: "glm-5.3", Platform: PlatformOpenCodeGo, GroupID: &groupID, Group: group})
	require.Equal(t, PricingSourceMissing, resolved.Source, "渠道删除模型后不能回填分组旧价格或内置参考价")
}

func TestOpenCodeAccountRuleUsesRequestTimeContextTiersAndFreeRates(t *testing.T) {
	ctx := context.Background()
	_, billing, channel := openCodeBillingTestServices(openCodeDefaultPricing)
	rule := channel.AccountStatsPricingRules[0]
	for _, test := range []struct {
		name   string
		model  string
		at     time.Time
		tokens UsageTokens
		want   float64
	}{
		{"工作日高峰", "deepseek-v4-pro", time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC), UsageTokens{InputTokens: 1_000_000}, 1.32 * 4},
		{"工作日低峰", "deepseek-v4-pro", time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC), UsageTokens{InputTokens: 1_000_000}, 0.66 * 4},
		{"周末低峰", "deepseek-v4-pro", time.Date(2026, 10, 11, 2, 0, 0, 0, time.UTC), UsageTokens{InputTokens: 1_000_000}, 0.66 * 4},
		{"输出不抬高上下文档位", "claude-haiku-5-5", time.Now(), UsageTokens{InputTokens: 100_000, OutputTokens: 100_000}, (0.01 + 0.05) * 4},
		{"超过上下文边界", "claude-haiku-5-5", time.Now(), UsageTokens{InputTokens: 100_001}, 100_001 * 0.5e-6 * 4},
		{"缓存计入上下文", "claude-haiku-5-5", time.Now(), UsageTokens{InputTokens: 100_000, CacheReadTokens: 1}, (100_000*0.5e-6 + 0.05e-6) * 4},
		{"官方免费模型统一价", "step-5-preview-free", time.Now(), UsageTokens{InputTokens: 1000, OutputTokens: 500}, 0.0000015},
		{"新增官方免费模型统一价", "cat-jam-free", time.Now(), UsageTokens{InputTokens: 1000, OutputTokens: 500}, 0.0000015},
	} {
		t.Run(test.name, func(t *testing.T) {
			cost := tryChannelAccountStatsRule(ctx, billing, channel, rule, 7, 1, PlatformOpenCodeGo, test.model, test.tokens, 1, "", "", test.at)
			require.NotNil(t, cost)
			require.InDelta(t, test.want, *cost, 1e-12)
		})
	}
	require.Nil(t, tryChannelAccountStatsRule(ctx, billing, channel, rule, 7, 2, PlatformOpenCodeGo, "glm-5.3", UsageTokens{InputTokens: 1}, 1, "", "", time.Now()))
	require.Nil(t, tryChannelAccountStatsRule(ctx, billing, channel, rule, 7, 1, PlatformCommandCode, "glm-5.3", UsageTokens{InputTokens: 1}, 1, "", "", time.Now()))
}

func TestGetAvailableModels_OpenCodeExcludesUnpricedModels(t *testing.T) {
	groupID := int64(1)
	for _, test := range []struct {
		name    string
		mapping map[string]any
		want    []string
	}{
		{"保留已定价别名", map[string]any{"priced-alias": "glm-5.3", "unpriced-model": "omen-alpha"}, []string{"priced-alias"}},
		{"全部无价格时返回空目录", map[string]any{"unpriced-model": "omen-alpha"}, []string{}},
		{"不再列出撤价模型别名", map[string]any{"priced-alias": "glm-5.3", "flash-alias": "deepseek-v4-flash", "vision-alias": "deepseek-v4-flash-vision-exp"}, []string{"priced-alias"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &modelsListAccountRepoStub{byGroup: map[int64][]Account{groupID: {{
				Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": test.mapping},
			}}}}
			gateway := &GatewayService{accountRepo: repo, modelsListCache: gocache.New(time.Minute, time.Minute), modelsListCacheTTL: time.Minute}
			for range 2 {
				require.Equal(t, test.want, gateway.GetAvailableModels(context.Background(), &groupID, PlatformOpenCodeGo))
			}
		})
	}
}

func TestOpenCodeUpstreamSyncExcludesModelsWithoutOfficialPricing(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.3"},{"id":"omen-alpha"},{"id":"step-5-preview-free"},{"id":"unknown-free"},{"id":"deepseek-v4-flash"},{"id":"deepseek-v4-flash-vision-exp"},{"id":"gemini-4-argon-contributor"}]}`))}}
	service := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	account := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "fixture", "base_url": DefaultOpenCodeGoBaseURL}}
	models, err := service.FetchUpstreamSupportedModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []string{"glm-5.3", "step-5-preview-free"}, models)
	account.Credentials["account_mode"] = AccountModeZen
	require.True(t, IsOpenCodeOfficiallyPricedModel(account, "claude-fable-5-1"))
	require.False(t, IsOpenCodeOfficiallyPricedModel(account, "unknown-free"))
	require.False(t, IsOpenCodeOfficiallyPricedModel(account, "gemini-3.8-flash"))
}
