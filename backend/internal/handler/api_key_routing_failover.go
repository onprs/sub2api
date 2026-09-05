package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const apiKeyRoutingErrorBufferLimit = 64 << 10

// 仅暂存可重试的 HTTP 错误；成功响应和 SSE 立即透传，不缓存生成内容。
type apiKeyRoutingResponseWriter struct {
	gin.ResponseWriter
	header    http.Header
	body      bytes.Buffer
	status    int
	size      int
	committed bool
}

func newAPIKeyRoutingResponseWriter(w gin.ResponseWriter) *apiKeyRoutingResponseWriter {
	return &apiKeyRoutingResponseWriter{ResponseWriter: w, header: w.Header().Clone(), status: http.StatusOK, size: -1}
}

func (w *apiKeyRoutingResponseWriter) Header() http.Header { return w.header }
func (w *apiKeyRoutingResponseWriter) Status() int         { return w.status }
func (w *apiKeyRoutingResponseWriter) Size() int           { return w.size }
func (w *apiKeyRoutingResponseWriter) Written() bool       { return w.size >= 0 }
func (w *apiKeyRoutingResponseWriter) WriteHeader(status int) {
	if status > 0 && !w.Written() {
		w.status = status
	}
}
func (w *apiKeyRoutingResponseWriter) WriteHeaderNow() {
	if w.Written() {
		return
	}
	w.size = 0
	if !apiKeyRoutingRetryableStatus(w.status) {
		w.commit()
	}
}
func (w *apiKeyRoutingResponseWriter) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	if !w.committed && w.body.Len()+len(data) > apiKeyRoutingErrorBufferLimit {
		w.commit()
	}
	var n int
	var err error
	if w.committed {
		n, err = w.ResponseWriter.Write(data)
	} else {
		n, err = w.body.Write(data)
	}
	w.size += n
	return n, err
}
func (w *apiKeyRoutingResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}
func (w *apiKeyRoutingResponseWriter) Flush() {
	w.WriteHeaderNow()
	if w.committed {
		w.ResponseWriter.Flush()
	}
}
func (w *apiKeyRoutingResponseWriter) commit() {
	if w.committed {
		return
	}
	w.committed = true
	dst := w.ResponseWriter.Header()
	clear(dst)
	for key, values := range w.header {
		dst[key] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.ResponseWriter.WriteHeaderNow()
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
		w.body.Reset()
	}
}

func apiKeyRoutingRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500 && status <= 599
}

func apiKeyRoutingRequestCanRetry(c *gin.Context, input service.APIKeyRoutingResolveInput) bool {
	if c.Request.Method != http.MethodPost || input.PreserveSession || c.Request.GetBody == nil {
		return false
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	// 异步任务和有服务端会话状态的请求不能作为无状态生成重放。
	if strings.Contains(path, "/responses") && !strings.HasSuffix(path, "/responses") {
		return false
	}
	if strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/messages") || strings.HasSuffix(path, "/embeddings") {
		body, err := c.Request.GetBody()
		if err != nil {
			return false
		}
		defer func() { _ = body.Close() }()
		data, err := io.ReadAll(body)
		return err == nil && !gjson.GetBytes(data, "background").Bool()
	}
	return strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")
}

// 第一次走原有中间件链；切组后恢复请求级快照，重新执行前置校验和终端 Handler。
// 保留原始 Gin Context 的路由信息；已结束的中间件链不会再次执行鉴权和 Ops 日志。
func (h *GatewayHandler) serveAPIKeyRoutingAttempts(c *gin.Context, apiKey, effectiveKey *service.APIKey, input service.APIKeyRoutingResolveInput, prepare []gin.HandlerFunc) {
	base := c.Copy()
	base.Request = c.Request.Clone(c.Request.Context())
	writer := c.Writer
	defer func() { c.Writer = writer }()
	retryable := apiKeyRoutingRequestCanRetry(c, input) && len(apiKey.ConfiguredRoutingGroups()) > 1
	input.ExcludedGroupIDs = make(map[int64]struct{})
	attempt := c
	firstAttempt := true
	dispatch := c.Handler()
	for {
		setEffectiveAPIKeyRoutingGroup(attempt, effectiveKey)
		started := time.Now()
		var buffered *apiKeyRoutingResponseWriter
		if retryable {
			buffered = newAPIKeyRoutingResponseWriter(writer)
			attempt.Writer = buffered
		}
		if firstAttempt {
			firstAttempt = false
			c.Next()
		} else {
			for _, preflight := range prepare {
				preflight(attempt)
				if attempt.Writer.Status() >= http.StatusBadRequest {
					break
				}
			}
			if attempt.Writer.Status() < http.StatusBadRequest {
				dispatch(attempt)
			}
		}
		status := attempt.Writer.Status()
		_, streamFailed := service.GetOpsStreamError(attempt)
		success := status >= 200 && status < 300 && !streamFailed && base.Request.Context().Err() == nil
		if success || apiKeyRoutingRetryableStatus(status) || streamFailed {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			h.gatewayService.RecordAPIKeyRoutingOutcome(ctx, effectiveKey.Group.ID, success, apiKeyRoutingObservedLatencyMs(attempt, time.Since(started)))
			cancel()
		}
		input.ExcludedGroupIDs[effectiveKey.Group.ID] = struct{}{}
		var nextKey *service.APIKey
		if buffered != nil && !buffered.committed && !streamFailed && apiKeyRoutingRetryableStatus(status) && base.Request.Context().Err() == nil && len(input.ExcludedGroupIDs) < service.APIKeyRoutingMaxGroups {
			nextKey, _ = h.gatewayService.ResolveAPIKeyRoutingGroup(base.Request.Context(), apiKey, input)
		}
		if nextKey == nil || nextKey.Group == nil {
			if buffered != nil {
				buffered.commit()
			}
			return
		}
		body, err := base.Request.GetBody()
		if err != nil {
			buffered.commit()
			return
		}
		attempt.Keys = base.Copy().Keys
		attempt.Errors = nil
		attempt.Request = base.Request.Clone(base.Request.Context())
		attempt.Request.Body = body
		effectiveKey = nextKey
	}
}
