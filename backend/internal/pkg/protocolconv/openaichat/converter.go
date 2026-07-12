// Package openaichat implements the OpenAI Chat Completions standard format.
package openaichat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/openairesponses"
)

// Converter translates Chat Completions through the shared IR semantics.
type Converter struct {
	responses *openairesponses.Converter
}

func New() *Converter { return &Converter{responses: openairesponses.New()} }

func (*Converter) Protocol() protocolconv.Protocol { return protocolconv.ProtocolOpenAIChat }

func (*Converter) Capabilities() protocolconv.CapabilitySet {
	return protocolconv.CapabilitySet{
		protocolconv.CapabilityText:           protocolconv.SupportFull,
		protocolconv.CapabilitySystem:         protocolconv.SupportFull,
		protocolconv.CapabilityDeveloper:      protocolconv.SupportFull,
		protocolconv.CapabilityImageURL:       protocolconv.SupportFull,
		protocolconv.CapabilityImageData:      protocolconv.SupportFull,
		protocolconv.CapabilityFile:           protocolconv.SupportNone,
		protocolconv.CapabilityAudio:          protocolconv.SupportLossy,
		protocolconv.CapabilityTools:          protocolconv.SupportFull,
		protocolconv.CapabilityParallelTools:  protocolconv.SupportFull,
		protocolconv.CapabilityReasoning:      protocolconv.SupportLossy,
		protocolconv.CapabilitySignature:      protocolconv.SupportNone,
		protocolconv.CapabilityResponseFormat: protocolconv.SupportFull,
		protocolconv.CapabilityCacheControl:   protocolconv.SupportNone,
		protocolconv.CapabilityCacheUsage:     protocolconv.SupportFull,
		protocolconv.CapabilityStreaming:      protocolconv.SupportFull,
		protocolconv.CapabilityStreamUsage:    protocolconv.SupportFull,
		protocolconv.CapabilityCitation:       protocolconv.SupportNone,
		protocolconv.CapabilityRefusal:        protocolconv.SupportLossy,
	}
}

func (c *Converter) DecodeRequest(body []byte, options protocolconv.Options) (*ir.Request, []protocolconv.Warning, error) {
	var wire apicompat.ChatCompletionsRequest
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	responses, err := apicompat.ChatCompletionsToResponses(&wire)
	if err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: c.Protocol(), Cause: err}
	}
	responses.Stream = wire.Stream
	canonical, err := json.Marshal(responses)
	if err != nil {
		return nil, nil, err
	}
	request, warnings, err := c.responses.DecodeRequest(canonical, options)
	if err != nil {
		return nil, warnings, err
	}
	request.Stream.IncludeUsage = wire.StreamOptions != nil && wire.StreamOptions.IncludeUsage
	ir.NormalizeSystemInstruction(request)
	injectChatReasoning(request, wire.Messages)
	return request, warnings, nil
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	canonical, warnings, err := c.responses.EncodeRequest(request, options)
	if err != nil {
		return nil, warnings, err
	}
	var responses apicompat.ResponsesRequest
	if err := json.Unmarshal(canonical, &responses); err != nil {
		return nil, warnings, err
	}
	wire, err := apicompat.ResponsesToChatCompletionsRequest(&responses)
	if err != nil {
		return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: c.Protocol(), Cause: err}
	}
	wire.Stream = request.Stream.Enabled
	if request.Stream.IncludeUsage {
		wire.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
	}
	signatureWarnings, err := checkSignatures(request.Messages, options)
	warnings = append(warnings, signatureWarnings...)
	if err != nil {
		return nil, warnings, err
	}
	body, err := json.Marshal(wire)
	return body, warnings, err
}

func (c *Converter) DecodeResponse(body []byte, options protocolconv.Options) (*ir.Response, []protocolconv.Warning, error) {
	var wire apicompat.ChatCompletionsResponse
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	responses := apicompat.ChatCompletionsResponseToResponses(&wire, wire.Model, nil, false, nil)
	canonical, err := json.Marshal(responses)
	if err != nil {
		return nil, nil, err
	}
	response, warnings, err := c.responses.DecodeResponse(canonical, options)
	if response != nil {
		response.Created = wire.Created
		if response.Usage != nil && wire.Usage != nil && wire.Usage.CompletionTokensDetails != nil {
			response.Usage.ReasoningTokens = wire.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}
	return response, warnings, err
}

func (c *Converter) EncodeResponse(response *ir.Response, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	canonical, warnings, err := c.responses.EncodeResponse(response, options)
	if err != nil {
		return nil, warnings, err
	}
	var responses apicompat.ResponsesResponse
	if err := json.Unmarshal(canonical, &responses); err != nil {
		return nil, warnings, err
	}
	wire := apicompat.ResponsesToChatCompletions(&responses, response.Model)
	wire.Created = response.Created
	for _, choice := range response.Choices {
		signatureWarnings, signatureErr := checkSignatures([]ir.Message{choice.Message}, options)
		warnings = append(warnings, signatureWarnings...)
		if signatureErr != nil {
			return nil, warnings, signatureErr
		}
	}
	body, err := json.Marshal(wire)
	return body, warnings, err
}

func checkSignatures(messages []ir.Message, options protocolconv.Options) ([]protocolconv.Warning, error) {
	var warnings []protocolconv.Warning
	for i, message := range messages {
		for j, part := range message.Content {
			if part.Type != ir.ContentReasoning || part.Signature == "" {
				continue
			}
			path := fmt.Sprintf("messages[%d].content[%d].signature", i, j)
			if options.LossPolicy == protocolconv.LossError {
				return warnings, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilitySignature, Path: path, Message: "Chat Completions has no standard reasoning signature field"}
			}
			warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningDroppedField, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilitySignature, Path: path, Message: "Chat Completions has no standard reasoning signature field"})
		}
	}
	return warnings, nil
}

func injectChatReasoning(request *ir.Request, messages []apicompat.ChatMessage) {
	searchFrom := 0
	for _, wireMessage := range messages {
		if wireMessage.Role != "assistant" || wireMessage.ReasoningContent == "" {
			continue
		}
		prefix := "<thinking>" + wireMessage.ReasoningContent + "</thinking>"
		for j := searchFrom; j < len(request.Messages); j++ {
			if request.Messages[j].Role != ir.RoleAssistant {
				continue
			}
			matched := false
			parts := request.Messages[j].Content[:0]
			for _, part := range request.Messages[j].Content {
				if part.Type == ir.ContentText && strings.HasPrefix(part.Text, prefix) {
					part.Text = strings.TrimPrefix(part.Text, prefix)
					part.Text = strings.TrimPrefix(part.Text, "\n")
					matched = true
				}
				if part.Type != ir.ContentText || part.Text != "" {
					parts = append(parts, part)
				}
			}
			if !matched {
				continue
			}
			request.Messages[j].Content = append([]ir.ContentPart{{Type: ir.ContentReasoning, Reasoning: wireMessage.ReasoningContent}}, parts...)
			searchFrom = j + 1
			break
		}
	}
}

func decode(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIChat, Cause: err}
	}
	return nil
}

var _ protocolconv.Converter = (*Converter)(nil)
