package anthropic

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStreamDecoderPreservesReasoningSignatureDelta(t *testing.T) {
	decoder := newStreamDecoder()

	_, _, err := decoder.Decode([]byte(`{"type":"message_start","message":{"id":"msg-1","type":"message","role":"assistant","model":"model","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`))
	require.NoError(t, err)
	_, _, err = decoder.Decode([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
	require.NoError(t, err)

	events, warnings, err := decoder.Decode([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-"}}`))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Empty(t, events)
	events, warnings, err = decoder.Decode([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"1"}}`))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Empty(t, events)

	events, warnings, err = decoder.Decode([]byte(`{"type":"content_block_stop","index":0}`))
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, []ir.StreamEvent{
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Signature: "sig-1"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
	}, events)
}

func TestStreamEncoderGeneratesAnthropicMessageIDOnlyWhenRequested(t *testing.T) {
	encodeStartID := func(options protocolconv.Options) string {
		t.Helper()
		encoder := newStreamEncoderWithOptions(options)
		payloads, warnings, err := encoder.Encode(ir.StreamEvent{
			Type:       ir.EventStreamStart,
			ResponseID: "upstream-response-id",
			Model:      "claude-test",
		})
		require.NoError(t, err)
		require.Empty(t, warnings)
		for _, payload := range payloads {
			if id := gjson.GetBytes(payload, "message.id"); id.Exists() {
				return id.String()
			}
		}
		t.Fatalf("Anthropic message_start payload missing: %q", payloads)
		return ""
	}

	require.Equal(t, "upstream-response-id", encodeStartID(protocolconv.Options{}))
	require.Regexp(t, `^msg_01[0-9A-Za-z]{22}$`, encodeStartID(protocolconv.Options{
		GenerateAnthropicResponseID: true,
	}))
}

func TestStreamEncoderPreservesReasoningSignatureDelta(t *testing.T) {
	encoder := newStreamEncoder()
	sequence := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "msg-1", Model: "model"},
		{Type: ir.EventContentBlockStart, BlockIndex: 0, BlockType: ir.ContentReasoning},
		{Type: ir.EventReasoningDelta, BlockIndex: 0, Reasoning: "plan", Signature: "sig-1"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
	}

	var payloads [][]byte
	for _, event := range sequence {
		out, warnings, err := encoder.Encode(event)
		require.NoError(t, err)
		require.Empty(t, warnings)
		payloads = append(payloads, out...)
	}

	var found bool
	for _, payload := range payloads {
		if string(payload) == `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}` {
			found = true
			break
		}
	}
	require.True(t, found, "Anthropic stream did not emit the reasoning signature: %q", payloads)
}
