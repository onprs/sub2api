//go:build unit

package service

import (
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestService_ClinePassGeneratesVisibleReply(t *testing.T) {
	account := &Account{
		ID:       91,
		Name:     "clinepass",
		Platform: PlatformClinePass,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key":  "cline-secret",
			"base_url": DefaultClinePassBaseURL,
			"model_mapping": map[string]any{
				"friendly-model": "cline-pass/glm-5.2",
			},
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{
			"success":true,
			"data":{
				"id":"chatcmpl-cline-test",
				"object":"chat.completion",
				"model":"cline-pass/glm-5.2",
				"choices":[{"index":0,"message":{"role":"assistant","content":"ClinePass replied successfully."},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}
			}
		}`),
	}}
	cfg := &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}
	svc := &AccountTestService{
		accountRepo:     repo,
		httpUpstream:    upstream,
		cfg:             cfg,
		clinePassClient: NewClinePassClient(upstream, cfg, nil),
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "friendly-model", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "https://api.cline.bot/api/v1/chat/completions", req.URL.String())
	require.Equal(t, "Bearer cline-secret", req.Header.Get("Authorization"))
	requestBody, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	require.Equal(t, "cline-pass/glm-5.2", gjson.GetBytes(requestBody, "model").String())
	require.Equal(t, accountGenerationTestPrompt, gjson.GetBytes(requestBody, "messages.0.content").String())
	require.Equal(t, int64(accountGenerationTestMaxTokens), gjson.GetBytes(requestBody, "max_tokens").Int())
	require.False(t, gjson.GetBytes(requestBody, "stream").Bool())

	sse := recorder.Body.String()
	require.Contains(t, sse, `"type":"test_start"`)
	require.Contains(t, sse, `"model":"friendly-model"`)
	require.Contains(t, sse, `"type":"content"`)
	require.Contains(t, sse, "ClinePass replied successfully.")
	require.Contains(t, sse, `"type":"test_complete"`)
	require.Contains(t, sse, `"success":true`)
}

func TestAccountTestService_ClinePassRejectsReasoningOnlyReply(t *testing.T) {
	account := &Account{
		ID:       92,
		Name:     "clinepass-empty",
		Platform: PlatformClinePass,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key": "cline-secret",
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{
			"success":true,
			"data":{
				"id":"chatcmpl-cline-empty",
				"object":"chat.completion",
				"model":"cline-pass/glm-5.2",
				"choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"thinking only"},"finish_reason":"length"}]
			}
		}`),
	}}
	cfg := &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}
	svc := &AccountTestService{
		accountRepo:     repo,
		httpUpstream:    upstream,
		cfg:             cfg,
		clinePassClient: NewClinePassClient(upstream, cfg, nil),
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "cline-pass/glm-5.2", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Contains(t, err.Error(), "returned no visible response text")
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}
