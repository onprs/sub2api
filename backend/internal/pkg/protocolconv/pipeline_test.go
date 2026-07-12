package protocolconv

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/ir"
	"github.com/stretchr/testify/require"
)

func TestPipelineBindsExplicitRouteAndRestoresClientModel(t *testing.T) {
	registry := NewRegistry()
	source := &stubConverter{
		protocol: ProtocolOpenAIChat,
		decodeRequest: func(body []byte) (*ir.Request, []Warning, error) {
			return &ir.Request{Model: "client-model", Messages: []ir.Message{{Role: ir.RoleUser}}}, nil, nil
		},
		encodeResponse: func(response *ir.Response, _ Options) ([]byte, []Warning, error) {
			body, err := json.Marshal(map[string]any{"model": response.Model})
			return body, nil, err
		},
		newStreamEncoder: func() StreamEncoder { return inertStreamEncoder{} },
	}
	target := &stubConverter{
		protocol: ProtocolAnthropic,
		encodeRequest: func(request *ir.Request, options Options) ([]byte, []Warning, error) {
			require.Equal(t, "upstream-model", options.SourceModel)
			return []byte(`{"model":"upstream-model"}`), []Warning{{Code: WarningNormalizedField}}, nil
		},
		decodeResponse: func(body []byte) (*ir.Response, []Warning, error) {
			return &ir.Response{Model: "upstream-model"}, nil, nil
		},
		newStreamDecoder: func() StreamDecoder { return inertStreamDecoder{} },
	}
	require.NoError(t, registry.Register(source))
	require.NoError(t, registry.Register(target))

	pipeline, err := NewPipeline(registry, PipelineConfig{
		Source:         ProtocolOpenAIChat,
		IntendedTarget: ProtocolAnthropic,
		ClientModel:    "client-model",
		UpstreamModel:  "upstream-model",
	})
	require.NoError(t, err)

	request, err := pipeline.ConvertRequest([]byte(`{"model":"client-model"}`))
	require.NoError(t, err)
	require.Equal(t, ProtocolOpenAIChat, request.Source)
	require.Equal(t, ProtocolAnthropic, request.IntendedTarget)
	require.Equal(t, "client-model", request.ClientModel)
	require.Equal(t, "upstream-model", request.UpstreamModel)
	require.Len(t, request.Warnings, 1)

	response, err := pipeline.ConvertResponse([]byte(`{"model":"upstream-model"}`), ProtocolAnthropic)
	require.NoError(t, err)
	require.Equal(t, ProtocolAnthropic, response.ActualUpstream)
	require.JSONEq(t, `{"model":"client-model"}`, string(response.Body))
	require.Len(t, pipeline.Warnings(), 1)
}

func TestPipelineIsOneShotEvenAfterRequestFailure(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolOpenAIChat}))
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolAnthropic}))
	pipeline, err := NewPipeline(registry, PipelineConfig{Source: ProtocolOpenAIChat, IntendedTarget: ProtocolAnthropic})
	require.NoError(t, err)

	_, err = pipeline.ConvertRequest([]byte(`{"broken":`))
	require.Error(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{}`))
	require.ErrorContains(t, err, "already attempted")
	_, err = pipeline.ConvertResponse([]byte(`{}`), ProtocolAnthropic)
	require.ErrorContains(t, err, "has not completed")
}

func TestPipelineRequiresExplicitActualUpstreamProtocol(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolOpenAIChat}))
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolAnthropic}))
	pipeline, err := NewPipeline(registry, PipelineConfig{Source: ProtocolOpenAIChat, IntendedTarget: ProtocolAnthropic})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{}`))
	require.NoError(t, err)

	_, err = pipeline.ConvertResponse([]byte(`{}`), "")
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorUnsupportedProtocol, conversionErr.Code)
}

func TestPipelineIdentityResponsePreservesBytes(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(&stubConverter{protocol: ProtocolOpenAIChat}))
	pipeline, err := NewPipeline(registry, PipelineConfig{Source: ProtocolOpenAIChat, IntendedTarget: ProtocolOpenAIChat})
	require.NoError(t, err)
	requestBody := []byte(" {\n \"model\": \"client\"\n}\n")
	_, err = pipeline.ConvertRequest(requestBody)
	require.NoError(t, err)

	responseBody := []byte(" {\n \"model\" : \"client\", \"unknown\": true\n}\n")
	response, err := pipeline.ConvertResponse(responseBody, ProtocolOpenAIChat)
	require.NoError(t, err)
	require.Equal(t, responseBody, response.Body)
}

func TestPipelineCreatesIsolatedStreamProcessors(t *testing.T) {
	registry := NewRegistry()
	source := &stubConverter{protocol: ProtocolOpenAIChat, newStreamEncoder: func() StreamEncoder { return inertStreamEncoder{} }}
	target := &stubConverter{protocol: ProtocolAnthropic, newStreamDecoder: func() StreamDecoder { return inertStreamDecoder{} }}
	require.NoError(t, registry.Register(source))
	require.NoError(t, registry.Register(target))
	pipeline, err := NewPipeline(registry, PipelineConfig{Source: ProtocolOpenAIChat, IntendedTarget: ProtocolAnthropic})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{}`))
	require.NoError(t, err)

	first, err := pipeline.NewStreamProcessor(ProtocolAnthropic)
	require.NoError(t, err)
	second, err := pipeline.NewStreamProcessor(ProtocolAnthropic)
	require.NoError(t, err)
	require.NotSame(t, first, second)
	require.NotSame(t, first.state, second.state)
}

type inertStreamDecoder struct{}

func (inertStreamDecoder) Decode([]byte) ([]ir.StreamEvent, []Warning, error) { return nil, nil, nil }
func (inertStreamDecoder) Finalize() ([]ir.StreamEvent, []Warning, error)     { return nil, nil, nil }

type inertStreamEncoder struct{}

func (inertStreamEncoder) Encode(ir.StreamEvent) ([][]byte, []Warning, error) { return nil, nil, nil }
func (inertStreamEncoder) Finalize() ([][]byte, []Warning, error)             { return nil, nil, nil }
