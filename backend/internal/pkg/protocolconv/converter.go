// Package protocolconv owns protocol translation independently from account
// selection, authentication, model mapping, and upstream transport.
package protocolconv

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

type Protocol string

const (
	ProtocolAnthropic       Protocol = "anthropic_messages"
	ProtocolGemini          Protocol = "gemini_generate_content"
	ProtocolOpenAICompat    Protocol = "openai_compat"
	ProtocolOpenAIResponses Protocol = "openai_responses"
)

type AntigravityFamily string

const (
	AntigravityFamilyClaude AntigravityFamily = "claude"
	AntigravityFamilyGemini AntigravityFamily = "gemini"
)

type Target struct {
	Protocol          Protocol
	AntigravityFamily AntigravityFamily
}

func (t Target) Validate() error {
	switch t.Protocol {
	case ProtocolAnthropic, ProtocolGemini, ProtocolOpenAICompat, ProtocolOpenAIResponses:
	default:
		return fmt.Errorf("unsupported target protocol %q", t.Protocol)
	}
	if t.AntigravityFamily == "" {
		return nil
	}
	if t.Protocol != ProtocolGemini {
		return errors.New("antigravity targets use the Gemini wire protocol")
	}
	switch t.AntigravityFamily {
	case AntigravityFamilyClaude, AntigravityFamilyGemini:
		return nil
	default:
		return fmt.Errorf("unsupported antigravity endpoint family %q", t.AntigravityFamily)
	}
}

type Options struct {
	// Model supplies the URL model for Gemini requests, whose body normally has
	// no model field. Other protocols keep the model carried by the body.
	Model string
}

// ConvertRequest translates a request without applying provider policy. An
// identity conversion returns the original bytes, preserving the exact prompt
// prefix and avoiding needless cache-key churn.
func ConvertRequest(body []byte, from Protocol, target Target, opts Options) ([]byte, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	if from == target.Protocol {
		if !json.Valid(body) {
			return nil, errors.New("invalid JSON request")
		}
		return body, nil
	}

	canonical, err := requestToResponses(body, from, opts)
	if err != nil {
		return nil, err
	}
	converted, err := responsesToRequest(canonical, target.Protocol, opts)
	if err != nil {
		return nil, err
	}
	if from == ProtocolAnthropic && target.Protocol == ProtocolOpenAICompat {
		return preserveAnthropicCompatControls(body, converted)
	}
	return converted, nil
}

func requestToResponses(body []byte, from Protocol, opts Options) (*apicompat.ResponsesRequest, error) {
	switch from {
	case ProtocolOpenAIResponses:
		var req apicompat.ResponsesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("decode Responses request: %w", err)
		}
		return &req, nil
	case ProtocolOpenAICompat:
		var req apicompat.ChatCompletionsRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("decode Chat Completions request: %w", err)
		}
		out, err := apicompat.ChatCompletionsToResponses(&req)
		if err != nil {
			return nil, fmt.Errorf("convert Chat Completions request: %w", err)
		}
		// Conversion describes protocol semantics; transport decides whether the
		// upstream is forced to stream.
		out.Stream = req.Stream
		return out, nil
	case ProtocolAnthropic:
		var req apicompat.AnthropicRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("decode Anthropic request: %w", err)
		}
		out, err := apicompat.AnthropicToResponses(&req)
		if err != nil {
			return nil, fmt.Errorf("convert Anthropic request: %w", err)
		}
		return out, nil
	case ProtocolGemini:
		return geminiRequestToResponses(body, opts.Model)
	default:
		return nil, fmt.Errorf("unsupported source protocol %q", from)
	}
}

func responsesToRequest(req *apicompat.ResponsesRequest, to Protocol, opts Options) ([]byte, error) {
	var value any
	switch to {
	case ProtocolOpenAIResponses:
		value = req
	case ProtocolOpenAICompat:
		out, err := apicompat.ResponsesToChatCompletionsRequest(req)
		if err != nil {
			return nil, fmt.Errorf("convert to Chat Completions request: %w", err)
		}
		value = out
	case ProtocolAnthropic:
		out, err := apicompat.ResponsesToAnthropicRequest(req)
		if err != nil {
			return nil, fmt.Errorf("convert to Anthropic request: %w", err)
		}
		value = out
	case ProtocolGemini:
		out, err := responsesRequestToGemini(req)
		if err != nil {
			return nil, err
		}
		value = out
	default:
		return nil, fmt.Errorf("unsupported target protocol %q", to)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", to, err)
	}
	return out, nil
}

func preserveAnthropicCompatControls(source, converted []byte) ([]byte, error) {
	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(source, &anthropicReq); err != nil {
		return nil, err
	}
	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(converted, &chatReq); err != nil {
		return nil, err
	}

	// max_tokens is the broadly supported Chat Completions spelling. Preserve
	// the exact client budget instead of leaking the Responses minimum/floor.
	if anthropicReq.MaxTokens > 0 {
		maxTokens := anthropicReq.MaxTokens
		chatReq.MaxTokens = &maxTokens
	}
	chatReq.MaxCompletionTokens = nil
	if len(anthropicReq.StopSeqs) == 1 {
		chatReq.Stop, _ = json.Marshal(anthropicReq.StopSeqs[0])
	} else if len(anthropicReq.StopSeqs) > 1 {
		chatReq.Stop, _ = json.Marshal(anthropicReq.StopSeqs)
	}
	if anthropicReq.Thinking != nil &&
		(anthropicReq.Thinking.Type == "enabled" || anthropicReq.Thinking.Type == "adaptive") &&
		(anthropicReq.OutputConfig == nil || strings.TrimSpace(anthropicReq.OutputConfig.Effort) == "") {
		// Chat Completions has no standard thinking request object. Strict
		// compatible providers reject unknown fields, so do not manufacture a
		// vendor extension or an implicit OpenAI reasoning_effort.
		chatReq.ReasoningEffort = ""
		chatReq.Thinking = nil
		chatReq.EnableThinking = nil
	}
	return json.Marshal(&chatReq)
}

func ProtocolFromEndpoint(path string) (Protocol, bool) {
	path = strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.Contains(path, "/messages"):
		return ProtocolAnthropic, true
	case strings.Contains(path, "/chat/completions"):
		return ProtocolOpenAICompat, true
	case strings.Contains(path, "/responses"):
		return ProtocolOpenAIResponses, true
	case strings.Contains(path, ":generatecontent"), strings.Contains(path, ":streamgeneratecontent"):
		return ProtocolGemini, true
	default:
		return "", false
	}
}
