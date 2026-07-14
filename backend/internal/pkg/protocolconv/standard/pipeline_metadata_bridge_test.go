package standard

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/metadata"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func metadataBridgeScope(tenantID, apiKeyID, accountID int64) protocolconv.MetadataScope {
	return protocolconv.MetadataScope{
		TenantID: tenantID, APIKeyID: apiKeyID, GroupID: 30,
		AccountID: accountID, Protocol: protocolconv.ProtocolGoogleGenAI,
	}
}

func newAnthropicGoogleMetadataPipeline(
	t *testing.T,
	store protocolconv.MetadataStore,
	scope protocolconv.MetadataScope,
) *protocolconv.Pipeline {
	t.Helper()
	registry, err := NewRegistry()
	require.NoError(t, err)
	pipeline, err := newAnthropicGoogleMetadataPipelineWithRegistry(registry, store, scope)
	require.NoError(t, err)
	return pipeline
}

func newAnthropicGoogleMetadataPipelineWithRegistry(
	registry *protocolconv.Registry,
	store protocolconv.MetadataStore,
	scope protocolconv.MetadataScope,
) (*protocolconv.Pipeline, error) {
	return protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolAnthropic, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel: "client-model", UpstreamModel: "google-model", Provider: "gemini", AccountID: scope.AccountID,
		},
		Options:       protocolconv.Options{SourceModel: "google-model", LossPolicy: protocolconv.LossWarn},
		MetadataStore: store,
		MetadataScope: scope,
	})
}

func convertAnthropicToolRequest(t *testing.T, pipeline *protocolconv.Pipeline, stream bool) protocolconv.ConvertedRequest {
	t.Helper()
	request := fmt.Sprintf(`{
		"model":"client-model","max_tokens":32,"stream":%t,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"lookup","input":{"q":"x"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"result"}]}
		]
	}`, stream)
	converted, err := pipeline.ConvertRequest([]byte(request))
	require.NoError(t, err)
	return converted
}

func TestPipelineMetadataBridgeRestoresGoogleToolSignatureAcrossRequests(t *testing.T) {
	store := metadata.NewStore(time.Minute, 16)
	scope := metadataBridgeScope(10, 20, 40)

	first := newAnthropicGoogleMetadataPipeline(t, store, scope)
	convertAnthropicToolRequest(t, first, false)
	response, err := first.ConvertResponse([]byte(`{
		"responseId":"google-response","modelVersion":"google-model",
		"candidates":[{"content":{"role":"model","parts":[{
			"functionCall":{"id":"call-1","name":"lookup","args":{"q":"x"}},
			"thoughtSignature":"real-google-signature"
		}]},"finishReason":"STOP"}]
	}`), protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)
	require.Empty(t, gjson.GetBytes(response.Body, "content.0.signature").String(), "Anthropic tool_use cannot expose Google signatures")

	second := newAnthropicGoogleMetadataPipeline(t, store, scope)
	converted := convertAnthropicToolRequest(t, second, false)
	require.Equal(t, "real-google-signature", gjson.GetBytes(converted.Body, "contents.0.parts.0.thoughtSignature").String())
}

func TestPipelineMetadataBridgeRestoresGoogleToolSignatureFromStream(t *testing.T) {
	store := metadata.NewStore(time.Minute, 16)
	scope := metadataBridgeScope(11, 21, 41)
	first := newAnthropicGoogleMetadataPipeline(t, store, scope)
	convertAnthropicToolRequest(t, first, true)
	session, err := first.NewStreamProcessor(protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)

	_, _, err = session.Convert([]byte(`{
		"responseId":"google-stream","modelVersion":"google-model",
		"candidates":[{"content":{"role":"model","parts":[{
			"functionCall":{"id":"call-1","name":"lookup","args":{"q":"x"}},
			"thoughtSignature":"stream-google-signature"
		}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}
	}`))
	require.NoError(t, err)
	_, _, err = session.Finalize()
	require.NoError(t, err)

	second := newAnthropicGoogleMetadataPipeline(t, store, scope)
	converted := convertAnthropicToolRequest(t, second, false)
	require.Equal(t, "stream-google-signature", gjson.GetBytes(converted.Body, "contents.0.parts.0.thoughtSignature").String())
}

func TestPipelineMetadataBridgeDoesNotCrossScopeOrFallbackProtocol(t *testing.T) {
	store := metadata.NewStore(time.Minute, 16)
	baseScope := metadataBridgeScope(12, 22, 42)
	first := newAnthropicGoogleMetadataPipeline(t, store, baseScope)
	convertAnthropicToolRequest(t, first, false)
	_, err := first.ConvertResponse([]byte(`{
		"responseId":"google-response","modelVersion":"google-model",
		"candidates":[{"content":{"role":"model","parts":[{
			"functionCall":{"id":"call-1","name":"lookup","args":{}},
			"thoughtSignature":"scoped-signature"
		}]},"finishReason":"STOP"}]
	}`), protocolconv.ProtocolGoogleGenAI)
	require.NoError(t, err)

	for _, scope := range []protocolconv.MetadataScope{
		metadataBridgeScope(99, 22, 42),
		metadataBridgeScope(12, 99, 42),
		metadataBridgeScope(12, 22, 99),
	} {
		pipeline := newAnthropicGoogleMetadataPipeline(t, store, scope)
		converted := convertAnthropicToolRequest(t, pipeline, false)
		require.Empty(t, gjson.GetBytes(converted.Body, "contents.0.parts.0.thoughtSignature").String())
	}

	fallbackScope := metadataBridgeScope(13, 23, 43)
	fallback := newAnthropicGoogleMetadataPipeline(t, store, fallbackScope)
	convertAnthropicToolRequest(t, fallback, false)
	_, err = fallback.ConvertResponse([]byte(`{
		"id":"msg-fallback","type":"message","role":"assistant","model":"claude-model",
		"content":[{"type":"tool_use","id":"call-1","name":"lookup","input":{}}],
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}
	}`), protocolconv.ProtocolAnthropic)
	require.NoError(t, err)

	next := newAnthropicGoogleMetadataPipeline(t, store, fallbackScope)
	converted := convertAnthropicToolRequest(t, next, false)
	require.Empty(t, gjson.GetBytes(converted.Body, "contents.0.parts.0.thoughtSignature").String())
}

func TestPipelineMetadataBridgeConcurrentScopesRemainIsolated(t *testing.T) {
	store := metadata.NewStore(time.Minute, 256)
	registry, err := NewRegistry()
	require.NoError(t, err)
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			scope := metadataBridgeScope(int64(i+1), int64(i+101), int64(i+201))
			first, err := newAnthropicGoogleMetadataPipelineWithRegistry(registry, store, scope)
			if err != nil {
				errs <- err
				return
			}
			if _, err := first.ConvertRequest([]byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"x"}]}`)); err != nil {
				errs <- err
				return
			}
			signature := fmt.Sprintf("signature-%d", i)
			response := []byte(fmt.Sprintf(`{
				"responseId":"r","modelVersion":"m","candidates":[{"content":{"role":"model","parts":[{
					"functionCall":{"id":"shared-call","name":"lookup","args":{}},"thoughtSignature":%q
				}]},"finishReason":"STOP"}]
			}`, signature))
			if _, err := first.ConvertResponse(response, protocolconv.ProtocolGoogleGenAI); err != nil {
				errs <- err
				return
			}
			next, err := newAnthropicGoogleMetadataPipelineWithRegistry(registry, store, scope)
			if err != nil {
				errs <- err
				return
			}
			converted, err := next.ConvertRequest([]byte(`{
				"model":"m","max_tokens":8,"messages":[{"role":"assistant","content":[{
					"type":"tool_use","id":"shared-call","name":"lookup","input":{}
				}]}]
			}`))
			if err != nil {
				errs <- err
				return
			}
			if got := gjson.GetBytes(converted.Body, "contents.0.parts.0.thoughtSignature").String(); got != signature {
				errs <- fmt.Errorf("worker %d got %q want %q", i, got, signature)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
