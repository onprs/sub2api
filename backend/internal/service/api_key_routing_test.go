//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type apiKeyRoutingAccountRepo struct {
	*mockAccountRepoForPlatform
	byGroup map[int64][]Account
}

func (r *apiKeyRoutingAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	accounts := r.byGroup[groupID]
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func (r *apiKeyRoutingAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, groupID int64, platforms []string) ([]Account, error) {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	accounts := r.byGroup[groupID]
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.Platform]; ok {
			result = append(result, account)
		}
	}
	return result, nil
}

type apiKeyRoutingCacheStub struct {
	*mockGatewayCacheForPlatform
	sticky           map[string]int64
	health           map[int64]APIKeyRoutingHealth
	healthBatchCalls int
	healthBatchSince time.Time
	outcome          map[int64][]bool
}

func (c *apiKeyRoutingCacheStub) GetAPIKeyRoutingGroupID(_ context.Context, _ int64, sessionKey string) (int64, error) {
	groupID, ok := c.sticky[sessionKey]
	if !ok {
		return 0, errors.New("routing binding not found")
	}
	return groupID, nil
}

func (c *apiKeyRoutingCacheStub) SetAPIKeyRoutingGroupID(_ context.Context, _ int64, sessionKey string, groupID int64, _ time.Duration) error {
	if c.sticky == nil {
		c.sticky = make(map[string]int64)
	}
	c.sticky[sessionKey] = groupID
	return nil
}

func (c *apiKeyRoutingCacheStub) GetAPIKeyRoutingHealth(_ context.Context, groupID int64, _ time.Time) (APIKeyRoutingHealth, error) {
	return c.health[groupID], nil
}

func (c *apiKeyRoutingCacheStub) GetAPIKeyRoutingHealthBatch(_ context.Context, groupIDs []int64, since time.Time) (map[int64]APIKeyRoutingHealth, error) {
	c.healthBatchCalls++
	c.healthBatchSince = since
	result := make(map[int64]APIKeyRoutingHealth, len(groupIDs))
	for _, groupID := range groupIDs {
		result[groupID] = c.health[groupID]
	}
	return result, nil
}

func (c *apiKeyRoutingCacheStub) RecordAPIKeyRoutingOutcome(_ context.Context, groupID int64, success bool, _ *int64, _ time.Time) error {
	if c.outcome == nil {
		c.outcome = make(map[int64][]bool)
	}
	c.outcome[groupID] = append(c.outcome[groupID], success)
	return nil
}

type apiKeyRoutingHealthProviderStub struct {
	snapshots []APIKeyRoutingHealthSnapshot
	calls     int
}

func (p *apiKeyRoutingHealthProviderStub) GetAPIKeyRoutingHealthSnapshots(_ context.Context, _ []int64) []APIKeyRoutingHealthSnapshot {
	p.calls++
	return append([]APIKeyRoutingHealthSnapshot(nil), p.snapshots...)
}

type apiKeyRoutingSubscriptionRepo struct {
	userSubRepoNoop
	active []UserSubscription
}

func (r apiKeyRoutingSubscriptionRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return append([]UserSubscription(nil), r.active...), nil
}

func (r apiKeyRoutingSubscriptionRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	for i := range r.active {
		if r.active[i].UserID == userID && r.active[i].GroupID == groupID {
			subscription := r.active[i]
			return &subscription, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func newAPIKeyRoutingTestService(accounts map[int64][]Account, cache *apiKeyRoutingCacheStub) *GatewayService {
	if cache == nil {
		cache = &apiKeyRoutingCacheStub{}
	}
	if cache.mockGatewayCacheForPlatform == nil {
		cache.mockGatewayCacheForPlatform = &mockGatewayCacheForPlatform{}
	}
	return &GatewayService{
		accountRepo: &apiKeyRoutingAccountRepo{
			mockAccountRepoForPlatform: &mockAccountRepoForPlatform{},
			byGroup:                    accounts,
		},
		cache: cache,
		cfg:   testConfig(),
	}
}

func newAPIKeyRoutingTestGroup(id int64) *Group {
	return &Group{
		ID:                           id,
		Name:                         "routing group",
		Platform:                     PlatformOpenAI,
		Status:                       StatusActive,
		SubscriptionType:             SubscriptionTypeStandard,
		RateMultiplier:               1,
		AllowImageGeneration:         true,
		AllowBatchImageGeneration:    true,
		BatchImageDiscountMultiplier: 1,
	}
}

func newAPIKeyRoutingTestAccount(id int64, model string) Account {
	account := Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
	}
	if model != "" {
		account.Credentials = map[string]any{
			"model_mapping": map[string]any{model: model},
		}
	}
	return account
}

func newAPIKeyRoutingTestKey(strategy string, groups ...*Group) *APIKey {
	bindings := make([]APIKeyGroupBinding, 0, len(groups))
	for i, group := range groups {
		bindings = append(bindings, APIKeyGroupBinding{
			GroupID:  group.ID,
			Priority: i,
			Group:    group,
		})
	}
	primaryID := groups[0].ID
	return &APIKey{
		ID:              101,
		UserID:          202,
		GroupID:         &primaryID,
		Group:           groups[0],
		RoutingPlatform: groups[0].Platform,
		RoutingStrategy: strategy,
		RoutingGroups:   bindings,
		Status:          StatusActive,
		User: &User{
			ID:      202,
			Status:  StatusActive,
			Balance: 100,
		},
	}
}

func TestResolveAPIKeyRoutingGroup_ManualUsesConfiguredBoundaryAndPriority(t *testing.T) {
	first := newAPIKeyRoutingTestGroup(11)
	second := newAPIKeyRoutingTestGroup(12)
	externalFallbackID := int64(99)
	second.FallbackGroupID = &externalFallbackID
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	key.RoutingGroups[0].Priority = 20
	key.RoutingGroups[1].Priority = 10

	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		first.ID:  {newAPIKeyRoutingTestAccount(111, "gpt-route")},
		second.ID: {newAPIKeyRoutingTestAccount(112, "gpt-route")},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.NotSame(t, key, got)
	require.Equal(t, second.ID, *got.GroupID)
	require.Nil(t, got.Group.FallbackGroupID, "未配置的普通兜底分组不能越过候选边界")
	require.Equal(t, first.ID, *key.GroupID, "请求级选择不能污染认证快照")
}

func TestResolveAPIKeyRoutingGroup_FiltersPermissionSubscriptionAndCapability(t *testing.T) {
	unauthorized := newAPIKeyRoutingTestGroup(21)
	unauthorized.IsExclusive = true
	subscription := newAPIKeyRoutingTestGroup(22)
	subscription.SubscriptionType = SubscriptionTypeSubscription
	mediaDisabled := newAPIKeyRoutingTestGroup(23)
	mediaDisabled.AllowImageGeneration = false
	eligible := newAPIKeyRoutingTestGroup(24)

	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, unauthorized, subscription, mediaDisabled, eligible)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		unauthorized.ID:  {newAPIKeyRoutingTestAccount(121, "gpt-image")},
		subscription.ID:  {newAPIKeyRoutingTestAccount(122, "gpt-image")},
		mediaDisabled.ID: {newAPIKeyRoutingTestAccount(123, "gpt-image")},
		eligible.ID:      {newAPIKeyRoutingTestAccount(124, "gpt-image")},
	}, nil)
	svc.userSubRepo = apiKeyRoutingSubscriptionRepo{}

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-image",
		Capability: APIKeyRoutingCapabilityImage,
	})
	require.NoError(t, err)
	require.Equal(t, eligible.ID, *got.GroupID)

	// 视频与图片使用同一媒体开关；批量图片使用独立批处理权限。
	mediaDisabled.AllowBatchImageGeneration = false
	eligible.AllowBatchImageGeneration = false
	_, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-image",
		Capability: APIKeyRoutingCapabilityBatchImage,
	})
	require.ErrorIs(t, err, ErrNoAvailableRoutingGroup)
}

func TestResolveAPIKeyRoutingGroup_SubscriptionCandidateRequiresActiveSubscription(t *testing.T) {
	subscription := newAPIKeyRoutingTestGroup(31)
	subscription.SubscriptionType = SubscriptionTypeSubscription
	standard := newAPIKeyRoutingTestGroup(32)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, subscription, standard)
	accounts := map[int64][]Account{
		subscription.ID: {newAPIKeyRoutingTestAccount(131, "gpt-route")},
		standard.ID:     {newAPIKeyRoutingTestAccount(132, "gpt-route")},
	}

	svc := newAPIKeyRoutingTestService(accounts, nil)
	svc.userSubRepo = apiKeyRoutingSubscriptionRepo{}
	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, standard.ID, *got.GroupID)

	now := time.Now()
	limit := 1.0
	subscriptionSnapshot := UserSubscription{
		UserID:              key.UserID,
		GroupID:             subscription.ID,
		Status:              SubscriptionStatusActive,
		StartsAt:            now.Add(-time.Hour),
		ExpiresAt:           now.Add(time.Hour),
		FiveHourLimitUSD:    &limit,
		FiveHourUsageUSD:    limit,
		FiveHourWindowStart: &now,
	}
	svc.userSubRepo = apiKeyRoutingSubscriptionRepo{active: []UserSubscription{subscriptionSnapshot}}
	got, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, standard.ID, *got.GroupID, "滚动配额耗尽的订阅候选必须在排名前剔除")

	subscriptionSnapshot.FiveHourUsageUSD = 0
	svc.userSubRepo = apiKeyRoutingSubscriptionRepo{active: []UserSubscription{subscriptionSnapshot}}
	got, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, subscription.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_PrefersDirectModelSupportBeforeStrategy(t *testing.T) {
	manualFirst := newAPIKeyRoutingTestGroup(41)
	direct := newAPIKeyRoutingTestGroup(42)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, manualFirst, direct)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		manualFirst.ID: {newAPIKeyRoutingTestAccount(141, "other-model")},
		direct.ID:      {newAPIKeyRoutingTestAccount(142, "requested-model")},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "requested-model"})
	require.NoError(t, err)
	require.Equal(t, direct.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_SkipsQuotaPausedGroup(t *testing.T) {
	quotaPaused := newAPIKeyRoutingTestGroup(421)
	healthy := newAPIKeyRoutingTestGroup(422)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, quotaPaused, healthy)
	pausedAccount := newAPIKeyRoutingTestAccount(1421, "gpt-route")
	pausedAccount.Extra = map[string]any{
		"codex_5h_used_percent":   96.0,
		"auto_pause_5h_threshold": 0.95,
	}
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		quotaPaused.ID: {pausedAccount},
		healthy.ID:     {newAPIKeyRoutingTestAccount(1422, "gpt-route")},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, healthy.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_SkipsQuotaPausedGroupWithGlobalThreshold(t *testing.T) {
	quotaPaused := newAPIKeyRoutingTestGroup(421)
	healthy := newAPIKeyRoutingTestGroup(422)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, quotaPaused, healthy)
	pausedAccount := newAPIKeyRoutingTestAccount(1421, "gpt-route")
	pausedAccount.Extra = map[string]any{"codex_5h_used_percent": 96.0}
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		quotaPaused.ID: {pausedAccount},
		healthy.ID:     {newAPIKeyRoutingTestAccount(1422, "gpt-route")},
	}, nil)
	svc.settingService = &SettingService{}
	svc.settingService.SetOpenAIQuotaAutoPauseSettings(OpsOpenAIAccountQuotaAutoPauseSettings{
		DefaultThreshold5h: 0.95,
	})

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, healthy.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_SkipsQuotaPausedGroupWithForcedOpenAIPlatform(t *testing.T) {
	quotaPaused := newAPIKeyRoutingTestGroup(425)
	healthy := newAPIKeyRoutingTestGroup(426)
	quotaPaused.Platform = PlatformAnthropic
	healthy.Platform = PlatformAnthropic
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, quotaPaused, healthy)

	pausedAccount := newAPIKeyRoutingTestAccount(1425, "gpt-route")
	pausedAccount.Extra = map[string]any{"codex_5h_used_percent": 96.0}
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		quotaPaused.ID: {pausedAccount},
		healthy.ID:     {newAPIKeyRoutingTestAccount(1426, "gpt-route")},
	}, nil)
	svc.settingService = &SettingService{}
	svc.settingService.SetOpenAIQuotaAutoPauseSettings(OpsOpenAIAccountQuotaAutoPauseSettings{
		DefaultThreshold5h: 0.95,
	})

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:         "gpt-route",
		ForcePlatform: PlatformOpenAI,
	})
	require.NoError(t, err)
	require.Equal(t, healthy.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_SkipsExhaustedGrokGroup(t *testing.T) {
	quotaPaused := newAPIKeyRoutingTestGroup(423)
	healthy := newAPIKeyRoutingTestGroup(424)
	quotaPaused.Platform = PlatformGrok
	healthy.Platform = PlatformGrok
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, quotaPaused, healthy)

	limit, remaining := int64(100), int64(0)
	resetUnix := time.Now().Add(time.Hour).Unix()
	pausedAccount := newAPIKeyRoutingTestAccount(1423, "grok-route")
	pausedAccount.Platform = PlatformGrok
	pausedAccount.Type = AccountTypeOAuth
	pausedAccount.Extra = map[string]any{
		grokQuotaSnapshotExtraKey: xai.QuotaSnapshot{
			Requests:  &xai.QuotaWindow{Limit: &limit, Remaining: &remaining, ResetUnix: &resetUnix},
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		},
	}
	healthyAccount := newAPIKeyRoutingTestAccount(1424, "grok-route")
	healthyAccount.Platform = PlatformGrok
	healthyAccount.Type = AccountTypeOAuth
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		quotaPaused.ID: {pausedAccount},
		healthy.ID:     {healthyAccount},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "grok-route"})
	require.NoError(t, err)
	require.Equal(t, healthy.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_MessagesIgnoresLegacyDispatchGate(t *testing.T) {
	first := newAPIKeyRoutingTestGroup(43)
	second := newAPIKeyRoutingTestGroup(44)
	first.AllowMessagesDispatch = false
	second.AllowMessagesDispatch = false
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		first.ID:  {newAPIKeyRoutingTestAccount(143, "gpt-5.4")},
		second.ID: {newAPIKeyRoutingTestAccount(144, "gpt-5.4")},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-5.4",
		Capability: APIKeyRoutingCapabilityMessages,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_FiltersAccountRestrictions(t *testing.T) {
	privacyRequired := newAPIKeyRoutingTestGroup(45)
	privacyRequired.RequirePrivacySet = true
	oauthRequired := newAPIKeyRoutingTestGroup(46)
	oauthRequired.RequireOAuthOnly = true
	eligible := newAPIKeyRoutingTestGroup(47)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, privacyRequired, oauthRequired, eligible)
	eligibleAccount := newAPIKeyRoutingTestAccount(147, "gpt-5.4")
	eligibleAccount.Type = AccountTypeOAuth
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		privacyRequired.ID: {newAPIKeyRoutingTestAccount(145, "gpt-5.4")},
		oauthRequired.ID:   {newAPIKeyRoutingTestAccount(146, "gpt-5.4")},
		eligible.ID:        {eligibleAccount},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-5.4"})
	require.NoError(t, err)
	require.Equal(t, eligible.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_FiltersEndpointAndTransportRestrictions(t *testing.T) {
	embeddingsUnavailable := newAPIKeyRoutingTestGroup(471)
	embeddingsAvailable := newAPIKeyRoutingTestGroup(472)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, embeddingsUnavailable, embeddingsAvailable)
	oauthAccount := newAPIKeyRoutingTestAccount(1471, "text-embedding-3-small")
	oauthAccount.Type = AccountTypeOAuth
	apiKeyAccount := newAPIKeyRoutingTestAccount(1472, "text-embedding-3-small")
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		embeddingsUnavailable.ID: {oauthAccount},
		embeddingsAvailable.ID:   {apiKeyAccount},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:                      "text-embedding-3-small",
		RequiredEndpointCapability: OpenAIEndpointCapabilityEmbeddings,
	})
	require.NoError(t, err)
	require.Equal(t, embeddingsAvailable.ID, *got.GroupID)

	wsDisabled := newAPIKeyRoutingTestGroup(473)
	wsEnabled := newAPIKeyRoutingTestGroup(474)
	key = newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, wsDisabled, wsEnabled)
	disabledAccount := newAPIKeyRoutingTestAccount(1473, "gpt-5.4")
	disabledAccount.Extra = map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeOff}
	enabledAccount := newAPIKeyRoutingTestAccount(1474, "gpt-5.4")
	enabledAccount.Extra = map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeHTTPBridge}
	svc = newAPIKeyRoutingTestService(map[int64][]Account{
		wsDisabled.ID: {disabledAccount},
		wsEnabled.ID:  {enabledAccount},
	}, nil)
	svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true

	got, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:                      "gpt-5.4",
		RequiredEndpointCapability: OpenAIEndpointCapabilityChatCompletions,
		RequiredTransport:          OpenAIUpstreamTransportResponsesWebsocketV2Ingress,
	})
	require.NoError(t, err)
	require.Equal(t, wsEnabled.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_ExcludesClaudeCodeOnlyCandidateOutsideClaudeCode(t *testing.T) {
	claudeCodeOnly := newAPIKeyRoutingTestGroup(48)
	outsideCandidateID := int64(999)
	claudeCodeOnly.ClaudeCodeOnly = true
	claudeCodeOnly.FallbackGroupID = &outsideCandidateID
	eligible := newAPIKeyRoutingTestGroup(49)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, claudeCodeOnly, eligible)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		claudeCodeOnly.ID: {newAPIKeyRoutingTestAccount(148, "gpt-5.4")},
		eligible.ID:       {newAPIKeyRoutingTestAccount(149, "gpt-5.4")},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-5.4"})
	require.NoError(t, err)
	require.Equal(t, eligible.ID, *got.GroupID)
}

func TestRoutingImageUnitPrice_UsesSharedBillingTierClassification(t *testing.T) {
	group := newAPIKeyRoutingTestGroup(50)
	group.ImagePrice1K = ptr(1.25)
	group.ImagePrice2K = ptr(2.5)

	require.Equal(t, 1.25, *routingImageUnitPrice(group, "1024x1024"))
	require.Equal(t, 2.5, *routingImageUnitPrice(group, "1536x1024"))
	require.Nil(t, routingImageUnitPrice(group, "auto"))
}

func TestResolveAPIKeyRoutingGroup_CostIncludesBatchDiscountAndMediaPrice(t *testing.T) {
	lowerRawRate := newAPIKeyRoutingTestGroup(51)
	lowerRawRate.Platform = PlatformGemini
	lowerRawRate.RateMultiplier = 0.4
	lowerRawRate.BatchImageDiscountMultiplier = 2
	lowerRawRate.ImagePrice1K = ptr(2.0)
	lowerEffectiveCost := newAPIKeyRoutingTestGroup(52)
	lowerEffectiveCost.Platform = PlatformGemini
	lowerEffectiveCost.RateMultiplier = 0.5
	lowerEffectiveCost.BatchImageDiscountMultiplier = 0.5
	lowerEffectiveCost.ImagePrice1K = ptr(1.0)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyCostFirst, lowerRawRate, lowerEffectiveCost)
	firstAccount := newAPIKeyRoutingTestAccount(151, "image-model")
	firstAccount.Platform = PlatformGemini
	secondAccount := newAPIKeyRoutingTestAccount(152, "image-model")
	secondAccount.Platform = PlatformGemini
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		lowerRawRate.ID:       {firstAccount},
		lowerEffectiveCost.ID: {secondAccount},
	}, nil)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "image-model",
		Capability: APIKeyRoutingCapabilityBatchImage,
		MediaSize:  "1K",
	})
	require.NoError(t, err)
	require.Equal(t, lowerEffectiveCost.ID, *got.GroupID)
}

func TestResolveAPIKeyRoutingGroup_StabilityUsesRedisAndStickySession(t *testing.T) {
	unstable := newAPIKeyRoutingTestGroup(61)
	stable := newAPIKeyRoutingTestGroup(62)
	cache := &apiKeyRoutingCacheStub{
		health: map[int64]APIKeyRoutingHealth{
			unstable.ID: {Failure: 100},
			stable.ID:   {Success: 100},
		},
		sticky: map[string]int64{},
	}
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyStabilityFirst, unstable, stable)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		unstable.ID: {newAPIKeyRoutingTestAccount(161, "gpt-route")},
		stable.ID:   {newAPIKeyRoutingTestAccount(162, "gpt-route")},
	}, cache)
	// Channel Monitor 故意给出相反结论；实际调度必须继续服从 Redis 近期结果。
	monitorUnstableRate := 100.0
	monitorStableRate := 0.0
	provider := &apiKeyRoutingHealthProviderStub{snapshots: []APIKeyRoutingHealthSnapshot{
		{GroupID: unstable.ID, Status: APIKeyRoutingHealthStatusOperational, SuccessRate: &monitorUnstableRate, SampleCount: 100},
		{GroupID: stable.ID, Status: APIKeyRoutingHealthStatusFailed, SuccessRate: &monitorStableRate, SampleCount: 100},
	}}
	svc.SetAPIKeyRoutingHealthProvider(provider)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-route",
		SessionKey: "conversation-1",
	})
	require.NoError(t, err)
	require.Equal(t, stable.ID, *got.GroupID)
	require.Equal(t, stable.ID, cache.sticky["conversation-1"])

	// 已有绑定优先于当前策略，但仍只能命中本次过滤后仍合格的候选。
	cache.sticky["conversation-2"] = unstable.ID
	got, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-route",
		SessionKey: "conversation-2",
	})
	require.NoError(t, err)
	require.Equal(t, unstable.ID, *got.GroupID)

	unstable.Status = StatusDisabled
	got, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{
		Model:      "gpt-route",
		SessionKey: "conversation-2",
	})
	require.NoError(t, err)
	require.Equal(t, stable.ID, *got.GroupID)
	require.Zero(t, provider.calls)
	require.Equal(t, 3, cache.healthBatchCalls)
	require.WithinDuration(t, time.Now().Add(-30*time.Minute), cache.healthBatchSince, 5*time.Second)
}

func TestResolveAPIKeyRoutingGroup_ManualDoesNotLoadChannelMonitorHealth(t *testing.T) {
	first := newAPIKeyRoutingTestGroup(63)
	second := newAPIKeyRoutingTestGroup(64)
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		first.ID:  {newAPIKeyRoutingTestAccount(163, "gpt-route")},
		second.ID: {newAPIKeyRoutingTestAccount(164, "gpt-route")},
	}, nil)
	provider := &apiKeyRoutingHealthProviderStub{}
	svc.SetAPIKeyRoutingHealthProvider(provider)

	got, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "gpt-route"})
	require.NoError(t, err)
	require.Equal(t, first.ID, *got.GroupID)
	require.Zero(t, provider.calls)
}

func TestGetAPIKeyRoutingHealthSnapshots_UsesChannelMonitorProviderWithoutCacheFallback(t *testing.T) {
	observedAt := time.Now().UTC().Truncate(time.Millisecond)
	successRate := 90.0
	latency := int64(250)
	cache := &apiKeyRoutingCacheStub{health: map[int64]APIKeyRoutingHealth{
		92: {Success: 10},
	}}
	svc := newAPIKeyRoutingTestService(nil, cache)
	svc.SetAPIKeyRoutingHealthProvider(&apiKeyRoutingHealthProviderStub{snapshots: []APIKeyRoutingHealthSnapshot{
		{
			GroupID:          91,
			Status:           APIKeyRoutingHealthStatusDegraded,
			SuccessRate:      &successRate,
			AverageLatencyMs: &latency,
			SampleCount:      20,
			LastObservedAt:   &observedAt,
		},
		{GroupID: 93, Status: APIKeyRoutingHealthStatusFailed},
	}})

	snapshots := svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{91, 92, 91, -1, 93})
	require.Len(t, snapshots, 3)
	require.Equal(t, int64(91), snapshots[0].GroupID)
	require.Equal(t, APIKeyRoutingHealthStatusDegraded, snapshots[0].Status)
	require.Equal(t, 90.0, *snapshots[0].SuccessRate)
	require.Equal(t, int64(250), *snapshots[0].AverageLatencyMs)
	require.Equal(t, int64(20), snapshots[0].SampleCount)
	require.Equal(t, observedAt, *snapshots[0].LastObservedAt)

	// 提供者未返回 92，即使 Redis 中仍有旧路由样本也必须展示为无数据。
	require.Equal(t, APIKeyRoutingHealthStatusUnknown, snapshots[1].Status)
	require.Nil(t, snapshots[1].SuccessRate)
	require.Nil(t, snapshots[1].AverageLatencyMs)
	require.Equal(t, APIKeyRoutingHealthStatusFailed, snapshots[2].Status)
	require.Zero(t, cache.healthBatchCalls, "编辑器展示不得读取 Redis 路由样本")
}

func TestOpenAIWSStateStore_BindsResponseIDToAPIKeyRoutingGroup(t *testing.T) {
	cache := &apiKeyRoutingCacheStub{
		mockGatewayCacheForPlatform: &mockGatewayCacheForPlatform{},
		sticky:                      map[string]int64{},
	}
	store := NewOpenAIWSStateStore(cache)
	ctx := context.WithValue(context.Background(), ctxkey.APIKeyRoutingAPIKeyID, int64(101))

	err := store.BindResponseAccount(ctx, 62, "resp_routing_1", 162, time.Hour)
	require.NoError(t, err)
	require.Equal(t, int64(62), cache.sticky["resp_routing_1"])
}

func TestAPIKeyCloneWithEffectiveGroup_UsesCandidateRPMOverride(t *testing.T) {
	first := newAPIKeyRoutingTestGroup(71)
	second := newAPIKeyRoutingTestGroup(72)
	staleOverride := 90
	selectedOverride := 12
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	key.User.UserGroupRPMOverride = &staleOverride
	key.RoutingGroups[1].RPMOverride = &selectedOverride

	firstClone := key.CloneWithEffectiveGroup(first)
	require.Nil(t, firstClone.User.UserGroupRPMOverride)
	require.NotSame(t, key.User, firstClone.User)
	require.Equal(t, staleOverride, *key.User.UserGroupRPMOverride)

	secondClone := key.CloneWithEffectiveGroup(second)
	require.NotNil(t, secondClone.User.UserGroupRPMOverride)
	require.Equal(t, selectedOverride, *secondClone.User.UserGroupRPMOverride)
}

func TestResolveRoutingConfiguration_ValidatesAndNormalizesCompleteConfiguration(t *testing.T) {
	first := newAPIKeyRoutingTestGroup(81)
	second := newAPIKeyRoutingTestGroup(82)
	second.SubscriptionType = SubscriptionTypeSubscription
	otherPlatform := newAPIKeyRoutingTestGroup(83)
	otherPlatform.Platform = PlatformGemini
	disabled := newAPIKeyRoutingTestGroup(84)
	disabled.Status = StatusDisabled
	exclusive := newAPIKeyRoutingTestGroup(85)
	exclusive.IsExclusive = true

	user := &User{ID: 901, Status: StatusActive, Balance: 10}
	svc := &APIKeyService{
		groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{
			first.ID:         first,
			second.ID:        second,
			otherPlatform.ID: otherPlatform,
			disabled.ID:      disabled,
			exclusive.ID:     exclusive,
		}},
		userSubRepo: apiKeyRoutingSubscriptionRepo{active: []UserSubscription{{
			UserID:  user.ID,
			GroupID: second.ID,
		}}},
	}

	platform, strategy, groups, primaryID, primaryGroup, err := svc.resolveRoutingConfiguration(
		context.Background(),
		user,
		&APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyBalanced,
			Groups: []APIKeyRoutingGroupRequest{
				{GroupID: first.ID, Priority: 9},
				{GroupID: second.ID, Priority: 2},
			},
		},
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, platform)
	require.Equal(t, APIKeyRoutingStrategyBalanced, strategy)
	require.Equal(t, second.ID, *primaryID)
	require.Same(t, second, primaryGroup)
	require.Equal(t, []int{0, 1}, []int{groups[0].Priority, groups[1].Priority})
	require.Equal(t, []int64{second.ID, first.ID}, []int64{groups[0].GroupID, groups[1].GroupID})

	t.Run("legacy group_id becomes one-group manual routing", func(t *testing.T) {
		platform, strategy, groups, primaryID, _, legacyErr := svc.resolveRoutingConfiguration(context.Background(), user, nil, &first.ID)
		require.NoError(t, legacyErr)
		require.Equal(t, first.Platform, platform)
		require.Equal(t, APIKeyRoutingStrategyManual, strategy)
		require.Equal(t, first.ID, *primaryID)
		require.Len(t, groups, 1)
	})

	t.Run("mixed platform is rejected", func(t *testing.T) {
		_, _, _, _, _, mixedErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups: []APIKeyRoutingGroupRequest{
				{GroupID: first.ID},
				{GroupID: otherPlatform.ID, Priority: 1},
			},
		}, nil)
		require.ErrorIs(t, mixedErr, ErrRoutingPlatformMix)
	})

	t.Run("duplicates and invalid priorities are rejected", func(t *testing.T) {
		_, _, _, _, _, duplicateErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups: []APIKeyRoutingGroupRequest{
				{GroupID: first.ID},
				{GroupID: first.ID, Priority: 1},
			},
		}, nil)
		require.ErrorIs(t, duplicateErr, ErrInvalidKeyRouting)

		_, _, _, _, _, priorityErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups:   []APIKeyRoutingGroupRequest{{GroupID: first.ID, Priority: -1}},
		}, nil)
		require.ErrorIs(t, priorityErr, ErrInvalidKeyRouting)
	})

	t.Run("inactive and unauthorized exclusive groups are rejected", func(t *testing.T) {
		_, _, _, _, _, inactiveErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups:   []APIKeyRoutingGroupRequest{{GroupID: disabled.ID}},
		}, nil)
		require.ErrorIs(t, inactiveErr, ErrInvalidKeyRouting)

		_, _, _, _, _, permissionErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups:   []APIKeyRoutingGroupRequest{{GroupID: exclusive.ID}},
		}, nil)
		require.ErrorIs(t, permissionErr, ErrGroupNotAllowed)
	})

	t.Run("candidate count is bounded", func(t *testing.T) {
		requests := make([]APIKeyRoutingGroupRequest, APIKeyRoutingMaxGroups+1)
		_, _, _, _, _, limitErr := svc.resolveRoutingConfiguration(context.Background(), user, &APIKeyRoutingRequest{
			Platform: PlatformOpenAI,
			Strategy: APIKeyRoutingStrategyManual,
			Groups:   requests,
		}, nil)
		require.ErrorIs(t, limitErr, ErrRoutingGroupLimit)
	})
}
