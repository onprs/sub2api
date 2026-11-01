package anthropic

import (
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRequestRoundTripPreservesServerToolTypes(t *testing.T) {
	for _, toolType := range []string{"web_search_20250305", "bash_20250124", "text_editor_20250728"} {
		t.Run(toolType, func(t *testing.T) {
			body := fmt.Sprintf(`{"model":"claude-test","messages":[{"role":"user","content":"查询状态"}],"tools":[{"type":%q,"name":"server_tool","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}]}`, toolType)
			converter := New()
			decoded, warnings, err := converter.DecodeRequest([]byte(body), protocolconv.Options{LossPolicy: protocolconv.LossError})
			require.NoError(t, err)
			require.Empty(t, warnings)
			encoded, warnings, err := converter.EncodeRequest(decoded, protocolconv.Options{LossPolicy: protocolconv.LossError})
			require.NoError(t, err)
			require.Empty(t, warnings)
			require.JSONEq(t, gjson.Get(body, "tools").Raw, gjson.GetBytes(encoded, "tools").Raw)
		})
	}
}
