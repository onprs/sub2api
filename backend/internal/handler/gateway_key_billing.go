package handler

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyBillingInfoSchemaVersion = 2

type keyBillingInfoResponse struct {
	Object                     string    `json:"object"`
	SchemaVersion              int       `json:"schema_version"`
	BillingScope               string    `json:"billing_scope"`
	GroupRateMultiplier        float64   `json:"group_rate_multiplier"`
	UserRateMultiplier         *float64  `json:"user_rate_multiplier,omitempty"`
	DynamicRateEnabled         bool      `json:"dynamic_rate_enabled"`
	DynamicRateMinMultiplier   float64   `json:"dynamic_rate_min_multiplier"`
	DynamicRateMaxMultiplier   float64   `json:"dynamic_rate_max_multiplier"`
	DynamicRateTargetTokens    int64     `json:"dynamic_rate_target_tokens,omitempty"`
	DynamicRateWindowMinutes   int       `json:"dynamic_rate_window_minutes,omitempty"`
	ResolvedRateMultiplier     float64   `json:"resolved_rate_multiplier"`
	ResolvedRateMultiplierMin  float64   `json:"resolved_rate_multiplier_min"`
	ResolvedRateMultiplierMax  float64   `json:"resolved_rate_multiplier_max"`
	PeakRateEnabled            bool      `json:"peak_rate_enabled"`
	PeakStart                  *string   `json:"peak_start,omitempty"`
	PeakEnd                    *string   `json:"peak_end,omitempty"`
	PeakRateMultiplier         *float64  `json:"peak_rate_multiplier,omitempty"`
	AppliedPeakMultiplier      *float64  `json:"applied_peak_multiplier,omitempty"`
	EffectiveRateMultiplier    float64   `json:"effective_rate_multiplier"`
	EffectiveRateMultiplierMin float64   `json:"effective_rate_multiplier_min"`
	EffectiveRateMultiplierMax float64   `json:"effective_rate_multiplier_max"`
	Timezone                   *string   `json:"timezone,omitempty"`
	ObservedAt                 time.Time `json:"observed_at"`
}

type resolvedKeyBillingRate struct {
	Multiplier   float64
	HasOverride  bool
	LookupFailed bool
}

// KeyBillingInfo returns the token billing multiplier effective for the authenticated API key.
// GET /v1/sub2api/billing
func (h *GatewayHandler) KeyBillingInfo(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Billing information is not supported in simple mode")
		return
	}
	if apiKey.GroupID == nil {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "API key is not assigned to a group")
		return
	}
	if apiKey.Group == nil {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Billing information is unavailable")
		return
	}

	resolvedRate, ok := h.resolveKeyBillingRate(c, apiKey)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Billing information is unavailable")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, buildKeyBillingInfo(apiKey, resolvedRate, timezone.Now()))
}

func (h *GatewayHandler) resolveKeyBillingRate(c *gin.Context, apiKey *service.APIKey) (resolvedKeyBillingRate, bool) {
	groupRate := apiKey.Group.RateMultiplier
	var multiplier float64
	var hasOverride, lookupFailed bool
	switch apiKey.Group.Platform {
	case service.PlatformOpenAI, service.PlatformGrok:
		if h.openAIGatewayService == nil {
			return resolvedKeyBillingRate{}, false
		}
		multiplier, hasOverride, lookupFailed = h.openAIGatewayService.ResolveUserGroupRateMultiplierState(
			c.Request.Context(), apiKey.UserID, *apiKey.GroupID, groupRate,
		)
	default:
		if h.gatewayService == nil {
			return resolvedKeyBillingRate{}, false
		}
		multiplier, hasOverride, lookupFailed = h.gatewayService.ResolveUserGroupRateMultiplierState(
			c.Request.Context(), apiKey.UserID, *apiKey.GroupID, groupRate,
		)
	}
	return resolvedKeyBillingRate{Multiplier: multiplier, HasOverride: hasOverride, LookupFailed: lookupFailed}, true
}

func buildKeyBillingInfo(apiKey *service.APIKey, resolved resolvedKeyBillingRate, now time.Time) keyBillingInfoResponse {
	group := apiKey.Group
	groupRate := group.RateMultiplier
	minRate := resolved.Multiplier
	maxRate := resolved.Multiplier
	var userRate *float64
	if resolved.HasOverride {
		userRate = &resolved.Multiplier
	} else if group.DynamicRateEnabled && !resolved.LookupFailed {
		minRate = group.DynamicRateMinMultiplier
		maxRate = group.DynamicRateMaxMultiplier
	}
	appliedPeak := group.PeakMultiplierAt(now)

	response := keyBillingInfoResponse{
		Object:                     "sub2api.key_billing",
		SchemaVersion:              keyBillingInfoSchemaVersion,
		BillingScope:               "token",
		GroupRateMultiplier:        groupRate,
		UserRateMultiplier:         userRate,
		DynamicRateEnabled:         group.DynamicRateEnabled,
		DynamicRateMinMultiplier:   group.DynamicRateMinMultiplier,
		DynamicRateMaxMultiplier:   group.DynamicRateMaxMultiplier,
		DynamicRateTargetTokens:    group.DynamicRateTargetTokens,
		DynamicRateWindowMinutes:   group.DynamicRateWindowMinutes,
		ResolvedRateMultiplier:     maxRate,
		ResolvedRateMultiplierMin:  minRate,
		ResolvedRateMultiplierMax:  maxRate,
		PeakRateEnabled:            group.PeakRateEnabled,
		EffectiveRateMultiplier:    maxRate * appliedPeak,
		EffectiveRateMultiplierMin: minRate * appliedPeak,
		EffectiveRateMultiplierMax: maxRate * appliedPeak,
		ObservedAt:                 now.UTC(),
	}
	if group.PeakRateEnabled {
		response.PeakStart = &group.PeakStart
		response.PeakEnd = &group.PeakEnd
		response.PeakRateMultiplier = &group.PeakRateMultiplier
		response.AppliedPeakMultiplier = &appliedPeak
		tz := timezone.Location().String()
		response.Timezone = &tz
	}
	return response
}
