package service

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Upstreams behind a custom base_url can stream a line that never ends. Every
// Gemini stream reader must stop at the configured line limit instead of
// buffering it, and must not forward a truncated prefix.
func TestGeminiStreamsRejectOversizedUpstreamLines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 256}}
	svc := &GeminiMessagesCompatService{cfg: cfg}
	oversized := `data: {"candidates":[{"content":{"parts":[{"text":"` + strings.Repeat("x", 4096)

	// 定制版流式读取经由 protocolconv pipeline；行上限在 pipeline 之前的 SSE 读取层生效。
	nativePipeline, _, err := newClaudeMessagesGooglePipeline(&Account{ID: 1, Platform: PlatformGemini}, []byte(`{"model":"gemini-2.5-pro","messages":[]}`), "gemini-2.5-pro", "gemini-2.5-pro")
	require.NoError(t, err)
	chatPipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel: "gemini-2.5-pro", UpstreamModel: "gemini-2.5-pro",
		},
		Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, err)
	// 流式处理器要求请求转换已完成。
	_, err = chatPipeline.ConvertRequest([]byte(`{"model":"gemini-2.5-pro","messages":[]}`))
	require.NoError(t, err)

	run := map[string]func(c *gin.Context, resp *http.Response) error{
		"native": func(c *gin.Context, resp *http.Response) error {
			_, err := svc.handleNativeStreamingResponseWithAccount(c, resp, nativePipeline, time.Now(), false, false, &Account{ID: 1}, "")
			return err
		},
		"messages": func(c *gin.Context, resp *http.Response) error {
			_, err := svc.handleStreamingResponse(c, resp, nativePipeline, time.Now())
			return err
		},
		"chat_completions": func(c *gin.Context, resp *http.Response) error {
			_, err := svc.handleGoogleProtocolStreamingResponse(c, resp, chatPipeline, protocolconv.ProtocolOpenAIChat, time.Now(), false, false)
			return err
		},
		"collector": func(_ *gin.Context, resp *http.Response) error {
			_, _, _, err := collectGeminiSSEObserved(cfg, resp.Body, false, nil)
			return err
		},
	}
	for name, fn := range run {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(oversized))}

			err := fn(c, resp)
			require.Error(t, err)
			// 定制读取层把超限行包装为 SSE record/line 超限错误（或 bufio.ErrTooLong）。
			require.True(t,
				errors.Is(err, bufio.ErrTooLong) ||
					strings.Contains(err.Error(), "exceeds") ||
					strings.Contains(err.Error(), "too large") ||
					strings.Contains(err.Error(), "too long") ||
					strings.Contains(err.Error(), "upstream error"),
				"got %v", err)
			require.NotContains(t, w.Body.String(), "xxxx")
		})
	}
}
