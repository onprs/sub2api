package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeAnthropicProviderOutputRendersGoogleClient(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "streaming"}[stream], func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/public-model:generateContent", nil)
			pipeline, _, err := newGoogleGenAIResponsesAttempt([]byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"sessions_lookup","parameters":{"type":"object","properties":{"term":{"type":"string"}}}}]}]}`), protocolconv.PipelineConfig{
				Route: protocolconv.Route{
					Source: protocolconv.ProtocolGoogleGenAI, IntendedTarget: protocolconv.ProtocolOpenAIResponses,
					ClientModel: "public-model", UpstreamModel: "claude-upstream", Provider: PlatformCommandCode,
				},
				Options: protocolconv.Options{SourceModel: "claude-upstream", LossPolicy: protocolconv.LossError},
			}, stream)
			require.NoError(t, err)
			output, err := newGoogleGenAIProtocolOutput(c.Writer, pipeline, stream)
			require.NoError(t, err)
			wire := strings.Join([]string{
				`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_native","type":"message","role":"assistant","model":"claude-upstream","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}`,
				`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
				`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
				`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_lookup","name":"cc_sess_lookup","input":{}}}`,
				`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"term\":\"sample\"}"}}`,
				`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":1}`,
				`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}`,
				`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
			}, "\n\n") + "\n\n"
			response := &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"native-request"}},
				Body:   io.NopCloser(strings.NewReader(wire)),
			}
			svc := &OpenAIGatewayService{}
			var result *OpenAIForwardResult
			if stream {
				result, err = svc.handleResponsesStreamingFromNativeAnthropicWithOutput(response, c,
					"public-model", "claude-upstream", "claude-upstream", nil, time.Now(), apicompat.ResponsesClientToolMapping{}, output)
			} else {
				result, err = svc.handleResponsesBufferedFromNativeAnthropicWithOutput(response, c,
					"public-model", "claude-upstream", "claude-upstream", nil, time.Now(), apicompat.ResponsesClientToolMapping{}, output)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "native-request", result.RequestID)
			require.Equal(t, "public-model", result.Model)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), `"candidates"`)
			require.Contains(t, recorder.Body.String(), `"text":"hello"`)
			require.Contains(t, recorder.Body.String(), `"name":"sessions_lookup"`)
			require.NotContains(t, recorder.Body.String(), "cc_sess_lookup")
			require.NotContains(t, recorder.Body.String(), "response.output_text")
			require.NotContains(t, recorder.Body.String(), `"type":"message_start"`)
		})
	}
}
