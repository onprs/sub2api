//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stickySettingRepoStub struct {
	service.SettingRepository
}

func (stickySettingRepoStub) GetValue(_ context.Context, _ string) (string, error) {
	return "", service.ErrSettingNotFound
}

type stickyMemoryGatewayCache struct {
	testutil.StubGatewayCache
	mu      sync.Mutex
	entries map[string]int64
}

func (c *stickyMemoryGatewayCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.entries[fmt.Sprintf("%d:%s", groupID, sessionHash)]
	if !ok {
		return 0, service.ErrStickySessionNotFound
	}
	return id, nil
}

func (c *stickyMemoryGatewayCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]int64)
	}
	c.entries[fmt.Sprintf("%d:%s", groupID, sessionHash)] = accountID
	return nil
}

func (c *stickyMemoryGatewayCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, fmt.Sprintf("%d:%s", groupID, sessionHash))
	return nil
}

func (c *stickyMemoryGatewayCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

type stickyCaptureUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	bodies     []string
}

func (u *stickyCaptureUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	raw, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.bodies = append(u.bodies, string(raw))
	u.mu.Unlock()

	sse := "data: {\"response\":{\"responseId\":\"resp-1\",\"modelVersion\":\"gemini-4-hydrogen\",\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"cachedContentTokenCount\":8,\"candidatesTokenCount\":2,\"totalTokenCount\":12}}}\n\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}, nil
}

func newAntigravityStickyTestHandler(t *testing.T, groupPlatform string, accountIDs ...int64) (*GatewayHandler, *stickyCaptureUpstream, *service.APIKey, *service.Group) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.MaxLineSize = 40 * 1024 * 1024
	groupID := int64(20)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: groupPlatform, Status: service.StatusActive, RateMultiplier: 1}
	accounts := make(map[int64]*service.Account, len(accountIDs))
	scheduled := make([]*service.Account, 0, len(accountIDs))
	lastUsed := time.Now().Add(-time.Hour)
	for i, id := range accountIDs {
		used := lastUsed.Add(time.Duration(i) * time.Minute)
		acc := &service.Account{
			ID:            id,
			Name:          fmt.Sprintf("ag-%d", id),
			Platform:      service.PlatformAntigravity,
			Type:          service.AccountTypeOAuth,
			Status:        service.StatusActive,
			Schedulable:   true,
			Concurrency:   5,
			Priority:      1,
			LastUsedAt:    &used,
			Credentials:   map[string]any{"access_token": "tok", "project_id": "proj-1"},
			AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: groupID}},
		}
		accounts[id] = acc
		scheduled = append(scheduled, acc)
	}

	cache := &stickyMemoryGatewayCache{}
	digestStore := service.NewDigestSessionStore()
	t.Cleanup(digestStore.Close)
	userRepo := &cachedBillingUserRepo{balance: 500}
	usage := &cachedBillingUsageRepo{}
	bcs := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(bcs.Stop)
	snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: scheduled}, nil, nil, nil, nil)
	accountRepo := &cachedBillingAccountRepo{accounts: accounts}
	upstream := &stickyCaptureUpstream{}
	gw := service.NewGatewayService(
		accountRepo, &fakeGroupRepo{group: group}, usage, nil, userRepo, nil, nil, cache, cfg,
		snapshot, nil, service.NewBillingService(cfg, nil), nil, &service.BillingCacheService{},
		nil, upstream, &service.DeferredService{}, nil, nil, nil, digestStore, nil,
		nil, nil, nil, nil, nil, nil,
	)
	settingSvc := service.NewSettingService(&stickySettingRepoStub{}, cfg)
	agSvc := service.NewAntigravityGatewayService(
		accountRepo, cache, snapshot, &service.AntigravityTokenProvider{}, nil, upstream, settingSvc, nil,
	)
	h := &GatewayHandler{
		gatewayService:            gw,
		antigravityGatewayService: agSvc,
		billingCacheService:       bcs,
		billingEligibilityService: allowBillingEligibilityResolver{},
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatNone, 0),
		maxAccountSwitches:        3,
		maxAccountSwitchesGemini:  3,
		cfg:                       cfg,
	}
	apiKey := &service.APIKey{
		ID: 15, UserID: 1, GroupID: &groupID, Group: group, Status: service.StatusActive,
		User: &service.User{ID: 1, Concurrency: 10, Balance: 500},
	}
	return h, upstream, apiKey, group
}

func TestGeminiV1BetaModels_MultiTurnRotatingIPStickySessionPreservesSignaturesAndAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, upstream, apiKey, group := newAntigravityStickyTestHandler(t, service.PlatformAntigravity, 5188, 8270, 8305)

	sendTurn := func(clientIP, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodPost, "/v1/models/gemini-4-hydrogen:streamGenerateContent", strings.NewReader(body))
		req.Header.Set("User-Agent", "pi/1.5.2 (linux; node/v24.22.1; x64)")
		req.Header.Set("X-Forwarded-For", clientIP)
		req.RemoteAddr = clientIP + ":12345"
		c.Request = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
		c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-4-hydrogen:streamGenerateContent"}}
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
		h.GeminiV1BetaModels(c)
		return rec
	}

	turn1 := `{"systemInstruction":{"parts":[{"text":"sys"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	turn2 := `{"systemInstruction":{"parts":[{"text":"sys"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]},{"role":"model","parts":[{"text":"thinking","thought":true,"thoughtSignature":"real-sig-turn1"},{"text":"hi"}]},{"role":"user","parts":[{"text":"next"}]}]}`
	turn3 := `{"systemInstruction":{"parts":[{"text":"sys"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]},{"role":"model","parts":[{"text":"thinking","thought":true,"thoughtSignature":"real-sig-turn1"},{"text":"hi"}]},{"role":"user","parts":[{"text":"next"}]},{"role":"model","parts":[{"text":"thinking2","thought":true,"thoughtSignature":"real-sig-turn2"},{"text":"ok"}]},{"role":"user","parts":[{"text":"more"}]}]}`

	rec1 := sendTurn("111.2.129.139", turn1)
	require.Equal(t, http.StatusOK, rec1.Code, rec1.Body.String())

	rec2 := sendTurn("36.150.72.107", turn2)
	require.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())

	rec3 := sendTurn("112.13.210.33", turn3)
	require.Equal(t, http.StatusOK, rec3.Code, rec3.Body.String())

	require.Len(t, upstream.accountIDs, 3)
	require.Equal(t, upstream.accountIDs[0], upstream.accountIDs[1], "turn 2 must stay on the same account despite IP change")
	require.Equal(t, upstream.accountIDs[0], upstream.accountIDs[2], "turn 3 must stay on the same account despite IP change")
	require.Contains(t, upstream.bodies[1], `"thoughtSignature":"real-sig-turn1"`, "sticky hit must not clean thoughtSignature")
	require.Contains(t, upstream.bodies[2], `"thoughtSignature":"real-sig-turn2"`, "sticky hit must preserve thoughtSignature chain")
	require.NotContains(t, upstream.bodies[1], "skip_thought_signature_validator")
	require.NotContains(t, upstream.bodies[2], "skip_thought_signature_validator")
}

func TestChatCompletions_MultiTurnStickySessionAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, upstream, apiKey, group := newAntigravityStickyTestHandler(t, service.PlatformAntigravity, 5188, 8270, 8305)

	sendTurn := func(clientIP, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("User-Agent", "magpie/0.1.4303")
		req.Header.Set("X-Forwarded-For", clientIP)
		req.RemoteAddr = clientIP + ":12345"
		c.Request = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
		h.ChatCompletions(c)
		return rec
	}

	turn1 := `{"model":"gemini-4-argon","stream":true,"messages":[{"role":"system","content":"sys"},{"role":"user","content":"step 1"}]}`
	turn2 := `{"model":"gemini-4-argon","stream":true,"messages":[{"role":"system","content":"sys"},{"role":"user","content":"step 1"},{"role":"assistant","content":"reply 1"},{"role":"user","content":"step 2"}]}`
	turn3 := `{"model":"gemini-4-argon","stream":true,"messages":[{"role":"system","content":"sys"},{"role":"user","content":"step 1"},{"role":"assistant","content":"reply 1"},{"role":"user","content":"step 2"},{"role":"assistant","content":"reply 2"},{"role":"user","content":"step 3"}]}`

	require.Equal(t, http.StatusOK, sendTurn("50.7.250.106", turn1).Code)
	require.Equal(t, http.StatusOK, sendTurn("50.7.250.106", turn2).Code)
	require.Equal(t, http.StatusOK, sendTurn("50.7.250.200", turn3).Code)

	require.Len(t, upstream.accountIDs, 3)
	require.Equal(t, upstream.accountIDs[0], upstream.accountIDs[1], "multi-turn chat completions must stick to same account")
	require.Equal(t, upstream.accountIDs[0], upstream.accountIDs[2], "multi-turn chat completions must stick even across IP changes")
}

func TestGeminiV1BetaModels_AntigravityClaudeModelAvoidsInvalidThinkingSignatureOnMissingBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, upstream, apiKey, group := newAntigravityStickyTestHandler(t, service.PlatformAntigravity, 5188)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := `{
		"contents":[
			{"role":"user","parts":[{"text":"hello"}]},
			{"role":"model","parts":[
				{"text":"secret plan","thought":true,"thoughtSignature":"old-account-signature"},
				{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"x"}},"thoughtSignature":"old-account-signature"}
			]},
			{"role":"user","parts":[{"functionResponse":{"id":"call-1","name":"lookup","response":{"result":"ok"}}}]}
		],
		"generationConfig":{"thinkingConfig":{"includeThoughts":true}}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/models/claude-opus-5-5:streamGenerateContent", strings.NewReader(body))
	req.Header.Set("User-Agent", "pi/1.5.2 (linux; node/v24.22.1; x64)")
	c.Request = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Params = gin.Params{{Key: "modelAction", Value: "/claude-opus-5-5:streamGenerateContent"}}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})
	h.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, upstream.bodies, 1)
	require.NotContains(t, upstream.bodies[0], "skip_thought_signature_validator", "must never forward dummy thoughtSignature to Antigravity Claude")
	require.NotContains(t, upstream.bodies[0], `"thought":true`, "downgraded Claude thinking block must not retain thought=true without valid signature")
	require.NotContains(t, upstream.bodies[0], `"thinkingConfig"`, "thinkingConfig must stay disabled after thinking block downgrade")
}
