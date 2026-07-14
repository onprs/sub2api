package transport

import (
	"bytes"
	"context"
	"errors"
)

// RecordTransform normalizes one provider transport envelope while preserving
// the SSE record metadata. It must return one complete JSON payload.
type RecordTransform func([]byte) ([]byte, error)

// TransformEventStream applies a request-scoped transport envelope transform
// before protocol decoding. Protocol converters still receive the actual
// provider schema, not vendor wrapper objects.
type TransformEventStream struct {
	inner     EventStream
	transform RecordTransform
}

func NewTransformEventStream(inner EventStream, transform RecordTransform) (*TransformEventStream, error) {
	if inner == nil {
		return nil, errors.New("nil inner event stream")
	}
	if transform == nil {
		return nil, errors.New("nil event stream transform")
	}
	return &TransformEventStream{inner: inner, transform: transform}, nil
}

func (s *TransformEventStream) Next(ctx context.Context) (SSERecord, error) {
	if s == nil || s.inner == nil {
		return SSERecord{}, errors.New("nil transformed event stream")
	}
	record, err := s.inner.Next(ctx)
	if err != nil {
		return SSERecord{}, err
	}
	rawData := append([]byte(nil), record.Data...)
	data, err := s.transform(record.Data)
	if err != nil {
		return SSERecord{}, err
	}
	if len(data) == 0 {
		return SSERecord{}, errors.New("event stream transform returned empty payload")
	}
	if !bytes.Equal(rawData, data) {
		if record.Metadata == nil {
			record.Metadata = make(map[string]any, 1)
		}
		record.Metadata["raw_upstream_data"] = rawData
	}
	record.Data = append([]byte(nil), data...)
	return record, nil
}

func (s *TransformEventStream) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

var _ EventStream = (*TransformEventStream)(nil)
