package anthropic

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/openairesponses"
)

type streamDecoder struct {
	bridge *apicompat.AnthropicEventToResponsesState
	inner  protocolconv.StreamDecoder
	ended  bool
}

type streamEncoder struct {
	bridge *apicompat.ResponsesEventToAnthropicState
	inner  protocolconv.StreamEncoder
}

func newStreamDecoder() *streamDecoder {
	return &streamDecoder{bridge: apicompat.NewAnthropicEventToResponsesState(), inner: openairesponses.NewStreamDecoder()}
}

func newStreamEncoder() *streamEncoder {
	return newStreamEncoderWithOptions(protocolconv.Options{})
}

func newStreamEncoderWithOptions(options protocolconv.Options) *streamEncoder {
	bridge := apicompat.NewResponsesEventToAnthropicState()
	bridge.Model = options.ResponseModel
	return &streamEncoder{bridge: bridge, inner: openairesponses.NewStreamEncoderWithOptions(options)}
}

func (d *streamDecoder) Decode(chunk []byte) ([]ir.StreamEvent, []protocolconv.Warning, error) {
	var wire apicompat.AnthropicStreamEvent
	if err := json.Unmarshal(chunk, &wire); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolAnthropic, Cause: err}
	}
	return d.decodeResponses(apicompat.AnthropicEventToResponsesEvents(&wire, d.bridge))
}

func (d *streamDecoder) Finalize() ([]ir.StreamEvent, []protocolconv.Warning, error) {
	if !d.ended {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolAnthropic, Message: "Anthropic stream ended without message_stop"}
	}
	_, warnings, err := d.inner.Finalize()
	return nil, warnings, err
}

func (d *streamDecoder) decodeResponses(responses []apicompat.ResponsesStreamEvent) ([]ir.StreamEvent, []protocolconv.Warning, error) {
	var out []ir.StreamEvent
	var warnings []protocolconv.Warning
	for _, event := range responses {
		body, err := json.Marshal(&event)
		if err != nil {
			return nil, warnings, err
		}
		events, eventWarnings, err := d.inner.Decode(body)
		warnings = append(warnings, eventWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		out = append(out, events...)
		for _, decoded := range events {
			if decoded.Type == ir.EventStreamEnd {
				d.ended = true
			}
		}
	}
	return out, warnings, nil
}

func (e *streamEncoder) Encode(event ir.StreamEvent) ([][]byte, []protocolconv.Warning, error) {
	responses, warnings, err := e.inner.Encode(event)
	if err != nil {
		return nil, warnings, err
	}
	var out [][]byte
	for _, body := range responses {
		var responseEvent apicompat.ResponsesStreamEvent
		if err := json.Unmarshal(body, &responseEvent); err != nil {
			return nil, warnings, err
		}
		for _, anthropicEvent := range apicompat.ResponsesEventToAnthropicEvents(&responseEvent, e.bridge) {
			encoded, err := json.Marshal(&anthropicEvent)
			if err != nil {
				return nil, warnings, err
			}
			out = append(out, encoded)
		}
	}
	return out, warnings, nil
}

func (e *streamEncoder) Finalize() ([][]byte, []protocolconv.Warning, error) {
	events := apicompat.FinalizeResponsesAnthropicStream(e.bridge)
	out := make([][]byte, 0, len(events))
	for _, event := range events {
		body, err := json.Marshal(&event)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, body)
	}
	return out, nil, nil
}

func (*Converter) NewStreamDecoder() protocolconv.StreamDecoder { return newStreamDecoder() }
func (*Converter) NewStreamEncoder() protocolconv.StreamEncoder { return newStreamEncoder() }
func (*Converter) NewStreamDecoderWithOptions(protocolconv.Options) protocolconv.StreamDecoder {
	return newStreamDecoder()
}
func (*Converter) NewStreamEncoderWithOptions(options protocolconv.Options) protocolconv.StreamEncoder {
	return newStreamEncoderWithOptions(options)
}
