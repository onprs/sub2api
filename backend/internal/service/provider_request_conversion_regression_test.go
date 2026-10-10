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
				response := providerRegressionJSONResponse(http.StatusOK, responseBody)
				if stream {
					response.Header.Set("Content-Type", "text/event-stream")
					response.Body = io.NopCloser(strings.NewReader("data: " + responseBody + "\n\ndata: [DONE]\n\n"))
				}
				upstream := &geminiCompatHTTPUpstreamStub{response: response}
				svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
				recorder, c := newProviderRegressionTestContext()
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

func TestGeminiSchemaPolicyScope(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		options := geminiProtocolConversionOptions(&Account{Platform: PlatformGemini, Type: accountType}, "model")
		require.Equal(t, accountType != AccountTypeOAuth, options.GoogleToolParametersJSONSchema)
		require.Equal(t, protocolconv.LossError, options.LossPolicy)
	}
	require.False(t, geminiProtocolConversionOptions(nil, "model").GoogleToolParametersJSONSchema)
	require.False(t, geminiProtocolConversionOptions(&Account{Platform: PlatformAntigravity, Type: AccountTypeAPIKey}, "model").GoogleToolParametersJSONSchema)
}
