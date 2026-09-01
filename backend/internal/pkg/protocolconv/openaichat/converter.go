// Package openaichat implements the OpenAI Chat Completions standard format.
package openaichat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	if !options.PreserveInstructionMessages {
		ir.NormalizeSystemInstruction(request)
	}
	restoreChatToolResultContent(request, toolResultContent)
	if !options.PreserveChatReasoningText {
		injectChatReasoning(request, wire.Messages)
	}
	return request, warnings, nil
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	cacheHints, cacheWarnings, err := collectChatCacheHints(request, options)
	if err != nil {
		return nil, cacheWarnings, err
	}
	canonical, warnings, err := c.responses.EncodeRequest(requestWithoutChatCacheHints(request), options)
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
	wire.Messages, err = injectChatCacheHints(wire.Messages, request, cacheHints)
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
	normalized, imageWarnings := normalizeAssistantImagesForChat(response)
	canonical, warnings, err := c.responses.EncodeResponse(normalized, options)
	warnings = append(imageWarnings, warnings...)
	if err != nil {
		return nil, warnings, err
	}
	var responses apicompat.ResponsesResponse
	if err := json.Unmarshal(canonical, &responses); err != nil {
		return nil, warnings, err
	}
	wire := apicompat.ResponsesToChatCompletions(&responses, normalized.Model)
	wire.Created = normalized.Created
	for _, choice := range normalized.Choices {
		signatureWarnings, signatureErr := checkSignatures([]ir.Message{choice.Message}, options)
		warnings = append(warnings, signatureWarnings...)
		if signatureErr != nil {
			return nil, warnings, signatureErr
		}
	}
	body, err := json.Marshal(wire)
	return body, warnings, err
}

func normalizeAssistantImagesForChat(response *ir.Response) (*ir.Response, []protocolconv.Warning) {
	if response == nil {
		return nil, nil
	}
	normalized := *response
	normalized.Choices = append([]ir.Choice(nil), response.Choices...)
	var warnings []protocolconv.Warning
	for choiceIndex := range normalized.Choices {
		choice := &normalized.Choices[choiceIndex]
		choice.Message.Content = append([]ir.ContentPart(nil), choice.Message.Content...)
		content := make([]ir.ContentPart, 0, len(choice.Message.Content))
		for partIndex, part := range choice.Message.Content {
			if part.Type != ir.ContentImage {
				content = append(content, part)
				continue
			}
			markdown, ok := assistantImageMarkdown(part)
			if ok {
				content = append(content, ir.ContentPart{Type: ir.ContentText, Text: markdown})
				continue
			}
			warnings = append(warnings, protocolconv.Warning{
				Code:       protocolconv.WarningDroppedField,
				Protocol:   protocolconv.ProtocolOpenAIChat,
				Capability: protocolconv.CapabilityImageData,
				Path:       fmt.Sprintf("choices[%d].message.content[%d]", choiceIndex, partIndex),
				Message:    "assistant image omitted because Chat output requires supported base64 image data",
			})
		}
		choice.Message.Content = content
	}
	return &normalized, warnings
}

func assistantImageMarkdown(part ir.ContentPart) (string, bool) {
	if part.Data == "" {
		return "", false
	}
	switch part.MediaType {
	case "image/gif", "image/jpeg", "image/png", "image/webp":
	default:
		return "", false
	}
	if _, err := base64.StdEncoding.DecodeString(part.Data); err != nil {
		return "", false
	}
	return fmt.Sprintf("![image](data:%s;base64,%s)", part.MediaType, part.Data), true
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

func requestWithoutChatCacheHints(request *ir.Request) *ir.Request {
	if request == nil {
		return nil
	}
	clone := *request
	clone.SystemInstruction = cloneChatCacheFreeParts(request.SystemInstruction)
	clone.Messages = make([]ir.Message, len(request.Messages))
	for i, message := range request.Messages {
		clone.Messages[i] = message
		clone.Messages[i].Content = cloneChatCacheFreeParts(message.Content)
	}
	return &clone
}

func cloneChatCacheFreeParts(parts []ir.ContentPart) []ir.ContentPart {
	if len(parts) == 0 {
		return nil
	}
	cloned := make([]ir.ContentPart, len(parts))
	for i, part := range parts {
		cloned[i] = part
		cloned[i].CacheHint = nil
		if len(part.ToolResultContent) > 0 {
			cloned[i].ToolResultContent = cloneChatCacheFreeParts(part.ToolResultContent)
		}
	}
	return cloned
}

type chatCacheHint struct {
	path   string
	system bool
}

func collectChatCacheHints(request *ir.Request, options protocolconv.Options) ([]chatCacheHint, []protocolconv.Warning, error) {
	if request == nil {
		return nil, nil, nil
	}
	var hints []chatCacheHint
	var warnings []protocolconv.Warning
	collect := func(parts []ir.ContentPart, basePath string, system bool) error {
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
			hints = append(hints, chatCacheHint{path: path, system: system})
		}
		return nil
	}
	if err := collect(request.SystemInstruction, "system", true); err != nil {
		return nil, warnings, err
	}
	for i, message := range request.Messages {
		if err := collect(message.Content, fmt.Sprintf("messages[%d].content", i), false); err != nil {
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

func injectChatCacheHints(messages []apicompat.ChatMessage, request *ir.Request, hints []chatCacheHint) ([]apicompat.ChatMessage, error) {
	if len(hints) == 0 {
		return messages, nil
	}
	if request == nil {
		return nil, &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: hints[0].path, Message: "failed to attach cache_control without the source request"}
	}
	var err error
	messages, hints, err = injectChatSystemCacheHints(messages, request.SystemInstruction, hints)
	if err != nil || len(hints) == 0 {
		return messages, err
	}
	return injectChatMessageCacheHints(messages, request.Messages, hints)
}

func injectChatMessageCacheHints(messages []apicompat.ChatMessage, sourceMessages []ir.Message, hints []chatCacheHint) ([]apicompat.ChatMessage, error) {
	searchFrom := 0
	injected := 0
	for sourceMessageIndex, sourceMessage := range sourceMessages {
		for start := 0; start < len(sourceMessage.Content); {
			if !isChatMessageSegmentPart(sourceMessage.Content[start]) {
				start++
				continue
			}
			end := start + 1
			for end < len(sourceMessage.Content) && isChatMessageSegmentPart(sourceMessage.Content[end]) {
				end++
			}
			segment := sourceMessage.Content[start:end]
			cacheCount, firstCachePath := chatMessageSegmentCacheInfo(segment, sourceMessageIndex, start)
			if !chatMessageSegmentHasWireContent(segment, sourceMessage.Role) {
				if cacheCount > 0 {
					return nil, chatCacheInjectionError(firstCachePath)
				}
				start = end
				continue
			}

			targetIndex := findChatMessageSegment(messages, searchFrom, chatMessageSegmentRole(sourceMessage.Role), segment)
			if targetIndex < 0 {
				if cacheCount > 0 {
					return nil, chatCacheInjectionError(firstCachePath)
				}
				start = end
				continue
			}
			searchFrom = targetIndex + 1
			if cacheCount == 0 {
				start = end
				continue
			}

			parts, encodedCacheCount := encodeChatMessageSegment(segment, sourceMessage.Role)
			if encodedCacheCount != cacheCount {
				return nil, chatCacheInjectionError(firstCachePath)
			}
			encoded, err := json.Marshal(parts)
			if err != nil {
				return nil, err
			}
			messages[targetIndex].Content = encoded
			injected += cacheCount
			start = end
		}
	}
	if injected != len(hints) {
		path := hints[0].path
		if injected < len(hints) {
			path = hints[injected].path
		}
		return nil, chatCacheInjectionError(path)
	}
	return messages, nil
}

func isChatMessageSegmentPart(part ir.ContentPart) bool {
	switch part.Type {
	case ir.ContentText, ir.ContentImage, ir.ContentRefusal:
		return true
	default:
		return false
	}
}

func chatMessageSegmentCacheInfo(segment []ir.ContentPart, messageIndex, start int) (int, string) {
	count := 0
	path := ""
	for index, part := range segment {
		if part.Type != ir.ContentText || len(part.CacheHint) == 0 {
			continue
		}
		if path == "" {
			path = fmt.Sprintf("messages[%d].content[%d].cache_control", messageIndex, start+index)
		}
		count++
	}
	return count, path
}

func chatMessageSegmentHasWireContent(segment []ir.ContentPart, role ir.Role) bool {
	for _, part := range segment {
		switch part.Type {
		case ir.ContentText:
			if part.Text != "" {
				return true
			}
		case ir.ContentImage:
			if chatMessageSegmentRole(role) == "user" {
				return true
			}
		}
	}
	return false
}

func chatMessageSegmentRole(role ir.Role) string {
	switch role {
	case ir.RoleTool:
		return "user"
	case ir.RoleDeveloper:
		return "system"
	default:
		return string(role)
	}
}

func findChatMessageSegment(messages []apicompat.ChatMessage, searchFrom int, role string, segment []ir.ContentPart) int {
	for index := searchFrom; index < len(messages); index++ {
		if messages[index].Role != role || !chatMessageSegmentMatches(messages[index].Content, role, segment) {
			continue
		}
		return index
	}
	return -1
}

func chatMessageSegmentMatches(content json.RawMessage, role string, segment []ir.ContentPart) bool {
	expectedText := make([]string, 0, len(segment))
	expectedParts := make([]apicompat.ChatContentPart, 0, len(segment))
	hasImage := false
	for _, part := range segment {
		switch part.Type {
		case ir.ContentText:
			if part.Text == "" {
				continue
			}
			expectedText = append(expectedText, part.Text)
			expectedParts = append(expectedParts, apicompat.ChatContentPart{Type: "text", Text: part.Text})
		case ir.ContentImage:
			hasImage = true
			expectedParts = append(expectedParts, apicompat.ChatContentPart{Type: "image_url", ImageURL: &apicompat.ChatImageURL{URL: chatImageURL(part)}})
		}
	}
	if !hasImage || role != "user" {
		var actual string
		return json.Unmarshal(content, &actual) == nil && actual == strings.Join(expectedText, "\n\n")
	}
	var actual []apicompat.ChatContentPart
	if json.Unmarshal(content, &actual) != nil || len(actual) != len(expectedParts) {
		return false
	}
	for index := range expectedParts {
		if actual[index].Type != expectedParts[index].Type || actual[index].Text != expectedParts[index].Text {
			return false
		}
		if expectedParts[index].ImageURL != nil {
			if actual[index].ImageURL == nil || actual[index].ImageURL.URL != expectedParts[index].ImageURL.URL {
				return false
			}
		}
	}
	return true
}

func encodeChatMessageSegment(segment []ir.ContentPart, role ir.Role) ([]apicompat.ChatContentPart, int) {
	parts := make([]apicompat.ChatContentPart, 0, len(segment))
	cacheCount := 0
	for _, part := range segment {
		switch part.Type {
		case ir.ContentText:
			if part.Text == "" {
				continue
			}
			parts = append(parts, apicompat.ChatContentPart{Type: "text", Text: part.Text, CacheControl: append(json.RawMessage(nil), part.CacheHint...)})
			if len(part.CacheHint) > 0 {
				cacheCount++
			}
		case ir.ContentImage:
			if chatMessageSegmentRole(role) == "user" {
				parts = append(parts, apicompat.ChatContentPart{Type: "image_url", ImageURL: &apicompat.ChatImageURL{URL: chatImageURL(part)}})
			}
		}
	}
	return parts, cacheCount
}

func chatCacheInjectionError(path string) error {
	return &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: path, Message: "failed to attach cache_control to encoded Chat content block"}
}

func injectChatSystemCacheHints(messages []apicompat.ChatMessage, systemInstruction []ir.ContentPart, hints []chatCacheHint) ([]apicompat.ChatMessage, []chatCacheHint, error) {
	systemHintCount := 0
	for systemHintCount < len(hints) && hints[systemHintCount].system {
		systemHintCount++
	}
	if systemHintCount == 0 {
		return messages, hints, nil
	}

	var expected strings.Builder
	parts := make([]apicompat.ChatContentPart, 0, len(systemInstruction))
	for _, part := range systemInstruction {
		_, _ = expected.WriteString(part.Text)
		parts = append(parts, apicompat.ChatContentPart{
			Type:         "text",
			Text:         part.Text,
			CacheControl: append(json.RawMessage(nil), part.CacheHint...),
		})
	}
	for messageIndex := range messages {
		if messages[messageIndex].Role != "system" {
			continue
		}
		var text string
		if err := json.Unmarshal(messages[messageIndex].Content, &text); err != nil || text != expected.String() {
			continue
		}
		encoded, err := json.Marshal(parts)
		if err != nil {
			return nil, nil, err
		}
		messages[messageIndex].Content = encoded
		return messages, hints[systemHintCount:], nil
	}
	return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: protocolconv.ProtocolOpenAIChat, Capability: protocolconv.CapabilityCacheControl, Path: hints[0].path, Message: "failed to attach cache_control to encoded Chat system content blocks"}
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
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIChat, Message: "multiple JSON values"}
		}
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIChat, Cause: err}
	}
	return nil
}

var _ protocolconv.Converter = (*Converter)(nil)
