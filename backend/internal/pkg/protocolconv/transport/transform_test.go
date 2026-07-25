package transport

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransformEventStreamNormalizesPayloadAndPreservesMetadata(t *testing.T) {
	inner := NewSSEParser(io.NopCloser(strings.NewReader("event: message\nid: 7\ndata: {\"response\":{\"value\":1}}\n\n")), 0)
	stream, err := NewTransformEventStream(inner, func(body []byte) ([]byte, error) {
		var wrapper struct {
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, err
		}
		return wrapper.Response, nil
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	record, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "message", record.Event)
	require.Equal(t, "7", record.ID)
	require.JSONEq(t, `{"value":1}`, string(record.Data))
	raw, ok := record.Metadata["raw_upstream_data"].([]byte)
	require.True(t, ok)
	require.JSONEq(t, `{"response":{"value":1}}`, string(raw))
	record.Data[0] = '['
	require.Equal(t, byte('{'), raw[0])
}

func TestTransformEventStreamOmitsMetadataWhenPayloadIsUnchanged(t *testing.T) {
	inner := NewSSEParser(io.NopCloser(strings.NewReader("data: {\"value\":1}\n\n")), 0)
	stream, err := NewTransformEventStream(inner, func(body []byte) ([]byte, error) { return body, nil })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	record, err := stream.Next(context.Background())
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}

func TestTransformEventStreamPropagatesTerminal(t *testing.T) {
	inner := NewSSEParser(io.NopCloser(strings.NewReader("data: [DONE]\n\n")), 0)
	stream, err := NewTransformEventStream(inner, func(body []byte) ([]byte, error) { return body, nil })
	require.NoError(t, err)
	_, err = stream.Next(context.Background())
	require.ErrorIs(t, err, ErrSSEDone)
}
