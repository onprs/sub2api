package standard

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
)

func FuzzRequestsDoNotPanic(f *testing.F) {
	for _, seed := range [][]byte{[]byte(`{}`), []byte(`{"model":"m","messages":[]}`), []byte(`{"contents":[]}`), []byte(`{"broken":`)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		registry, err := NewRegistry()
		if err != nil {
			t.Fatal(err)
		}
		for _, protocol := range protocolconv.StandardProtocols() {
			_, _, _ = registry.DecodeRequest(body, protocol, protocolconv.Options{SourceModel: "fuzz-model", LossPolicy: protocolconv.LossWarn})
			_, _, _ = registry.DecodeResponse(body, protocol, protocolconv.Options{SourceModel: "fuzz-model", LossPolicy: protocolconv.LossWarn})
		}
	})
}

func FuzzStreamsDoNotPanic(f *testing.F) {
	f.Add([]byte(`{"type":"response.created","response":{"id":"r","model":"m"}}`))
	f.Add([]byte(`{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`))
	f.Add([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`))
	f.Add([]byte(`{"choices":[{"delta":{"content":"x"}}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		registry, err := NewRegistry()
		if err != nil {
			t.Fatal(err)
		}
		for _, protocol := range protocolconv.StandardProtocols() {
			converter, err := registry.Converter(protocol)
			if err != nil {
				t.Fatal(err)
			}
			decoder := converter.NewStreamDecoder()
			if decoder == nil {
				t.Fatalf("nil decoder for %s", protocol)
			}
			_, _, _ = decoder.Decode(body)
		}
	})
}
