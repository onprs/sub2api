package standard

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPipelineRestoresResponsesExtendedToolsAfterChatFallback(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source:         protocolconv.ProtocolOpenAIResponses,
		IntendedTarget: protocolconv.ProtocolOpenAIChat,
		ClientModel:    "client-model",
		UpstreamModel:  "upstream-model",
	}})
	require.NoError(t, err)
	requestBody := []byte(`{
		"model":"client-model",
		"input":"use tools",
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		],
		"tool_choice":{"type":"tool_search"}
	}`)

	converted, err := pipeline.ConvertRequest(requestBody)
	require.NoError(t, err)
	require.Equal(t, "exec", gjson.GetBytes(converted.Body, "tools.0.function.name").String())
	require.Equal(t, "tool_search", gjson.GetBytes(converted.Body, "tools.1.function.name").String())
	require.Equal(t, "gmail__send", gjson.GetBytes(converted.Body, "tools.2.function.name").String())
	require.Equal(t, "function", gjson.GetBytes(converted.Body, "tool_choice.type").String())
	require.Equal(t, "tool_search", gjson.GetBytes(converted.Body, "tool_choice.function.name").String())

	chatResponse := []byte(`{
		"id":"chatcmpl-tools",
		"object":"chat.completion",
		"model":"upstream-model",
		"choices":[{"index":0,"message":{"role":"assistant","tool_calls":[
			{"id":"custom-1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"dir /b\"}"}},
			{"id":"search-1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"gmail\"}"}},
			{"id":"ns-1","type":"function","function":{"name":"gmail__send","arguments":"{\"to\":\"a@example.com\"}"}}
		]},"finish_reason":"tool_calls"}],
		"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
	}`)
	response, err := pipeline.ConvertResponse(chatResponse, protocolconv.ProtocolOpenAIChat)
	require.NoError(t, err)
	require.Equal(t, "custom_tool_call", gjson.GetBytes(response.Body, "output.0.type").String())
	require.Equal(t, "exec", gjson.GetBytes(response.Body, "output.0.name").String())
	require.Equal(t, "dir /b", gjson.GetBytes(response.Body, "output.0.input").String())
	require.Equal(t, "tool_search_call", gjson.GetBytes(response.Body, "output.1.type").String())
	require.Equal(t, "client", gjson.GetBytes(response.Body, "output.1.execution").String())
	require.True(t, gjson.GetBytes(response.Body, "output.1.arguments").IsObject())
	require.Equal(t, "function_call", gjson.GetBytes(response.Body, "output.2.type").String())
	require.Equal(t, "send", gjson.GetBytes(response.Body, "output.2.name").String())
	require.Equal(t, "gmail", gjson.GetBytes(response.Body, "output.2.namespace").String())
	require.Equal(t, "client-model", gjson.GetBytes(response.Body, "model").String())
}

func TestPipelineToolRoutesAreRequestScopedUnderConcurrency(t *testing.T) {
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
			kind := "custom"
			want := "custom_tool_call"
			if i%2 == 1 {
				kind = "function"
				want = "function_call"
			}
			pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
				Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: protocolconv.ProtocolOpenAIChat,
			}})
			if err != nil {
				errs <- err
				return
			}
			request := []byte(fmt.Sprintf(`{"model":"m","input":"x","tools":[{"type":%q,"name":"shared","parameters":{"type":"object"}}]}`, kind))
			if _, err := pipeline.ConvertRequest(request); err != nil {
				errs <- err
				return
			}
			response, err := pipeline.ConvertResponse([]byte(`{"id":"c","model":"m","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"shared","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`), protocolconv.ProtocolOpenAIChat)
			if err != nil {
				errs <- err
				return
			}
			if got := gjson.GetBytes(response.Body, "output.0.type").String(); got != want {
				errs <- fmt.Errorf("worker %d: got %q want %q", i, got, want)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
