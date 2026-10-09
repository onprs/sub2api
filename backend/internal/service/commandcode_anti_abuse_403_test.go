//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// commandCodeAntiAbuse403Body 是 2026-10-09 事故中 Command Code 上游返回的
// 反滥用 403 原文：结构化 permission_error，文案为认证失败（账号凭据未失效）。
const commandCodeAntiAbuse403Body = `{"error":{"message":"Authentication failed. Please check your credentials.","type":"permission_error"}}`

type commandCode403TestHarness struct {
	svc     *RateLimitService
	repo    *rateLimitAccountRepoStub
	counter *countingOpenAI403CounterCache
	blocker *runtimeBlockRecorder
	account *Account
}

func newCommandCode403TestHarness(t *testing.T, accountID int64, counts ...int64) *commandCode403TestHarness {
	t.Helper()
	repo := &rateLimitAccountRepoStub{}
	counter := &countingOpenAI403CounterCache{openAI403CounterCacheStub: openAI403CounterCacheStub{counts: counts}}
	blocker := &runtimeBlockRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.SetOpenAI403CounterCache(counter)
	svc.SetAccountRuntimeBlocker(blocker)
	return &commandCode403TestHarness{
		svc:     svc,
		repo:    repo,
		counter: counter,
		blocker: blocker,
		account: &Account{ID: accountID, Platform: PlatformCommandCode, Type: AccountTypeAPIKey},
	}
}

func (h *commandCode403TestHarness) handle(body string) bool {
	return h.svc.HandleUpstreamError(
		context.Background(), h.account, http.StatusForbidden, http.Header{}, []byte(body),
		"inclusionai/ling-3.1-flash:free",
	)
}

// 事故场景：上游对短时高并发流量返回反滥用 403，403 又在 failover 状态集里
// 被逐账号重放。修复后这类 403 只做临时冷却，绝不写 error 永久禁用。
func TestHandleUpstreamError_CommandCodeAntiAbuse403TempCooldownOnly(t *testing.T) {
	// 计数器返回值故意取永久禁用阈值：反滥用 403 也不得消费升级计数。
	h := newCommandCode403TestHarness(t, 9101, openAI403DisableThreshold)

	require.True(t, h.handle(commandCodeAntiAbuse403Body), "反滥用 403 仍需 failover 信号")

	require.Equal(t, 0, h.repo.setErrorCalls, "反滥用 403 不得永久禁用账号")
	require.Equal(t, 1, h.repo.tempCalls, "反滥用 403 应临时冷却")
	require.Equal(t, h.account.ID, h.repo.lastTempID)
	require.Contains(t, h.repo.lastTempReason, commandCodeAntiAbuse403ReasonPrefix)
	require.Contains(t, h.repo.lastTempReason, "Authentication failed")
	require.Equal(t, 0, h.counter.increments, "反滥用 403 不得计入 403 升级计数")
	require.Len(t, h.blocker.accounts, 1, "反滥用 403 应触发调度阻断通知")
	require.Contains(t, h.blocker.reasons[0], commandCodeAntiAbuse403ReasonPrefix)
}

func TestHandleUpstreamError_CommandCodeAntiAbuse403RepeatedNeverDisables(t *testing.T) {
	// 即使计数器已处于阈值，反复命中反滥用 403 也不能把账号永久禁用。
	rounds := openAI403DisableThreshold + 3
	counts := make([]int64, rounds)
	for i := range counts {
		counts[i] = openAI403DisableThreshold
	}
	h := newCommandCode403TestHarness(t, 9102, counts...)

	for i := 0; i < rounds; i++ {
		require.True(t, h.handle(commandCodeAntiAbuse403Body), "第 %d 次反滥用 403 仍应 failover", i+1)
	}

	require.Equal(t, 0, h.repo.setErrorCalls, "反复反滥用 403 不得升级为永久禁用")
	require.Equal(t, rounds, h.repo.tempCalls)
	require.Equal(t, 0, h.counter.increments)
}

// HTML / Cloudflare 指纹响应属于链路级噪声，不构成账号证据。
func TestHandleUpstreamError_CommandCodeEdge403DoesNotPenalizeAccount(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"html_page", openAI403HTMLBody},
		{"cloudflare_1010", openAI403Cloudflare1010Body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCommandCode403TestHarness(t, 9103, 1)

			require.False(t, h.handle(tc.body), "链路级 403 不得判定账号应下线")

			require.Equal(t, 0, h.repo.setErrorCalls)
			require.Equal(t, 0, h.repo.tempCalls)
			require.Equal(t, 0, h.counter.increments)
			require.Empty(t, h.blocker.accounts)
		})
	}
}

// 对照不变式：语义不明的结构化 403 保留升级能力（计数 + 冷却 → 阈值后禁用），
// 但没有计数器时不得退回首次即永久禁用。
func TestHandleUpstreamError_CommandCodeStructured403StillEscalatesWithCounter(t *testing.T) {
	t.Run("first_hit_temp_unschedulable", func(t *testing.T) {
		h := newCommandCode403TestHarness(t, 9104, 1)

		require.True(t, h.handle(`{"error":{"message":"Your account is not authorized","type":"forbidden_error"}}`))
		require.Equal(t, 1, h.counter.increments)
		require.Equal(t, 1, h.repo.tempCalls)
		require.Equal(t, 0, h.repo.setErrorCalls)
	})

	t.Run("threshold_disables", func(t *testing.T) {
		h := newCommandCode403TestHarness(t, 9105, int64(openAI403DisableThreshold))

		require.True(t, h.handle(`{"error":{"message":"workspace forbidden by policy","type":"forbidden_error"}}`))
		require.Equal(t, 1, h.counter.increments)
		require.Equal(t, 1, h.repo.setErrorCalls, "持续的非反滥用 403 仍应升级为永久禁用")
	})
}

func TestHandleUpstreamError_CommandCodeStructured403WithoutCounterUsesTempCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 9106, Platform: PlatformCommandCode, Type: AccountTypeAPIKey}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(), account, http.StatusForbidden, http.Header{},
		[]byte(`{"error":{"message":"Your account is not authorized","type":"forbidden_error"}}`),
		"inclusionai/ling-3.1-flash:free",
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls, "计数器缺失时不得首次即永久禁用")
	require.Equal(t, 1, repo.tempCalls)
}

func TestIsCommandCodeAntiAbuse403(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		body string
		want bool
	}{
		{
			name: "事故原文",
			msg:  "Authentication failed. Please check your credentials.",
			body: commandCodeAntiAbuse403Body,
			want: true,
		},
		{
			name: "大小写与空白变体",
			msg:  "  AUTHENTICATION FAILED. Please check your credentials.  ",
			body: `{"error":{"message":"Authentication failed.","type":"PERMISSION_ERROR"}}`,
			want: true,
		},
		{
			name: "缺少 error.type",
			msg:  "Authentication failed. Please check your credentials.",
			body: `{"error":{"message":"Authentication failed."}}`,
			want: false,
		},
		{
			name: "其它错误类型",
			msg:  "Authentication failed. Please check your credentials.",
			body: `{"error":{"message":"Authentication failed.","type":"forbidden_error"}}`,
			want: false,
		},
		{
			name: "无认证失败文案",
			msg:  "Your account is not authorized",
			body: `{"error":{"message":"Your account is not authorized","type":"permission_error"}}`,
			want: false,
		},
		{
			name: "空响应",
			msg:  "",
			body: "",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isCommandCodeAntiAbuse403(tc.msg, []byte(tc.body)))
		})
	}
}
