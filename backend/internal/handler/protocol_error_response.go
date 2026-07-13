package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
)

// writeProtocolError applies caller-owned status/message policy through the
// source protocol renderer. Upstream error passthrough remains service-owned.
func writeProtocolError(c *gin.Context, protocol protocolconv.Protocol, status int, errorType, code, message string) {
	if c == nil || c.Writer == nil {
		return
	}
	renderer, err := protocolconv.NewRenderer(protocol)
	if err == nil {
		err = renderer.RenderError(c.Writer, status, errorType, code, message)
	}
	if err != nil {
		_ = c.Error(err)
	}
}
