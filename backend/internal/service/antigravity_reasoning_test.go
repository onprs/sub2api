package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAntigravityReasoningAggregationForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gemini-3.8-flash", "gemini-3.7-flash"} {
		for _, effort := range []string{"low", "medium", "high"} {
			for _, source := range []protocolconv.Protocol{protocolconv.ProtocolGoogleGenAI, protocolconv.ProtocolAnthropic, protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolOpenAIResponses} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/%t", model, effort, source, stream), func(t *testing.T) {
						response := fmt.Sprintf("data: {\"response\":{\"responseId\":\"reasoning-test\",\"modelVersion\":%q,\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":2,\"totalTokenCount\":9}}}\n\ndata: [DONE]\n\n", model)
						upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}}}
						svc := newAntigravityStandardTestService(upstream)
						account := antigravityStandardTestAccount()
						var payload string
						switch source {
						case protocolconv.ProtocolGoogleGenAI:
							payload = fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":%q}}}`, strings.ToUpper(effort))
						case protocolconv.ProtocolAnthropic:
							payload = fmt.Sprintf(`{"model":%q,"stream":%t,"max_tokens":8192,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"},"output_config":{"effort":%q}}`, model, stream, effort)
						case protocolconv.ProtocolOpenAIChat:
							payload = fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q}`, model, stream, effort)
						case protocolconv.ProtocolOpenAIResponses:
							payload = fmt.Sprintf(`{"model":%q,"stream":%t,"input":"hello","reasoning":{"effort":%q}}`, model, stream, effort)
						}
						body := []byte(payload)
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
						var result *ForwardResult
						var err error
						switch source {
						case protocolconv.ProtocolGoogleGenAI:
							result, err = svc.ForwardGemini(context.Background(), c, account, model, "streamGenerateContent", stream, body, false)
						case protocolconv.ProtocolAnthropic:
							result, err = svc.Forward(context.Background(), c, account, body, false)
						case protocolconv.ProtocolOpenAIChat:
							result, err = svc.ForwardStandardAsChatCompletions(context.Background(), c, account, body, model, false)
						case protocolconv.ProtocolOpenAIResponses:
							result, err = svc.ForwardStandardAsResponses(context.Background(), c, account, body, model, false)
						}
						require.NoError(t, err, recorder.Body.String())
						require.Equal(t, model, result.Model)
						require.Equal(t, model+"-"+effort, result.UpstreamModel)
						require.NotNil(t, result.ReasoningEffort)
						require.Equal(t, effort, *result.ReasoningEffort)
						require.Len(t, upstream.requestBodies, 1)
						wire := upstream.requestBodies[0]
						require.Equal(t, model+"-"+effort, gjson.GetBytes(wire, "model").String())
						if model == "gemini-3.8-flash" {
							require.Equal(t, strings.ToUpper(effort), gjson.GetBytes(wire, "request.generationConfig.thinkingConfig.thinkingLevel").String())
						} else {
							budget := map[string]int64{"low": 1000, "medium": 4000, "high": -1}[effort]
							require.Equal(t, budget, gjson.GetBytes(wire, "request.generationConfig.thinkingConfig.thinkingBudget").Int())
						}
						require.Contains(t, recorder.Body.String(), "hello")
						require.NotContains(t, recorder.Body.String(), model+"-"+effort)
					})
				}
			}
		}
	}
}

func TestAntigravityReasoningSelection(t *testing.T) {
	for _, tc := range []struct {
		model, body string
		source      protocolconv.Protocol
		want        string
		invalid     bool
	}{
		{"gemini-3.8-flash", `{}`, protocolconv.ProtocolGoogleGenAI, "high", false},
		{"gemini-3.7-flash-low", `{}`, protocolconv.ProtocolGoogleGenAI, "low", false},
		{"gemini-3.7-flash-low", `{"reasoning_effort":"high"}`, protocolconv.ProtocolOpenAIChat, "high", false},
		{"gemini-3.7-flash", `{"reasoning":{"effort":"MEDIUM"}}`, protocolconv.ProtocolOpenAIChat, "medium", false},
		{"gemini-3.7-flash", `{"thinking":{"type":"enabled","budget_tokens":4000}}`, protocolconv.ProtocolAnthropic, "medium", false},
		{"gemini-3.8-flash", `{"generationConfig":{"thinkingConfig":{"thinkingBudget":1000}}}`, protocolconv.ProtocolGoogleGenAI, "low", false},
		{"gemini-3.8-flash", `{"reasoning_effort":"xhigh"}`, protocolconv.ProtocolOpenAIChat, "", true},
		{"gemini-3.1-pro", `{"reasoning_effort":"medium"}`, protocolconv.ProtocolOpenAIChat, "", true},
		{"gemini-3.8-flash", `{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, protocolconv.ProtocolGoogleGenAI, "", true},
		{"gemini-3.8-flash", `{"generationConfig":{"thinkingConfig":{"thinkingBudget":-2}}}`, protocolconv.ProtocolGoogleGenAI, "", true},
	} {
		t.Run(tc.model+tc.body, func(t *testing.T) {
			route, ok := domain.ResolveDefaultAntigravityModelRoute(tc.model)
			require.True(t, ok)
			selected, effort, err := resolveAntigravityRequestReasoning(route, []byte(tc.body), tc.source)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, *effort)
			require.True(t, strings.HasSuffix(selected.ModelID, "-"+tc.want))
		})
	}
}
