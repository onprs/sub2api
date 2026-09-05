package googlegenai

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestToolParametersJSONSchemaRoundTrip(t *testing.T) {
	schema := `{"type":"object","properties":{"records":{"type":"array","items":{"type":"object","properties":{"labels":{"type":"object","additionalProperties":{"type":"string"}}},"additionalProperties":false}}},"required":["records"],"additionalProperties":false}`
	request := &ir.Request{Model: "model", Messages: []ir.Message{{Role: ir.RoleUser, Content: []ir.ContentPart{{Type: ir.ContentText, Text: "check"}}}}, Tools: []ir.ToolDefinition{{Type: "function", Name: "validate_records", Parameters: []byte(schema)}}}
	for _, enabled := range []bool{false, true} {
		body, warnings, err := New().EncodeRequest(request, protocolconv.Options{GoogleToolParametersJSONSchema: enabled})
		require.NoError(t, err)
		require.Empty(t, warnings)
		declaration := gjson.GetBytes(body, "tools.0.functionDeclarations.0")
		field, absent := "parameters", "parametersJsonSchema"
		if enabled {
			field, absent = absent, field
		}
		require.False(t, declaration.Get(absent).Exists())
		require.JSONEq(t, schema, declaration.Get(field).Raw)
		restored, warnings, err := New().DecodeRequest(body, protocolconv.Options{SourceModel: "model"})
		require.NoError(t, err)
		require.Empty(t, warnings)
		require.Len(t, restored.Tools, 1)
		require.JSONEq(t, schema, string(restored.Tools[0].Parameters))
	}
}

func TestDecodeRequestRejectsConflictingToolSchemaFields(t *testing.T) {
	_, _, err := New().DecodeRequest([]byte(`{"contents":[{"role":"user","parts":[{"text":"check"}]}],"tools":[{"functionDeclarations":[{"name":"validate_records","parameters":{"type":"object"},"parametersJsonSchema":{"type":"object"}}]}]}`), protocolconv.Options{SourceModel: "model"})
	require.ErrorContains(t, err, "mutually exclusive")
}
