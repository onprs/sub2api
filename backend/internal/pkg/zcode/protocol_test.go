package zcode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, key string) json.RawMessage {
	t.Helper()
	raw, e := os.ReadFile("testdata/parity.json")
	require.NoError(t, e)
	var data map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &data))
	return data[key]
}
func syntheticJWT(iat, exp int64) string {
	payload := map[string]int64{"iat": iat}
	if exp > 0 {
		payload["exp"] = exp
	}
	raw, _ := json.Marshal(payload)
	return "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".c3ludGhldGlj"
}
func respond(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
}
func mockClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return &Client{Origin: server.URL, Identity: DefaultIdentity("", "synthetic-device"), Send: server.Client().Do}
}

type solveFunc func(context.Context, CaptchaConfig, string) (string, error)

func (f solveFunc) Solve(ctx context.Context, cfg CaptchaConfig, proxy string) (string, error) {
	return f(ctx, cfg, proxy)
}
func captchaParam(id string) string {
	raw, _ := json.Marshal(map[string]string{"certifyId": id, "securityToken": strings.Repeat("synthetic-", 20)})
	return base64.StdEncoding.EncodeToString(raw)
}
func goodPool() *CaptchaPool {
	var n atomic.Int64
	return NewCaptchaPool(solveFunc(func(context.Context, CaptchaConfig, string) (string, error) {
		return captchaParam(fmt.Sprint(n.Add(1))), nil
	}))
}
func captchaResponse(w http.ResponseWriter) {
	respond(w, map[string]any{"configs": map[string]any{"captcha": CaptchaConfig{Enabled: true, Prefix: "synthetic-prefix", SceneID: "synthetic-scene", Region: "cn"}}})
}

func TestJWTExpiryAndNoExpiration(t *testing.T) {
	now := time.Now()
	issued, expires, err := InspectJWT(syntheticJWT(1, 0))
	require.NoError(t, err)
	require.Equal(t, int64(1), issued)
	require.Zero(t, expires)
	require.False(t, (Tokens{IssuedAt: 1}).Expired(now))
	_, expires, err = InspectJWT(syntheticJWT(1, now.Unix()-1))
	require.NoError(t, err)
	require.True(t, (Tokens{ExpiresAt: expires}).Expired(now))
	for _, bad := range []string{"opaque", "a.!bad.c", "a.bm90LWpzb24.c"} {
		_, _, err = InspectJWT(bad)
		require.Error(t, err)
		require.NotContains(t, err.Error(), bad)
	}
}
func TestOAuthInitPollParity(t *testing.T) {
	for _, provider := range []Provider{ZAI, BigModel} {
		t.Run(string(provider), func(t *testing.T) {
			var initToken string
			client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/oauth/cli/init":
					initToken = r.Header.Get("Authorization")
					require.Len(t, strings.TrimPrefix(initToken, "Bearer "), 64)
					respond(w, map[string]any{"flow_id": "synthetic-flow", "authorize_url": "https://chat.z.ai/oauth?state=synthetic-state", "expires_at": time.Now().Add(time.Hour).Unix(), "poll_interval_sec": 1})
				case strings.Contains(r.URL.Path, "/poll/"):
					require.Equal(t, initToken, r.Header.Get("Authorization"))
					respond(w, map[string]any{"status": "ready", "token": syntheticJWT(time.Now().Unix(), 0), string(provider): map[string]string{"access_token": "synthetic-access"}, "user": map[string]string{"user_id": "synthetic-user"}})
				default:
					t.Errorf("非预期路径 %s", r.URL.Path)
				}
			})
			flow, authorize, err := client.StartLogin(context.Background(), provider)
			require.NoError(t, err)
			require.LessOrEqual(t, flow.ExpiresAt, time.Now().Add(5*time.Minute).Unix())
			require.Contains(t, authorize, "synthetic-state")
			require.Contains(t, authorize, "redirect")
			tokens, err := client.PollLogin(context.Background(), flow)
			require.NoError(t, err)
			require.Equal(t, "synthetic-access", tokens.AccessToken)
			require.Equal(t, "synthetic-user", tokens.UserID)
			require.Zero(t, tokens.ExpiresAt)
		})
	}
}
func TestOAuthPendingMalformedAndExpired(t *testing.T) {
	cases := []struct {
		name    string
		http    int
		body    string
		wantErr bool
	}{{"pending", 200, `{"code":200,"data":{"status":"pending"}}`, false}, {"malformed", 200, "not-json", false}, {"rate", 429, `{"code":429}`, false}, {"expired", 401, `{"code":401,"msg":"synthetic-sensitive"}`, true}, {"past-exp", 200, `{"code":200,"data":{"status":"ready","token":"opaque","zai":{"access_token":"synthetic"}}}`, true}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.http)
				_, _ = io.WriteString(w, test.body)
			})
			tokens, e := c.PollLogin(context.Background(), LoginFlow{Provider: ZAI, FlowID: "f", PollToken: "synthetic", ExpiresAt: time.Now().Add(time.Minute).Unix()})
			require.Nil(t, tokens)
			if test.wantErr {
				require.Error(t, e)
				require.NotContains(t, e.Error(), "synthetic-sensitive")
			} else {
				require.NoError(t, e)
			}
		})
	}
}
func TestQuotaFixturesAndUnknownTotal(t *testing.T) {
	remaining, e := ParseQuota(fixture(t, "quota_remaining_only"), PlanStart)
	require.NoError(t, e)
	require.Equal(t, float64(1250), *remaining.Balances[0].Remaining)
	require.Nil(t, remaining.Balances[0].Total)
	require.Nil(t, remaining.Balances[0].Percent)
	require.False(t, remaining.Exhausted(time.Now()))
	total, e := ParseQuota(fixture(t, "quota_with_total"), PlanStart)
	require.NoError(t, e)
	require.Equal(t, float64(75), *total.Balances[0].Percent)
	coding, e := ParseQuota(fixture(t, "coding_quota"), PlanCoding)
	require.NoError(t, e)
	require.Nil(t, coding.Balances[0].Total)
	require.Equal(t, float64(90), *coding.Balances[0].Percent)
	require.Equal(t, int64(2000000000), coding.Balances[0].ResetAt)
	for _, bad := range []string{`{}`, `{"balances":"changed"}`, `{"balances":["changed"]}`} {
		_, e = ParseQuota([]byte(bad), PlanStart)
		require.Error(t, e)
	}
	zero := float64(0)
	positive := float64(1)
	q := Quota{Plan: PlanStart, Balances: []Balance{{Remaining: &zero}}}
	require.True(t, q.Exhausted(time.Now()))
	q.Balances = append(q.Balances, Balance{Remaining: &positive})
	require.False(t, q.Exhausted(time.Now()))
	q.Balances[1].ExpiresAt = 1
	require.True(t, q.Exhausted(time.Now()))
}
func TestIdentityParity(t *testing.T) {
	var expected map[string]string
	require.NoError(t, json.Unmarshal(fixture(t, "identity"), &expected))
	identity := DefaultIdentity("", "synthetic-device")
	identity.Platform = "linux-x64"
	for key, value := range expected {
		require.Equal(t, value, identity.Headers(true).Get(key), key)
	}
	require.Empty(t, identity.Headers(true).Get("X-Device-Mid"))
	require.Equal(t, "synthetic-device", identity.Headers(false).Get("X-Device-Mid"))
	identity.AppVersion = "x\r\nAuthorization: secret"
	require.Equal(t, "ZCode/unknown", identity.Headers(true).Get("User-Agent"))
}
func TestClaimPreviewAndResultParity(t *testing.T) {
	preview, e := ParsePreview(fixture(t, "preview"))
	require.NoError(t, e)
	require.True(t, preview.Claimable)
	require.Equal(t, "synthetic-plan", preview.Campaign.Plans[0].ID)
	for code, result := range map[int]string{1001: "none", 1002: "not_in_window", 1003: "already_claimed", 1004: "not_eligible", 1005: "quota_exhausted", 3001: "version_too_low", 3007: "captcha", 3012: "login_required"} {
		c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "Bearer synthetic-jwt", r.Header.Get("Authorization"))
			require.Equal(t, "3.14.0", r.Header.Get("X-ZCode-App-Version"))
			require.NotEmpty(t, r.Header.Get(CaptchaHeader))
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "synthetic-sensitive", "data": map[string]int64{"next_available_at": 2000000000}})
		})
		resultData, e := c.Claim(context.Background(), "synthetic-jwt", "synthetic-plan", CaptchaToken{Param: captchaParam("1"), Region: "cn"})
		require.NoError(t, e)
		require.Equal(t, result, resultData.Result)
		require.Equal(t, int64(2000000000), resultData.RetryAt)
	}
	require.True(t, VersionAtLeast("3.14.0", "3.13.9"))
	require.False(t, VersionAtLeast("3.14.0", "3.15.0"))
	require.False(t, VersionAtLeast("broken", "3.1.0"))
}
func TestEndpointRoutingUpdateAndFallback(t *testing.T) {
	var calls atomic.Int64
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		respond(w, json.RawMessage(fixture(t, "routing")))
	})
	resolver := NewResolver(func(s string) error {
		if strings.HasPrefix(s, "https://api.z.ai/") {
			return nil
		}
		return errors.New("拒绝非官方主机")
	})
	original := DefaultOrigin + StartMessagesPath
	require.Equal(t, "https://api.z.ai/api/v1/off-peak/anthropic/v1/messages", resolver.Resolve(context.Background(), c, original, "synthetic"))
	require.Equal(t, "https://api.z.ai/api/v1/off-peak/anthropic/v1/messages?test=1", resolver.Resolve(context.Background(), c, original+"?test=1", "synthetic"))
	require.Equal(t, int64(1), calls.Load())
	resolver.mu.Lock()
	resolver.expires = time.Time{}
	resolver.mu.Unlock()
	c.Send = func(*http.Request) (*http.Response, error) { return nil, errors.New("synthetic-network") }
	require.Equal(t, "https://api.z.ai/api/v1/off-peak/anthropic/v1/messages", resolver.Resolve(context.Background(), c, original, "synthetic"))
	fresh := NewResolver(nil)
	require.Equal(t, original, fresh.Resolve(context.Background(), c, original, "synthetic"))
}
func TestCaptchaCacheExpiryConcurrencyAndFailure(t *testing.T) {
	var configs atomic.Int64
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { configs.Add(1); captchaResponse(w) })
	pool := goodPool()
	cfg, e := pool.configuration(context.Background(), c)
	require.NoError(t, e)
	require.NoError(t, pool.Cache(CaptchaToken{Param: captchaParam("cached"), Region: "cn", Key: configKey(cfg), IssuedAt: time.Now()}))
	token, e := pool.Take(context.Background(), c, "")
	require.NoError(t, e)
	require.Equal(t, captchaParam("cached"), token.Param)
	require.NoError(t, pool.Cache(CaptchaToken{Param: captchaParam("expired"), Region: "cn", Key: configKey(cfg), IssuedAt: time.Now().Add(-2 * time.Minute)}))
	token, e = pool.Take(context.Background(), c, "")
	require.NoError(t, e)
	require.NotEqual(t, captchaParam("expired"), token.Param)
	seen := sync.Map{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, e := pool.Take(context.Background(), c, "")
			require.NoError(t, e)
			_, duplicate := seen.LoadOrStore(token.Param, true)
			require.False(t, duplicate)
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1), configs.Load())
	broken := NewCaptchaPool(solveFunc(func(context.Context, CaptchaConfig, string) (string, error) { return "changed-format", nil }))
	_, e = broken.Take(context.Background(), c, "")
	require.Error(t, e)
	failed := NewCaptchaPool(solveFunc(func(context.Context, CaptchaConfig, string) (string, error) {
		return "", errors.New("synthetic-secret")
	}))
	_, e = failed.Take(context.Background(), c, "")
	require.Error(t, e)
	require.NotContains(t, e.Error(), "synthetic-secret")
	blocked := NewCaptchaPool(solveFunc(func(ctx context.Context, _ CaptchaConfig, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, e = blocked.Take(ctx, c, "")
	require.ErrorIs(t, e, context.DeadlineExceeded)
}
func TestStartRequestStreamCaptchaRetryAndErrors(t *testing.T) {
	for _, status := range []int{200, 401, 429, 500, 402} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var messages atomic.Int64
			c := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "client/configs") {
					captchaResponse(w)
					return
				}
				if strings.Contains(r.URL.Path, "agent/configs") {
					respond(w, map[string]any{})
					return
				}
				messages.Add(1)
				require.Equal(t, StartMessagesPath, r.URL.Path)
				require.Equal(t, "Bearer synthetic-jwt", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Api-Key"))
				require.NotEmpty(t, r.Header.Get(CaptchaHeader))
				require.Contains(t, r.Header.Get("User-Agent"), "ai-sdk/anthropic/3.0.81")
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "glm-5.3", body["model"])
				require.Len(t, body["system"], 3)
				require.Len(t, body["messages"], 2)
				if status == 200 && messages.Load() == 1 {
					w.WriteHeader(403)
					_, _ = io.WriteString(w, `{"code":3007}`)
					return
				}
				w.WriteHeader(status)
				if status == 200 {
					_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
				} else {
					_, _ = io.WriteString(w, `{"code":0,"msg":"synthetic-sensitive"}`)
				}
			})
			req, _ := http.NewRequest("POST", c.OriginURL()+StartMessagesPath, nil)
			resp, upstreamErr, err := c.Forward(context.Background(), req, fixture(t, "start_request"), ForwardOptions{Provider: ZAI, Plan: PlanStart, Tokens: Tokens{JWT: "synthetic-jwt"}, Captcha: goodPool()})
			require.NoError(t, err)
			if status == 200 {
				require.Nil(t, upstreamErr)
				data, e := io.ReadAll(resp.Body)
				require.NoError(t, e)
				_ = resp.Body.Close()
				require.Contains(t, string(data), "message_start")
				require.Equal(t, int64(2), messages.Load())
			} else {
				require.Nil(t, resp)
				require.NotNil(t, upstreamErr)
				require.Equal(t, Classify(status, 0), upstreamErr.Kind)
				require.NotContains(t, upstreamErr.Error(), "synthetic-sensitive")
			}
		})
	}
}
func TestNetworkCancellation(t *testing.T) {
	c := mockClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, e := c.Preview(ctx, "synthetic")
	require.Error(t, e)
	require.Equal(t, ErrNetwork, KindOf(e))
}
func TestRedisAtomicConsumptionAndLeaseOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	c1 := redis.NewClient(&redis.Options{Addr: server.Addr()})
	c2 := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	a, b := &RedisStore{c1}, &RedisStore{c2}
	ctx := context.Background()
	require.NoError(t, a.Put(ctx, "session", "encrypted-fixture", time.Minute))
	v, e := a.Take(ctx, "session")
	require.NoError(t, e)
	require.Equal(t, "encrypted-fixture", v)
	v, e = b.Take(ctx, "session")
	require.NoError(t, e)
	require.Empty(t, v)
	ok, e := a.Acquire(ctx, "lock", "owner-a", time.Second)
	require.NoError(t, e)
	require.True(t, ok)
	ok, e = b.Acquire(ctx, "lock", "owner-b", time.Minute)
	require.NoError(t, e)
	require.False(t, ok)
	server.FastForward(2 * time.Second)
	ok, e = b.Acquire(ctx, "lock", "owner-b", time.Minute)
	require.NoError(t, e)
	require.True(t, ok)
	require.NoError(t, a.Release(ctx, "lock", "owner-a"))
	v, e = b.Get(ctx, "lock")
	require.NoError(t, e)
	require.Equal(t, "owner-b", v)
}
