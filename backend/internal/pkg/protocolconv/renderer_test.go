package protocolconv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRendererFramesAllStandardProtocols(t *testing.T) {
	tests := []struct {
		protocol Protocol
		body     string
		want     string
		terminal string
	}{
		{ProtocolOpenAIChat, `{"id":"chat-1","choices":[]}`, "data: {\"id\":\"chat-1\",\"choices\":[]}\n\n", "data: [DONE]\n\n"},
		{ProtocolOpenAIResponses, `{"type":"response.created","response":{}}`, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{}}\n\n", "data: [DONE]\n\n"},
		{ProtocolAnthropic, `{"type":"message_start","message":{}}`, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\n", ""},
		{ProtocolGoogleGenAI, `{"candidates":[]}`, "data: {\"candidates\":[]}\n\n", ""},
	}

	for _, test := range tests {
		t.Run(test.protocol.String(), func(t *testing.T) {
			renderer, err := NewRenderer(test.protocol)
			require.NoError(t, err)
			got, err := renderer.FrameStreamEvent([]byte(test.body))
			require.NoError(t, err)
			require.Equal(t, test.want, string(got))
			require.Equal(t, ":\n\n", string(renderer.StreamKeepalive()))
			require.Equal(t, test.terminal, string(renderer.StreamTerminal()))
		})
	}
}

func TestRendererFramesMultilineJSONAsSSEDataFields(t *testing.T) {
	tests := []struct {
		protocol Protocol
		body     string
		want     string
	}{
		{ProtocolOpenAIChat, "{\n\"id\":\"chat-1\"\n}", "data: {\ndata: \"id\":\"chat-1\"\ndata: }\n\n"},
		{ProtocolOpenAIResponses, "{\n\"type\":\"response.created\"\n}", "event: response.created\ndata: {\ndata: \"type\":\"response.created\"\ndata: }\n\n"},
		{ProtocolAnthropic, "{\n\"type\":\"message_start\"\n}", "event: message_start\ndata: {\ndata: \"type\":\"message_start\"\ndata: }\n\n"},
		{ProtocolGoogleGenAI, "{\n\"candidates\":[]\n}", "data: {\ndata: \"candidates\":[]\ndata: }\n\n"},
	}

	for _, test := range tests {
		t.Run(test.protocol.String(), func(t *testing.T) {
			renderer, err := NewRenderer(test.protocol)
			require.NoError(t, err)
			got, err := renderer.FrameStreamEvent([]byte(test.body))
			require.NoError(t, err)
			require.Equal(t, test.want, string(got))
		})
	}
}

func TestRendererRejectsInvalidOrUntypedStreamEvents(t *testing.T) {
	responses, err := NewRenderer(ProtocolOpenAIResponses)
	require.NoError(t, err)
	_, err = responses.FrameStreamEvent([]byte(`{"response":{}}`))
	require.ErrorContains(t, err, "type is required")

	chat, err := NewRenderer(ProtocolOpenAIChat)
	require.NoError(t, err)
	_, err = chat.FrameStreamEvent([]byte(`{"broken":`))
	var conversionErr *Error
	require.ErrorAs(t, err, &conversionErr)
	require.Equal(t, ErrorInvalidJSON, conversionErr.Code)
}

func TestRendererBuildsSourceSpecificErrorEnvelopes(t *testing.T) {
	tests := []struct {
		protocol Protocol
		status   int
		want     string
	}{
		{ProtocolOpenAIChat, http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"bad input","type":"invalid_request_error"}}`},
		{ProtocolOpenAIResponses, http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"bad input","type":"invalid_request_error"}}`},
		{ProtocolAnthropic, http.StatusBadRequest, `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_request","message":"bad input"}}`},
		{ProtocolGoogleGenAI, http.StatusBadRequest, `{"error":{"code":400,"message":"bad input","status":"INVALID_ARGUMENT"}}`},
	}
	for _, test := range tests {
		t.Run(test.protocol.String(), func(t *testing.T) {
			renderer, err := NewRenderer(test.protocol)
			require.NoError(t, err)
			body, err := renderer.ErrorBody(test.status, "invalid_request_error", "invalid_request", "bad input")
			require.NoError(t, err)
			require.JSONEq(t, test.want, string(body))
		})
	}
}

func TestRendererGoogleErrorStatusMappingPreservesUnknownClientErrors(t *testing.T) {
	renderer, err := NewRenderer(ProtocolGoogleGenAI)
	require.NoError(t, err)

	body, err := renderer.ErrorBody(http.StatusPaymentRequired, "api_error", "api_error", "payment required")
	require.NoError(t, err)
	require.JSONEq(t, `{"error":{"code":402,"message":"payment required","status":"UNKNOWN"}}`, string(body))

	body, err = renderer.ErrorBody(http.StatusBadGateway, "api_error", "api_error", "upstream failed")
	require.NoError(t, err)
	require.JSONEq(t, `{"error":{"code":502,"message":"upstream failed","status":"INTERNAL"}}`, string(body))
}

func TestRendererWritesJSONAndSSEHeadersWithoutHopByHopHeaders(t *testing.T) {
	renderer, err := NewRenderer(ProtocolAnthropic)
	require.NoError(t, err)
	headers := http.Header{
		"X-Request-Id":      []string{"req-1"},
		"Content-Length":    []string{"999"},
		"Connection":        []string{"close"},
		"Content-Type":      []string{"wrong/type"},
		"Transfer-Encoding": []string{"chunked"},
	}

	jsonRecorder := httptest.NewRecorder()
	require.NoError(t, renderer.RenderJSON(jsonRecorder, http.StatusCreated, headers, []byte(`{"ok":true}`)))
	require.Equal(t, http.StatusCreated, jsonRecorder.Code)
	require.Equal(t, "application/json; charset=utf-8", jsonRecorder.Header().Get("Content-Type"))
	require.Equal(t, "req-1", jsonRecorder.Header().Get("X-Request-Id"))
	require.Empty(t, jsonRecorder.Header().Get("Connection"))

	streamRecorder := httptest.NewRecorder()
	require.NoError(t, renderer.WriteStreamHeaders(streamRecorder, http.StatusOK, headers))
	require.Equal(t, "text/event-stream", streamRecorder.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", streamRecorder.Header().Get("Cache-Control"))
	require.Equal(t, "keep-alive", streamRecorder.Header().Get("Connection"))
}
