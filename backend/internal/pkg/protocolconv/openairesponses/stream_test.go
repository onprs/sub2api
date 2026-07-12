package openairesponses

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestStreamDecoderAllowsInProgressAfterCreated(t *testing.T) {
	decoder := newStreamDecoder()
	events, _, err := decoder.Decode([]byte(`{"type":"response.created","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`))
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, ir.EventStreamStart, events[0].Type)

	events, _, err = decoder.Decode([]byte(`{"type":"response.in_progress","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`))
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestStreamDecoderRejectsDuplicateCreated(t *testing.T) {
	decoder := newStreamDecoder()
	payload := []byte(`{"type":"response.created","response":{"id":"resp-1","object":"response","model":"model","status":"in_progress","output":[]}}`)
	_, _, err := decoder.Decode(payload)
	require.NoError(t, err)
	_, _, err = decoder.Decode(payload)
	require.ErrorContains(t, err, "duplicate response.created")
}
