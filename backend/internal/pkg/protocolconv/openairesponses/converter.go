package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
)

// Converter implements the OpenAI Responses standard wire format.
type Converter struct{}

func New() *Converter { return &Converter{} }

func (*Converter) Protocol() protocolconv.Protocol { return protocolconv.ProtocolOpenAIResponses }

func (*Converter) Capabilities() protocolconv.CapabilitySet {
	return protocolconv.CapabilitySet{
		protocolconv.CapabilityText:           protocolconv.SupportFull,
		protocolconv.CapabilitySystem:         protocolconv.SupportFull,
		protocolconv.CapabilityDeveloper:      protocolconv.SupportFull,
		protocolconv.CapabilityImageURL:       protocolconv.SupportFull,
		protocolconv.CapabilityImageData:      protocolconv.SupportFull,
		protocolconv.CapabilityFile:           protocolconv.SupportLossy,
		protocolconv.CapabilityAudio:          protocolconv.SupportLossy,
		protocolconv.CapabilityTools:          protocolconv.SupportFull,
		protocolconv.CapabilityParallelTools:  protocolconv.SupportFull,
		protocolconv.CapabilityReasoning:      protocolconv.SupportFull,
		protocolconv.CapabilitySignature:      protocolconv.SupportFull,
		protocolconv.CapabilityResponseFormat: protocolconv.SupportFull,
		protocolconv.CapabilityCacheControl:   protocolconv.SupportFull,
		protocolconv.CapabilityCacheUsage:     protocolconv.SupportFull,
		protocolconv.CapabilityStreaming:      protocolconv.SupportFull,
		protocolconv.CapabilityStreamUsage:    protocolconv.SupportFull,
		protocolconv.CapabilityCitation:       protocolconv.SupportFull,
		protocolconv.CapabilityRefusal:        protocolconv.SupportFull,
	}
}

type requestWire struct {
	Model              string                        `json:"model"`
	Instructions       json.RawMessage               `json:"instructions,omitempty"`
	Input              json.RawMessage               `json:"input"`
	MaxOutputTokens    *int                          `json:"max_output_tokens,omitempty"`
	Temperature        *float64                      `json:"temperature,omitempty"`
	TopP               *float64                      `json:"top_p,omitempty"`
	Stream             bool                          `json:"stream,omitempty"`
	Tools              []apicompat.ResponsesTool     `json:"tools,omitempty"`
	ToolChoice         json.RawMessage               `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool                         `json:"parallel_tool_calls,omitempty"`
	Reasoning          *apicompat.ResponsesReasoning `json:"reasoning,omitempty"`
	Text               *apicompat.ResponsesText      `json:"text,omitempty"`
	ServiceTier        string                        `json:"service_tier,omitempty"`
	PromptCacheKey     string                        `json:"prompt_cache_key,omitempty"`
	PreviousResponseID string                        `json:"previous_response_id,omitempty"`
	Store              *bool                         `json:"store,omitempty"`
	Include            []string                      `json:"include,omitempty"`
}

type responseWire struct {
	ID                string                                `json:"id"`
	Model             string                                `json:"model"`
	Status            string                                `json:"status"`
	ServiceTier       string                                `json:"service_tier,omitempty"`
	Output            []responseOutputWire                  `json:"output"`
	Usage             *apicompat.ResponsesUsage             `json:"usage,omitempty"`
	IncompleteDetails *apicompat.ResponsesIncompleteDetails `json:"incomplete_details,omitempty"`
	Error             *apicompat.ResponsesError             `json:"error,omitempty"`
}

type responseOutputWire struct {
	Type             string                           `json:"type"`
	Role             string                           `json:"role,omitempty"`
	Content          []apicompat.ResponsesContentPart `json:"content,omitempty"`
	Status           string                           `json:"status,omitempty"`
	EncryptedContent string                           `json:"encrypted_content,omitempty"`
	Summary          []apicompat.ResponsesSummary     `json:"summary,omitempty"`
	CallID           string                           `json:"call_id,omitempty"`
	Name             string                           `json:"name,omitempty"`
	Arguments        json.RawMessage                  `json:"arguments,omitempty"`
	Namespace        string                           `json:"namespace,omitempty"`
	Input            string                           `json:"input,omitempty"`
}

type inputItemWire struct {
	Type             string                       `json:"type,omitempty"`
	Role             string                       `json:"role,omitempty"`
	Content          json.RawMessage              `json:"content,omitempty"`
	CallID           string                       `json:"call_id,omitempty"`
	Name             string                       `json:"name,omitempty"`
	Arguments        json.RawMessage              `json:"arguments,omitempty"`
	Input            string                       `json:"input,omitempty"`
	Output           json.RawMessage              `json:"output,omitempty"`
	Namespace        string                       `json:"namespace,omitempty"`
	ID               string                       `json:"id,omitempty"`
	Summary          []apicompat.ResponsesSummary `json:"summary,omitempty"`
	Tools            []apicompat.ResponsesTool    `json:"tools,omitempty"`
	EncryptedContent string                       `json:"encrypted_content,omitempty"`
}

func (*Converter) DecodeRequest(body []byte, options protocolconv.Options) (*ir.Request, []protocolconv.Warning, error) {
	var wire requestWire
	if err := decodeJSON(body, &wire); err != nil {
		return nil, nil, err
	}
	out := &ir.Request{
		Model: wire.Model,
		Generation: ir.GenerationConfig{
			MaxTokens:   wire.MaxOutputTokens,
			Temperature: wire.Temperature,
			TopP:        wire.TopP,
		},
		Stream: ir.StreamConfig{Enabled: wire.Stream},
	}
	if err := decodeInstructions(wire.Instructions, &out.SystemInstruction); err != nil {
		return nil, nil, fieldError("instructions", err)
	}
	var additionalTools []apicompat.ResponsesTool
	if err := decodeInput(wire.Input, &out.Messages, &additionalTools); err != nil {
		return nil, nil, fieldError("input", err)
	}
	out.Tools = decodeTools(append(wire.Tools, additionalTools...))
	out.ToolChoice = decodeToolChoice(wire.ToolChoice)
	if wire.ParallelToolCalls != nil {
		out.ToolConfig = &ir.ToolConfig{DisableParallel: boolPointer(!*wire.ParallelToolCalls)}
	}
	if wire.Reasoning != nil {
		out.Reasoning = &ir.ReasoningConfig{Mode: "enabled", Effort: wire.Reasoning.Effort, Summary: wire.Reasoning.Summary}
	}
	if wire.Text != nil && len(wire.Text.Format) > 0 {
		out.ResponseFormat = decodeResponseFormat(wire.Text.Format)
	}
	out.Cache = nil
	if wire.PromptCacheKey != "" {
		out.Cache = &ir.CacheConfig{Key: wire.PromptCacheKey}
	}
	out.Extensions = encodeRequestExtensions(wire)
	if !options.PreserveInstructionMessages {
		ir.NormalizeSystemInstruction(out)
	}
	if err := ir.ValidateRequest(out); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: protocolconv.ProtocolOpenAIResponses, Cause: err}
	}
	return out, nil, nil
}

func (c *Converter) EncodeRequest(request *ir.Request, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateRequest(request); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	wire := requestWire{
		Model:           request.Model,
		MaxOutputTokens: request.Generation.MaxTokens,
		Temperature:     request.Generation.Temperature,
		TopP:            request.Generation.TopP,
		Stream:          request.Stream.Enabled,
		Tools:           encodeTools(request.Tools),
		ToolChoice:      encodeToolChoice(request.ToolChoice),
	}
	var warnings []protocolconv.Warning
	var err error
	wire.Instructions, err = encodeInstructions(request.SystemInstruction)
	if err != nil {
		return nil, warnings, err
	}
	wire.Input, warnings, err = encodeInput(request.Messages, c.Capabilities(), options)
	if err != nil {
		return nil, warnings, err
	}
	if request.ToolConfig != nil && request.ToolConfig.DisableParallel != nil {
		wire.ParallelToolCalls = boolPointer(!*request.ToolConfig.DisableParallel)
	}
	if request.Reasoning != nil && request.Reasoning.Mode != "disabled" {
		wire.Reasoning = &apicompat.ResponsesReasoning{Effort: request.Reasoning.Effort, Summary: request.Reasoning.Summary}
	}
	if request.ResponseFormat != nil {
		format, formatWarnings, err := encodeResponseFormat(request.ResponseFormat, c.Capabilities(), options)
		warnings = append(warnings, formatWarnings...)
		if err != nil {
			return nil, warnings, err
		}
		wire.Text = &apicompat.ResponsesText{Format: format}
	}
	if request.Cache != nil {
		wire.PromptCacheKey = request.Cache.Key
	}
	decodeRequestExtensions(request.Extensions, &wire)
	body, err := json.Marshal(&wire)
	if err != nil {
		return nil, warnings, err
	}
	return body, warnings, nil
}

func (*Converter) DecodeResponse(body []byte, _ protocolconv.Options) (*ir.Response, []protocolconv.Warning, error) {
	var wire responseWire
	if err := decodeJSON(body, &wire); err != nil {
		return nil, nil, err
	}
	out := &ir.Response{
		ID:               wire.ID,
		Model:            wire.Model,
		Status:           wire.Status,
		Created:          time.Now().Unix(),
		ProviderMetadata: encodeResponseProviderMetadata(wire.ServiceTier),
	}
	message := ir.Message{Role: ir.RoleAssistant}
	finish := ir.FinishReason{Reason: responseWireFinishReason(&wire)}
	for _, item := range wire.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					message.Content = append(message.Content, ir.ContentPart{Type: ir.ContentText, Text: part.Text})
				case "refusal":
					message.Content = append(message.Content, ir.ContentPart{Type: ir.ContentRefusal, Refusal: part.Text})
				}
			}
		case "reasoning":
			var text bytes.Buffer
			for _, summary := range item.Summary {
				_, _ = text.WriteString(summary.Text)
			}
			message.Content = append(message.Content, ir.ContentPart{Type: ir.ContentReasoning, Reasoning: text.String(), Signature: item.EncryptedContent, Status: item.Status})
		case "function_call", "custom_tool_call", "tool_search_call":
			args := decodeToolArguments(item.Type, item.Arguments, item.Input)
			message.Content = append(message.Content, ir.ContentPart{
				Type:          ir.ContentToolCall,
				ToolCallID:    item.CallID,
				ToolName:      item.Name,
				ToolKind:      item.Type,
				ToolNamespace: item.Namespace,
				ToolInput:     args,
			})
			finish.Reason = "tool_calls"
		}
	}
	out.Choices = []ir.Choice{{Index: 0, Message: message, FinishReason: finish}}
	out.Usage = decodeUsage(wire.Usage)
	if wire.Error != nil {
		out.Error = &ir.ErrorInfo{Code: wire.Error.Code, Message: wire.Error.Message}
	}
	return out, nil, ir.ValidateResponse(out)
}

func (c *Converter) EncodeResponse(response *ir.Response, options protocolconv.Options) ([]byte, []protocolconv.Warning, error) {
	if err := ir.ValidateResponse(response); err != nil {
		return nil, nil, &protocolconv.Error{Code: protocolconv.ErrorInvalidIR, Protocol: c.Protocol(), Cause: err}
	}
	wire := apicompat.ResponsesResponse{
		ID:          response.ID,
		Object:      "response",
		Model:       response.Model,
		Status:      response.Status,
		ServiceTier: decodeResponseServiceTier(response.ProviderMetadata),
		Usage:       encodeUsage(response.Usage),
	}
	if wire.Status == "" {
		wire.Status = "completed"
	}
	var warnings []protocolconv.Warning
	for _, choice := range response.Choices {
		for _, part := range choice.Message.Content {
			switch part.Type {
			case ir.ContentText:
				wire.Output = append(wire.Output, apicompat.ResponsesOutput{Type: "message", Role: "assistant", Status: "completed", Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: part.Text}}})
			case ir.ContentReasoning:
				wire.Output = append(wire.Output, apicompat.ResponsesOutput{Type: "reasoning", Status: part.Status, EncryptedContent: part.Signature, Summary: []apicompat.ResponsesSummary{{Type: "summary_text", Text: part.Reasoning}}})
			case ir.ContentToolCall:
				part = restoreResponseToolRoute(part, options.ToolRoutes)
				wire.Output = append(wire.Output, encodeResponseToolCall(part))
			case ir.ContentRefusal:
				wire.Output = append(wire.Output, apicompat.ResponsesOutput{Type: "message", Role: "assistant", Status: "completed", Content: []apicompat.ResponsesContentPart{{Type: "refusal", Text: part.Refusal}}})
			default:
				warning, err := unsupported(c.Protocol(), capabilityForPart(part.Type), "choices.content", options)
				warnings = append(warnings, warning...)
				if err != nil {
					return nil, warnings, err
				}
			}
		}
		if choice.FinishReason.Reason == "length" {
			wire.Status = "incomplete"
			wire.IncompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	if response.Error != nil {
		wire.Status = "failed"
		wire.Error = &apicompat.ResponsesError{Code: response.Error.Code, Message: response.Error.Message}
	}
	body, err := json.Marshal(&wire)
	return body, warnings, err
}

func decodeJSON(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIResponses, Cause: err}
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIResponses, Message: "multiple JSON values"}
		}
		return &protocolconv.Error{Code: protocolconv.ErrorInvalidJSON, Protocol: protocolconv.ProtocolOpenAIResponses, Cause: err}
	}
	return nil
}

func fieldError(path string, err error) error {
	return &protocolconv.Error{Code: protocolconv.ErrorConversion, Protocol: protocolconv.ProtocolOpenAIResponses, Path: path, Cause: err}
}

func responseWireFinishReason(response *responseWire) string {
	if response == nil {
		return "stop"
	}
	if response.Status == "failed" {
		return "error"
	}
	if response.Status == "incomplete" && response.IncompleteDetails != nil {
		switch response.IncompleteDetails.Reason {
		case "content_filter":
			return "content_filter"
		case "max_output_tokens":
			return "length"
		}
	}
	for _, item := range response.Output {
		switch item.Type {
		case "function_call", "custom_tool_call", "tool_search_call":
			return "tool_calls"
		}
	}
	return "stop"
}

func decodeToolArguments(kind string, arguments json.RawMessage, input string) json.RawMessage {
	if kind == "custom_tool_call" {
		body, _ := json.Marshal(input)
		return body
	}
	var encoded string
	if json.Unmarshal(arguments, &encoded) == nil {
		args := json.RawMessage(encoded)
		if json.Valid(args) {
			return args
		}
	}
	if json.Valid(arguments) {
		return append(json.RawMessage(nil), arguments...)
	}
	return json.RawMessage(`{}`)
}

func restoreResponseToolRoute(part ir.ContentPart, routes map[string]protocolconv.ToolRoute) ir.ContentPart {
	route, ok := routes[part.ToolName]
	if !ok {
		return part
	}
	part.ToolKind = route.SourceKind
	part.ToolName = route.SourceName
	part.ToolNamespace = route.Namespace
	if route.SourceKind == "custom_tool_call" {
		part.ToolInput = restoreCustomToolInput(part.ToolInput)
	}
	return part
}

func restoreCustomToolInput(arguments json.RawMessage) json.RawMessage {
	var value struct {
		Input *string `json:"input"`
	}
	if json.Unmarshal(arguments, &value) == nil && value.Input != nil {
		body, _ := json.Marshal(*value.Input)
		return body
	}
	var text string
	if json.Unmarshal(arguments, &text) == nil {
		body, _ := json.Marshal(text)
		return body
	}
	body, _ := json.Marshal(string(arguments))
	return body
}

func encodeResponseToolCall(part ir.ContentPart) apicompat.ResponsesOutput {
	kind := part.ToolKind
	if kind == "" {
		kind = "function_call"
	}
	output := apicompat.ResponsesOutput{
		Type:      kind,
		CallID:    part.ToolCallID,
		Name:      part.ToolName,
		Namespace: part.ToolNamespace,
		Status:    "completed",
	}
	switch kind {
	case "custom_tool_call":
		if json.Unmarshal(part.ToolInput, &output.Input) != nil {
			output.Input = string(part.ToolInput)
		}
	default:
		output.Arguments = rawJSONString(part.ToolInput)
	}
	return output
}

func boolPointer(value bool) *bool { return &value }
func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func unsupported(protocol protocolconv.Protocol, capability protocolconv.Capability, path string, options protocolconv.Options) ([]protocolconv.Warning, error) {
	if options.LossPolicy == protocolconv.LossWarn {
		return []protocolconv.Warning{{Code: protocolconv.WarningUnsupportedCapability, Protocol: protocol, Capability: capability, Path: path, Message: "content part dropped"}}, nil
	}
	return nil, &protocolconv.Error{Code: protocolconv.ErrorUnsupportedCapability, Protocol: protocol, Capability: capability, Path: path, Message: "content part is not supported"}
}

func capabilityForPart(part ir.ContentType) protocolconv.Capability {
	switch part {
	case ir.ContentImage:
		return protocolconv.CapabilityImageData
	case ir.ContentFile:
		return protocolconv.CapabilityFile
	case ir.ContentAudio:
		return protocolconv.CapabilityAudio
	case ir.ContentCitation:
		return protocolconv.CapabilityCitation
	case ir.ContentRefusal:
		return protocolconv.CapabilityRefusal
	case ir.ContentReasoning:
		return protocolconv.CapabilityReasoning
	case ir.ContentToolCall, ir.ContentToolResult:
		return protocolconv.CapabilityTools
	default:
		return protocolconv.CapabilityText
	}
}

var _ protocolconv.Converter = (*Converter)(nil)
var _ = fmt.Sprintf
