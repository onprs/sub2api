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
	toolResultContent, cleanMessages := extractChatToolResultContent(wire.Messages)
	wire.Messages = cleanMessages
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
	restoreChatToolResultContent(request, toolResultContent)
	injectChatReasoning(request, wire.Messages)
	return request, warnings, nil
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	cacheHints, cacheWarnings, err := collectChatCacheHints(request, options)
	if err != nil {
		return nil, cacheWarnings, err
	}
	canonical, warnings, err := c.responses.EncodeRequest(request, options)
	warnings = append(cacheWarnings, warnings...)
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
	wire.Messages = injectChatToolResultContent(wire.Messages, request)
	wire.Messages, err = injectChatCacheHints(wire.Messages, cacheHints)
	if err != nil {
		return nil, warnings, err
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

type chatCacheHint struct {
	text string
	hint json.RawMessage
	path string
}

func collectChatCacheHints(request *ir.Request, options protocolconv.Options) ([]chatCacheHint, []protocolconv.Warning, error) {
	if request == nil {
		return nil, nil, nil
	}
	var hints []chatCacheHint
	var warnings []protocolconv.Warning
	collect := func(parts []ir.ContentPart, basePath string) error {
		for i, part := range parts {
			if len(part.CacheHint) == 0 {
				continue
			}
			path := fmt.Sprintf("%s[%d].cache_control", basePath, i)
			if part.Type != ir.ContentText || !options.ChatExtensions.AnthropicCacheControl {
				if options.LossPolicy == protocolconv.LossWarn {
					warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: path, Message: "Chat Completions cache_control extension is not enabled for this field"})
					continue
				}
				return &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: path, Message: "Chat Completions cache_control extension is not enabled for this field"}
			}
			hints = append(hints, chatCacheHint{text: part.Text, hint: append(json.RawMessage(nil), part.CacheHint...), path: path})
		}
		return nil
	}
	if err := collect(request.SystemInstruction, "system"); err != nil {
		return nil, warnings, err
	}
	for i, message := range request.Messages {
		if err := collect(message.Content, fmt.Sprintf("messages[%d].content", i)); err != nil {
			return nil, warnings, err
		}
	}
	for i, tool := range request.Tools {
		if len(tool.CacheHint) == 0 {
			continue
		}
		path := fmt.Sprintf("tools[%d].cache_control", i)
		if options.LossPolicy == protocolconv.LossWarn {
			warnings = append(warnings, protocolconv.Warning{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: path, Message: "Chat content-block cache_control cannot represent tool-level cache hints"})
			continue
		}
		return nil, warnings, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: path, Message: "Chat content-block cache_control cannot represent tool-level cache hints"}
	}
	return hints, warnings, nil
}

func injectChatCacheHints(messages []apicompat.ChatMessage, hints []chatCacheHint) ([]apicompat.ChatMessage, error) {
	if len(hints) == 0 {
		return messages, nil
	}
	index := 0
	for messageIndex := range messages {
		if index >= len(hints) || len(messages[messageIndex].Content) == 0 {
			continue
		}
		var text string
		if err := json.Unmarshal(messages[messageIndex].Content, &text); err == nil {
			if text == hints[index].text {
				parts := []apicompat.ChatContentPart{{Type: "text", Text: text, CacheControl: hints[index].hint}}
				encoded, marshalErr := json.Marshal(parts)
				if marshalErr != nil {
					return nil, marshalErr
				}
				messages[messageIndex].Content = encoded
				index++
			}
			continue
		}
		var parts []apicompat.ChatContentPart
		if err := json.Unmarshal(messages[messageIndex].Content, &parts); err != nil {
			continue
		}
		changed := false
		for partIndex := range parts {
			if index >= len(hints) {
				break
			}
			if parts[partIndex].Type == "text" && parts[partIndex].Text == hints[index].text {
				parts[partIndex].CacheControl = hints[index].hint
				index++
				changed = true
			}
		}
		if changed {
			encoded, err := json.Marshal(parts)
			if err != nil {
				return nil, err
			}
			messages[messageIndex].Content = encoded
		}
	}
	if index != len(hints) {
		return nil, &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: hints[index].path, Message: "failed to attach cache_control to encoded Chat content block"}
	}
	return messages, nil
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
