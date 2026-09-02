//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type countingReadCloser struct {
	io.Reader
	closeCount atomic.Int32
}

func (r *countingReadCloser) Close() error {
	r.closeCount.Add(1)
	return nil
}

func TestCollectCCUpstreamStreamParsesBoundedRecordsAndOwnsBody(t *testing.T) {
	body := &countingReadCloser{Reader: strings.NewReader(strings.Join([]string{
		": keepalive\r\n",
		"event: chat.chunk\r\n",
		"data: {\"id\":\"chatcmpl_structured\",\"object\":\"chat.completion.chunk\",\r\n",
		"data: \"model\":\"gpt-5.4\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\r\n",
		"\r\n",
		"data: [DONE]\r\n\r\n",
	}, ""))}
	svc := &OpenAIGatewayService{responseHeaderFilter: compileResponseHeaderFilter(rawChatCompletionsTestConfig())}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":      []string{"text/event-stream"},
			"X-Request-Id":      []string{"req-stream"},
			"X-Internal-Secret": []string{"must-not-pass"},
		},
		Body: body,
	}

	stream, err := svc.collectCCUpstreamStream(resp, time.Now().Add(-time.Second))
	require.NoError(t, err)
	require.Equal(t, protocolconv.ProtocolOpenAIChat, stream.ActualProtocol)
	require.Equal(t, "req-stream", stream.RequestID)
	require.GreaterOrEqual(t, stream.Duration, time.Second)
	require.Equal(t, "text/event-stream", stream.Headers.Get("Content-Type"))
	require.Empty(t, stream.Headers.Get("X-Internal-Secret"))

	var chunks int
	scan := svc.scanCCStreamEvents(stream, "test", time.Now(), func(chunk *apicompat.ChatCompletionsChunk) {
		chunks++
		require.Equal(t, "chatcmpl_structured", chunk.ID)
	})
	require.NoError(t, scan.Err)
	require.True(t, scan.SawDone)
	require.Equal(t, "chatcmpl_structured", scan.ResponseID)
	require.Equal(t, "chatcmpl_structured", stream.ResponseID)
	require.Equal(t, 7, scan.Usage.InputTokens)
	require.Equal(t, 3, scan.Usage.OutputTokens)
	require.Equal(t, 1, chunks)
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	require.Equal(t, int32(1), body.closeCount.Load())
}

func TestCollectBufferedChatCompletionsResponseReturnsStructuredActualProtocol(t *testing.T) {
	svc := &OpenAIGatewayService{responseHeaderFilter: compileResponseHeaderFilter(rawChatCompletionsTestConfig())}
	started := time.Now().Add(-time.Second)
	serviceTier := "priority"
	reasoningEffort := "high"
	upstreamBody := []byte(`{"id":"chatcmpl_structured","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5},"vendor_extension":{"trace":"preserved"}}`)

	structured, result, err := svc.collectBufferedChatCompletionsResponse(
		http.StatusOK,
		http.Header{
			"Content-Type":      []string{"application/json"},
			"X-Request-Id":      []string{"req-structured"},
			"X-Internal-Secret": []string{"must-not-pass"},
		},
		upstreamBody,
		OpenAIUsage{InputTokens: 3, OutputTokens: 2},
		"client-model",
		"billing-model",
		"upstream-model",
		&reasoningEffort,
		&serviceTier,
		started,
	)

	require.NoError(t, err)
	require.Equal(t, protocolconv.ProtocolOpenAIChat, structured.ActualProtocol)
	require.Equal(t, "req-structured", structured.RequestID)
	require.Equal(t, "chatcmpl_structured", structured.ResponseID)
	require.Equal(t, "application/json", structured.Headers.Get("Content-Type"))
	require.Empty(t, structured.Headers.Get("X-Internal-Secret"))
	require.Equal(t, "upstream-model", gjson.GetBytes(structured.Body, "model").String())
	require.Equal(t, "ok", gjson.GetBytes(structured.Body, "choices.0.message.content").String())
	require.Equal(t, "preserved", gjson.GetBytes(structured.Body, "vendor_extension.trace").String())
	require.Equal(t, upstreamBody, structured.Body)
	var ccResp apicompat.ChatCompletionsResponse
	require.NoError(t, json.Unmarshal(structured.Body, &ccResp))
	downstream := apicompat.ChatCompletionsResponseToResponses(&ccResp, "client-model", nil, false, nil)
	downstreamBody, err := json.Marshal(downstream)
	require.NoError(t, err)
	require.Equal(t, "client-model", gjson.GetBytes(downstreamBody, "model").String())
	require.Equal(t, "ok", gjson.GetBytes(downstreamBody, "output.0.content.0.text").String())
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, "billing-model", result.BillingModel)
	require.Equal(t, "upstream-model", result.UpstreamModel)
	require.Equal(t, "chatcmpl_structured", result.ResponseID)
	require.Equal(t, "priority", *result.ServiceTier)
	require.Equal(t, "high", *result.ReasoningEffort)
}

func TestForwardResponses_ForceChatCompletionsRoutesNonStreamingToChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_chat_json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_json","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"prompt_tokens_details":{"cached_tokens":1}}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, protocolconv.ProtocolOpenAIChat, result.ActualProtocol)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
	require.Empty(t, rec.Header().Get("x-request-id"))
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, 1, result.Usage.CacheReadInputTokens)
	require.False(t, result.Stream)
}

func TestForwardResponses_ChatFallbackPipelineRestoresExtendedTools(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{
		"model":"gpt-5.4","input":"use tools","stream":false,
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		]
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_extended_tools"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_extended","object":"chat.completion","model":"gpt-5.4",
			"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[
				{"id":"custom-1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"dir /b\"}"}},
				{"id":"search-1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"gmail\"}"}},
				{"id":"ns-1","type":"function","function":{"name":"gmail__send","arguments":"{\"to\":\"a@example.com\"}"}}
			]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}
		}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "exec", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
	require.Equal(t, "tool_search", gjson.GetBytes(upstream.lastBody, "tools.1.function.name").String())
	require.Equal(t, "gmail__send", gjson.GetBytes(upstream.lastBody, "tools.2.function.name").String())
	require.Equal(t, "custom_tool_call", gjson.Get(rec.Body.String(), "output.0.type").String())
	require.Equal(t, "dir /b", gjson.Get(rec.Body.String(), "output.0.input").String())
	require.Equal(t, "tool_search_call", gjson.Get(rec.Body.String(), "output.1.type").String())
	require.Equal(t, "client", gjson.Get(rec.Body.String(), "output.1.execution").String())
	require.Equal(t, "send", gjson.Get(rec.Body.String(), "output.2.name").String())
	require.Equal(t, "gmail", gjson.Get(rec.Body.String(), "output.2.namespace").String())
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

func TestForwardResponses_ChatFallbackPipelineRestoresExtendedToolsStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{
		"model":"gpt-5.4","input":"use tools","stream":true,
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		]
	}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_tools_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"custom-1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"dir"}},{"index":1,"id":"search-1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"gmail\"}"}},{"index":2,"id":"ns-1","type":"function","function":{"name":"gmail__send","arguments":"{\"to\":\"a@example.com\"}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_tools_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":" /b\"}"}}]},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_tools_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_extended_tools_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "exec", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
	require.Equal(t, "tool_search", gjson.GetBytes(upstream.lastBody, "tools.1.function.name").String())
	require.Equal(t, "gmail__send", gjson.GetBytes(upstream.lastBody, "tools.2.function.name").String())
	wire := rec.Body.String()
	require.Contains(t, wire, `"type":"custom_tool_call"`)
	require.Contains(t, wire, `event: response.custom_tool_call_input.done`)
	require.Contains(t, wire, `"input":"dir /b"`)
	require.Contains(t, wire, `"type":"tool_search_call"`)
	require.Contains(t, wire, `"execution":"client"`)
	require.Contains(t, wire, `"namespace":"gmail"`)
	require.Contains(t, wire, `"name":"send"`)
	require.Contains(t, wire, "event: response.completed")
	completed := responsesSSEEventData(t, wire, "response.completed")
	require.Equal(t, "gpt-5.4", gjson.Get(completed, "response.model").String())
	require.Equal(t, "custom_tool_call", gjson.Get(completed, "response.output.0.type").String())
	require.Equal(t, "tool_search_call", gjson.Get(completed, "response.output.1.type").String())
	require.Equal(t, "gmail", gjson.Get(completed, "response.output.2.namespace").String())
	require.Contains(t, wire, "data: [DONE]")
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

func TestForwardResponses_PassthroughFlagWithUnsupportedResponsesUsesAccountMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		path := path
		t.Run(path, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.4-channel","input":"hello","stream":false}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl_mapping","object":"chat.completion","model":"gpt-5.4-account","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				)),
			}}
			svc := &OpenAIGatewayService{
				cfg:          rawChatCompletionsTestConfig(),
				httpUpstream: upstream,
			}
			account := rawChatCompletionsTestAccount()
			account.Credentials["model_mapping"] = map[string]any{
				"gpt-5.4-channel": "gpt-5.4-account",
			}
			account.Credentials["compact_model_mapping"] = map[string]any{
				"gpt-5.4-account": "gpt-5.4-compact",
			}
			account.Extra = map[string]any{
				"openai_passthrough":                     true,
				openai_compat.ExtraKeyResponsesSupported: false,
			}

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
			require.Equal(t, "gpt-5.4-account", gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func TestForwardResponses_ForceChatCompletionsRoutesStreamingToChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"he"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: {"id":"chatcmpl_stream","object":"chat.completion.chunk","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_resp_chat_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"he"`)
	require.Contains(t, rec.Body.String(), "event: response.completed")
	require.Contains(t, rec.Body.String(), `"input_tokens":4`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.True(t, result.Stream)
	require.NotNil(t, result.FirstTokenMs)
}

func TestForwardResponses_ChatFallbackImmediateStream400BeforeSSECommit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := &openAICompatCloseTrackingBody{Reader: strings.NewReader(`{"error":{"message":"invalid input","type":"invalid_request_error"}}`)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_chat_400"}},
		Body:       upstreamBody,
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, "invalid input", gjson.Get(rec.Body.String(), "error.message").String())
	require.NotContains(t, rec.Body.String(), "event:")
	require.NotContains(t, rec.Body.String(), "data:")
	require.Equal(t, 1, upstreamBody.closeCount)
}

func TestForwardResponses_ChatFallbackMalformedStreamDoesNotFinalize(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_malformed"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"id":"chatcmpl_malformed","object":"chat.completion.chunk","model":"gpt-5.4","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":1}}`,
			"",
			`data: {broken}`,
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)

	require.ErrorContains(t, err, "stream usage incomplete")
	require.NotNil(t, result)
	require.Equal(t, 4, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.Equal(t, "chatcmpl_malformed", result.ResponseID)
	require.NotContains(t, rec.Body.String(), "response.completed")
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardResponses_DeepSeekReasoningOnlyStreamProducesVisibleText(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deepseek-reasoner","input":"hello","stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"role":"assistant","content":null,"reasoning_content":""},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"visible fallback"},"finish_reason":null}]}`,
		"",
		`data: {"id":"chatcmpl_reasoning","object":"chat.completion.chunk","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_deepseek_reasoning_responses_stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}

	result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, rec.Body.String(), "event: response.output_text.delta")
	require.Contains(t, rec.Body.String(), `"delta":"visible fallback"`)
	require.Contains(t, rec.Body.String(), `"status":"incomplete"`)
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestForwardResponses_AutoSupportedAccountStillUsesResponsesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","input":"hello","stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_resp_native"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_native","object":"response","model":"gpt-5.4","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`,
		)),
	}}
	svc := &OpenAIGatewayService{
		cfg:          rawChatCompletionsTestConfig(),
		httpUpstream: upstream,
	}
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
		openai_compat.ExtraKeyResponsesSupported: true,
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://upstream.example/v1/responses", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.Equal(t, "ok", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
}

func responsesSSEEventData(t *testing.T, wire, eventType string) string {
	t.Helper()
	lines := strings.Split(wire, "\n")
	for i, line := range lines {
		if line == "event: "+eventType && i+1 < len(lines) {
			return strings.TrimPrefix(lines[i+1], "data: ")
		}
	}
	t.Fatalf("SSE event %q not found", eventType)
	return ""
}

func forceChatResponsesFallbackAccount() *Account {
	account := rawChatCompletionsTestAccount()
	account.Extra = map[string]any{
		openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
	}
	return account
}
