package protocolconv

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var productionGPTTextModels = []string{
	"gpt-5.2",
	"gpt-5.2-2025-12-11",
	"gpt-5.2-chat-latest",
	"gpt-5.2-pro",
	"gpt-5.2-pro-2025-12-11",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
	"gpt-5.4",
	"gpt-5.4-2026-03-05",
	"gpt-5.4-mini",
	"gpt-5.5",
	"gpt-5.6",
	"gpt-5.6-luna",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
}

func TestProductionGPTModelsConvertAcrossProtocolsWithoutLosingStablePrefix(t *testing.T) {
	stablePrefix := "production-cache-prefix-v1"
	for _, model := range productionGPTTextModels {
		t.Run(model, func(t *testing.T) {
			chatBody, err := json.Marshal(map[string]any{
				"model": model,
				"messages": []map[string]any{
					{"role": "system", "content": stablePrefix},
					{"role": "user", "content": "hi"},
				},
				"reasoning_effort": "low",
				"stream":           false,
			})
			require.NoError(t, err)

			responses, err := ConvertRequest(chatBody, ProtocolOpenAICompat, Target{Protocol: ProtocolOpenAIResponses}, Options{})
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(responses, "model").String())
			require.Equal(t, "low", gjson.GetBytes(responses, "reasoning.effort").String())
			require.Contains(t, string(responses), stablePrefix)

			anthropic, err := ConvertRequest(responses, ProtocolOpenAIResponses, Target{Protocol: ProtocolAnthropic}, Options{})
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(anthropic, "model").String())
			require.Equal(t, "low", gjson.GetBytes(anthropic, "output_config.effort").String())
			require.Equal(t, "enabled", gjson.GetBytes(anthropic, "thinking.type").String())
			require.Contains(t, string(anthropic), stablePrefix)

			roundTrip, err := ConvertRequest(anthropic, ProtocolAnthropic, Target{Protocol: ProtocolOpenAIResponses}, Options{})
			require.NoError(t, err)
			require.Equal(t, model, gjson.GetBytes(roundTrip, "model").String())
			require.Equal(t, "low", gjson.GetBytes(roundTrip, "reasoning.effort").String())
			require.Contains(t, string(roundTrip), stablePrefix)
		})
	}
}
