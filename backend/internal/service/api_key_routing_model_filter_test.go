//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyRoutingModelFilter_RequiresEligibleAccountForRequestedModel(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformOpenCodeGo, PlatformClinePass, PlatformOpenRouter, PlatformCommandCode} {
		t.Run(platform, func(t *testing.T) {
			first, second := newAPIKeyRoutingTestGroup(11), newAPIKeyRoutingTestGroup(12)
			first.Platform, second.Platform = platform, platform
			wrong := newAPIKeyRoutingTestAccount(101, "other-model")
			blocked := newAPIKeyRoutingTestAccount(102, "requested-model")
			blocked.Extra = map[string]any{"model_rate_limits": map[string]any{"requested-model": map[string]any{"rate_limit_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)}}}
			eligible := newAPIKeyRoutingTestAccount(103, "requested-model")
			wrong.Platform, blocked.Platform, eligible.Platform = platform, platform, platform
			key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
			svc := newAPIKeyRoutingTestService(map[int64][]Account{first.ID: {wrong, blocked}, second.ID: {eligible}}, nil)
			input := APIKeyRoutingResolveInput{Model: "requested-model"}
			selected, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
			require.NoError(t, err)
			require.Equal(t, second.ID, *selected.GroupID)

			input.ExcludedGroupIDs = map[int64]struct{}{second.ID: {}}
			_, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
			require.ErrorIs(t, err, ErrNoAvailableRoutingGroup, "不能把只支持其它模型的分组作为兜底")
		})
	}
}

func TestAPIKeyRoutingModelFilter_CompositeChecksEachGroupsUpstreamModel(t *testing.T) {
	first, second := newAPIKeyRoutingTestGroup(51), newAPIKeyRoutingTestGroup(52)
	first.Platform, second.Platform = PlatformComposite, PlatformComposite
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	wrong := newAPIKeyRoutingTestAccount(501, "other-model")
	eligible := newAPIKeyRoutingTestAccount(502, "gemini-route")
	eligible.Platform = PlatformGemini
	svc := newAPIKeyRoutingTestService(map[int64][]Account{first.ID: {wrong}, second.ID: {eligible}}, nil)
	svc.compositeResolver = NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{
		{GroupID: first.ID, PublicModel: "public-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-route", Endpoint: CompositeRouteEndpointResponses, Enabled: true},
		{GroupID: second.ID, PublicModel: "public-model", MatchType: CompositeRouteMatchExact, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-route", Endpoint: CompositeRouteEndpointResponses, Enabled: true},
	}})
	input := APIKeyRoutingResolveInput{Model: "public-model", CompositeEndpoint: CompositeRouteEndpointResponses}
	selected, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
	require.NoError(t, err)
	require.Equal(t, second.ID, *selected.GroupID)
	input.ExcludedGroupIDs = map[int64]struct{}{second.ID: {}}
	_, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
	require.ErrorIs(t, err, ErrNoAvailableRoutingGroup)
}

func TestAPIKeyRoutingModelFilter_AnyEligibleAccountAndWildcardMapping(t *testing.T) {
	first, second := newAPIKeyRoutingTestGroup(21), newAPIKeyRoutingTestGroup(22)
	mapped := newAPIKeyRoutingTestAccount(201, "")
	mapped.Credentials = map[string]any{"model_mapping": map[string]any{"public-*": "upstream-model"}}
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{
		first.ID:  {newAPIKeyRoutingTestAccount(202, "other-model"), mapped},
		second.ID: {newAPIKeyRoutingTestAccount(203, "public-model")},
	}, nil)
	selected, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "public-model"})
	require.NoError(t, err)
	require.Equal(t, first.ID, *selected.GroupID)
}

func TestAPIKeyRoutingModelFilter_OAuthFamilyDenyList(t *testing.T) {
	first, second := newAPIKeyRoutingTestGroup(31), newAPIKeyRoutingTestGroup(32)
	oauth := newAPIKeyRoutingTestAccount(301, "")
	oauth.Type = AccountTypeOAuth
	key := newAPIKeyRoutingTestKey(APIKeyRoutingStrategyManual, first, second)
	svc := newAPIKeyRoutingTestService(map[int64][]Account{first.ID: {oauth}, second.ID: {newAPIKeyRoutingTestAccount(302, "deepseek-v4-pro")}}, nil)
	selected, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, APIKeyRoutingResolveInput{Model: "deepseek-v4-pro"})
	require.NoError(t, err)
	require.Equal(t, second.ID, *selected.GroupID)
}

func TestAPIKeyRoutingStrategies_ExclusionAndSessionDoNotOverrideRanking(t *testing.T) {
	for _, strategy := range []string{APIKeyRoutingStrategyManual, APIKeyRoutingStrategyCostFirst, APIKeyRoutingStrategyStabilityFirst, APIKeyRoutingStrategyBalanced} {
		t.Run(strategy, func(t *testing.T) {
			first, second, third := newAPIKeyRoutingTestGroup(41), newAPIKeyRoutingTestGroup(42), newAPIKeyRoutingTestGroup(43)
			first.RateMultiplier, second.RateMultiplier, third.RateMultiplier = 0.5, 1, 2
			key := newAPIKeyRoutingTestKey(strategy, first, second, third)
			cache := &apiKeyRoutingCacheStub{sticky: map[string]int64{"session": third.ID}, health: map[int64]APIKeyRoutingHealth{first.ID: {Success: 200}, second.ID: {Success: 100}, third.ID: {Failure: 200}}}
			svc := newAPIKeyRoutingTestService(map[int64][]Account{first.ID: {newAPIKeyRoutingTestAccount(401, "gpt-route")}, second.ID: {newAPIKeyRoutingTestAccount(402, "gpt-route")}, third.ID: {newAPIKeyRoutingTestAccount(403, "gpt-route")}}, cache)
			input := APIKeyRoutingResolveInput{Model: "gpt-route", SessionKey: "session"}
			selected, err := svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
			require.NoError(t, err)
			if strategy == APIKeyRoutingStrategyBalanced {
				require.Equal(t, third.ID, *selected.GroupID)
			} else {
				require.Equal(t, first.ID, *selected.GroupID)
			}
			input.ExcludedGroupIDs = map[int64]struct{}{first.ID: {}, third.ID: {}}
			selected, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
			require.NoError(t, err)
			require.Equal(t, second.ID, *selected.GroupID)

			input.ExcludedGroupIDs = nil
			input.PreserveSession = true
			cache.sticky["session"] = third.ID
			selected, err = svc.ResolveAPIKeyRoutingGroup(context.Background(), key, input)
			require.NoError(t, err)
			require.Equal(t, third.ID, *selected.GroupID)
		})
	}
}
