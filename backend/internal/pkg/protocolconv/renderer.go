package protocolconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Renderer serializes already-converted JSON for one client protocol. It owns
// downstream JSON/SSE framing, not provider semantics or status policy.
type Renderer struct {
	protocol Protocol
}

// NewRenderer creates a source-protocol renderer.
func NewRenderer(protocol Protocol) (*Renderer, error) {
	if err := protocol.Validate(); err != nil {
		return nil, err
	}
	return &Renderer{protocol: protocol}, nil
}

// Protocol returns the downstream wire protocol.
func (r *Renderer) Protocol() Protocol {
	if r == nil {
		return ""
	}
	return r.protocol
}

// RenderJSON validates and writes one complete converted JSON response.
func (r *Renderer) RenderJSON(w http.ResponseWriter, status int, headers http.Header, body []byte) error {
	if err := r.validate(); err != nil {
		return err
	}
	if w == nil {
		return &Error{Code: ErrorConversion, Protocol: r.protocol, Message: "nil response writer"}
	}
	if !json.Valid(body) {
		return &Error{Code: ErrorInvalidJSON, Protocol: r.protocol, Message: "invalid downstream JSON response"}
	}
	copyResponseHeaders(w.Header(), headers)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(normalizeHTTPStatus(status))
	_, err := w.Write(body)
	return err
}

// WriteStreamHeaders commits a successful SSE response only after the caller
// has inspected the structured upstream status.
func (r *Renderer) WriteStreamHeaders(w http.ResponseWriter, status int, headers http.Header) error {
	if err := r.validate(); err != nil {
		return err
	}
	if w == nil {
		return &Error{Code: ErrorConversion, Protocol: r.protocol, Message: "nil response writer"}
	}
	copyResponseHeaders(w.Header(), headers)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(normalizeHTTPStatus(status))
	return nil
}

// FrameStreamEvent validates and frames one source-protocol JSON event. The
// converter must have built the provider schema object before this call.
func (r *Renderer) FrameStreamEvent(body []byte) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, &Error{Code: ErrorInvalidJSON, Protocol: r.protocol, Message: "invalid downstream stream event"}
	}

	switch r.protocol {
	case ProtocolOpenAIChat, ProtocolGoogleGenAI:
		return frameDataOnly(body), nil
	case ProtocolOpenAIResponses, ProtocolAnthropic:
		eventType, err := streamEventType(body)
		if err != nil {
			return nil, &Error{Code: ErrorInvalidStream, Protocol: r.protocol, Message: "stream event type is required", Cause: err}
		}
		var framed bytes.Buffer
		framed.Grow(len(eventType) + len(body) + 16)
		_, _ = framed.WriteString("event: ")
		_, _ = framed.WriteString(eventType)
		_ = framed.WriteByte('\n')
		writeSSEDataFields(&framed, body)
		_ = framed.WriteByte('\n')
		return framed.Bytes(), nil
	default:
		return nil, &Error{Code: ErrorUnsupportedProtocol, Protocol: r.protocol, Message: "stream renderer is unavailable"}
	}
}

// StreamKeepalive returns a protocol-neutral SSE comment frame. Whether and
// when to emit it remains service-owned transport policy.
func (r *Renderer) StreamKeepalive() []byte {
	if r == nil {
		return nil
	}
	return []byte(":\n\n")
}

// StreamTerminal returns the source-protocol terminal sentinel. OpenAI Chat
// Completions and Responses use a separate [DONE] frame after their JSON
// lifecycle events.
func (r *Renderer) StreamTerminal() []byte {
	if r != nil && (r.protocol == ProtocolOpenAIChat || r.protocol == ProtocolOpenAIResponses) {
		return []byte("data: [DONE]\n\n")
	}
	return nil
}

// ErrorBody builds a source-protocol error envelope. Status mapping and public
// message policy remain the caller's responsibility.
func (r *Renderer) ErrorBody(status int, errorType, code, message string) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if errorType == "" {
		errorType = "api_error"
	}
	if code == "" {
		code = errorType
	}

	var envelope any
	switch r.protocol {
	case ProtocolOpenAIChat:
		envelope = map[string]any{
			"error": map[string]any{"message": message, "type": errorType, "code": code},
		}
	case ProtocolOpenAIResponses:
		envelope = map[string]any{
			"error": map[string]any{"message": message, "type": errorType, "code": code},
		}
	case ProtocolAnthropic:
		envelope = map[string]any{
			"type":  "error",
			"error": map[string]any{"type": errorType, "message": message},
		}
	case ProtocolGoogleGenAI:
		envelope = map[string]any{
			"error": map[string]any{"code": normalizeHTTPStatus(status), "message": message, "status": googleStatusName(status)},
		}
	default:
		return nil, &Error{Code: ErrorUnsupportedProtocol, Protocol: r.protocol, Message: "error renderer is unavailable"}
	}
	return json.Marshal(envelope)
}

// RenderError writes a source-protocol error envelope.
func (r *Renderer) RenderError(w http.ResponseWriter, status int, errorType, code, message string) error {
	body, err := r.ErrorBody(status, errorType, code, message)
	if err != nil {
		return err
	}
	return r.RenderJSON(w, status, nil, body)
}

func (r *Renderer) validate() error {
	if r == nil {
		return &Error{Code: ErrorConversion, Message: "nil protocol renderer"}
	}
	return r.protocol.Validate()
}

func frameDataOnly(body []byte) []byte {
	var framed bytes.Buffer
	framed.Grow(len(body) + 8)
	writeSSEDataFields(&framed, body)
	_ = framed.WriteByte('\n')
	return framed.Bytes()
}

func writeSSEDataFields(framed *bytes.Buffer, body []byte) {
	lines := bytes.Split(body, []byte{'\n'})
	for _, line := range lines {
		_, _ = framed.WriteString("data: ")
		_, _ = framed.Write(line)
		_ = framed.WriteByte('\n')
	}
}

func streamEventType(body []byte) (string, error) {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &header); err != nil {
		return "", err
	}
	if header.Type == "" {
		return "", fmt.Errorf("missing type")
	}
	return header.Type, nil
}

func copyResponseHeaders(target, source http.Header) {
	for key, values := range source {
		switch http.CanonicalHeaderKey(key) {
		case "Content-Length", "Transfer-Encoding", "Connection", "Content-Type":
			continue
		}
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func normalizeHTTPStatus(status int) int {
	if status < 100 || status > 999 {
		return http.StatusInternalServerError
	}
	return status
}

func googleStatusName(status int) string {
	switch normalizeHTTPStatus(status) {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "ALREADY_EXISTS"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusNotImplemented:
		return "UNIMPLEMENTED"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		if normalizeHTTPStatus(status) >= http.StatusInternalServerError {
			return "INTERNAL"
		}
		return "UNKNOWN"
	}
}
