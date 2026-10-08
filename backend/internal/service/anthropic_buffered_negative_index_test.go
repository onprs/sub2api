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

// anthropicSSEWithNegativeBlockIndex is a buffered Anthropic stream in which a
// malformed upstream sends content_block_delta with a negative index. The
// buffered aggregators only bounded the index from above, so indexing
// finalResp.Content with it panicked.
func anthropicSSEWithNegativeBlockIndex() string {
	return strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_neg","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4.5","usage":{"input_tokens":10}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":-1,"delta":{"type":"text_delta","text":"IGNORED"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
}

func TestAnthropicBufferedAggregators_IgnoreNegativeBlockIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	chatPipeline, pipelineErr := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIChat,
			IntendedTarget: protocolconv.ProtocolAnthropic,
			ClientModel:    "claude-sonnet-4.5",
			UpstreamModel:  "claude-sonnet-4.5",
		},
		Options: protocolconv.Options{SourceModel: "claude-sonnet-4.5", LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, pipelineErr)
	if _, err := chatPipeline.ConvertRequest([]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)); err != nil {
		t.Fatalf("convert test chat request: %v", err)
	}
	responsesPipeline, pipelineErr := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIResponses,
			IntendedTarget: protocolconv.ProtocolAnthropic,
			ClientModel:    "claude-sonnet-4.5",
			UpstreamModel:  "claude-sonnet-4.5",
		},
		Options: protocolconv.Options{SourceModel: "claude-sonnet-4.5", LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, pipelineErr)
	if _, err := responsesPipeline.ConvertRequest([]byte(`{"model":"claude-sonnet-4.5","input":"hi"}`)); err != nil {
		t.Fatalf("convert test responses request: %v", err)
	}

	tests := []struct {
		name string
		run  func(resp *http.Response, c *gin.Context) error
	}{
		{
			name: "gateway chat completions",
			run: func(resp *http.Response, c *gin.Context) error {
				_, err := (&GatewayService{}).handleCCBufferedFromAnthropic(resp, c, chatPipeline, "gpt-5", "claude-sonnet-4.5", nil, time.Now())
				return err
			},
		},
		{
			name: "gateway responses",
			run: func(resp *http.Response, c *gin.Context) error {
				_, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(resp, c, responsesPipeline, "claude-sonnet-4.5", "claude-sonnet-4.5", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				return err
			},
		},
		{
			name: "openai native anthropic chat completions",
			run: func(resp *http.Response, c *gin.Context) error {
				_, err := newNativeAnthropicHangTestService(5).handleCCBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now())
				return err
			},
		},
		{
			name: "openai native anthropic responses",
			run: func(resp *http.Response, c *gin.Context) error {
				_, err := newNativeAnthropicHangTestService(5).handleResponsesBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(anthropicSSEWithNegativeBlockIndex())),
			}

			var err error
			require.NotPanics(t, func() { err = tt.run(resp, c) })
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), "Hello")
			require.NotContains(t, rec.Body.String(), "IGNORED")
		})
	}
}
