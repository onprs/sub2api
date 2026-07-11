package protocolconv

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

type AntigravityOptions struct {
	ProjectID        string
	MappedModel      string
	TransformOptions antigravity.TransformOptions
}

// ConvertAntigravityRequest keeps the two Antigravity endpoint families
// explicit. Claude-family requests use Antigravity's Claude-aware transformer;
// Gemini-family requests stay on the native Gemini shape. Callers must never
// infer the family from a client-facing protocol or model alias.
func ConvertAntigravityRequest(body []byte, from Protocol, family AntigravityFamily, opts AntigravityOptions) ([]byte, error) {
	target := Target{Protocol: ProtocolGemini, AntigravityFamily: family}
	if err := target.Validate(); err != nil {
		return nil, err
	}

	switch family {
	case AntigravityFamilyClaude:
		anthropicBody := body
		if from != ProtocolAnthropic {
			var err error
			anthropicBody, err = ConvertRequest(body, from, Target{Protocol: ProtocolAnthropic}, Options{})
			if err != nil {
				return nil, err
			}
		}
		var req antigravity.ClaudeRequest
		if err := json.Unmarshal(anthropicBody, &req); err != nil {
			return nil, fmt.Errorf("decode Antigravity Claude-family request: %w", err)
		}
		return antigravity.TransformClaudeToGeminiWithOptions(
			&req,
			opts.ProjectID,
			opts.MappedModel,
			opts.TransformOptions,
		)
	case AntigravityFamilyGemini:
		return ConvertRequest(body, from, target, Options{Model: opts.MappedModel})
	default:
		return nil, fmt.Errorf("unsupported Antigravity endpoint family %q", family)
	}
}
