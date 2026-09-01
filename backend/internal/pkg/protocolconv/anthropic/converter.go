// Package anthropic implements the standard Anthropic Messages format.
package anthropic

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

// Converter translates standard Anthropic Messages without vendor policy.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) Protocol() protocolconv.Protocol { return protocolconv.ProtocolAnthropic }

func (*Converter) Capabilities() protocolconv.CapabilitySet {
	return protocolconv.CapabilitySet{
		protocolconv.CapabilityText:           protocolconv.SupportFull,
		protocolconv.CapabilitySystem:         protocolconv.SupportFull,
		protocolconv.CapabilityDeveloper:      protocolconv.SupportLossy,
		protocolconv.CapabilityImageURL:       protocolconv.SupportNone,
		protocolconv.CapabilityImageData:      protocolconv.SupportFull,
		protocolconv.CapabilityFile:           protocolconv.SupportLossy,
		protocolconv.CapabilityAudio:          protocolconv.SupportNone,
		protocolconv.CapabilityTools:          protocolconv.SupportFull,
		protocolconv.CapabilityParallelTools:  protocolconv.SupportFull,
		protocolconv.CapabilityReasoning:      protocolconv.SupportFull,
		protocolconv.CapabilitySignature:      protocolconv.SupportFull,
		protocolconv.CapabilityResponseFormat: protocolconv.SupportLossy,
		protocolconv.CapabilityCacheControl:   protocolconv.SupportFull,
		protocolconv.CapabilityCacheUsage:     protocolconv.SupportFull,
		protocolconv.CapabilityStreaming:      protocolconv.SupportFull,
		protocolconv.CapabilityStreamUsage:    protocolconv.SupportFull,
		protocolconv.CapabilityCitation:       protocolconv.SupportFull,
		protocolconv.CapabilityRefusal:        protocolconv.SupportLossy,
	}
}

func (c *Converter) DecodeRequest(body []byte, options protocolconv.Options) (*ir.Request, []protocolconv.Warning, error) {
	return decodeDirectRequest(body, options)
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	return encodeDirectRequest(request, options)
}

func (c *Converter) DecodeResponse(body []byte, _ protocolconv.Options) (*ir.Response, []protocolconv.Warning, error) {
	var wire responseWire
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	parts, err := decodeBlocksFromSlice(wire.Content)
	if err != nil {
		return nil, nil, err
	}
	finish := ir.FinishReason{Reason: finishFromAnthropic(wire.StopReason), ProviderReason: wire.StopReason}
	if wire.StopSequence != nil {
		finish.StopSequence = *wire.StopSequence
	}
	inputTokens := wire.Usage.InputTokens + wire.Usage.CacheReadInputTokens + wire.Usage.CacheCreationInputTokens
	response := &ir.Response{
		ID: wire.ID, Model: wire.Model, Created: time.Now().Unix(), Status: "completed",
		Choices: []ir.Choice{{Index: 0, Message: ir.Message{Role: ir.RoleAssistant, Content: parts}, FinishReason: finish}},
		Usage:   &ir.Usage{InputTokens: inputTokens, OutputTokens: wire.Usage.OutputTokens, TotalTokens: inputTokens + wire.Usage.OutputTokens, CacheReadTokens: wire.Usage.CacheReadInputTokens, CacheCreationTokens: wire.Usage.CacheCreationInputTokens},
	}
	if finish.Reason == "length" {
		response.Status = "incomplete"
	}
	return response, nil, ir.ValidateResponse(response)
}

func (c *Converter) EncodeResponse(response *ir.Response, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateResponse(response); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	responseID := response.ID
	if options.GenerateAnthropicResponseID {
		responseID = generateAnthropicMessageID()
	}
	wire := responseWire{ID: responseID, Type: "message", Role: "assistant", Model: response.Model}
	var warnings []protocolconv.Warning
	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		blocks, blockWarnings, err := encodeBlocks(choice.Message.Content, options)
		warnings = append(warnings, blockWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		wire.Content = blocks
		wire.StopReason = finishToAnthropic(choice.FinishReason)
		if choice.FinishReason.StopSequence != "" {
			wire.StopSequence = &choice.FinishReason.StopSequence
		}
	}
	if response.Usage != nil {
		input := response.Usage.InputTokens - response.Usage.CacheReadTokens - response.Usage.CacheCreationTokens
		if input < 0 {
			input = 0
		}
		wire.Usage = usageWire{InputTokens: input, OutputTokens: response.Usage.OutputTokens, CacheReadInputTokens: response.Usage.CacheReadTokens, CacheCreationInputTokens: response.Usage.CacheCreationTokens}
	}
	body, err := json.Marshal(&wire)
	return body, warnings, err
}

func decode(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolAnthropic, Cause: err}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolAnthropic, Message: "multiple JSON values"}
		}
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolAnthropic, Cause: err}
	}
	return nil
}

var _ protocolconv.Converter = (*Converter)(nil)
