package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReadOpenAICompatBufferedTerminalPreservesRawResponse(t *testing.T) {
	upstreamBody := `data: {"type":"response.completed","response":{"id":"resp_raw","model":"upstream-model","status":"completed","output":[],"vendor_extension":{"opaque":true}},"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}` + "\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstreamBody))}

	final, raw, usage, _, err := (&OpenAIGatewayService{}).readOpenAICompatBufferedTerminal(resp, "raw terminal test", "rid_raw")
	require.NoError(t, err)
	require.NotNil(t, final)
	require.Equal(t, "resp_raw", final.ID)
	require.JSONEq(t, `{"id":"resp_raw","model":"upstream-model","status":"completed","output":[],"vendor_extension":{"opaque":true}}`, string(raw))
	require.Equal(t, 4, usage.InputTokens)
	require.Equal(t, 1, usage.OutputTokens)
}

func TestPrepareOpenAICompatBufferedResponseBodyPreservesRawAndSupplementsTerminal(t *testing.T) {
	acc := apicompat.NewBufferedResponseAccumulator()
	acc.ProcessEvent(&apicompat.ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "restored"})
	final := &apicompat.ResponsesResponse{
		ID: "resp_raw", Object: "response", Model: "upstream-model", Status: "completed",
		Usage: &apicompat.ResponsesUsage{InputTokens: 4, OutputTokens: 1, TotalTokens: 5},
	}
	raw := []byte(`{"id":"resp_raw","object":"response","model":"upstream-model","status":"completed","output":[],"vendor_extension":{"opaque":true}}`)

	body, err := prepareOpenAICompatBufferedResponseBody(final, raw, acc)
	require.NoError(t, err)
	require.Equal(t, "restored", gjson.GetBytes(body, "output.0.content.0.text").String())
	require.Equal(t, 4, int(gjson.GetBytes(body, "usage.input_tokens").Int()))
	require.True(t, gjson.GetBytes(body, "vendor_extension.opaque").Bool())
}

func TestForwardAsAnthropic_ResponsesStreamUsesRequestPipeline(t *testing.T) {
	body := []byte(`{"model":"client-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"lookup"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`)
	upstreamBody := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_pipeline","model":"upstream-model","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":""}}`,
		"",
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_pipeline","name":"lookup","delta":"{\"q\":\"x\"}"}`,
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":"{\"q\":\"x\"}"}}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_pipeline","object":"response","model":"upstream-model","status":"completed","output":[{"type":"function_call","call_id":"call_pipeline","name":"lookup","arguments":"{\"q\":\"x\"}"}],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11,"input_tokens_details":{"cached_tokens":2}}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_pipeline"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
	}
	account := &Account{
		ID: 71, Name: "responses-pipeline", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-key", "base_url": "https://api.openai.com/v1",
			"model_mapping": map[string]any{"client-model": "upstream-model"},
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "client-model", result.Model)
	require.Equal(t, "upstream-model", result.UpstreamModel)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	wire := rec.Body.String()
	require.Contains(t, wire, "event: message_start\n")
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"type":"tool_use"`)
	require.Contains(t, wire, `"id":"call_pipeline"`)
	require.Contains(t, wire, `"name":"lookup"`)
	require.Contains(t, wire, `"partial_json":"{\"q\":\"x\"}"`)
	require.Contains(t, wire, `"stop_reason":"tool_use"`)
	require.Contains(t, wire, "event: message_stop\n")
	require.NotContains(t, wire, "data: [DONE]")
}
