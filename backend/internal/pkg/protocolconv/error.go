package protocolconv

import "fmt"

// ErrorCode is stable enough for callers and tests to classify failures.
type ErrorCode string

const (
	ErrorInvalidJSON           ErrorCode = "invalid_json"
	ErrorInvalidIR             ErrorCode = "invalid_ir"
	ErrorUnsupportedProtocol   ErrorCode = "unsupported_protocol"
	ErrorConverterUnavailable  ErrorCode = "converter_unavailable"
	ErrorUnsupportedCapability ErrorCode = "unsupported_capability"
	ErrorInvalidStream         ErrorCode = "invalid_stream"
	ErrorConversion            ErrorCode = "conversion_failed"
)

// Error is a typed conversion failure. Path points to the semantic field, not
// an incidental Go struct field.
type Error struct {
	Code       ErrorCode
	Protocol   Protocol
	Capability Capability
	Path       string
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Message
	if message == "" {
		message = string(e.Code)
	}
	if e.Path != "" {
		message = fmt.Sprintf("%s at %s", message, e.Path)
	}
	if e.Protocol != "" {
		message = fmt.Sprintf("%s: %s", e.Protocol, message)
	}
	if e.Cause != nil {
		message = fmt.Sprintf("%s: %v", message, e.Cause)
	}
	return message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// WarningCode classifies an explicit, accepted semantic degradation.
type WarningCode string

const (
	WarningDroppedField          WarningCode = "dropped_field"
	WarningNormalizedField       WarningCode = "normalized_field"
	WarningUnsupportedCapability WarningCode = "unsupported_capability"
)

// Warning records a non-fatal semantic change.
type Warning struct {
	Code       WarningCode
	Protocol   Protocol
	Capability Capability
	Path       string
	Message    string
}

func (w Warning) Error() string {
	message := w.Message
	if message == "" {
		message = string(w.Code)
	}
	if w.Path != "" {
		message = fmt.Sprintf("%s at %s", message, w.Path)
	}
	if w.Protocol != "" {
		message = fmt.Sprintf("%s: %s", w.Protocol, message)
	}
	return message
}
