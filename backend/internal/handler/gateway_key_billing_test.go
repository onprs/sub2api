package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type keyBillingUserGroupRateRepo struct {
	service.UserGroupRateRepository
	rate        *float64
	err         error
	gotUserID   int64
	gotGroupID  int64
	lookupCalls int
}

func (r *keyBillingUserGroupRateRepo) GetByUserAndGroup(_ context.Context, userID, groupID int64) (*float64, error) {
	r.gotUserID = userID
	r.gotGroupID = groupID
	r.lookupCalls++
	return r.rate, r.err
}

func newKeyBillingHandler(repo service.UserGroupRateRepository) *GatewayHandler {
	return &GatewayHandler{
		gatewayService:       newKeyBillingGatewayService(repo),
		openAIGatewayService: newKeyBillingOpenAIGatewayService(repo),
	}
}

func newKeyBillingGatewayService(repo service.UserGroupRateRepository) *service.GatewayService {
	return service.NewGatewayService(
		nil, nil, nil, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

func newKeyBillingOpenAIGatewayService(repo service.UserGroupRateRepository) *service.OpenAIGatewayService {
	return service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, repo, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

func newKeyBillingContext(apiKey *service.APIKey) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/sub2api/billing", nil)
	if apiKey != nil {
		c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	}
	return c, w
}

func TestGatewayHandlerKeyBillingInfoUsesGroupRate(t *testing.T) {
	groupID := int64(7)
	apiKey := &service.APIKey{
		UserID:  11,
		GroupID: &groupID,
		Key:     "sk-sensitive-value",
		Group: &service.Group{
			ID:             groupID,
			Name:           "private-group-name",
			RateMultiplier: 0.75,
		},
	}
	c, w := newKeyBillingContext(apiKey)

	newKeyBillingHandler(nil).KeyBillingInfo(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var got keyBillingInfoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "sub2api.key_billing", got.Object)
	require.Equal(t, 2, got.SchemaVersion)
	require.Equal(t, "token", got.BillingScope)
	require.Equal(t, 0.75, got.GroupRateMultiplier)
	require.Nil(t, got.UserRateMultiplier)
	require.Equal(t, 0.75, got.ResolvedRateMultiplier)
	require.Equal(t, 0.75, got.ResolvedRateMultiplierMin)
	require.Equal(t, 0.75, got.ResolvedRateMultiplierMax)
	require.False(t, got.PeakRateEnabled)
	require.Nil(t, got.PeakStart)
	require.Nil(t, got.PeakEnd)
	require.Nil(t, got.PeakRateMultiplier)
	require.Nil(t, got.AppliedPeakMultiplier)
	require.Equal(t, 0.75, got.EffectiveRateMultiplier)
	require.Equal(t, 0.75, got.EffectiveRateMultiplierMin)
	require.Equal(t, 0.75, got.EffectiveRateMultiplierMax)
	require.Nil(t, got.Timezone)
	require.False(t, got.ObservedAt.IsZero())
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	require.NotContains(t, fields, "user_rate_multiplier")
	require.NotContains(t, fields, "peak_start")
	require.NotContains(t, fields, "peak_end")
	require.NotContains(t, fields, "peak_rate_multiplier")
	require.NotContains(t, fields, "applied_peak_multiplier")
	require.NotContains(t, fields, "timezone")
	require.NotContains(t, w.Body.String(), apiKey.Key)
	require.NotContains(t, w.Body.String(), apiKey.Group.Name)
}

func TestGatewayHandlerKeyBillingInfoUsesUserOverride(t *testing.T) {
	groupID := int64(7)
	userRate := 0.5
	apiKey := &service.APIKey{
		UserID:  11,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, RateMultiplier: 0.75},
	}
	c, w := newKeyBillingContext(apiKey)
	repo := &keyBillingUserGroupRateRepo{rate: &userRate}

	newKeyBillingHandler(repo).KeyBillingInfo(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, repo.lookupCalls)
	require.Equal(t, apiKey.UserID, repo.gotUserID)
	require.Equal(t, groupID, repo.gotGroupID)
	var got keyBillingInfoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.UserRateMultiplier)
	require.Equal(t, 0.5, *got.UserRateMultiplier)
	require.Equal(t, 0.5, got.ResolvedRateMultiplier)
	require.Equal(t, 0.5, got.EffectiveRateMultiplier)
}

func TestGatewayHandlerKeyBillingInfoUsesEqualValuedUserOverride(t *testing.T) {
	groupID := int64(8)
	userRate := 0.75
	apiKey := &service.APIKey{
		UserID:  12,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                       groupID,
			RateMultiplier:           0.75,
			DynamicRateEnabled:       true,
			DynamicRateMaxMultiplier: 0.9,
			DynamicRateMinMultiplier: 0.5,
			DynamicRateTargetTokens:  1_000,
			DynamicRateWindowMinutes: 60,
		},
	}
	c, w := newKeyBillingContext(apiKey)

	newKeyBillingHandler(&keyBillingUserGroupRateRepo{rate: &userRate}).KeyBillingInfo(c)

	require.Equal(t, http.StatusOK, w.Code)
	var got keyBillingInfoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.UserRateMultiplier)
	require.Equal(t, userRate, *got.UserRateMultiplier)
	require.Equal(t, userRate, got.ResolvedRateMultiplierMin)
	require.Equal(t, userRate, got.ResolvedRateMultiplierMax)
}

func TestGatewayHandlerKeyBillingInfoReportsDynamicRange(t *testing.T) {
	groupID := int64(9)
	apiKey := &service.APIKey{
		UserID:  13,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                       groupID,
			RateMultiplier:           0.75,
			DynamicRateEnabled:       true,
			DynamicRateMaxMultiplier: 0.15,
			DynamicRateMinMultiplier: 0.13,
			DynamicRateTargetTokens:  1_000_000,
			DynamicRateWindowMinutes: 1440,
		},
	}
	c, w := newKeyBillingContext(apiKey)

	newKeyBillingHandler(nil).KeyBillingInfo(c)

	require.Equal(t, http.StatusOK, w.Code)
	var got keyBillingInfoResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.True(t, got.DynamicRateEnabled)
	require.Nil(t, got.UserRateMultiplier)
	require.Equal(t, 0.15, got.ResolvedRateMultiplier)
	require.Equal(t, 0.13, got.ResolvedRateMultiplierMin)
	require.Equal(t, 0.15, got.ResolvedRateMultiplierMax)
	require.Equal(t, 0.13, got.EffectiveRateMultiplierMin)
	require.Equal(t, 0.15, got.EffectiveRateMultiplierMax)
}

func TestBuildKeyBillingInfoAppliesPeakMultiplier(t *testing.T) {
	groupID := int64(7)
	apiKey := &service.APIKey{
		GroupID: &groupID,
		Group: &service.Group{
			ID:                 groupID,
			RateMultiplier:     1.2,
			SubscriptionType:   service.SubscriptionTypeSubscription,
			PeakRateEnabled:    true,
			PeakStart:          "09:00",
			PeakEnd:            "18:00",
			PeakRateMultiplier: 1.5,
		},
	}
	now := time.Date(2026, time.July, 12, 10, 0, 0, 0, timezone.Location())
	userRate := 0.8

	got := buildKeyBillingInfo(apiKey, resolvedKeyBillingRate{Multiplier: userRate, HasOverride: true}, now)

	require.Equal(t, 1.2, got.GroupRateMultiplier)
	require.NotNil(t, got.UserRateMultiplier)
	require.Equal(t, 0.8, *got.UserRateMultiplier)
	require.Equal(t, 0.8, got.ResolvedRateMultiplier)
	require.True(t, got.PeakRateEnabled)
	require.NotNil(t, got.PeakStart)
	require.Equal(t, "09:00", *got.PeakStart)
	require.NotNil(t, got.PeakEnd)
	require.Equal(t, "18:00", *got.PeakEnd)
	require.NotNil(t, got.PeakRateMultiplier)
	require.Equal(t, 1.5, *got.PeakRateMultiplier)
	require.NotNil(t, got.AppliedPeakMultiplier)
	require.Equal(t, 1.5, *got.AppliedPeakMultiplier)
	require.InDelta(t, 1.2, got.EffectiveRateMultiplier, 1e-12)
	require.NotNil(t, got.Timezone)
	require.Equal(t, timezone.Location().String(), *got.Timezone)
	require.Equal(t, now.UTC(), got.ObservedAt)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	for _, field := range []string{
		"user_rate_multiplier",
		"peak_start",
		"peak_end",
		"peak_rate_multiplier",
		"applied_peak_multiplier",
		"timezone",
	} {
		require.Contains(t, fields, field)
	}
}

func TestKeyBillingInfoJSONKeepsZeroPeakMultiplierWhenEnabled(t *testing.T) {
	groupID := int64(7)
	apiKey := &service.APIKey{
		GroupID: &groupID,
		Group: &service.Group{
			ID:                 groupID,
			SubscriptionType:   service.SubscriptionTypeSubscription,
			PeakRateEnabled:    true,
			PeakStart:          "00:00",
			PeakEnd:            "23:59",
			PeakRateMultiplier: 0,
		},
	}
	now := time.Date(2026, time.July, 12, 12, 0, 0, 0, timezone.Location())
	encoded, err := json.Marshal(buildKeyBillingInfo(
		apiKey,
		resolvedKeyBillingRate{Multiplier: apiKey.Group.RateMultiplier},
		now,
	))
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.JSONEq(t, "0", string(fields["peak_rate_multiplier"]))
	require.JSONEq(t, "0", string(fields["applied_peak_multiplier"]))
}

func TestGatewayHandlerKeyBillingInfoErrorsAreSafe(t *testing.T) {
	t.Run("missing API key", func(t *testing.T) {
		c, w := newKeyBillingContext(nil)
		newKeyBillingHandler(nil).KeyBillingInfo(c)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ungrouped API key", func(t *testing.T) {
		c, w := newKeyBillingContext(&service.APIKey{})
		newKeyBillingHandler(nil).KeyBillingInfo(c)
		require.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing billing service", func(t *testing.T) {
		groupID := int64(7)
		c, w := newKeyBillingContext(&service.APIKey{
			UserID:  11,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, RateMultiplier: 1},
		})
		(&GatewayHandler{}).KeyBillingInfo(c)
		require.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("rate lookup failure matches billing fallback", func(t *testing.T) {
		groupID := int64(7)
		c, w := newKeyBillingContext(&service.APIKey{
			UserID:  11,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, RateMultiplier: 1},
		})
		newKeyBillingHandler(&keyBillingUserGroupRateRepo{err: errors.New("database password leaked")}).KeyBillingInfo(c)
		require.Equal(t, http.StatusOK, w.Code)
		var got keyBillingInfoResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Equal(t, 1.0, got.ResolvedRateMultiplier)
		require.NotContains(t, w.Body.String(), "database password leaked")
	})

	t.Run("dynamic rate lookup failure keeps the static billing fallback", func(t *testing.T) {
		groupID := int64(8)
		c, w := newKeyBillingContext(&service.APIKey{
			UserID:  12,
			GroupID: &groupID,
			Group: &service.Group{
				ID:                       groupID,
				RateMultiplier:           0.9,
				DynamicRateEnabled:       true,
				DynamicRateMaxMultiplier: 0.15,
				DynamicRateMinMultiplier: 0.13,
			},
		})
		newKeyBillingHandler(&keyBillingUserGroupRateRepo{err: errors.New("lookup failed")}).KeyBillingInfo(c)
		require.Equal(t, http.StatusOK, w.Code)
		var got keyBillingInfoResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Equal(t, 0.9, got.ResolvedRateMultiplierMin)
		require.Equal(t, 0.9, got.ResolvedRateMultiplierMax)
	})
}

func TestGatewayHandlerKeyBillingInfoSharesBillingResolverCacheByPlatform(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		openAI   bool
	}{
		{name: "anthropic", platform: service.PlatformAnthropic},
		{name: "openai", platform: service.PlatformOpenAI, openAI: true},
		{name: "grok", platform: service.PlatformGrok, openAI: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(7)
			oldRate, newRate := 0.5, 1.8
			repo := &keyBillingUserGroupRateRepo{rate: &oldRate}
			gatewayService := newKeyBillingGatewayService(repo)
			openAIGatewayService := newKeyBillingOpenAIGatewayService(repo)
			h := &GatewayHandler{
				gatewayService:       gatewayService,
				openAIGatewayService: openAIGatewayService,
			}
			apiKey := &service.APIKey{
				UserID:  11,
				GroupID: &groupID,
				Group: &service.Group{
					ID:             groupID,
					Platform:       tc.platform,
					RateMultiplier: 0.75,
				},
			}

			if tc.openAI {
				require.Equal(t, oldRate, openAIGatewayService.ResolveUserGroupRateMultiplier(context.Background(), apiKey.UserID, groupID, apiKey.Group.RateMultiplier))
			} else {
				require.Equal(t, oldRate, gatewayService.ResolveUserGroupRateMultiplier(context.Background(), apiKey.UserID, groupID, apiKey.Group.RateMultiplier))
			}
			repo.rate = &newRate

			for range 2 {
				c, w := newKeyBillingContext(apiKey)
				h.KeyBillingInfo(c)
				require.Equal(t, http.StatusOK, w.Code)
				var got keyBillingInfoResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
				require.Equal(t, oldRate, got.ResolvedRateMultiplier)
				require.Equal(t, oldRate, got.EffectiveRateMultiplier)
			}

			var billedRate float64
			if tc.openAI {
				billedRate = openAIGatewayService.ResolveUserGroupRateMultiplier(context.Background(), apiKey.UserID, groupID, apiKey.Group.RateMultiplier)
			} else {
				billedRate = gatewayService.ResolveUserGroupRateMultiplier(context.Background(), apiKey.UserID, groupID, apiKey.Group.RateMultiplier)
			}
			require.Equal(t, oldRate, billedRate)
			require.Equal(t, 1, repo.lookupCalls)
		})
	}
}
