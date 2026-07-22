// Package googlegenai implements standard Gemini generateContent semantics.
package googlegenai

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) Protocol() protocolconv.Protocol { return protocolconv.ProtocolGoogleGenAI }

func (*Converter) Capabilities() protocolconv.CapabilitySet {
	return protocolconv.CapabilitySet{
		protocolconv.CapabilityText:           protocolconv.SupportFull,
		protocolconv.CapabilitySystem:         protocolconv.SupportFull,
		protocolconv.CapabilityDeveloper:      protocolconv.SupportLossy,
		protocolconv.CapabilityImageURL:       protocolconv.SupportFull,
		protocolconv.CapabilityImageData:      protocolconv.SupportFull,
		protocolconv.CapabilityFile:           protocolconv.SupportFull,
		protocolconv.CapabilityAudio:          protocolconv.SupportFull,
		protocolconv.CapabilityTools:          protocolconv.SupportFull,
		protocolconv.CapabilityParallelTools:  protocolconv.SupportFull,
		protocolconv.CapabilityReasoning:      protocolconv.SupportFull,
		protocolconv.CapabilitySignature:      protocolconv.SupportFull,
		protocolconv.CapabilityResponseFormat: protocolconv.SupportFull,
		protocolconv.CapabilityCacheControl:   protocolconv.SupportLossy,
		protocolconv.CapabilityCacheUsage:     protocolconv.SupportFull,
		protocolconv.CapabilityStreaming:      protocolconv.SupportFull,
		protocolconv.CapabilityStreamUsage:    protocolconv.SupportFull,
		protocolconv.CapabilityCitation:       protocolconv.SupportFull,
		protocolconv.CapabilityRefusal:        protocolconv.SupportLossy,
	}
}

func (c *Converter) DecodeRequest(body []byte, options protocolconv.Options) (*ir.Request, []protocolconv.Warning, error) {
	var wire requestWire
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	out := &ir.Request{Model: options.SourceModel}
	if wire.SystemInstruction != nil {
		for _, part := range wire.SystemInstruction.Parts {
			if part.Text != "" {
				out.SystemInstruction = append(out.SystemInstruction, ir.ContentPart{Type: ir.ContentText, Text: part.Text, CacheHint: cloneRaw(part.CacheControl)})
			}
		}
	}
	for _, content := range wire.Contents {
		message := ir.Message{Role: roleFromGoogle(content.Role)}
		for i := 0; i < len(content.Parts); i++ {
			converted, err := partFromGoogle(content.Parts[i])
			if err != nil {
				return nil, nil, err
			}
			message.Content = append(message.Content, converted...)
			if len(converted) == 1 && converted[0].Type == ir.ContentToolResult && len(converted[0].ToolResultContent) > 0 {
				i += googleToolResultDuplicateNativePrefix(converted[0].ToolResultContent, content.Parts[i+1:])
			}
		}
		out.Messages = append(out.Messages, message)
	}
	for _, group := range wire.Tools {
		for _, tool := range group.FunctionDeclarations {
			out.Tools = append(out.Tools, ir.ToolDefinition{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters)})
		}
		if len(group.GoogleSearch) > 0 {
			out.Tools = append(out.Tools, ir.ToolDefinition{Type: "function", ProviderType: "google_search", Name: "google_search"})
		}
	}
	out.ToolChoice = toolChoiceFromGoogle(wire.ToolConfig)
	if wire.GenerationConfig != nil {
		generation := wire.GenerationConfig
		out.Generation = ir.GenerationConfig{Temperature: generation.Temperature, TopP: generation.TopP, TopK: generation.TopK, CandidateCount: generation.CandidateCount, MaxTokens: generation.MaxOutputTokens, StopSequences: append([]string(nil), generation.StopSequences...), PresencePenalty: generation.PresencePenalty, FrequencyPenalty: generation.FrequencyPenalty, Seed: generation.Seed}
		if generation.ResponseMIMEType != "" || len(generation.ResponseSchema) > 0 {
			kind := "text"
			if generation.ResponseMIMEType == "application/json" {
				kind = "json_object"
			}
			if len(generation.ResponseSchema) > 0 {
				kind = "json_schema"
			}
			out.ResponseFormat = &ir.ResponseFormat{Type: kind, MIMEType: generation.ResponseMIMEType, JSONSchema: cloneRaw(generation.ResponseSchema)}
		}
		if generation.ThinkingConfig != nil {
			thinking := generation.ThinkingConfig
			out.Reasoning = &ir.ReasoningConfig{Mode: "auto", Effort: strings.ToLower(thinking.ThinkingLevel), BudgetTokens: thinking.ThinkingBudget}
			if thinking.IncludeThoughts != nil && !*thinking.IncludeThoughts {
				out.Reasoning.Mode = "disabled"
			}
			if thinking.ThinkingBudget != nil {
				switch {
				case *thinking.ThinkingBudget == 0:
					out.Reasoning.Mode = "disabled"
				case *thinking.ThinkingBudget < 0:
					out.Reasoning.Mode = "auto"
				default:
					out.Reasoning.Mode = "enabled"
				}
			}
		}
	}
	if wire.CachedContent != "" {
		out.Cache = &ir.CacheConfig{Key: wire.CachedContent}
	}
	if len(wire.SafetySettings) > 0 {
		out.Extensions = map[string]json.RawMessage{"google_genai.safety_settings": cloneRaw(wire.SafetySettings)}
	}
	if err := ir.ValidateRequest(out); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	return out, nil, nil
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateRequest(request); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	wire := requestWire{}
	if len(request.SystemInstruction) > 0 {
		wire.SystemInstruction = &contentWire{Role: "system"}
		for _, part := range request.SystemInstruction {
			wire.SystemInstruction.Parts = append(wire.SystemInstruction.Parts, partWire{Text: part.Text})
		}
	}
	var warnings []protocolconv.Warning
	for _, message := range request.Messages {
		content := contentWire{Role: roleToGoogle(message.Role)}
		for _, part := range message.Content {
			converted, partWarnings, err := partToGoogle(part, c.Protocol(), options)
			warnings = append(warnings, partWarnings...)
			if err != nil {
				return nil, warnings, err
			}
			content.Parts = append(content.Parts, converted...)
		}
		wire.Contents = append(wire.Contents, content)
	}
	if len(request.Tools) > 0 {
		group := toolGroupWire{}
		for _, tool := range request.Tools {
			if isGoogleSearchTool(tool) {
				if len(group.FunctionDeclarations) > 0 {
					wire.Tools = append(wire.Tools, group)
					group = toolGroupWire{}
				}
				wire.Tools = append(wire.Tools, toolGroupWire{GoogleSearch: json.RawMessage(`{}`)})
				continue
			}
			group.FunctionDeclarations = append(group.FunctionDeclarations, functionDeclarationWire{Name: tool.Name, Description: tool.Description, Parameters: cloneRaw(tool.Parameters)})
		}
		if len(group.FunctionDeclarations) > 0 {
			wire.Tools = append(wire.Tools, group)
		}
	}
	wire.ToolConfig = toolChoiceToGoogle(request.ToolChoice)
	wire.GenerationConfig = generationToGoogle(request)
	if request.Cache != nil {
		wire.CachedContent = request.Cache.Key
	}
	if raw := request.Extensions["google_genai.safety_settings"]; len(raw) > 0 {
		wire.SafetySettings = cloneRaw(raw)
	}
	body, err := json.Marshal(&wire)
	return body, warnings, err
}

func (c *Converter) DecodeResponse(body []byte, options protocolconv.Options) (*ir.Response, []protocolconv.Warning, error) {
	var wire responseWire
	if err := decode(body, &wire); err != nil {
		return nil, nil, err
	}
	model := wire.ModelVersion
	if model == "" {
		model = options.SourceModel
	}
	out := &ir.Response{ID: wire.ResponseID, Model: model, Created: time.Now().Unix(), Status: "completed", Usage: usageFromGoogle(wire.UsageMetadata)}
	for candidateIndex, candidate := range wire.Candidates {
		message := ir.Message{Role: ir.RoleAssistant}
		for partIndex, part := range candidate.Content.Parts {
			part = ensureGoogleFunctionCallID(part, candidateIndex, partIndex)
			converted, err := partFromGoogle(part)
			if err != nil {
				return nil, nil, err
			}
			message.Content = append(message.Content, converted...)
		}
		finish := finishFromGoogle(candidate.FinishReason)
		finish.ProviderReason = candidate.FinishReason
		for _, part := range message.Content {
			if part.Type == ir.ContentToolCall {
				finish.Reason = "tool_calls"
				break
			}
		}
		out.Choices = append(out.Choices, ir.Choice{Index: candidate.Index, Message: message, FinishReason: finish})
	}
	if err := ir.ValidateResponse(out); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	return out, nil, nil
}

func (c *Converter) EncodeResponse(response *ir.Response, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateResponse(response); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	wire := responseWire{ResponseID: response.ID, ModelVersion: response.Model, UsageMetadata: usageToGoogle(response.Usage)}
	var warnings []protocolconv.Warning
	for _, choice := range response.Choices {
		candidate := candidateWire{Index: choice.Index, Content: contentWire{Role: "model"}, FinishReason: finishToGoogle(choice.FinishReason)}
		for _, part := range choice.Message.Content {
			converted, partWarnings, err := partToGoogle(part, c.Protocol(), options)
			warnings = append(warnings, partWarnings...)
			if err != nil {
				return nil, warnings, err
			}
			candidate.Content.Parts = append(candidate.Content.Parts, converted...)
		}
		wire.Candidates = append(wire.Candidates, candidate)
	}
	body, err := json.Marshal(&wire)
	return body, warnings, err
}

func decode(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolGoogleGenAI, Cause: err}
	}
	return nil
}

var _ protocolconv.Converter = (*Converter)(nil)
