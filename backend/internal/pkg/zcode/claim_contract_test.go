package zcode

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// 使用固定上游 client.ts 的真实协议结构，防止合成 campaign 结构掩盖接口差异。
func TestClaimDesktopPreviewContract(t *testing.T) {
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/zcode-plan/billing/preview", r.URL.Path)
		require.Equal(t, DefaultAppVersion, r.URL.Query().Get("app_version"))
		require.Equal(t, "Bearer synthetic-jwt", r.Header.Get("Authorization"))
		require.Equal(t, "synthetic-device", r.Header.Get("X-Device-Mid"))
		require.Empty(t, r.Header.Get("X-ZCode-App-Version"))
		respond(w, map[string]any{"plans": []any{
			map[string]any{"plan_id": "trial-plan-low", "name": "体验套餐", "priority": 1, "starts_at": 2000000000, "ends_at": 2000100000},
			map[string]any{"plan_id": "trial-plan-high", "name": "优先体验套餐", "priority": 2, "starts_at": 2000000000, "ends_at": 2000200000},
		}})
	})
	preview, err := client.Preview(context.Background(), "synthetic-jwt")
	require.NoError(t, err)
	require.True(t, preview.Claimable)
	require.NotNil(t, preview.Campaign)
	require.Equal(t, "trial-plan-high", preview.Campaign.Plans[0].ID)
	require.Equal(t, int64(2000000000), preview.Campaign.Plans[0].StartsAt)
	// 套餐生效时间属于领取结果，不能误作活动开始时间而阻止提前领取。
	require.Zero(t, preview.Campaign.StartsAt)
}

func TestClaimDesktopEmptyAndMalformedPreviews(t *testing.T) {
	preview, err := ParsePreview([]byte(`{"plans":[]}`))
	require.NoError(t, err)
	require.Nil(t, preview.Campaign)
	require.False(t, preview.Claimable)
	for _, payload := range []string{`{"plans":{}}`, `{"plans":["invalid"]}`, `{"plans":[{"plan_id":123}]}`} {
		_, err = ParsePreview([]byte(payload))
		require.Error(t, err)
	}
}

func TestClaimDesktopResultPlanAliases(t *testing.T) {
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/zcode-plan/billing/claim", r.URL.Path)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "trial-plan", payload["plan_id"])
		respond(w, map[string]any{"plan": map[string]any{"plan_id": "trial-plan", "name": "体验套餐", "starts_at": 2000000000, "ends_at": 2000100000}})
	})
	result, err := client.Claim(context.Background(), "synthetic-jwt", "trial-plan", &CaptchaToken{Param: captchaParam("contract"), Region: "cn"})
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "trial-plan", result.Plan.ID)
	require.Equal(t, int64(2000000000), result.Plan.StartsAt)
}

func TestClaimMarketingPlansEnumeration(t *testing.T) {
	var marketingCalls int
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/zcode-plan/billing/preview":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","logid":"synthetic"}`))
		case "/api/v1/marketing/touch":
			marketingCalls++
			require.NotEmpty(t, r.URL.Query().Get("seq"))
			require.Equal(t, "Bearer synthetic-jwt", r.Header.Get("Authorization"))
			require.Equal(t, "synthetic-device", r.Header.Get("X-Device-Mid"))
			require.Equal(t, "zh-CN", r.Header.Get("X-Client-Language"))
			require.Equal(t, DefaultAppVersion, r.Header.Get("X-ZCode-App-Version"))
			require.Equal(t, "linux-x64", r.Header.Get("X-Platform"))
			respond(w, map[string]any{"deliveries": []any{
				map[string]any{
					"campaign_id": "synthetic-campaign",
					"banner": map[string]any{
						"buttons": []any{map[string]any{"action": map[string]any{"type": "claim_zcode_plan", "args": map[string]any{"plan_id": "synthetic-weekend"}}}},
						"background": map[string]any{"args": map[string]any{"zcode_plan": map[string]any{
							"plan_id": "synthetic-weekend", "name": "ZCODE WEEKEND BUILD", "priority": 101,
							"starts_at": 100, "ends_at": 2000000000,
						}}},
					},
				},
			}})
		default:
			t.Errorf("非预期路径 %s", r.URL.Path)
		}
	})
	preview, err := client.ListPlans(context.Background(), "synthetic-jwt")
	require.NoError(t, err)
	require.True(t, preview.Marketing)
	require.True(t, preview.Claimable)
	require.NotNil(t, preview.Campaign)
	require.Len(t, preview.Campaign.Plans, 1)
	require.Equal(t, "synthetic-weekend", preview.Campaign.Plans[0].ID)
	require.Equal(t, "ZCODE WEEKEND BUILD", preview.Campaign.Plans[0].Name)
	require.Equal(t, 101, preview.Campaign.Plans[0].Priority)
	require.Equal(t, int64(2000000000), preview.Campaign.Plans[0].EndsAt)
	require.Equal(t, 1, marketingCalls)
}

func TestClaimMarketingMergePreviewWins(t *testing.T) {
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/zcode-plan/billing/preview":
			respond(w, map[string]any{"plans": []any{
				map[string]any{"plan_id": "shared-plan", "name": "preview-name", "priority": 5},
				map[string]any{"plan_id": "preview-only", "name": "preview-only", "priority": 1},
			}})
		case "/api/v1/marketing/touch":
			respond(w, map[string]any{"deliveries": []any{map[string]any{
				"banner": map[string]any{
					"buttons": []any{
						map[string]any{"action": map[string]any{"type": "claim_zcode_plan", "args": map[string]any{"plan_id": "shared-plan"}}},
						map[string]any{"action": map[string]any{"type": "claim_zcode_plan", "args": map[string]any{"plan_id": "banner-only"}}},
					},
					"background": map[string]any{"args": map[string]any{"zcode_plan": map[string]any{"plan_id": "shared-plan", "name": "banner-name"}}},
				},
			}}})
		default:
			t.Errorf("非预期路径 %s", r.URL.Path)
		}
	})
	preview, err := client.ListPlans(context.Background(), "synthetic-jwt")
	require.NoError(t, err)
	require.Len(t, preview.Campaign.Plans, 3)
	require.Equal(t, "shared-plan", preview.Campaign.Plans[0].ID)
	require.Equal(t, "preview-name", preview.Campaign.Plans[0].Name)
	require.Equal(t, "preview-only", preview.Campaign.Plans[1].ID)
	require.Equal(t, "banner-only", preview.Campaign.Plans[2].ID)
}

func TestClaimMarketingPlansFallbackWithoutDevice(t *testing.T) {
	client := &Client{Origin: "https://zcode.z.ai", Identity: Identity{AppVersion: "3.17.2", Platform: "linux-x64"}}
	require.Empty(t, client.marketingPlans(context.Background(), "synthetic-jwt"))
}

func TestParseMarketingPlansShapes(t *testing.T) {
	require.Nil(t, ParseMarketingPlans([]byte(`not-json`)))
	require.Empty(t, ParseMarketingPlans([]byte(`{"deliveries":[]}`)))
	plans := ParseMarketingPlans([]byte(`{"deliveries":[{"popup":{"buttons":[{"action":{"type":"claim_zcode_plan","args":{"plan_id":"nested-plan"}}}]}},{"banner":{"buttons":[{"action":{"type":"claim_zcode_plan","args":{"plan_id":"nested-plan"}}},{"action":{"type":"claim_zcode_plan","args":{"plan_id":"plain-plan"}}}]}}]}`))
	require.Len(t, plans, 2)
	require.Equal(t, "nested-plan", plans[0].ID)
	require.Equal(t, "nested-plan", plans[0].Name)
	require.Equal(t, "plain-plan", plans[1].ID)
}
