package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
)

func convertGoogleResponseToChatForTest(t *testing.T, geminiResp map[string]any) (*apicompat.ChatCompletionsResponse, []protocolconv.Warning) {
	t.Helper()
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source:         protocolconv.ProtocolOpenAIChat,
			IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel:    "gemini-test",
			UpstreamModel:  "gemini-upstream",
		},
		Options: protocolconv.Options{LossPolicy: protocolconv.LossError},
	})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"model":"gemini-test","messages":[{"role":"user","content":"draw"}]}`))
	require.NoError(t, err)

	rawData, err := json.Marshal(geminiResp)
	require.NoError(t, err)
	converted, err := pipeline.ConvertResponse(rawData, protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)

	var response apicompat.ChatCompletionsResponse
	require.NoError(t, json.Unmarshal(converted.Body, &response))
	return &response, converted.Warnings
}

func TestGooglePipelineToChatCompletionsPreservesInlineData(t *testing.T) {
	tests := []struct {
		name  string
		parts []any
		want  string
	}{
		{
			name: "image only",
			parts: []any{
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aW1hZ2U="}},
			},
			want: "![image](data:image/png;base64,aW1hZ2U=)",
		},
		{
			name: "text and image",
			parts: []any{
				map[string]any{"text": "rendered image:\n"},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/webp", "data": "d2VicA=="}},
			},
			want: "rendered image:\n![image](data:image/webp;base64,d2VicA==)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, warnings := convertGoogleResponseToChatForTest(t, map[string]any{
				"responseId": "gemini-response",
				"candidates": []any{map[string]any{
					"content":      map[string]any{"role": "model", "parts": tt.parts},
					"finishReason": "STOP",
				}},
			})
			require.Empty(t, warnings)
			require.Len(t, response.Choices, 1)
			var content string
			require.NoError(t, json.Unmarshal(response.Choices[0].Message.Content, &content))
			require.Equal(t, tt.want, content)
			require.Equal(t, "stop", response.Choices[0].FinishReason)
		})
	}
}

func TestGooglePipelineToChatCompletionsOmitsInvalidInlineData(t *testing.T) {
	tests := []struct {
		name       string
		inlineData map[string]any
	}{
		{name: "unsupported MIME type", inlineData: map[string]any{"mimeType": "image/svg+xml", "data": "PHN2Zz48L3N2Zz4="}},
		{name: "malformed MIME type", inlineData: map[string]any{"mimeType": "image/png; charset=utf-8", "data": "aW1hZ2U="}},
		{name: "malformed base64", inlineData: map[string]any{"mimeType": "image/png", "data": "not-valid-base64!!!"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, warnings := convertGoogleResponseToChatForTest(t, map[string]any{
				"candidates": []any{map[string]any{
					"content": map[string]any{"role": "model", "parts": []any{
						map[string]any{"text": "before"},
						map[string]any{"inlineData": tt.inlineData},
						map[string]any{"text": "after"},
					}},
					"finishReason": "STOP",
				}},
			})
			require.Len(t, warnings, 1)
			require.Equal(t, protocolconv.WarningDroppedField, warnings[0].Code)
			var content string
			require.NoError(t, json.Unmarshal(response.Choices[0].Message.Content, &content))
			require.Equal(t, "beforeafter", content)
		})
	}
}

func TestGooglePipelineToChatCompletionsRetainsTextAndToolBehavior(t *testing.T) {
	response, warnings := convertGoogleResponseToChatForTest(t, map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "checking"},
				map[string]any{"functionCall": map[string]any{
					"name": "get_weather",
					"args": map[string]any{"city": "Paris"},
				}},
			}},
			"finishReason": "STOP",
		}},
	})
	require.Empty(t, warnings)
	require.Len(t, response.Choices, 1)
	choice := response.Choices[0]
	var content string
	require.NoError(t, json.Unmarshal(choice.Message.Content, &content))
	require.Equal(t, "checking", content)
	require.Equal(t, "tool_calls", choice.FinishReason)
	require.Len(t, choice.Message.ToolCalls, 1)
	require.Equal(t, "get_weather", choice.Message.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"city":"Paris"}`, choice.Message.ToolCalls[0].Function.Arguments)
}
