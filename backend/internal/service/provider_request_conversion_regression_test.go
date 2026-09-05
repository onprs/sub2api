package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestGeminiIngressPreservesNestedToolJSONSchema(t *testing.T) {
	for _, source := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolAnthropic} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", source, stream), func(t *testing.T) {
				responseBody := `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`
				response := commandCodeJSONResponse(http.StatusOK, responseBody)
				if stream {
					response.Header.Set("Content-Type", "text/event-stream")
					response.Body = io.NopCloser(strings.NewReader("data: " + responseBody + "\n\ndata: [DONE]\n\n"))
				}
				upstream := &geminiCompatHTTPUpstreamStub{response: response}
				svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
				recorder, c := newCommandCodeTestContext()
				schema := `{"type":"object","properties":{"records":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"labels":{"type":"object","additionalProperties":{"type":"string"}}},"required":["labels"]}}},"additionalProperties":false,"required":["records"]}`
				var body []byte
				switch source {
				case protocolconv.ProtocolOpenAIResponses:
					body = []byte(`{"model":"responses-client","input":"check","tools":[{"type":"function","name":"validate_records","parameters":` + schema + `}]}`)
				case protocolconv.ProtocolOpenAIChat:
					body = []byte(`{"model":"responses-client","messages":[{"role":"user","content":"check"}],"tools":[{"type":"function","function":{"name":"validate_records","parameters":` + schema + `}}]}`)
				case protocolconv.ProtocolAnthropic:
					body = []byte(`{"model":"responses-client","max_tokens":32,"messages":[{"role":"user","content":"check"}],"tools":[{"name":"validate_records","input_schema":` + schema + `}]}`)
				}
				body, err := sjson.SetBytes(body, "stream", stream)
				require.NoError(t, err)
				account := geminiResponsesAPIKeyAccount()
				if source == protocolconv.ProtocolAnthropic {
					_, converted, err := newClaudeMessagesGooglePipeline(account, body, "responses-client", "gemini-upstream")
					require.NoError(t, err)
					declaration := gjson.GetBytes(converted, "tools.0.functionDeclarations.0")
					require.False(t, declaration.Get("parameters").Exists())
					require.JSONEq(t, schema, declaration.Get("parametersJsonSchema").Raw)
					return
				}
				var result *ForwardResult
				if source == protocolconv.ProtocolOpenAIResponses {
					result, err = svc.ForwardAsResponses(context.Background(), c, account, body)
				} else {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, stream, result.Stream)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.NotNil(t, upstream.lastReq)
				upstreamBody, err := io.ReadAll(upstream.lastReq.Body)
				require.NoError(t, err)
				declaration := gjson.GetBytes(upstreamBody, "tools.0.functionDeclarations.0")
				require.False(t, declaration.Get("parameters").Exists())
				require.JSONEq(t, schema, declaration.Get("parametersJsonSchema").Raw)
			})
		}
	}
}

func TestCommandCodeRequestSignaturePolicyScope(t *testing.T) {
	body := []byte(`{"model":"model","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"check"}],"encrypted_content":"test-signature"},{"role":"user","content":"continue"}]}`)
	account := commandCodeGatewayTestAccount()
	pipeline, converted, err := newCommandCodePipelineRequest(body, protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolOpenAIChat, account, "model", "model")
	require.NoError(t, err)
	require.NotContains(t, string(converted), "test-signature")
	warnings := pipeline.Warnings()
	require.Len(t, warnings, 1)
	require.Equal(t, protocolconv.CapabilitySignature, warnings[0].Capability)
	require.Equal(t, protocolconv.WarningDroppedField, warnings[0].Code)

	_, _, err = newOpenCodeGoPipelineRequest(body, protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolOpenAIChat, &Account{Platform: PlatformOpenCodeGo}, "model", "model")
	require.Error(t, err)
	_, converted, err = newCommandCodePipelineRequest(body, protocolconv.ProtocolOpenAIResponses, protocolconv.ProtocolAnthropic, account, "model", "model")
	require.NoError(t, err)
	require.Contains(t, string(converted), "test-signature")

	messages := []byte(`{"model":"model","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"check","signature":"test-signature"}]},{"role":"user","content":"continue"}]}`)
	_, _, err = newCommandCodePipelineRequest(messages, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, account, "model", "model")
	require.Error(t, err)
}

func TestGeminiSchemaPolicyScope(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		options := geminiProtocolConversionOptions(&Account{Platform: PlatformGemini, Type: accountType}, "model")
		require.Equal(t, accountType != AccountTypeOAuth, options.GoogleToolParametersJSONSchema)
		require.Equal(t, protocolconv.LossError, options.LossPolicy)
	}
	require.False(t, geminiProtocolConversionOptions(nil, "model").GoogleToolParametersJSONSchema)
	require.False(t, geminiProtocolConversionOptions(&Account{Platform: PlatformAntigravity, Type: AccountTypeAPIKey}, "model").GoogleToolParametersJSONSchema)
}

func TestCommandCodeResponsesAcceptsSignedReasoningHistory(t *testing.T) {
	for _, tc := range []struct {
		summary string
		stream  bool
	}{
		{`[]`, false}, {`[]`, true},
		{`[{"type":"summary_text","text":"check records"}]`, false},
		{`[{"type":"summary_text","text":"check records"}]`, true},
	} {
		summary := tc.summary
		t.Run(fmt.Sprintf("stream=%t/summary=%s", tc.stream, summary), func(t *testing.T) {
			upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
				"/provider/v1/chat/completions": commandCodeJSONResponse(http.StatusOK, `{"id":"chat-history","model":"google/gemini-3.8-flash","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`),
			}}
			if tc.stream {
				upstream.responses["/provider/v1/chat/completions"] = &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: " + `{"id":"chat-history","model":"google/gemini-3.8-flash","choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}` + "\n\ndata: [DONE]\n\n")),
				}
			}
			svc := newCommandCodeGatewayForTest(upstream)
			recorder, c := newCommandCodeTestContext()
			body := []byte(`{"model":"google/gemini-3.8-flash","input":[{"role":"user","content":"check"},{"type":"reasoning","summary":` + summary + `,"encrypted_content":"test-opaque-signature"},{"type":"function_call","call_id":"call-history","name":"validate_records","arguments":"{\"value\":1}"},{"type":"function_call_output","call_id":"call-history","output":"valid"},{"role":"user","content":"continue"}],"tools":[{"type":"function","name":"validate_records","parameters":{"type":"object","properties":{"value":{"type":"integer"}}}}]}`)
			body, err := sjson.SetBytes(body, "stream", tc.stream)
			require.NoError(t, err)
			result, err := svc.ForwardResponses(context.Background(), c, commandCodeGatewayTestAccount(), body, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, tc.stream, result.Stream)
			if tc.stream {
				require.Contains(t, recorder.Body.String(), "event: response.completed\n")
				require.True(t, strings.HasSuffix(recorder.Body.String(), "data: [DONE]\n\n"))
			}
			require.Len(t, upstream.requests, 1)
			upstreamBody, err := io.ReadAll(upstream.requests[0].Body)
			require.NoError(t, err)
			require.NotContains(t, string(upstreamBody), "test-opaque-signature")
			require.NotContains(t, string(upstreamBody), "encrypted_content")
			require.Contains(t, string(upstreamBody), `"content":"continue"`)
			require.Contains(t, string(upstreamBody), `"tool_call_id":"call-history"`)
			require.Contains(t, string(upstreamBody), `"id":"call-history"`)
			require.Contains(t, string(upstreamBody), `"content":"valid"`)
			if strings.Contains(summary, "check records") {
				require.Contains(t, string(upstreamBody), "check records")
			}
		})
	}
}
