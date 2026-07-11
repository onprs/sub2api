package protocolconv

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// ConvertResponse translates a complete non-streaming response. Streaming
// transports use the existing stateful apicompat event converters, but share
// the same canonical Responses representation and field semantics.
func ConvertResponse(body []byte, from Protocol, to Protocol, model string) ([]byte, error) {
	if from == to {
		if !json.Valid(body) {
			return nil, fmt.Errorf("invalid JSON response")
		}
		return body, nil
	}
	canonical, err := responseToResponses(body, from, model)
	if err != nil {
		return nil, err
	}
	return responsesToResponse(canonical, to, model)
}

func responseToResponses(body []byte, from Protocol, model string) (*apicompat.ResponsesResponse, error) {
	switch from {
	case ProtocolOpenAIResponses:
		var resp apicompat.ResponsesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode Responses response: %w", err)
		}
		return &resp, nil
	case ProtocolOpenAICompat:
		var resp apicompat.ChatCompletionsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode Chat Completions response: %w", err)
		}
		return apicompat.ChatCompletionsResponseToResponses(&resp, model, nil, false, nil), nil
	case ProtocolAnthropic:
		var resp apicompat.AnthropicResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode Anthropic response: %w", err)
		}
		return apicompat.AnthropicToResponsesResponse(&resp), nil
	case ProtocolGemini:
		return geminiResponseToResponses(body, model)
	default:
		return nil, fmt.Errorf("unsupported source response protocol %q", from)
	}
}

func responsesToResponse(resp *apicompat.ResponsesResponse, to Protocol, model string) ([]byte, error) {
	var out any
	switch to {
	case ProtocolOpenAIResponses:
		out = resp
	case ProtocolOpenAICompat:
		out = apicompat.ResponsesToChatCompletions(resp, model)
	case ProtocolAnthropic:
		out = apicompat.ResponsesToAnthropic(resp, model)
	case ProtocolGemini:
		out = responsesResponseToGemini(resp)
	default:
		return nil, fmt.Errorf("unsupported target response protocol %q", to)
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode %s response: %w", to, err)
	}
	return body, nil
}

type geminiResponse struct {
	Candidates    []geminiCandidate    `json:"candidates,omitempty"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
	ResponseID    string               `json:"responseId,omitempty"`
	ModelVersion  string               `json:"modelVersion,omitempty"`
}

type geminiResponseEnvelope struct {
	Response     *geminiResponse `json:"response,omitempty"`
	ResponseID   string          `json:"responseId,omitempty"`
	ModelVersion string          `json:"modelVersion,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
	Index        int           `json:"index,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount        int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int `json:"candidatesTokenCount,omitempty"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount,omitempty"`
	TotalTokenCount         int `json:"totalTokenCount,omitempty"`
}

func geminiResponseToResponses(body []byte, model string) (*apicompat.ResponsesResponse, error) {
	var envelope geminiResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode Gemini response: %w", err)
	}
	resp := envelope.Response
	if resp == nil {
		var direct geminiResponse
		if err := json.Unmarshal(body, &direct); err != nil {
			return nil, fmt.Errorf("decode direct Gemini response: %w", err)
		}
		resp = &direct
	}
	if resp.ResponseID == "" {
		resp.ResponseID = envelope.ResponseID
	}
	if resp.ModelVersion == "" {
		resp.ModelVersion = envelope.ModelVersion
	}
	out := &apicompat.ResponsesResponse{
		ID: resp.ResponseID, Object: "response", Model: model, Status: "completed",
	}
	if out.Model == "" {
		out.Model = resp.ModelVersion
	}
	if len(resp.Candidates) > 0 {
		candidate := resp.Candidates[0]
		for _, part := range candidate.Content.Parts {
			switch {
			case part.Thought && part.Text != "":
				out.Output = append(out.Output, apicompat.ResponsesOutput{
					Type: "reasoning", EncryptedContent: part.ThoughtSignature,
					Summary: []apicompat.ResponsesSummary{{Type: "summary_text", Text: part.Text}},
				})
			case part.FunctionCall != nil:
				args := strings.TrimSpace(string(part.FunctionCall.Args))
				if args == "" || args == "null" {
					args = "{}"
				}
				out.Output = append(out.Output, apicompat.ResponsesOutput{
					Type: "function_call", CallID: part.FunctionCall.ID,
					Name: part.FunctionCall.Name, Arguments: args, Status: "completed",
				})
			case part.Text != "":
				out.Output = append(out.Output, apicompat.ResponsesOutput{
					Type: "message", Role: "assistant", Status: "completed",
					Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: part.Text}},
				})
			}
		}
		if strings.EqualFold(candidate.FinishReason, "MAX_TOKENS") {
			out.Status = "incomplete"
			out.IncompleteDetails = &apicompat.ResponsesIncompleteDetails{Reason: "max_output_tokens"}
		}
	}
	if len(out.Output) == 0 {
		out.Output = []apicompat.ResponsesOutput{{
			Type: "message", Role: "assistant", Status: "completed",
			Content: []apicompat.ResponsesContentPart{{Type: "output_text", Text: ""}},
		}}
	}
	if usage := resp.UsageMetadata; usage != nil {
		out.Usage = &apicompat.ResponsesUsage{
			InputTokens:         usage.PromptTokenCount,
			OutputTokens:        usage.CandidatesTokenCount + usage.ThoughtsTokenCount,
			TotalTokens:         usage.TotalTokenCount,
			InputTokensDetails:  &apicompat.ResponsesInputTokensDetails{CachedTokens: usage.CachedContentTokenCount},
			OutputTokensDetails: &apicompat.ResponsesOutputTokensDetails{ReasoningTokens: usage.ThoughtsTokenCount},
		}
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.InputTokens + out.Usage.OutputTokens
		}
	}
	return out, nil
}

func responsesResponseToGemini(resp *apicompat.ResponsesResponse) *geminiResponse {
	out := &geminiResponse{ResponseID: resp.ID, ModelVersion: resp.Model}
	candidate := geminiCandidate{Content: geminiContent{Role: "model"}, FinishReason: "STOP"}
	for _, item := range resp.Output {
		switch item.Type {
		case "reasoning":
			for _, summary := range item.Summary {
				if summary.Type == "summary_text" {
					candidate.Content.Parts = append(candidate.Content.Parts, geminiPart{
						Text: summary.Text, Thought: true, ThoughtSignature: item.EncryptedContent,
					})
				}
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					candidate.Content.Parts = append(candidate.Content.Parts, geminiPart{Text: part.Text})
				}
			}
		case "function_call":
			args := json.RawMessage(item.Arguments)
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			candidate.Content.Parts = append(candidate.Content.Parts, geminiPart{FunctionCall: &geminiFunctionCall{
				ID: item.CallID, Name: item.Name, Args: args,
			}})
		}
	}
	if resp.Status == "incomplete" {
		candidate.FinishReason = "MAX_TOKENS"
	}
	out.Candidates = []geminiCandidate{candidate}
	if resp.Usage != nil {
		cached := 0
		if resp.Usage.InputTokensDetails != nil {
			cached = resp.Usage.InputTokensDetails.CachedTokens
		}
		reasoning := 0
		if resp.Usage.OutputTokensDetails != nil {
			reasoning = resp.Usage.OutputTokensDetails.ReasoningTokens
		}
		out.UsageMetadata = &geminiUsageMetadata{
			PromptTokenCount:        resp.Usage.InputTokens,
			CandidatesTokenCount:    max(resp.Usage.OutputTokens-reasoning, 0),
			CachedContentTokenCount: cached,
			ThoughtsTokenCount:      reasoning,
			TotalTokenCount:         resp.Usage.TotalTokens,
		}
	}
	return out
}
