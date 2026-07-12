// Package transport defines transport-neutral upstream results and HTTP/SSE
// framing primitives. It does not interpret provider message semantics.
package transport

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
)

// Response is a complete upstream HTTP result. ActualProtocol identifies the
// wire schema in Body independently of account platform or requested endpoint.
type Response struct {
	StatusCode     int
	Headers        http.Header
	Body           []byte
	ActualProtocol protocolconv.Protocol
	RequestID      string
	ResponseID     string
	Duration       time.Duration
	Metadata       map[string]any
}

// Validate rejects incomplete results before they cross the provider/renderer
// boundary.
func (r Response) Validate() error {
	if r.StatusCode < 100 || r.StatusCode > 999 {
		return fmt.Errorf("invalid upstream HTTP status %d", r.StatusCode)
	}
	if err := r.ActualProtocol.Validate(); err != nil {
		return err
	}
	if len(r.Body) == 0 {
		return fmt.Errorf("upstream response body is empty")
	}
	return nil
}

// IsError reports whether the upstream returned an HTTP error status.
func (r Response) IsError() bool {
	return r.StatusCode >= http.StatusBadRequest
}

// Stream exposes upstream status and protocol before downstream headers are
// committed. Events yields complete provider JSON payloads without SSE framing.
type Stream struct {
	StatusCode     int
	Headers        http.Header
	ActualProtocol protocolconv.Protocol
	RequestID      string
	ResponseID     string
	Duration       time.Duration
	Metadata       map[string]any
	Events         *SSEParser
	ErrorBody      io.ReadCloser
}

// Validate rejects a stream without an explicit protocol or without the body
// form required by its HTTP status.
func (s *Stream) Validate() error {
	if s == nil {
		return fmt.Errorf("nil upstream stream")
	}
	if s.StatusCode < 100 || s.StatusCode > 999 {
		return fmt.Errorf("invalid upstream HTTP status %d", s.StatusCode)
	}
	if err := s.ActualProtocol.Validate(); err != nil {
		return err
	}
	if s.IsError() {
		if s.ErrorBody == nil {
			return fmt.Errorf("upstream error stream has no error body")
		}
		if s.Events != nil {
			return fmt.Errorf("upstream error stream must not expose SSE events")
		}
		return nil
	}
	if s.Events == nil {
		return fmt.Errorf("successful upstream stream has no SSE parser")
	}
	if s.ErrorBody != nil {
		return fmt.Errorf("successful upstream stream must not expose an error body")
	}
	return nil
}

// IsError reports whether the upstream returned an HTTP error status.
func (s *Stream) IsError() bool {
	return s != nil && s.StatusCode >= http.StatusBadRequest
}

// Close releases the parser or error body. It is safe to call more than once.
func (s *Stream) Close() error {
	if s == nil {
		return nil
	}
	if s.Events != nil {
		return s.Events.Close()
	}
	if s.ErrorBody != nil {
		err := s.ErrorBody.Close()
		s.ErrorBody = nil
		return err
	}
	return nil
}

// CloneHeaders returns a detached copy suitable for crossing service ownership
// boundaries.
func CloneHeaders(headers http.Header) http.Header {
	if headers == nil {
		return nil
	}
	return headers.Clone()
}
