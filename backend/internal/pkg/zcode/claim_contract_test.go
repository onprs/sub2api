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
		require.Equal(t, "3.14.0", r.URL.Query().Get("app_version"))
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
	result, err := client.Claim(context.Background(), "synthetic-jwt", "trial-plan", CaptchaToken{Param: captchaParam("contract"), Region: "cn"})
	require.NoError(t, err)
	require.True(t, result.OK)
	require.Equal(t, "trial-plan", result.Plan.ID)
	require.Equal(t, int64(2000000000), result.Plan.StartsAt)
}
