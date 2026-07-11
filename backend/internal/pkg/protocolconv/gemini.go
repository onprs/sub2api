package protocolconv

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiTool            `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	InlineData       *geminiInlineData       `json:"inlineData,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int                   `json:"maxOutputTokens,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	TopP            *float64              `json:"topP,omitempty"`
	StopSequences   []string              `json:"stopSequences,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  int  `json:"thinkingBudget,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations,omitempty"`
	GoogleSearch         *struct{}                   `json:"googleSearch,omitempty"`
}

type geminiFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func geminiRequestToResponses(body []byte, model string) (*apicompat.ResponsesRequest, error) {
	var req geminiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode Gemini request: %w", err)
	}
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if model == "" {
		return nil, fmt.Errorf("Gemini conversion requires URL model")
	}

	items := make([]apicompat.ResponsesInputItem, 0, len(req.Contents)+1)
	if req.SystemInstruction != nil {
		if content := geminiPartsToResponsesContent(req.SystemInstruction.Parts, false); len(content) > 0 {
			items = append(items, apicompat.ResponsesInputItem{Role: "developer", Content: content})
		}
	}
	for _, content := range req.Contents {
		role := "user"
		assistant := strings.EqualFold(content.Role, "model")
		if assistant {
			role = "assistant"
		}
		var textParts []apicompat.ResponsesContentPart
		for _, part := range content.Parts {
			switch {
			case part.Thought && part.Text != "":
				items = append(items, apicompat.ResponsesInputItem{
					Type:             "reasoning",
					Summary:          []apicompat.ResponsesSummary{{Type: "summary_text", Text: part.Text}},
					EncryptedContent: part.ThoughtSignature,
				})
			case part.FunctionCall != nil:
				args := strings.TrimSpace(string(part.FunctionCall.Args))
				if args == "" || args == "null" {
					args = "{}"
				}
				items = append(items, apicompat.ResponsesInputItem{
					Type: "function_call", CallID: part.FunctionCall.ID,
					Name: part.FunctionCall.Name, Arguments: args,
				})
			case part.FunctionResponse != nil:
				output := strings.TrimSpace(string(part.FunctionResponse.Response))
				if output == "" || output == "null" {
					output = "{}"
				}
				items = append(items, apicompat.ResponsesInputItem{
					Type: "function_call_output", CallID: part.FunctionResponse.ID, Output: output,
				})
			case part.Text != "":
				kind := "input_text"
				if assistant {
					kind = "output_text"
				}
				textParts = append(textParts, apicompat.ResponsesContentPart{Type: kind, Text: part.Text})
			case part.InlineData != nil:
				textParts = append(textParts, apicompat.ResponsesContentPart{
					Type:     "input_image",
					ImageURL: "data:" + part.InlineData.MIMEType + ";base64," + part.InlineData.Data,
				})
			}
		}
		if len(textParts) > 0 {
			raw, _ := json.Marshal(textParts)
			items = append(items, apicompat.ResponsesInputItem{Role: role, Content: raw})
		}
	}
	input, _ := json.Marshal(items)
	out := &apicompat.ResponsesRequest{Model: model, Input: input}
	if cfg := req.GenerationConfig; cfg != nil {
		out.Temperature = cfg.Temperature
		out.TopP = cfg.TopP
		if cfg.MaxOutputTokens > 0 {
			out.MaxOutputTokens = &cfg.MaxOutputTokens
		}
		if cfg.ThinkingConfig != nil && cfg.ThinkingConfig.IncludeThoughts {
			out.Reasoning = &apicompat.ResponsesReasoning{Effort: "medium", Summary: "auto"}
		}
	}
	for _, group := range req.Tools {
		for _, fn := range group.FunctionDeclarations {
			out.Tools = append(out.Tools, apicompat.ResponsesTool{
				Type: "function", Name: fn.Name, Description: fn.Description, Parameters: fn.Parameters,
			})
		}
	}
	return out, nil
}

func responsesRequestToGemini(req *apicompat.ResponsesRequest) (*geminiRequest, error) {
	out := &geminiRequest{}
	if strings.TrimSpace(req.Instructions) != "" {
		out.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.Instructions}}}
	}

	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil {
		var text string
		if textErr := json.Unmarshal(req.Input, &text); textErr != nil {
			return nil, fmt.Errorf("decode Responses input for Gemini: %w", err)
		}
		out.Contents = append(out.Contents, geminiContent{Role: "user", Parts: []geminiPart{{Text: text}}})
	}

	toolNames := make(map[string]string)
	pendingThoughtSignature := ""
	for _, item := range items {
		switch item.Type {
		case "reasoning":
			parts := make([]geminiPart, 0, len(item.Summary))
			for _, summary := range item.Summary {
				if summary.Type == "summary_text" && summary.Text != "" {
					parts = append(parts, geminiPart{Text: summary.Text, Thought: true, ThoughtSignature: item.EncryptedContent})
				}
			}
			if len(parts) > 0 {
				out.Contents = appendGeminiContent(out.Contents, geminiContent{Role: "model", Parts: parts})
			}
			pendingThoughtSignature = item.EncryptedContent
		case "function_call":
			args := json.RawMessage(item.Arguments)
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			signature := pendingThoughtSignature
			if signature == "" {
				signature = "skip_thought_signature_validator"
			}
			out.Contents = appendGeminiContent(out.Contents, geminiContent{Role: "model", Parts: []geminiPart{{
				ThoughtSignature: signature,
				FunctionCall:     &geminiFunctionCall{ID: item.CallID, Name: item.Name, Args: args},
			}}})
			toolNames[item.CallID] = item.Name
			pendingThoughtSignature = ""
		case "function_call_output":
			response := geminiFunctionResponsePayload(item.Output)
			out.Contents = appendGeminiContent(out.Contents, geminiContent{Role: "user", Parts: []geminiPart{{
				FunctionResponse: &geminiFunctionResponse{ID: item.CallID, Name: toolNames[item.CallID], Response: response},
			}}})
			pendingThoughtSignature = ""
		default:
			if item.Role == "system" || item.Role == "developer" {
				parts := responsesContentToGeminiParts(item.Content)
				if len(parts) > 0 {
					if out.SystemInstruction == nil {
						out.SystemInstruction = &geminiContent{}
					}
					out.SystemInstruction.Parts = append(out.SystemInstruction.Parts, parts...)
				}
				continue
			}
			parts := responsesContentToGeminiParts(item.Content)
			if len(parts) == 0 {
				continue
			}
			role := "user"
			if item.Role == "assistant" {
				role = "model"
			}
			out.Contents = appendGeminiContent(out.Contents, geminiContent{Role: role, Parts: parts})
			if role != "model" {
				pendingThoughtSignature = ""
			}
		}
	}

	out.GenerationConfig = &geminiGenerationConfig{
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
	if req.MaxOutputTokens != nil {
		out.GenerationConfig.MaxOutputTokens = *req.MaxOutputTokens
	}
	if req.Reasoning != nil {
		out.GenerationConfig.ThinkingConfig = &geminiThinkingConfig{IncludeThoughts: true}
	}
	decls := make([]geminiFunctionDeclaration, 0, len(req.Tools))
	hasWebSearch := false
	for _, tool := range req.Tools {
		switch tool.Type {
		case "function":
			decls = append(decls, geminiFunctionDeclaration{
				Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters,
			})
		case "web_search":
			hasWebSearch = true
		}
	}
	if len(decls) > 0 {
		out.Tools = append(out.Tools, geminiTool{FunctionDeclarations: decls})
	}
	if hasWebSearch {
		out.Tools = append(out.Tools, geminiTool{GoogleSearch: &struct{}{}})
	}
	return out, nil
}

func geminiFunctionResponsePayload(output string) json.RawMessage {
	output = strings.TrimSpace(output)
	if output == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(output)) {
		return json.RawMessage(output)
	}
	encoded, _ := json.Marshal(output)
	return encoded
}

func appendGeminiContent(contents []geminiContent, next geminiContent) []geminiContent {
	if len(next.Parts) == 0 {
		return contents
	}
	if len(contents) > 0 && contents[len(contents)-1].Role == next.Role {
		contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, next.Parts...)
		return contents
	}
	return append(contents, next)
}

func responsesContentToGeminiParts(raw json.RawMessage) []geminiPart {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []geminiPart{{Text: text}}
	}
	var content []apicompat.ResponsesContentPart
	if json.Unmarshal(raw, &content) != nil {
		return nil
	}
	parts := make([]geminiPart, 0, len(content))
	for _, part := range content {
		switch part.Type {
		case "input_text", "output_text", "text":
			parts = append(parts, geminiPart{Text: part.Text})
		case "input_image":
			const marker = ";base64,"
			if strings.HasPrefix(part.ImageURL, "data:") {
				if idx := strings.Index(part.ImageURL, marker); idx > len("data:") {
					parts = append(parts, geminiPart{InlineData: &geminiInlineData{
						MIMEType: part.ImageURL[len("data:"):idx],
						Data:     part.ImageURL[idx+len(marker):],
					}})
				}
			}
		}
	}
	return parts
}

func geminiPartsToResponsesContent(parts []geminiPart, assistant bool) json.RawMessage {
	out := make([]apicompat.ResponsesContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Text == "" || part.Thought {
			continue
		}
		kind := "input_text"
		if assistant {
			kind = "output_text"
		}
		out = append(out, apicompat.ResponsesContentPart{Type: kind, Text: part.Text})
	}
	raw, _ := json.Marshal(out)
	return raw
}

func anthropicSystemToGeminiParts(raw json.RawMessage) []geminiPart {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []geminiPart{{Text: text}}
	}
	var blocks []apicompat.AnthropicContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	parts := make([]geminiPart, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, geminiPart{Text: block.Text})
		}
	}
	return parts
}

func anthropicMessageToGemini(msg apicompat.AnthropicMessage) (geminiContent, map[string]string, error) {
	role := "user"
	if msg.Role == "assistant" {
		role = "model"
	}
	out := geminiContent{Role: role}
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		out.Parts = append(out.Parts, geminiPart{Text: text})
		return out, nil, nil
	}
	var blocks []apicompat.AnthropicContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return out, nil, fmt.Errorf("decode Anthropic message content: %w", err)
	}
	names := make(map[string]string)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out.Parts = append(out.Parts, geminiPart{Text: block.Text})
		case "thinking":
			out.Parts = append(out.Parts, geminiPart{Text: block.Thinking, Thought: true, ThoughtSignature: block.Signature})
		case "image":
			if block.Source != nil {
				out.Parts = append(out.Parts, geminiPart{InlineData: &geminiInlineData{MIMEType: block.Source.MediaType, Data: block.Source.Data}})
			}
		case "tool_use":
			names[block.ID] = block.Name
			args := block.Input
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			out.Parts = append(out.Parts, geminiPart{FunctionCall: &geminiFunctionCall{ID: block.ID, Name: block.Name, Args: args}})
		case "tool_result":
			response := block.Content
			if len(response) == 0 {
				response = json.RawMessage(`{}`)
			}
			out.Parts = append(out.Parts, geminiPart{FunctionResponse: &geminiFunctionResponse{ID: block.ToolUseID, Response: response}})
		}
	}
	return out, names, nil
}
