package handler

import (
	"net/http"

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

// writeProtocolStreamError writes a synthesized error through the source
// renderer after HTTP status commitment. Callers retain lifecycle and ops
// policy; the renderer owns the protocol JSON envelope and SSE framing.
func writeProtocolStreamError(c *gin.Context, protocol protocolconv.Protocol, status int, errorType, code, message string) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return false
	}
	renderer, err := protocolconv.NewRenderer(protocol)
	if err == nil {
		var payload []byte
		payload, err = renderer.ErrorBody(status, errorType, code, message)
		if err == nil {
			var framed []byte
			framed, err = renderer.FrameStreamEvent(payload)
			if err == nil {
				_, err = c.Writer.Write(framed)
			}
		}
	}
	if err != nil {
		_ = c.Error(err)
		return true
	}
	flusher.Flush()
	return true
}

func protocolForInboundEndpoint(c *gin.Context) protocolconv.Protocol {
	switch GetInboundEndpoint(c) {
	case EndpointChatCompletions:
		return protocolconv.ProtocolOpenAIChat
	case EndpointResponses, EndpointResponsesCompact:
		return protocolconv.ProtocolOpenAIResponses
	default:
		return protocolconv.ProtocolAnthropic
	}
}
