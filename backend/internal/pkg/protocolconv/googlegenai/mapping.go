package googlegenai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

func ensureGoogleFunctionCallID(part partWire, candidateIndex, partIndex int) partWire {
	if part.FunctionCall != nil && part.FunctionCall.ID == "" {
		call := *part.FunctionCall
		call.ID = fmt.Sprintf("call_google_%d_%d", candidateIndex, partIndex)
		part.FunctionCall = &call
	}
	return part
}

func googleCandidateIndex(position int, candidate candidateWire) int {
	if candidate.Index != 0 || position == 0 {
		return candidate.Index
	}
	return position
}

func partFromGoogle(part partWire) ([]ir.ContentPart, error) {
	switch {
	case part.FunctionCall != nil:
		args := cloneRaw(part.FunctionCall.Args)
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		return []ir.ContentPart{{Type: ir.ContentToolCall, ToolCallID: part.FunctionCall.ID, ToolName: part.FunctionCall.Name, ToolInput: args, Signature: part.ThoughtSignature}}, nil
	case part.FunctionResponse != nil:
		result, content, isError, err := googleToolResultFromResponse(part.FunctionResponse.Response)
		if err != nil {
			return nil, err
		}
		return []ir.ContentPart{{
			Type:              ir.ContentToolResult,
			ToolCallID:        part.FunctionResponse.ID,
			ToolName:          part.FunctionResponse.Name,
			ToolResult:        result,
			ToolResultContent: content,
			IsError:           isError,
		}}, nil
	case part.InlineData != nil:
		kind := ir.ContentImage
		if strings.HasPrefix(part.InlineData.MIMEType, "audio/") {
			kind = ir.ContentAudio
		}
		return []ir.ContentPart{{Type: kind, MediaType: part.InlineData.MIMEType, Data: part.InlineData.Data}}, nil
	case part.FileData != nil:
		return []ir.ContentPart{{Type: ir.ContentFile, MediaType: part.FileData.MIMEType, URL: part.FileData.FileURI}}, nil
	case part.Thought:
		return []ir.ContentPart{{Type: ir.ContentReasoning, Reasoning: part.Text, Signature: part.ThoughtSignature}}, nil
	default:
		return []ir.ContentPart{{Type: ir.ContentText, Text: part.Text, Signature: part.ThoughtSignature, CacheHint: cloneRaw(part.CacheControl)}}, nil
	}
}

func partToGoogle(part ir.ContentPart, target protocolconv.Protocol, options protocolconv.Options) ([]partWire, []protocolconv.Warning, error) {
	switch part.Type {
	case ir.ContentText:
		return []partWire{{Text: part.Text, ThoughtSignature: part.Signature, CacheControl: cloneRaw(part.CacheHint)}}, nil, nil
	case ir.ContentImage, ir.ContentAudio:
		if part.Data != "" {
			return []partWire{{InlineData: &blobWire{MIMEType: part.MediaType, Data: part.Data}}}, nil, nil
		}
		return []partWire{{FileData: &fileDataWire{MIMEType: part.MediaType, FileURI: part.URL}}}, nil, nil
	case ir.ContentFile:
		if part.Data != "" {
			return []partWire{{InlineData: &blobWire{MIMEType: part.MediaType, Data: part.Data}}}, nil, nil
		}
		return []partWire{{FileData: &fileDataWire{MIMEType: part.MediaType, FileURI: part.URL}}}, nil, nil
	case ir.ContentReasoning:
		return []partWire{{Text: part.Reasoning, Thought: true, ThoughtSignature: part.Signature}}, nil, nil
	case ir.ContentToolCall:
		return []partWire{{FunctionCall: &functionCallWire{ID: part.ToolCallID, Name: part.ToolName, Args: cloneRaw(part.ToolInput)}, ThoughtSignature: part.Signature}}, nil, nil
	case ir.ContentToolResult:
		return googleToolResultToParts(part, target, options)
	case ir.ContentRefusal:
		return []partWire{{Text: part.Refusal}}, []protocolconv.Warning{{Code: protocolconv.WarningNormalizedField, Protocol: target, Capability: protocolconv.CapabilityRefusal, Message: "refusal normalized to text"}}, nil
	default:
		capability := protocolconv.CapabilityCitation
		if options.LossPolicy == protocolconv.LossWarn {
			return nil, []protocolconv.Warning{{Code: protocolconv.WarningUnsupportedCapability, Protocol: target, Capability: capability, Message: "content part dropped"}}, nil
		}
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: target, Capability: capability, Message: "content part is not supported"}
	}
}

func googleFunctionResponse(result json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`)
	}
	if trimmed[0] == '{' {
		return cloneRaw(trimmed)
	}
	wrapped, _ := json.Marshal(map[string]json.RawMessage{"content": cloneRaw(trimmed)})
	return wrapped
}

func isGoogleSearchTool(tool ir.ToolDefinition) bool {
	providerType := strings.ToLower(strings.TrimSpace(tool.ProviderType))
	name := strings.ToLower(strings.TrimSpace(tool.Name))
	return strings.HasPrefix(providerType, "web_search") || providerType == "google_search" ||
		name == "web_search" || name == "google_search" || name == "web_search_20250305"
}

func roleFromGoogle(role string) ir.Role {
	if role == "model" {
		return ir.RoleAssistant
	}
	return ir.RoleUser
}
func roleToGoogle(role ir.Role) string {
	if role == ir.RoleAssistant {
		return "model"
	}
	return "user"
}

func toolChoiceFromGoogle(config *toolConfigWire) *ir.ToolChoice {
	if config == nil || config.FunctionCallingConfig == nil {
		return nil
	}
	call := config.FunctionCallingConfig
	switch strings.ToUpper(call.Mode) {
	case "NONE":
		return &ir.ToolChoice{Mode: "none"}
	case "ANY":
		if len(call.AllowedFunctionNames) == 1 {
			return &ir.ToolChoice{Mode: "tool", Name: call.AllowedFunctionNames[0]}
		}
		return &ir.ToolChoice{Mode: "any"}
	default:
		return &ir.ToolChoice{Mode: "auto"}
	}
}
func toolChoiceToGoogle(choice *ir.ToolChoice) *toolConfigWire {
	if choice == nil {
		return nil
	}
	call := &functionCallingConfigWire{}
	switch choice.Mode {
	case "none":
		call.Mode = "NONE"
	case "any":
		call.Mode = "ANY"
	case "tool":
		call.Mode = "ANY"
		call.AllowedFunctionNames = []string{choice.Name}
	default:
		call.Mode = "AUTO"
	}
	return &toolConfigWire{FunctionCallingConfig: call}
}

func generationToGoogle(request *ir.Request) *generationWire {
	g := request.Generation
	wire := &generationWire{Temperature: g.Temperature, TopP: g.TopP, TopK: g.TopK, CandidateCount: g.CandidateCount, MaxOutputTokens: g.MaxTokens, StopSequences: append([]string(nil), g.StopSequences...), PresencePenalty: g.PresencePenalty, FrequencyPenalty: g.FrequencyPenalty, Seed: g.Seed}
	if request.ResponseFormat != nil {
		wire.ResponseMIMEType = request.ResponseFormat.MIMEType
		if wire.ResponseMIMEType == "" && request.ResponseFormat.Type != "text" {
			wire.ResponseMIMEType = "application/json"
		}
		wire.ResponseSchema = cloneRaw(request.ResponseFormat.JSONSchema)
	}
	if request.Reasoning != nil {
		include := request.Reasoning.Mode != "disabled"
		wire.ThinkingConfig = &thinkingWire{IncludeThoughts: &include, ThinkingBudget: request.Reasoning.BudgetTokens, ThinkingLevel: strings.ToUpper(request.Reasoning.Effort)}
		if request.Reasoning.Mode == "auto" && wire.ThinkingConfig.ThinkingBudget == nil {
			budget := -1
			wire.ThinkingConfig.ThinkingBudget = &budget
		}
		if request.Reasoning.Mode == "disabled" && wire.ThinkingConfig.ThinkingBudget == nil {
			budget := 0
			wire.ThinkingConfig.ThinkingBudget = &budget
		}
	}
	return wire
}

func finishFromGoogle(reason string) ir.FinishReason {
	switch strings.ToUpper(reason) {
	case "MAX_TOKENS":
		return ir.FinishReason{Reason: "length"}
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return ir.FinishReason{Reason: "content_filter"}
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return ir.FinishReason{Reason: "error"}
	default:
		return ir.FinishReason{Reason: "stop"}
	}
}
func finishToGoogle(reason ir.FinishReason) string {
	if providerReason := strings.ToUpper(strings.TrimSpace(reason.ProviderReason)); isGoogleFinishReason(providerReason) {
		return providerReason
	}
	switch reason.Reason {
	case "length":
		return "MAX_TOKENS"
	case "content_filter", "refusal":
		return "SAFETY"
	case "error":
		return "OTHER"
	default:
		return "STOP"
	}
}

func isGoogleFinishReason(reason string) bool {
	switch reason {
	case "STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "LANGUAGE", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "MALFORMED_FUNCTION_CALL", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_OTHER", "NO_IMAGE", "IMAGE_RECITATION", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS":
		return true
	default:
		return false
	}
}

func usageFromGoogle(usage *usageWire) *ir.Usage {
	if usage == nil {
		return nil
	}
	return &ir.Usage{InputTokens: usage.PromptTokenCount, OutputTokens: usage.CandidatesTokenCount + usage.ThoughtsTokenCount, TotalTokens: usage.TotalTokenCount, CacheReadTokens: usage.CachedContentTokenCount, ReasoningTokens: usage.ThoughtsTokenCount, InputTokenDetails: tokenDetails(usage.PromptTokensDetails), OutputTokenDetails: tokenDetails(usage.CandidatesTokensDetails)}
}
func usageToGoogle(usage *ir.Usage) *usageWire {
	if usage == nil {
		return nil
	}
	candidateTokens := usage.OutputTokens - usage.ReasoningTokens
	if candidateTokens < 0 {
		candidateTokens = 0
	}
	return &usageWire{PromptTokenCount: usage.InputTokens, CandidatesTokenCount: candidateTokens, ThoughtsTokenCount: usage.ReasoningTokens, TotalTokenCount: usage.TotalTokens, CachedContentTokenCount: usage.CacheReadTokens, PromptTokensDetails: detailsToGoogle(usage.InputTokenDetails), CandidatesTokensDetails: detailsToGoogle(usage.OutputTokenDetails)}
}
func tokenDetails(details []tokenDetailWire) map[string]int {
	if len(details) == 0 {
		return nil
	}
	out := make(map[string]int, len(details))
	for _, detail := range details {
		out[strings.ToLower(detail.Modality)] = detail.TokenCount
	}
	return out
}
func detailsToGoogle(details map[string]int) []tokenDetailWire {
	out := make([]tokenDetailWire, 0, len(details))
	for modality, count := range details {
		out = append(out, tokenDetailWire{Modality: strings.ToUpper(modality), TokenCount: count})
	}
	return out
}
func cloneRaw(raw json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), raw...) }
