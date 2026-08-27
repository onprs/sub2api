//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelNotFoundRateLimitCall struct {
	accountID int64
	scope     string
	resetAt   time.Time
	reason    string
}

type modelNotFoundAccountRepoStub struct {
	mockAccountRepoForGemini
	setErrorCalls       int
	tempCalls           int
	modelRateLimitCalls []modelNotFoundRateLimitCall
	modelRateLimitErr   error
}

func (r *modelNotFoundAccountRepoStub) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrorCalls++
	return nil
}

func (r *modelNotFoundAccountRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return nil
}

func (r *modelNotFoundAccountRepoStub) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	call := modelNotFoundRateLimitCall{
		accountID: id,
		scope:     scope,
		resetAt:   resetAt,
	}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, call)
	return r.modelRateLimitErr
}

func TestRateLimitService_HandleUpstreamError_ModelNotFoundUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAIModelNotFoundTempAccount()

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusNotFound,
		http.Header{},
		[]byte(`{"error":{"code":"model_not_found","message":"model not found"}}`),
		"gpt-5.4",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, "gpt-5.4", call.scope)
	require.Equal(t, upstreamModelNotFoundReason, call.reason)
	require.WithinDuration(t, time.Now().Add(upstreamModelNotFoundCooldown), call.resetAt, 5*time.Second)
}

func TestRateLimitService_HandleUpstreamError_ChatGPTCodexUnsupportedUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAIModelNotFoundTempAccount()
	account.Credentials["custom_error_codes_enabled"] = true
	account.Credentials["custom_error_codes"] = []any{float64(http.StatusTooManyRequests)}

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"error":{"message":"Model \"gpt-5.6-sol\" is not supported when using Codex with a ChatGPT account."}}`),
		"gpt-5.6-sol",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, "gpt-5.6-sol", call.scope)
	require.Equal(t, upstreamModelNotFoundReason, call.reason)
	require.WithinDuration(t, time.Now().Add(upstreamModelNotFoundCooldown), call.resetAt, 5*time.Second)
}

func TestOpenAICompatErrorResponse_ChatGPTCodexUnsupportedTriggersFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &modelNotFoundAccountRepoStub{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := openAIModelNotFoundTempAccount()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	upstream := protocoltransport.Response{
		StatusCode:     http.StatusBadRequest,
		Headers:        http.Header{"x-request-id": []string{"rid-codex-model"}},
		Body:           []byte(`{"error":{"message":"Model \"gpt-5.6-sol\" is not supported when using Codex with a ChatGPT account."}}`),
		ActualProtocol: protocolconv.ProtocolOpenAIChat,
		RequestID:      "rid-codex-model",
	}

	_, err := svc.handleChatCompletionsErrorResponse(upstream, c, account, "gpt-5.6-sol")

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadRequest, failoverErr.StatusCode)
	require.False(t, c.Writer.Written(), "handler owns the response after account failover")
	require.Empty(t, rec.Body.String())
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.6-sol", repo.modelRateLimitCalls[0].scope)
}

func TestRateLimitService_HandleUpstreamError_OpenCodeGoModelUnsupportedUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          202,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
		},
	}

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusUnauthorized,
		http.Header{},
		[]byte(`{"type":"error","error":{"type":"ModelError","message":"Model is not supported"}}`),
		"qwen3.7-max",
	)

	require.True(t, handled)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, "qwen3.7-max", call.scope)
	require.Equal(t, upstreamModelUnsupportedReason, call.reason)
	require.WithinDuration(t, time.Now().Add(openCodeGoModelUnsupportedCooldown), call.resetAt, 5*time.Second)
	require.True(t, account.IsSchedulable())
}

func TestRateLimitService_HandleUpstreamError_OpenCodeGoUnauthorizedStillSetsError(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          203,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusUnauthorized,
		http.Header{},
		[]byte(`{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`),
		"qwen3.7-max",
	)

	require.True(t, handled)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestOpenCodeGoGatewayServiceModelUnsupportedUsesMappedModelRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &modelNotFoundAccountRepoStub{}
	upstreamBody := `{"type":"error","error":{"type":"ModelError","message":"Model is not supported"}}`
	upstream := &openCodeGoHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		},
	}
	svc := &OpenCodeGoGatewayService{
		httpUpstream:     upstream,
		rateLimitService: &RateLimitService{accountRepo: repo},
	}
	account := &Account{
		ID:          204,
		Platform:    PlatformOpenCodeGo,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "ocg-secret",
			"model_mapping": map[string]any{
				"opencode-go/qwen": "qwen3.7-max",
			},
			"model_protocols": map[string]any{
				"qwen3.7-max": OpenCodeGoProtocolChatCompletions,
			},
		},
	}
	requestBody := []byte(`{"model":"opencode-go/qwen","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	rec := newTestGinContextRecorder(http.MethodPost, "/v1/chat/completions", string(requestBody))

	_, err := svc.ForwardChatCompletions(context.Background(), rec.Context, account, requestBody)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusUnauthorized, failoverErr.StatusCode)
	require.False(t, rec.Context.Writer.Written())
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "qwen3.7-max", repo.modelRateLimitCalls[0].scope)
	require.Equal(t, upstreamModelUnsupportedReason, repo.modelRateLimitCalls[0].reason)
	require.True(t, account.IsSchedulable())
}

func TestRateLimitService_HandleUpstreamError_ModelNotFoundWriteFailureDoesNotTempUnschedule(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{modelRateLimitErr: errors.New("write failed")}
	svc := &RateLimitService{accountRepo: repo}
	account := openAIModelNotFoundTempAccount()

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusNotFound,
		http.Header{},
		[]byte(`{"error":{"code":"model_not_found","message":"model not found"}}`),
		"gpt-5.4",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
}

func TestRateLimitService_HandleUpstreamError_Bare404KeepsTempUnschedulablePath(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAIModelNotFoundTempAccount()

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusNotFound,
		http.Header{},
		[]byte(`{"error":{"message":"endpoint not found"}}`),
		"gpt-5.4",
	)

	require.True(t, handled)
	require.Equal(t, 1, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func openAIModelNotFoundTempAccount() *Account {
	return &Account{
		ID:          101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusNotFound),
					"keywords":         []any{"not found"},
					"duration_minutes": float64(10),
				},
			},
		},
	}
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAICodexPlanGatedOAuthAccount()

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`),
		"gpt-5.6-sol",
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, "gpt-5.6-sol", call.scope)
	require.Equal(t, upstreamCodexPlanGatedModelReason, call.reason)
	require.WithinDuration(t, time.Now().Add(upstreamCodexPlanGatedModelCooldown), call.resetAt, 5*time.Second)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelRespectsModelMapping(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAICodexPlanGatedOAuthAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.6-sol": "gpt-5.6-sol-upstream"}

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol-upstream' model is not supported when using Codex with a ChatGPT account."}`),
		"gpt-5.6-sol",
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.6-sol-upstream", repo.modelRateLimitCalls[0].scope)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelIgnoresAPIKeyAccount(t *testing.T) {
	repo := &modelNotFoundAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := openAICodexPlanGatedOAuthAccount()
	account.Type = AccountTypeAPIKey

	handled := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`),
		"gpt-5.6-sol",
	)

	// 定制语义：ChatGPT Codex unsupported 400 对 APIKey 账号同样确定性 failover
	// （isUpstreamModelNotFoundErrorForAccount 的 400 分支），但 reason 是
	// model-not-found 而非 OAuth 专属的 plan-gated reason。
	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, upstreamModelNotFoundReason, repo.modelRateLimitCalls[0].reason)
}

func openAICodexPlanGatedOAuthAccount() *Account {
	return &Account{
		ID:          202,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{},
	}
}
