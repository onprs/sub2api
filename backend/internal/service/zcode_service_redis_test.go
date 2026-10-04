//go:build unit

package service

// 本文件使用 miniredis 验证会话、锁与多实例语义；依赖 go-redis，按仓库约定
// 归入 unit 构建标签（参考 batch_image_worker_runtime_redis_test.go），
// 由 `go test -tags=unit ./...` 门禁执行。

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type zcodeTestRepo struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]*Account
}

func cloneZCodeAccount(a *Account) *Account {
	b := *a
	b.Credentials = nil
	b.Extra = nil
	raw, _ := json.Marshal(a.Credentials)
	_ = json.Unmarshal(raw, &b.Credentials)
	raw, _ = json.Marshal(a.Extra)
	_ = json.Unmarshal(raw, &b.Extra)
	return &b
}
func (r *zcodeTestRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.accounts[id]; a != nil {
		return cloneZCodeAccount(a), nil
	}
	return nil, errors.New("账号不存在")
}
func (r *zcodeTestRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if a.Extra == nil {
		a.Extra = map[string]any{}
	}
	for key, value := range updates {
		raw, _ := json.Marshal(value)
		var decoded any
		_ = json.Unmarshal(raw, &decoded)
		a.Extra[key] = decoded
	}
	return nil
}
func (r *zcodeTestRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[id].TempUnschedulableUntil = &until
	r.accounts[id].TempUnschedulableReason = reason
	return nil
}
func (r *zcodeTestRepo) ClearTempUnschedulable(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts[id].TempUnschedulableUntil = nil
	r.accounts[id].TempUnschedulableReason = ""
	return nil
}

// UpdateZCodeStateIfIdentityUnchanged 模拟生产 SQL 的身份条件、extra 合并与冷却语义。
func (r *zcodeTestRepo) UpdateZCodeStateIfIdentityUnchanged(_ context.Context, id int64, update ZCodeStateMutation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if a == nil {
		return false, nil
	}
	if a.GetCredential(zcodeTokensKey) != update.CredentialCipher ||
		string(a.ZCodeProvider()) != update.Provider ||
		a.GetAccountMode() != update.Plan {
		return false, nil
	}
	if (a.ProxyID == nil) != (update.ProxyID == nil) ||
		(a.ProxyID != nil && *a.ProxyID != *update.ProxyID) {
		return false, nil
	}
	if a.Extra == nil {
		a.Extra = map[string]any{}
	}
	for key, value := range update.Extra {
		raw, _ := json.Marshal(value)
		var decoded any
		_ = json.Unmarshal(raw, &decoded)
		a.Extra[key] = decoded
	}
	switch {
	case update.CooldownUntil != nil:
		until := *update.CooldownUntil
		a.TempUnschedulableUntil = &until
		a.TempUnschedulableReason = update.CooldownReason
	case update.ClearOwnCooldown && strings.HasPrefix(a.TempUnschedulableReason, "zcode_"):
		a.TempUnschedulableUntil = nil
		a.TempUnschedulableReason = ""
	}
	return true, nil
}
func (r *zcodeTestRepo) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := []Account{}
	for _, a := range r.accounts {
		if a.Platform == platform {
			rows = append(rows, *cloneZCodeAccount(a))
		}
	}
	return rows, nil
}

type zcodeTestCipher struct{ aead cipher.AEAD }

func (c zcodeTestCipher) Encrypt(raw string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	_, err := rand.Read(nonce)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c.aead.Seal(nonce, nonce, []byte(raw), nil)), nil
}
func (c zcodeTestCipher) Decrypt(raw string) (string, error) {
	data, e := base64.StdEncoding.DecodeString(raw)
	if e != nil || len(data) < c.aead.NonceSize() {
		return "", errors.New("密文无效")
	}
	plain, e := c.aead.Open(nil, data[:c.aead.NonceSize()], data[c.aead.NonceSize():], nil)
	return string(plain), e
}

type zcodeTestHTTP struct {
	HTTPUpstream
	do func(*http.Request) (*http.Response, error)
}

func (h *zcodeTestHTTP) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return h.do(r)
}
func zcodeTestResponse(status int, data any) *http.Response {
	raw, _ := json.Marshal(data)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}
}
func zcodeTestEnvelope(data any) *http.Response {
	return zcodeTestResponse(200, map[string]any{"code": 200, "data": data})
}

type zcodeTestSolver struct{ counter atomic.Int64 }

func (s *zcodeTestSolver) Solve(context.Context, zcode.CaptchaConfig, string) (string, error) {
	raw, _ := json.Marshal(map[string]string{"certifyId": fmt.Sprint(s.counter.Add(1)), "securityToken": strings.Repeat("synthetic-captcha-", 20)})
	return base64.StdEncoding.EncodeToString(raw), nil
}
func newZCodeTestService(t *testing.T, handler func(*http.Request) (*http.Response, error)) (*ZCodeService, *zcodeTestRepo, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	block, e := aes.NewCipher(make([]byte, 32))
	require.NoError(t, e)
	aead, e := cipher.NewGCM(block)
	require.NoError(t, e)
	encryptor := zcodeTestCipher{aead}
	tokens, _ := json.Marshal(zcode.Tokens{JWT: "synthetic-jwt", AccessToken: "synthetic-access", CodingKey: "synthetic-key"})
	encrypted, e := encryptor.Encrypt(string(tokens))
	require.NoError(t, e)
	repo := &zcodeTestRepo{accounts: map[int64]*Account{91: {ID: 91, Platform: PlatformZhipu, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"auth_mode": "zcode_oauth", "account_mode": AccountModeStart, "zcode_provider": "zai", zcodeTokensKey: encrypted, "zcode_device_id": "synthetic-device"}, Extra: map[string]any{}}}}
	cfg := &config.Config{}
	cfg.Totp.EncryptionKeyConfigured = true
	svc := NewZCodeService(repo, nil, &zcodeTestHTTP{do: handler}, encryptor, cfg, &zcode.RedisStore{Client: client}, nil)
	svc.solver = &zcodeTestSolver{}
	return svc, repo, server
}
func TestZCodeOAuthSessionEncryptedBoundAndTransactional(t *testing.T) {
	svc, repo, _ := newZCodeTestService(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/init"):
			return zcodeTestEnvelope(map[string]any{"flow_id": "synthetic-flow", "authorize_url": "https://chat.z.ai/oauth?state=synthetic", "expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 1}), nil
		case strings.Contains(r.URL.Path, "/poll/"):
			raw, _ := json.Marshal(map[string]int64{"iat": 1})
			jwt := "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".c3ludGhldGlj"
			return zcodeTestEnvelope(map[string]any{"status": "ready", "token": jwt, "zai": map[string]string{"access_token": "synthetic-access"}}), nil
		default:
			return nil, errors.New("非预期 endpoint")
		}
	})
	login, e := svc.StartLogin(context.Background(), "admin:1", ZCodeLoginInput{Provider: zcode.ZAI, Plan: AccountModeStart})
	require.NoError(t, e)
	stored, e := svc.store.Get(context.Background(), sessionStoreKey(login.SessionID))
	require.NoError(t, e)
	require.NotContains(t, stored, "synthetic-flow")
	_, e = svc.PollLogin(context.Background(), "admin:2", login.SessionID)
	require.Error(t, e)
	ready, e := svc.PollLogin(context.Background(), "admin:1", login.SessionID)
	require.NoError(t, e)
	require.Equal(t, "ready", ready.Status)
	output, _ := json.Marshal(ready)
	require.NotContains(t, string(output), "synthetic-access")
	require.NotContains(t, string(output), "jwt")
	_, _, e = svc.PrepareCredentials(context.Background(), "admin:1", 91, map[string]any{"zcode_oauth_session_id": login.SessionID})
	require.Error(t, e)
	creds, finish, e := svc.PrepareCredentials(context.Background(), "admin:1", 0, map[string]any{"zcode_oauth_session_id": login.SessionID})
	require.NoError(t, e)
	require.Empty(t, creds["api_key"])
	require.NotContains(t, creds[zcodeTokensKey], "synthetic-access")
	finish(false)
	_, finish, e = svc.PrepareCredentials(context.Background(), "admin:1", 0, map[string]any{"zcode_oauth_session_id": login.SessionID})
	require.NoError(t, e)
	finish(true)
	_, _, e = svc.PrepareCredentials(context.Background(), "admin:1", 0, map[string]any{"zcode_oauth_session_id": login.SessionID})
	require.Error(t, e)
	a, _ := repo.GetByID(context.Background(), 91)
	require.True(t, IsSensitiveCredentialKey(zcodeTokensKey))
	require.Equal(t, a.GetCredential(zcodeTokensKey), MergePreservingSensitiveCreds(a.Credentials, map[string]any{})[zcodeTokensKey])
	raw, _ := json.Marshal(map[string]any{"credentials": a.Credentials, "zcode_oauth_session_id": login.SessionID})
	audit := RedactAuditBody(raw, "application/json")
	require.NotContains(t, audit, a.GetCredential(zcodeTokensKey))
	require.NotContains(t, audit, login.SessionID)
}
func TestZCodeMultiInstanceClaimOnceAndServerCooldown(t *testing.T) {
	var claims atomic.Int64
	handler := func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "client/configs"):
			return zcodeTestEnvelope(map[string]any{"configs": map[string]any{"captcha": zcode.CaptchaConfig{Enabled: true, SceneID: "scene", Prefix: "prefix", Region: "cn"}}}), nil
		case strings.HasSuffix(r.URL.Path, "/preview"):
			return zcodeTestEnvelope(map[string]any{"campaign": map[string]any{"id": "synthetic-campaign", "name": "合成活动", "starts_at": 1, "ends_at": 2000000000, "min_app_version": "3.14.0", "plans": []any{map[string]any{"id": "synthetic-plan", "show_name": "合成套餐", "starts_at": 1, "ends_at": 2000000000}}}, "claimable": true}), nil
		case strings.HasSuffix(r.URL.Path, "/claim"):
			claims.Add(1)
			return zcodeTestEnvelope(map[string]any{"plan": map[string]any{"id": "synthetic-plan", "show_name": "合成套餐", "starts_at": time.Now().Add(time.Hour).Unix(), "ends_at": 2000000000}}), nil
		default:
			return nil, errors.New("非预期 endpoint")
		}
	}
	first, repo, redisServer := newZCodeTestService(t, handler)
	secondClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	defer func() { _ = secondClient.Close() }()
	second := NewZCodeService(repo, nil, first.upstream, first.cipher, first.cfg, &zcode.RedisStore{Client: secondClient}, nil)
	second.solver = &zcodeTestSolver{}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc := first
			if i%2 == 1 {
				svc = second
			}
			_, e := svc.CheckClaim(context.Background(), 91, true)
			require.NoError(t, e)
		}(i)
	}
	wg.Wait()
	require.Equal(t, int64(1), claims.Load())
	a, _ := repo.GetByID(context.Background(), 91)
	state := ZCodeClaimState{}
	require.NoError(t, decodeZCodeExtra(a.Extra[zcodeClaimKey], &state))
	require.Equal(t, "claimed", state.Result)
	require.Greater(t, state.NextAttempt, time.Now().Unix())
	require.Equal(t, state.Plan.StartsAt, state.NextAttempt-int64(accountJitter(91, time.Now())/time.Second))
	// Redis 重启后，持久化 next_attempt 仍阻止同周期重复领取。
	redisServer.FlushAll()
	_, e := second.CheckClaim(context.Background(), 91, true)
	require.NoError(t, e)
	require.Equal(t, int64(1), claims.Load())
}
func TestZCodeQuotaCacheNoTotalAndSchedulerState(t *testing.T) {
	var queries atomic.Int64
	svc, repo, _ := newZCodeTestService(t, func(r *http.Request) (*http.Response, error) {
		queries.Add(1)
		return zcodeTestEnvelope(map[string]any{"balances": []any{map[string]any{"remaining_units": 0, "show_name": "合成积分", "expires_at": 2000000000}}}), nil
	})
	quota, e := svc.QueryQuota(context.Background(), 91)
	require.NoError(t, e)
	require.Nil(t, quota.Balances[0].Total)
	_, e = svc.QueryQuota(context.Background(), 91)
	require.NoError(t, e)
	require.Equal(t, int64(1), queries.Load())
	a, _ := repo.GetByID(context.Background(), 91)
	require.False(t, a.IsSchedulable())
	require.NotEqual(t, StatusError, a.Status)
	require.Contains(t, a.TempUnschedulableReason, "zcode_quota_exhausted")
	svc.stateError(context.Background(), a, &zcode.Error{Kind: zcode.ErrLogin})
	a, _ = repo.GetByID(context.Background(), 91)
	require.False(t, a.IsSchedulable())
	require.Equal(t, StatusActive, a.Status)
}
func TestZCodeForwardFailureSanitizedAndBounded(t *testing.T) {
	var calls atomic.Int64
	svc, repo, _ := newZCodeTestService(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		switch {
		case strings.Contains(r.URL.Path, "client/configs"):
			return zcodeTestEnvelope(map[string]any{"configs": map[string]any{"captcha": zcode.CaptchaConfig{Enabled: true, SceneID: "s", Prefix: "p", Region: "cn"}}}), nil
		case strings.Contains(r.URL.Path, "agent/configs"):
			return zcodeTestEnvelope(map[string]any{}), nil
		default:
			return zcodeTestResponse(401, map[string]any{"code": 401, "msg": "synthetic-sensitive-token"}), nil
		}
	})
	a, _ := repo.GetByID(context.Background(), 91)
	request, _ := http.NewRequest("POST", zcode.DefaultOrigin+zcode.StartMessagesPath, strings.NewReader(`{"model":"glm-5.3","messages":[{"role":"user","content":"合成输入"}],"max_tokens":20}`))
	resp, e := svc.RoundTrip(request, "", a)
	require.NoError(t, e)
	require.Equal(t, 503, resp.StatusCode)
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NotContains(t, string(raw), "synthetic-sensitive")
	require.Contains(t, string(raw), "login_required")
	require.LessOrEqual(t, calls.Load(), int64(3))
	stored, _ := repo.GetByID(context.Background(), 91)
	require.Equal(t, StatusActive, stored.Status)
	require.False(t, stored.IsSchedulable())
}
