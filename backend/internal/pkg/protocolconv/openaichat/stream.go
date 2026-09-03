package openaichat

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/openairesponses"
)

type streamDecoder struct {
	bridge *apicompat.ChatCompletionsToResponsesStreamState
	inner  protocolconv.StreamDecoder
	ended  bool
}

type streamEncoder struct {
	bridge  *apicompat.ResponsesEventToChatState
	inner   protocolconv.StreamEncoder
	options protocolconv.Options
}

func newStreamDecoder() *streamDecoder {
	return &streamDecoder{
		bridge: apicompat.NewChatCompletionsToResponsesStreamState(""),
		inner:  openairesponses.NewStreamDecoder(),
	}
}

func newStreamEncoder() *streamEncoder {
	return newStreamEncoderWithOptions(protocolconv.Options{})
}

func newStreamEncoderWithOptions(options protocolconv.Options) *streamEncoder {
	bridge := apicompat.NewResponsesEventToChatState()
	bridge.IncludeUsage = true
	bridge.Model = options.ResponseModel
	return &streamEncoder{
		bridge:  bridge,
		inner:   openairesponses.NewStreamEncoderWithOptions(options),
		options: options,
	}
}

func (d *streamDecoder) Decode(chunk []byte) ([]ir.StreamEvent, []protocolconv.Warning, error) {
	var wire apicompat.ChatCompletionsChunk
	if err := json.Unmarshal(chunk, &wire); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIChat, Cause: err}
	}
	return d.decodeResponses(apicompat.ChatCompletionsChunkToResponsesEvents(&wire, d.bridge))
}

func (d *streamDecoder) Finalize() ([]ir.StreamEvent, []protocolconv.Warning, error) {
	if err := d.bridge.ValidateToolCallArguments(); err != nil {
		return nil, nil, &protocolconv.Error{
			Code:     protocolconv.ErrorInvalidStream,
			Protocol: protocolconv.ProtocolOpenAIChat,
			Message:  "invalid tool call arguments",
			Cause:    err,
		}
	}
	events, warnings, err := d.decodeResponses(apicompat.FinalizeChatCompletionsResponsesStream(d.bridge))
	if err != nil {
		return nil, warnings, err
	}
	if !d.ended {
		return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorInvalidStream, Protocol: protocolconv.ProtocolOpenAIChat, Message: "Chat stream ended without terminal event"}
	}
	_, finalWarnings, err := d.inner.Finalize()
	warnings = append(warnings, finalWarnings...)
	return events, warnings, err
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
	var signatureWarnings []protocolconv.Warning
	if event.Type == ir.EventReasoningDelta && event.Signature != "" {
		path := "stream.reasoning.signature"
		if e.options.LossPolicy == protocolconv.LossError {
			return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilitySignature, Path: path, Message: "Chat Completions has no standard reasoning signature field"}
		}
		signatureWarnings = append(signatureWarnings, protocolconv.Warning{Code: protocolconv.WarningDroppedField, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilitySignature, Path: path, Message: "Chat Completions has no standard reasoning signature field"})
		event.Signature = ""
	}
	responses, warnings, err := e.inner.Encode(event)
	warnings = append(signatureWarnings, warnings...)
	if err != nil {
		return nil, warnings, err
	}
	var out [][]byte
	for _, body := range responses {
		var responseEvent apicompat.ResponsesStreamEvent
		if decodeErr := json.Unmarshal(body, &responseEvent); decodeErr != nil {
			return nil, warnings, decodeErr
		}
		for _, chunk := range apicompat.ResponsesEventToChatChunks(&responseEvent, e.bridge) {
			encoded, encodeErr := json.Marshal(&chunk)
			if encodeErr != nil {
				return nil, warnings, encodeErr
			}
			out = append(out, encoded)
		}
	}
	return out, warnings, nil
}

func (e *streamEncoder) Finalize() ([][]byte, []protocolconv.Warning, error) {
	chunks := apicompat.FinalizeResponsesChatStream(e.bridge)
	out := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		body, err := json.Marshal(&chunk)
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
