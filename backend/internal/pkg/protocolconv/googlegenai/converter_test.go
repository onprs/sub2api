package googlegenai

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEncodeRequestMapsServerSearchOutsideFunctionDeclarations(t *testing.T) {
	maxTokens := 16
	request := &ir.Request{
		Model:    "gemini-test",
		Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "search"}}}},
		Tools: []ir.ToolDefinition{
			{Type: "function", Name: "get_weather", Parameters: []byte(`{"type":"object"}`)},
			{Type: "function", ProviderType: "web_search_20250305", Name: "web_search"},
		},
		Generation: ir.GenerationConfig{MaxTokens: &maxTokens},
	}

	body, warnings, err := New().EncodeRequest(request, protocolconv.Options{})
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, int64(2), gjson.GetBytes(body, "tools.#").Int())
	require.Equal(t, "get_weather", gjson.GetBytes(body, "tools.0.functionDeclarations.0.name").String())
	require.True(t, gjson.GetBytes(body, "tools.1.googleSearch").Exists())
	require.False(t, gjson.GetBytes(body, "tools.1.functionDeclarations").Exists())
}

func TestDecodeRequestPreservesGoogleSearchProviderType(t *testing.T) {
	request, _, err := New().DecodeRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"search"}]}],"tools":[{"googleSearch":{}}]}`), protocolconv.Options{SourceModel: "gemini-test"})
	require.NoError(t, err)
	require.Len(t, request.Tools, 1)
	require.Equal(t, "google_search", request.Tools[0].ProviderType)
}
