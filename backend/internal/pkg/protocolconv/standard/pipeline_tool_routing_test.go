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

func TestPipelineStreamRestoresResponsesExtendedToolsAndClientModel(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: protocolconv.ProtocolOpenAIChat,
		ClientModel: "client-model", UpstreamModel: "upstream-model",
	}})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{
		"model":"client-model","input":"use tools","stream":true,
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		]
	}`))
	require.NoError(t, err)
	session, err := pipeline.NewStreamProcessor(protocolconv.ProtocolOpenAIChat)
	require.NoError(t, err)

	chunks := [][]byte{
		[]byte(`{"id":"chat-stream","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"custom-1","type":"function","function":{"name":"exec","arguments":"{\"input\":\"dir"}},{"index":1,"id":"search-1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"gmail\"}"}},{"index":2,"id":"ns-1","type":"function","function":{"name":"gmail__send","arguments":"{\"to\":\"a@example.com\"}"}}]},"finish_reason":null}]}`),
		[]byte(`{"id":"chat-stream","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":" /b\"}"}}]},"finish_reason":null}]}`),
		[]byte(`{"id":"chat-stream","object":"chat.completion.chunk","model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`),
	}
	var payloads [][]byte
	for _, chunk := range chunks {
		converted, _, err := session.Convert(chunk)
		require.NoError(t, err)
		payloads = append(payloads, converted...)
	}
	final, _, err := session.Finalize()
	require.NoError(t, err)
	payloads = append(payloads, final...)

	joined := make([]byte, 0)
	for _, payload := range payloads {
		joined = append(joined, payload...)
		joined = append(joined, '\n')
	}
	wire := string(joined)
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"type":"custom_tool_call"`)
	require.Contains(t, wire, `"type":"response.custom_tool_call_input.done"`)
	require.Contains(t, wire, `"input":"dir /b"`)
	require.Contains(t, wire, `"type":"tool_search_call"`)
	require.Contains(t, wire, `"execution":"client"`)
	require.Contains(t, wire, `"namespace":"gmail"`)
	require.Contains(t, wire, `"name":"send"`)
}

func TestPipelineRestoresResponsesExtendedToolsAfterAnthropicRoute(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: protocolconv.ProtocolAnthropic,
		ClientModel: "client-model", UpstreamModel: "upstream-model",
	}})
	require.NoError(t, err)
	converted, err := pipeline.ConvertRequest([]byte(`{
		"model":"client-model","input":"use tools",
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"tool_search"},
			{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
		],
		"tool_choice":{"type":"tool_search"}
	}`))
	require.NoError(t, err)
	require.Equal(t, "exec", gjson.GetBytes(converted.Body, "tools.0.name").String())
	require.Equal(t, "tool_search", gjson.GetBytes(converted.Body, "tools.1.name").String())
	require.Equal(t, "gmail__send", gjson.GetBytes(converted.Body, "tools.2.name").String())
	require.Equal(t, "tool_search", gjson.GetBytes(converted.Body, "tool_choice.name").String())

	response, err := pipeline.ConvertResponse([]byte(`{
		"id":"msg-tools","type":"message","role":"assistant","model":"upstream-model",
		"content":[
			{"type":"tool_use","id":"custom-1","name":"exec","input":{"input":"dir /b"}},
			{"type":"tool_use","id":"search-1","name":"tool_search","input":{"query":"gmail"}},
			{"type":"tool_use","id":"ns-1","name":"gmail__send","input":{"to":"a@example.com"}}
		],"stop_reason":"tool_use","usage":{"input_tokens":4,"output_tokens":2}
	}`), protocolconv.ProtocolAnthropic)
	require.NoError(t, err)
	require.Equal(t, "custom_tool_call", gjson.GetBytes(response.Body, "output.0.type").String())
	require.Equal(t, "dir /b", gjson.GetBytes(response.Body, "output.0.input").String())
	require.Equal(t, "tool_search_call", gjson.GetBytes(response.Body, "output.1.type").String())
	require.Equal(t, "gmail", gjson.GetBytes(response.Body, "output.2.namespace").String())
	require.Equal(t, "send", gjson.GetBytes(response.Body, "output.2.name").String())
	require.Equal(t, "client-model", gjson.GetBytes(response.Body, "model").String())
}

func TestPipelineStreamRestoresResponsesExtendedToolAfterAnthropicRoute(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: protocolconv.ProtocolAnthropic,
		ClientModel: "client-model", UpstreamModel: "upstream-model",
	}})
	require.NoError(t, err)
	_, err = pipeline.ConvertRequest([]byte(`{"model":"client-model","input":"use tool","stream":true,"tools":[{"type":"custom","name":"exec"}]}`))
	require.NoError(t, err)
	session, err := pipeline.NewStreamProcessor(protocolconv.ProtocolAnthropic)
	require.NoError(t, err)
	payloads := [][]byte{
		[]byte(`{"type":"message_start","message":{"id":"msg-stream","type":"message","role":"assistant","model":"upstream-model","content":[],"usage":{"input_tokens":2}}}`),
		[]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"custom-1","name":"exec","input":{}}}`),
		[]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"dir /b\"}"}}`),
		[]byte(`{"type":"content_block_stop","index":0}`),
		[]byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":1}}`),
		[]byte(`{"type":"message_stop"}`),
	}
	var converted [][]byte
	for _, payload := range payloads {
		out, _, err := session.Convert(payload)
		require.NoError(t, err)
		converted = append(converted, out...)
	}
	final, _, err := session.Finalize()
	require.NoError(t, err)
	converted = append(converted, final...)
	var wire string
	for _, payload := range converted {
		wire += string(payload) + "\n"
	}
	require.Contains(t, wire, `"model":"client-model"`)
	require.Contains(t, wire, `"type":"custom_tool_call"`)
	require.Contains(t, wire, `"type":"response.custom_tool_call_input.done"`)
	require.Contains(t, wire, `"input":"dir /b"`)
}

func TestPipelineRejectsAmbiguousFlattenedToolRoute(t *testing.T) {
	registry, err := NewRegistry()
	require.NoError(t, err)
	for _, target := range []protocolconv.Protocol{protocolconv.ProtocolOpenAIChat, protocolconv.ProtocolAnthropic} {
		pipeline, err := protocolconv.NewPipeline(registry, protocolconv.PipelineConfig{Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: target,
		}})
		require.NoError(t, err)
		_, err = pipeline.ConvertRequest([]byte(`{
			"model":"client-model","input":"use tools",
			"tools":[
				{"type":"function","name":"gmail__send","parameters":{"type":"object"}},
				{"type":"namespace","name":"gmail","tools":[{"type":"function","name":"send","parameters":{"type":"object"}}]}
			]
		}`))
		require.ErrorContains(t, err, `target tool name "gmail__send" has ambiguous source routes`)
	}
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
