package zcode

// 上游 v4.8.7 协议差异的回归：验证码开关、领取平面可选验证码、
// 端点路由的 X-ZCode-Authorization、start-plan 三块 system 与签名凭据形态。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCaptchaConfigDisabledYieldsNoToken(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, map[string]any{"configs": map[string]any{"captcha": map[string]any{
			"enabled": false, "prefix": "", "sceneId": "", "region": "cn", "skip_model_request": true,
		}}})
	})
	pool := NewCaptchaPool(solveFunc(func(context.Context, CaptchaConfig, string) (string, error) {
		t.Fatal("关闭验证码时不应求解")
		return "", nil
	}))
	token, err := pool.Token(context.Background(), c, "")
	require.NoError(t, err)
	require.Nil(t, token)
	_, err = pool.Take(context.Background(), c, "")
	require.Error(t, err)
	require.Equal(t, ErrCaptcha, KindOf(err))
}

func TestCaptchaConfigMissingIsError(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, map[string]any{"configs": map[string]any{}})
	})
	pool := goodPool()
	_, err := pool.Token(context.Background(), c, "")
	require.Error(t, err)
	require.Equal(t, ErrCaptcha, KindOf(err))
}

func TestClaimWithoutCaptchaOmitsVerificationHeaders(t *testing.T) {
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get(CaptchaHeader))
		require.Empty(t, r.Header.Get(CaptchaRegionHeader))
		require.Equal(t, "3.16.3", r.Header.Get("X-ZCode-App-Version"))
		respond(w, map[string]any{"plan": map[string]any{"plan_id": "trial-plan", "name": "体验套餐", "starts_at": 1, "ends_at": 2}})
	})
	result, err := client.Claim(context.Background(), "synthetic-jwt", "trial-plan", nil)
	require.NoError(t, err)
	require.True(t, result.OK)
}

func TestResolverAuthorizationHeaderInjection(t *testing.T) {
	var calls int
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		origin := "https://" + r.Host
		respond(w, map[string]any{
			"proxyEndpoint": map[string]any{"mapping": []any{map[string]any{
				"from":                     origin + "/api/v1/zcode-plan/anthropic/v1/messages",
				"to":                       origin + "/api/v1/ultra/anthropic/v1/messages",
				"zcodeAuthorizationHeader": "x-zcode-authorization",
			}}},
			"configs": map[string]any{"captcha": map[string]any{"enabled": true, "skip_model_request": true}},
		})
	})
	resolver := NewResolver(nil)
	routed := resolver.Resolve(context.Background(), c, c.OriginURL()+"/api/v1/zcode-plan/anthropic/v1/messages", "synthetic", "synthetic-zcode-jwt")
	require.Equal(t, c.OriginURL()+"/api/v1/ultra/anthropic/v1/messages", routed.URL)
	require.Equal(t, "Bearer synthetic-zcode-jwt", routed.Headers.Get(zcodeAuthorizationHeader))
	require.Equal(t, 1, calls)
	require.False(t, resolver.ClaimNeedsCaptcha(context.Background(), c, "synthetic"))
}

func TestResolverRejectsUnsafeAuthorizationTarget(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		origin := "https://" + r.Host
		respond(w, map[string]any{"proxyEndpoint": map[string]any{"mapping": []any{map[string]any{
			"from":                     origin + "/api/v1/zcode-plan/anthropic/v1/messages",
			"to":                       origin + "/api/anthropic/v1/messages",
			"zcodeAuthorizationHeader": "x-zcode-authorization",
		}}}})
	})
	resolver := NewResolver(nil)
	original := c.OriginURL() + "/api/v1/zcode-plan/anthropic/v1/messages"
	routed := resolver.Resolve(context.Background(), c, original, "synthetic", "synthetic-zcode-jwt")
	require.Equal(t, original, routed.URL)
	require.Empty(t, routed.Headers)
}

func TestResolverClaimVerificationDefaultsToRequired(t *testing.T) {
	missing := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, map[string]any{"proxyEndpoint": map[string]any{"mapping": []any{}}})
	})
	resolver := NewResolver(nil)
	require.True(t, resolver.ClaimNeedsCaptcha(context.Background(), missing, ""))

	disabled := mockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(w, map[string]any{
			"proxyEndpoint": map[string]any{"mapping": []any{}},
			"configs":       map[string]any{"captcha": map[string]any{"enabled": false}},
		})
	})
	require.False(t, NewResolver(nil).ClaimNeedsCaptcha(context.Background(), disabled, ""))
}

func TestStartPlanSystemShapeAndCustomReplacement(t *testing.T) {
	identity := DefaultIdentity("", "synthetic-device")
	body, _ := json.Marshal(map[string]any{
		"model":    "glm-5.3",
		"system":   []any{map[string]any{"type": "text", "text": "客户端系统提示"}},
		"messages": []any{map[string]any{"role": "user", "content": "合成协议测试"}},
	})
	out, err := TransformAnthropic(body, true, ZAI, identity, "synthetic-session", time.Now())
	require.NoError(t, err)
	var transformed map[string]any
	require.NoError(t, json.Unmarshal(out, &transformed))
	blocks, ok := transformed["system"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 3)
	first, ok := blocks[0].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, first, "cache_control")
	for _, index := range []int{1, 2} {
		block, ok := blocks[index].(map[string]any)
		require.True(t, ok)
		require.Contains(t, block, "cache_control")
	}
	third, ok := blocks[2].(map[string]any)
	require.True(t, ok)
	text, ok := third["text"].(string)
	require.True(t, ok)
	require.Equal(t, "\n\n客户端系统提示", text)
}

func TestSigningCredentialForms(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","sign_type":"SIGN"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"token_use":"project_access","api_key":"synthetic-id"}`))
	jwt := header + "." + payload + ".synthetic-signature"
	parsed, ok := parseSigningCredential(jwt)
	require.True(t, ok)
	require.Equal(t, "synthetic-id", parsed.apiKeyID)
	require.Equal(t, jwt, parsed.secret)
	require.Equal(t, handshakeFieldAccessToken, parsed.handshakeField)

	parsed, ok = parseSigningCredential("synthetic-id.synthetic-secret")
	require.True(t, ok)
	require.Equal(t, "synthetic-id", parsed.apiKeyID)
	require.Equal(t, "synthetic-secret", parsed.secret)
	require.Equal(t, handshakeFieldAPIKey, parsed.handshakeField)

	_, ok = parseSigningCredential("not-signable")
	require.False(t, ok)
	// 普通 JWT 缺少 project_access 标记时不能当成签名凭据。
	regular := header + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`)) + ".sig"
	_, ok = parseSigningCredential(regular)
	require.False(t, ok)
}

// 上游 preview 在不同活动下返回 plans 数组、campaign 对象或两者并存；
// claimable 字段可能缺失，不能据此判定为格式错误。
func TestParsePreviewFormats(t *testing.T) {
	t.Run("plans_only", func(t *testing.T) {
		preview, err := ParsePreview([]byte(`{"plans":[{"plan_id":"p1","name":"体验","priority":2,"starts_at":1,"ends_at":2},{"plan_id":"p2","name":"体验二","priority":1,"starts_at":1,"ends_at":2}]}`))
		require.NoError(t, err)
		require.True(t, preview.Claimable)
		require.NotNil(t, preview.Campaign)
		require.Equal(t, "p1", preview.Campaign.Plans[0].ID)
	})
	t.Run("campaign_without_claimable", func(t *testing.T) {
		preview, err := ParsePreview([]byte(`{"campaign":{"id":"c1","name":"周末活动","starts_at":1,"ends_at":2000000000,"min_app_version":"3.14.0","plans":[{"plan_id":"p1","name":"体验","starts_at":1,"ends_at":2}]}}`))
		require.NoError(t, err)
		require.True(t, preview.Claimable)
		require.NotNil(t, preview.Campaign)
		require.Equal(t, "3.14.0", preview.Campaign.MinimumVersion)
		require.Empty(t, preview.Blocked)
	})
	t.Run("campaign_with_blocked_reason", func(t *testing.T) {
		preview, err := ParsePreview([]byte(`{"campaign":{"id":"c1","plans":[{"plan_id":"p1"}]},"reason":"not_eligible"}`))
		require.NoError(t, err)
		require.False(t, preview.Claimable)
		require.Equal(t, "not_eligible", preview.Blocked)
	})
	t.Run("campaign_and_plans", func(t *testing.T) {
		preview, err := ParsePreview([]byte(`{"campaign":{"id":"c1","name":"活动","starts_at":10,"ends_at":20,"min_app_version":"3.16.3"},"plans":[{"plan_id":"p9","name":"体验","priority":5,"starts_at":10,"ends_at":20}]}`))
		require.NoError(t, err)
		require.True(t, preview.Claimable)
		require.Equal(t, "c1", preview.Campaign.ID)
		require.Equal(t, int64(10), preview.Campaign.StartsAt)
		require.Equal(t, "3.16.3", preview.Campaign.MinimumVersion)
		require.Equal(t, "p9", preview.Campaign.Plans[0].ID)
	})
	t.Run("empty_plans", func(t *testing.T) {
		preview, err := ParsePreview([]byte(`{"plans":[]}`))
		require.NoError(t, err)
		require.Equal(t, "none", preview.Blocked)
		require.False(t, preview.Claimable)
	})
}
