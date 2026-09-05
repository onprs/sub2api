package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGoogleProviderResponsesStreamPreservesSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformGemini, PlatformAntigravity} {
		for _, scenario := range []string{"incremental_text", "separate_tool_calls"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				parts := []string{`{"text":"ha"}`, `{"text":"ha"}`, `{"text":"happy"}`}
				finishReason := "MAX_TOKENS"
				if scenario == "separate_tool_calls" {
					parts = []string{
						`{"functionCall":{"name":"lookup","args":{"q":"first"}}}`,
						`{"functionCall":{"name":"lookup","args":{"q":"second"}}}`,
					}
					finishReason = "STOP"
				}
				var wire strings.Builder
				writeChunk := func(chunk string) {
					if platform == PlatformAntigravity {
						chunk = `{"response":` + chunk + `}`
					}
					_, _ = wire.WriteString("data: " + chunk + "\n\n")
				}
				for _, part := range parts {
					writeChunk(`{"responseId":"resp-stream","modelVersion":"upstream-model","candidates":[{"content":{"role":"model","parts":[` + part + `]}}]}`)
				}
				writeChunk(`{"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3,"totalTokenCount":11}}`)
				writeChunk(`{"candidates":[{"finishReason":"` + finishReason + `"}]}`)
				_, _ = wire.WriteString("data: [DONE]\n\n")
				response := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(wire.String())),
				}
				model := "responses-client"
				if platform == PlatformAntigravity {
					model = "gemini-3.1-pro-high"
				}
				body := []byte(`{"model":"` + model + `","stream":true,"input":"lookup","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
				var result *ForwardResult
				var err error
				if platform == PlatformGemini {
					svc := &GeminiMessagesCompatService{
						httpUpstream: &geminiCompatHTTPUpstreamStub{response: response},
						cfg:          &config.Config{},
					}
					result, err = svc.ForwardAsResponses(context.Background(), c, geminiResponsesAPIKeyAccount(), body)
				} else {
					svc := newAntigravityStandardTestService(&queuedHTTPUpstreamStub{responses: []*http.Response{response}})
					result, err = svc.ForwardStandardAsResponses(context.Background(), c, antigravityStandardTestAccount(), body, model, false)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, protocolconv.ProtocolGoogleGenAI, result.ActualProtocol)
				require.Equal(t, 8, result.Usage.InputTokens)
				require.Equal(t, 3, result.Usage.OutputTokens)
				require.Equal(t, http.StatusOK, recorder.Code)

				parser := protocoltransport.NewSSEParser(io.NopCloser(strings.NewReader(recorder.Body.String())), protocoltransport.DefaultMaxSSERecordBytes)
				defer func() { _ = parser.Close() }()
				var terminal gjson.Result
				var text strings.Builder
				terminalCount := 0
				for {
					record, readErr := parser.Next(context.Background())
					if readErr == protocoltransport.ErrSSEDone {
						break
					}
					require.NoError(t, readErr)
					event := gjson.ParseBytes(record.Data)
					switch event.Get("type").String() {
					case "response.output_text.delta":
						_, _ = text.WriteString(event.Get("delta").String())
					case "response.completed", "response.incomplete":
						terminal = event
						terminalCount++
					}
				}
				require.Equal(t, 1, terminalCount)
				require.Equal(t, int64(3), terminal.Get("response.usage.output_tokens").Int())
				if scenario == "incremental_text" {
					require.Equal(t, "hahahappy", text.String())
					require.Equal(t, "response.incomplete", terminal.Get("type").String())
					require.Equal(t, "incomplete", terminal.Get("response.status").String())
					require.Equal(t, "hahahappy", terminal.Get("response.output.0.content.0.text").String())
				} else {
					require.Equal(t, "response.completed", terminal.Get("type").String())
					calls := terminal.Get("response.output").Array()
					require.Len(t, calls, 2)
					require.NotEmpty(t, calls[0].Get("call_id").String())
					require.NotEqual(t, calls[0].Get("call_id").String(), calls[1].Get("call_id").String())
					require.JSONEq(t, `{"q":"first"}`, calls[0].Get("arguments").String())
					require.JSONEq(t, `{"q":"second"}`, calls[1].Get("arguments").String())
				}
			})
		}
	}
}
