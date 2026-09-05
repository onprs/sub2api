package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
)

// resolveAntigravityRequestReasoning 在账号路由之后选择档位，不写入 model_mapping。
func resolveAntigravityRequestReasoning(route domain.AntigravityModelRoute, body []byte, source protocolconv.Protocol) (domain.AntigravityModelRoute, *string, error) {
	base, _ := domain.AntigravityReasoningModel(route.ModelID)
	if base == "" {
		return route, nil, nil
	}
	var request struct {
		ReasoningEffort string `json:"reasoning_effort"`
		Reasoning       struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
		Thinking struct {
			Type   string `json:"type"`
			Budget *int   `json:"budget_tokens"`
		} `json:"thinking"`
		GenerationConfig struct {
			ThinkingConfig struct {
				Level  string `json:"thinkingLevel"`
				Budget *int   `json:"thinkingBudget"`
			} `json:"thinkingConfig"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return route, nil, fmt.Errorf("invalid reasoning configuration: %w", err)
	}
	var effort string
	var budget *int
	switch source {
	case protocolconv.ProtocolOpenAIChat:
		effort = request.ReasoningEffort
		if effort == "" {
			effort = request.Reasoning.Effort
		}
	case protocolconv.ProtocolOpenAIResponses:
		effort = request.Reasoning.Effort
	case protocolconv.ProtocolAnthropic:
		effort = request.OutputConfig.Effort
		budget = request.Thinking.Budget
		if request.Thinking.Type == "disabled" {
			effort = "none"
		}
	default:
		effort = request.GenerationConfig.ThinkingConfig.Level
		budget = request.GenerationConfig.ThinkingConfig.Budget
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" && budget != nil {
		switch {
		case *budget == 0:
			effort = "none"
		case *budget == -1:
			effort = "high"
		case *budget < -1:
			return route, nil, fmt.Errorf("thinking budget must be -1 or nonnegative")
		case base == "gemini-3.1-pro":
			effort = "high"
			if *budget <= 1001 {
				effort = "low"
			}
		case *budget <= 1000:
			effort = "low"
		case *budget <= 4000:
			effort = "medium"
		default:
			effort = "high"
		}
	}
	selected, ok := domain.ResolveAntigravityReasoningRoute(route.ModelID, effort)
	if !ok {
		return route, nil, fmt.Errorf("reasoning effort %q is not supported by %s", effort, base)
	}
	_, actualEffort := domain.AntigravityReasoningModel(selected.ModelID)
	return selected, &actualEffort, nil
}

// antigravityPublicBillingModel 仅在 Antigravity 计费边界折叠物理档位。
func antigravityPublicBillingModel(model string) string {
	if base, _ := domain.AntigravityReasoningModel(model); base != "" {
		return base
	}
	for _, route := range domain.AntigravityUserModelRoutes() {
		if route.WireModel == model {
			return domain.AntigravityPublicModelID(route.ModelID)
		}
	}
	return model
}
