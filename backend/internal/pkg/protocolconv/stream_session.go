package protocolconv

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	streamstate "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/stream"
)

// StreamSession converts one source stream to one target stream. It owns all
// state and must not be shared between requests.
type StreamSession struct {
	decoder          StreamDecoder
	encoder          StreamEncoder
	state            *streamstate.Context
	identityProtocol Protocol
}

func newIdentityStreamSession(protocol Protocol) *StreamSession {
	return &StreamSession{identityProtocol: protocol}
}

// NewStreamSession creates isolated source and target state.
func (r *Registry) NewStreamSession(source, target Protocol) (*StreamSession, error) {
	return r.NewStreamSessionWithOptions(source, target, Options{})
}

// NewStreamSessionWithOptions creates isolated stream state with immutable
// request-scoped metadata for converters that opt into it.
func (r *Registry) NewStreamSessionWithOptions(source, target Protocol, options Options) (*StreamSession, error) {
	sourceConverter, err := r.Converter(source)
	if err != nil {
		return nil, err
	}
	targetConverter, err := r.Converter(target)
	if err != nil {
		return nil, err
	}
	decoder := sourceConverter.NewStreamDecoder()
	if factory, ok := sourceConverter.(StreamFactoryWithOptions); ok {
		decoder = factory.NewStreamDecoderWithOptions(options)
	}
	encoder := targetConverter.NewStreamEncoder()
	if factory, ok := targetConverter.(StreamFactoryWithOptions); ok {
		encoder = factory.NewStreamEncoderWithOptions(options)
	}
	if decoder == nil {
		return nil, &Error{Code: ErrorConverterUnavailable, Protocol: source, Message: "stream decoder is not implemented"}
	}
	if encoder == nil {
		return nil, &Error{Code: ErrorConverterUnavailable, Protocol: target, Message: "stream encoder is not implemented"}
	}
	return &StreamSession{decoder: decoder, encoder: encoder, state: streamstate.NewContext()}, nil
}

// Convert decodes one source payload and encodes all resulting target payloads.
func (s *StreamSession) Convert(chunk []byte) ([][]byte, []Warning, error) {
	if s == nil {
		return nil, nil, &Error{Code: ErrorInvalidStream, Message: "nil stream session"}
	}
	if s.identityProtocol != "" {
		if !json.Valid(chunk) {
			return nil, nil, &Error{Code: ErrorInvalidJSON, Protocol: s.identityProtocol, Message: "invalid upstream stream event"}
		}
		return [][]byte{append([]byte(nil), chunk...)}, nil, nil
	}
	events, warnings, err := s.decoder.Decode(chunk)
	if err != nil {
		return nil, warnings, err
	}
	return s.encode(events, warnings)
}

// Finalize flushes source and target state. It does not invent a finish reason;
// an incomplete source lifecycle is rejected by the state validator.
func (s *StreamSession) Finalize() ([][]byte, []Warning, error) {
	if s == nil {
		return nil, nil, &Error{Code: ErrorInvalidStream, Message: "nil stream session"}
	}
	if s.identityProtocol != "" {
		return nil, nil, nil
	}
	events, warnings, err := s.decoder.Finalize()
	if err != nil {
		return nil, warnings, err
	}
	payloads, warnings, err := s.encode(events, warnings)
	if err != nil {
		return nil, warnings, err
	}
	final, finalWarnings, err := s.encoder.Finalize()
	warnings = append(warnings, finalWarnings...)
	payloads = append(payloads, final...)
	if err != nil {
		return nil, warnings, err
	}
	if !s.state.Ended() {
		return nil, warnings, &Error{Code: ErrorInvalidStream, Message: "stream finalized without stream_end"}
	}
	return payloads, warnings, nil
}

func (s *StreamSession) encode(events []ir.StreamEvent, warnings []Warning) ([][]byte, []Warning, error) {
	var payloads [][]byte
	for _, event := range events {
		if err := s.state.Apply(event); err != nil {
			return nil, warnings, &Error{Code: ErrorInvalidStream, Message: "invalid IR stream lifecycle", Cause: err}
		}
		encoded, eventWarnings, err := s.encoder.Encode(event)
		warnings = append(warnings, eventWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		payloads = append(payloads, encoded...)
	}
	return payloads, warnings, nil
}
