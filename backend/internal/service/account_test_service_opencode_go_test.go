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

func TestAccountTestService_OpenCodeGoGeneratesChatCompletion(t *testing.T) {
	account := &Account{
		ID:       88,
		Name:     "opencode-go",
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_mapping": map[string]any{
				"friendly-kimi": "kimi-k2.7-code",
			},
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{
			"id":"chatcmpl-test",
			"model":"kimi-k2.7-code",
			"choices":[{"index":0,"message":{"role":"assistant","content":"OpenCode Go replied successfully."},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}
		}`),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "friendly-kimi", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "https://opencode.ai/zen/go/v1/chat/completions", req.URL.String())
	require.Equal(t, "Bearer ocg-secret", req.Header.Get("Authorization"))
	requestBody, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	require.Equal(t, "kimi-k2.7-code", gjson.GetBytes(requestBody, "model").String())
	require.Equal(t, accountGenerationTestPrompt, gjson.GetBytes(requestBody, "messages.0.content").String())
	require.Equal(t, int64(accountGenerationTestMaxTokens), gjson.GetBytes(requestBody, "max_tokens").Int())
	require.False(t, gjson.GetBytes(requestBody, "stream").Bool())

	sse := recorder.Body.String()
	require.Contains(t, sse, `"type":"test_start"`)
	require.Contains(t, sse, `"model":"friendly-kimi"`)
	require.Contains(t, sse, `"type":"content"`)
	require.Contains(t, sse, "OpenCode Go replied successfully.")
	require.Contains(t, sse, `"type":"test_complete"`)
	require.Contains(t, sse, `"success":true`)
}

func TestAccountTestService_OpenCodeGoGeneratesThroughMessagesProtocol(t *testing.T) {
	account := &Account{
		ID:       89,
		Name:     "opencode-go-messages",
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key":  "ocg-secret",
			"base_url": "https://opencode.ai/zen/go/v1",
			"model_protocols": map[string]any{
				"qwen3.7-plus": OpenCodeGoProtocolMessages,
			},
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"model":"qwen3.7-plus",
			"content":[{"type":"thinking","thinking":"internal"},{"type":"text","text":"Messages endpoint replied successfully."}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":4}
		}`),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "qwen3.7-plus", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, "https://opencode.ai/zen/go/v1/messages", req.URL.String())
	require.Equal(t, "ocg-secret", req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
	requestBody, readErr := io.ReadAll(req.Body)
	require.NoError(t, readErr)
	require.Equal(t, "qwen3.7-plus", gjson.GetBytes(requestBody, "model").String())
	require.Equal(t, accountGenerationTestPrompt, gjson.GetBytes(requestBody, "messages.0.content.0.text").String())
	require.Contains(t, recorder.Body.String(), "Messages endpoint replied successfully.")
	require.NotContains(t, recorder.Body.String(), "internal")
}

func TestAccountTestService_OpenCodeGoRejectsReasoningOnlyResponse(t *testing.T) {
	account := &Account{
		ID:       90,
		Name:     "opencode-go-empty",
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
		Credentials: map[string]any{
			"api_key": "ocg-secret",
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{
			"id":"chatcmpl-empty",
			"model":"kimi-k2.7-code",
			"choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"thinking only"},"finish_reason":"length"}]
		}`),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	ctx, recorder := newTestContext()

	err := svc.TestAccountConnection(ctx, account.ID, "kimi-k2.7-code", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Contains(t, err.Error(), "returned no visible response text")
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountGenerationTestModelUsesSortedExplicitMapping(t *testing.T) {
	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"z-model": "upstream-z",
			"a-model": "upstream-a",
		},
	}}

	model, err := accountGenerationTestModel(account, "")
	require.NoError(t, err)
	require.Equal(t, "a-model", model)
}
