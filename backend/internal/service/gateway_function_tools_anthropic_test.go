package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardStandardFunctionToolsUseAnthropicSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := strings.Join([]string{
		"event: message_start\n" + `data: {"type":"message_start","message":{"id":"msg_status","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":10}}}`,
		"event: content_block_start\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_status","name":"lookup_status","input":{}}}`,
		"event: content_block_delta\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"id\":\"status_1\"}"}}`,
		"event: content_block_stop\n" + `data: {"type":"content_block_stop","index":0}`,
		"event: message_delta\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		"event: message_stop\n" + `data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"

	for _, chat := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("chat=%t/stream=%t", chat, stream), func(t *testing.T) {
				path := "/v1/responses"
				body := fmt.Sprintf(`{"model":"claude-opus-5-5","stream":%t,"input":"查询状态","tools":[{"type":"function","name":"lookup_status","parameters":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}]}`, stream)
				if chat {
					path = "/v1/chat/completions"
					body = fmt.Sprintf(`{"model":"claude-opus-5-5","stream":%t,"messages":[{"role":"user","content":"查询状态"}],"tools":[{"type":"function","function":{"name":"lookup_status","parameters":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}}]}`, stream)
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(payload)),
				}}
				svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{
					ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"api_key": "fixture-key", "base_url": "https://example.com"},
				}

				var result *ForwardResult
				var err error
				if chat {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(body), nil)
				} else {
					result, err = svc.ForwardAsResponses(context.Background(), c, account, []byte(body), nil)
				}

				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), "lookup_status")
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, "/v1/messages", upstream.lastReq.URL.Path)
				require.Len(t, gjson.GetBytes(upstream.lastBody, "tools").Array(), 1)
				require.False(t, gjson.GetBytes(upstream.lastBody, "tools.0.type").Exists(), "实际发往 Anthropic 的工具必须使用合法类型")
				require.Equal(t, "lookup_status", gjson.GetBytes(upstream.lastBody, "tools.0.name").String())
				require.JSONEq(t, `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, gjson.GetBytes(upstream.lastBody, "tools.0.input_schema").Raw)
			})
		}
	}
}
