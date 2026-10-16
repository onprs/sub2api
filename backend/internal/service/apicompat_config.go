package service

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func configuredMinOutputTokens(cfg *config.Config) int {
	if cfg != nil && cfg.Gateway.MinOutputTokens >= 16 {
		return cfg.Gateway.MinOutputTokens
	}
	return apicompat.DefaultMinMaxOutputTokens
}

func chatCompletionsToResponses(cfg *config.Config, req *apicompat.ChatCompletionsRequest) (*apicompat.ResponsesRequest, error) {
	return apicompat.ChatCompletionsToResponsesWithMinOutputTokens(req, configuredMinOutputTokens(cfg))
}

// applyMinOutputTokensFloor 保证 Responses 请求体的 max_output_tokens 不低于配置下限：
// 过小的值会被上游拒绝，nil 或已高于下限时保持原样。
func applyMinOutputTokensFloor(body []byte, minOutputTokens int) []byte {
	if minOutputTokens <= 0 {
		return body
	}
	current := gjson.GetBytes(body, "max_output_tokens")
	if !current.Exists() || current.Int() <= 0 || current.Int() >= int64(minOutputTokens) {
		return body
	}
	updated, err := sjson.SetBytes(body, "max_output_tokens", minOutputTokens)
	if err != nil {
		return body
	}
	return updated
}

// applyMinOutputTokensFloorToRequest 与 applyMinOutputTokensFloor 相同，作用于已解析的请求。
func applyMinOutputTokensFloorToRequest(req *apicompat.ResponsesRequest, minOutputTokens int) {
	if req == nil || req.MaxOutputTokens == nil || minOutputTokens <= 0 {
		return
	}
	if *req.MaxOutputTokens <= 0 || *req.MaxOutputTokens >= minOutputTokens {
		return
	}
	value := minOutputTokens
	req.MaxOutputTokens = &value
}

func settingServiceConfig(settingService *SettingService) *config.Config {
	if settingService == nil {
		return nil
	}
	return settingService.cfg
}
