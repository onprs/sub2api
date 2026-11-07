package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAICompatBufferedReasoningContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A populated terminal output prevents the accumulator from reconstructing
	// the reasoning from deltas, as in vLLM's Responses stream (#8546/#8547).
	const stream = `data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"think "}

data: {"type":"response.reasoning_text.delta","output_index":0,"delta":"more"}

data: {"type":"response.output_text.delta","output_index":1,"delta":"answer"}

data: {"type":"response.completed","response":{"id":"resp_reasoning","model":"reasoning-model","status":"completed","output":[{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"think more"}],"encrypted_content":null,"status":"completed"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":20,"output_tokens":15,"total_tokens":35,"output_tokens_details":{"reasoning_tokens":10}}}}

`
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"reasoning-model","stream":false}`))
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"reasoning-rid"}},
				Body:       io.NopCloser(strings.NewReader(stream)),
			}
			account := &Account{ID: 40, Name: "responses-api-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			svc := &OpenAIGatewayService{}
			// 定制版 buffer 输出经 protocolconv pipeline：客户端协议为 Source，
			// 上游 Responses 为 IntendedTarget，ActualProtocol=Responses 时 Decode+Encode。
			source := protocolconv.ProtocolOpenAIChat
			requestBody := []byte(`{"model":"reasoning-model","stream":false,"messages":[]}`)
			if path == "/v1/messages" {
				source = protocolconv.ProtocolAnthropic
				requestBody = []byte(`{"model":"reasoning-model","stream":false,"messages":[]}`)
			}
			pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
				Route: protocolconv.Route{
					Source: source, IntendedTarget: protocolconv.ProtocolOpenAIResponses,
					ClientModel: "reasoning-model", UpstreamModel: "reasoning-model",
				},
				Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
			})
			require.NoError(t, err)
			_, err = pipeline.ConvertRequest(requestBody)
			require.NoError(t, err)
			handle := svc.handleChatBufferedStreamingResponse
			if path == "/v1/messages" {
				handle = svc.handleAnthropicBufferedStreamingResponse
			}
			result, err := handle(resp, c, account, pipeline, "reasoning-model", "reasoning-model", "reasoning-model", time.Now())
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
			require.Equal(t, "reasoning-rid", result.RequestID)
			require.False(t, result.Stream)
			require.Equal(t, 20, result.Usage.InputTokens)
			require.Equal(t, 15, result.Usage.OutputTokens)

			body := rec.Body.Bytes()
			if path == "/v1/chat/completions" {
				require.Equal(t, "think more", gjson.GetBytes(body, "choices.0.message.reasoning_content").String())
				require.Equal(t, "answer", gjson.GetBytes(body, "choices.0.message.content").String())
				require.Equal(t, "stop", gjson.GetBytes(body, "choices.0.finish_reason").String())
				require.EqualValues(t, 15, gjson.GetBytes(body, "usage.completion_tokens").Int())
				require.EqualValues(t, 10, gjson.GetBytes(body, "usage.completion_tokens_details.reasoning_tokens").Int())
			} else {
				require.EqualValues(t, 2, gjson.GetBytes(body, "content.#").Int())
				// 定制 renderer 省略空 signature 字段；语义等价。
				require.Equal(t, "thinking", gjson.GetBytes(body, "content.0.type").String())
				require.Equal(t, "think more", gjson.GetBytes(body, "content.0.thinking").String())
				require.Equal(t, "answer", gjson.GetBytes(body, "content.1.text").String())
				require.Equal(t, "end_turn", gjson.GetBytes(body, "stop_reason").String())
				require.EqualValues(t, 15, gjson.GetBytes(body, "usage.output_tokens").Int())
			}
		})
	}
}
