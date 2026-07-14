package anthropic

import (
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRequestRoundTripPreservesMultimodalToolResult(t *testing.T) {
	body := []byte(`{
		"model":"claude-test",
		"max_tokens":64,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call-image","name":"inspect","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-image","content":[
				{"type":"text","text":"chart"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
			]}]}
		]
	}`)
	converter := New()
	options := protocolconv.Options{LossPolicy: protocolconv.LossError}

	request, warnings, err := converter.DecodeRequest(body, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Len(t, request.Messages, 2)
	result := request.Messages[1].Content[0]
	require.Empty(t, result.ToolResult)
	require.Len(t, result.ToolResultContent, 2)
	require.Equal(t, "chart", result.ToolResultContent[0].Text)
	require.Equal(t, "image/png", result.ToolResultContent[1].MediaType)
	require.Equal(t, "AAAA", result.ToolResultContent[1].Data)

	encoded, warnings, err := converter.EncodeRequest(request, options)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, "text", gjson.GetBytes(encoded, "messages.1.content.0.content.0.type").String())
	require.Equal(t, "chart", gjson.GetBytes(encoded, "messages.1.content.0.content.0.text").String())
	require.Equal(t, "image", gjson.GetBytes(encoded, "messages.1.content.0.content.1.type").String())
	require.Equal(t, "image/png", gjson.GetBytes(encoded, "messages.1.content.0.content.1.source.media_type").String())
	require.Equal(t, "AAAA", gjson.GetBytes(encoded, "messages.1.content.0.content.1.source.data").String())
}

func TestEncodeRequestRejectsURLImageInsideToolResultInStrictMode(t *testing.T) {
	request := &ir.Request{
		Model: "claude-test",
		Messages: []ir.Message{{
			Role: ir.RoleTool,
			Content: []ir.ContentPart{{
				Type:       ir.ContentToolResult,
				ToolCallID: "call-image",
				ToolResultContent: []ir.ContentPart{{
					Type: ir.ContentImage,
					URL:  "https://example.invalid/chart.png",
				}},
			}},
		}},
	}

	_, warnings, err := New().EncodeRequest(request, protocolconv.Options{LossPolicy: protocolconv.LossError})
	require.Error(t, err)
	require.Empty(t, warnings)
	var conversionErr *protocolconv.Error
	require.True(t, errors.As(err, &conversionErr))
	require.Equal(t, protocolconv.CapabilityImageURL, conversionErr.Capability)
}
