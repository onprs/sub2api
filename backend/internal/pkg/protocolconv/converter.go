package protocolconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

// Converter translates one standard protocol to and from the IR. Implementations
// may be stateless; all per-request stream state is supplied explicitly.
type Converter interface {
	Protocol() Protocol
	Capabilities() CapabilitySet

	DecodeRequest(body []byte, options Options) (*ir.Request, []Warning, error)
	EncodeRequest(request *ir.Request, options Options) ([]byte, []Warning, error)

	DecodeResponse(body []byte, options Options) (*ir.Response, []Warning, error)
	EncodeResponse(response *ir.Response, options Options) ([]byte, []Warning, error)

	NewStreamDecoder() StreamDecoder
	NewStreamEncoder() StreamEncoder
}

// StreamDecoder owns source-protocol state for exactly one upstream response.
type StreamDecoder interface {
	Decode(chunk []byte) ([]ir.StreamEvent, []Warning, error)
	Finalize() ([]ir.StreamEvent, []Warning, error)
}

// StreamEncoder owns target-protocol state for exactly one client response.
type StreamEncoder interface {
	Encode(event ir.StreamEvent) ([][]byte, []Warning, error)
	Finalize() ([][]byte, []Warning, error)
}

// StreamFactoryWithOptions is an optional converter extension for request-
// scoped stream metadata. Stateless converters can keep the base factories.
type StreamFactoryWithOptions interface {
	NewStreamDecoderWithOptions(options Options) StreamDecoder
	NewStreamEncoderWithOptions(options Options) StreamEncoder
}

// Registry stores standard converters. It is safe for concurrent conversion;
// converter implementations must not store per-request state themselves.
type Registry struct {
	mu         sync.RWMutex
	converters map[Protocol]Converter
}

// NewRegistry creates an empty explicit registry.
func NewRegistry() *Registry {
	return &Registry{converters: make(map[Protocol]Converter, len(standardProtocols))}
}

// Register adds a converter. Duplicate registration is rejected so init order
// cannot silently replace protocol semantics.
func (r *Registry) Register(converter Converter) error {
	if r == nil {
		return &Error{Code: ErrorConverterUnavailable, Message: "nil converter registry"}
	}
	if converter == nil {
		return &Error{Code: ErrorConverterUnavailable, Message: "nil converter"}
	}
	protocol := converter.Protocol()
	if err := protocol.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.converters[protocol]; exists {
		return &Error{
			Code:     ErrorConversion,
			Protocol: protocol,
			Message:  "converter is already registered",
		}
	}
	r.converters[protocol] = converter
	return nil
}

// Converter returns the registered implementation for protocol.
func (r *Registry) Converter(protocol Protocol) (Converter, error) {
	if err := protocol.Validate(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, &Error{Code: ErrorConverterUnavailable, Protocol: protocol, Message: "nil converter registry"}
	}
	r.mu.RLock()
	converter := r.converters[protocol]
	r.mu.RUnlock()
	if converter == nil {
		return nil, &Error{Code: ErrorConverterUnavailable, Protocol: protocol, Message: "converter is not registered"}
	}
	return converter, nil
}

// DecodeRequest parses a standard request into IR.
func (r *Registry) DecodeRequest(body []byte, source Protocol, options Options) (*ir.Request, []Warning, error) {
	converter, err := r.Converter(source)
	if err != nil {
		return nil, nil, err
	}
	request, warnings, err := converter.DecodeRequest(body, options)
	if err != nil {
		return nil, warnings, wrapConversionError(source, "decode request", err)
	}
	if err := ir.LinkToolResults(request); err != nil {
		return nil, warnings, &Error{Code: ErrorInvalidIR, Protocol: source, Message: "link tool results", Cause: err}
	}
	return request, warnings, nil
}

// EncodeRequest encodes IR into one standard request format.
func (r *Registry) EncodeRequest(request *ir.Request, target Protocol, options Options) ([]byte, []Warning, error) {
	converter, err := r.Converter(target)
	if err != nil {
		return nil, nil, err
	}
	body, warnings, err := converter.EncodeRequest(request, options)
	if err != nil {
		return nil, warnings, wrapConversionError(target, "encode request", err)
	}
	return body, warnings, nil
}

// DecodeResponse parses a complete standard response into IR.
func (r *Registry) DecodeResponse(body []byte, source Protocol, options Options) (*ir.Response, []Warning, error) {
	converter, err := r.Converter(source)
	if err != nil {
		return nil, nil, err
	}
	response, warnings, err := converter.DecodeResponse(body, options)
	if err != nil {
		return nil, warnings, wrapConversionError(source, "decode response", err)
	}
	return response, warnings, nil
}

// EncodeResponse encodes a complete IR response into one standard format.
func (r *Registry) EncodeResponse(response *ir.Response, target Protocol, options Options) ([]byte, []Warning, error) {
	converter, err := r.Converter(target)
	if err != nil {
		return nil, nil, err
	}
	body, warnings, err := converter.EncodeResponse(response, options)
	if err != nil {
		return nil, warnings, wrapConversionError(target, "encode response", err)
	}
	return body, warnings, nil
}

// ConvertRequest decodes source through IR and encodes target. Identity
// conversion validates JSON and returns the original bytes without re-encoding.
func (r *Registry) ConvertRequest(body []byte, source, target Protocol, options Options) ([]byte, []Warning, error) {
	if source == target {
		return identityJSON(body, source, "request")
	}
	request, decodeWarnings, err := r.DecodeRequest(body, source, options)
	if err != nil {
		return nil, decodeWarnings, err
	}
	converted, encodeWarnings, err := r.EncodeRequest(request, target, options)
	warnings := append(decodeWarnings, encodeWarnings...)
	if err != nil {
		return nil, warnings, wrapConversionError(target, "encode request", err)
	}
	return converted, warnings, nil
}

// ConvertResponse decodes a complete source response through IR and encodes it
// for target. Identity conversion preserves the exact bytes.
func (r *Registry) ConvertResponse(body []byte, source, target Protocol, options Options) ([]byte, []Warning, error) {
	if source == target {
		return identityJSON(body, source, "response")
	}
	response, decodeWarnings, err := r.DecodeResponse(body, source, options)
	if err != nil {
		return nil, decodeWarnings, err
	}
	converted, encodeWarnings, err := r.EncodeResponse(response, target, options)
	warnings := append(decodeWarnings, encodeWarnings...)
	if err != nil {
		return nil, warnings, wrapConversionError(target, "encode response", err)
	}
	return converted, warnings, nil
}

func identityJSON(body []byte, protocol Protocol, kind string) ([]byte, []Warning, error) {
	if err := protocol.Validate(); err != nil {
		return nil, nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 || !json.Valid(body) {
		return nil, nil, &Error{
			Code:     ErrorInvalidJSON,
			Protocol: protocol,
			Message:  fmt.Sprintf("invalid JSON %s", kind),
		}
	}
	return body, nil, nil
}

func wrapConversionError(protocol Protocol, operation string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return &Error{Code: ErrorConversion, Protocol: protocol, Message: operation, Cause: err}
}
