package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteProtocolErrorUsesSourceRenderer(t *testing.T) {
	tests := []struct {
		name      string
		protocol  protocolconv.Protocol
		status    int
		errorType string
		code      string
		want      string
	}{
		{
			name: "chat", protocol: protocolconv.ProtocolOpenAIChat,
			status: http.StatusBadRequest, errorType: "invalid_request_error", code: "invalid_request_error",
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "responses", protocol: protocolconv.ProtocolOpenAIResponses,
			status: http.StatusBadRequest, errorType: "invalid_request_error", code: "invalid_request_error",
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "anthropic", protocol: protocolconv.ProtocolAnthropic,
			status: http.StatusBadRequest, errorType: "invalid_request_error", code: "invalid_request_error",
			want: `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`,
		},
		{
			name: "google", protocol: protocolconv.ProtocolGoogleGenAI,
			status: http.StatusBadRequest, errorType: "api_error", code: "api_error",
			want: `{"error":{"code":400,"message":"bad input","status":"INVALID_ARGUMENT"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)

			writeProtocolError(c, test.protocol, test.status, test.errorType, test.code, "bad input")

			wantContentType := "application/json"
			if test.protocol == protocolconv.ProtocolAnthropic {
				wantContentType = "application/json; charset=utf-8"
			}
			require.Equal(t, test.status, recorder.Code)
			require.Equal(t, wantContentType, recorder.Header().Get("Content-Type"))
			require.JSONEq(t, test.want, recorder.Body.String())
		})
	}
}

func TestWriteProtocolStreamErrorUsesSourceRenderer(t *testing.T) {
	tests := []struct {
		name     string
		protocol protocolconv.Protocol
		want     string
	}{
		{name: "chat", protocol: protocolconv.ProtocolOpenAIChat, want: "data: {\"error\":{\"code\":\"upstream_error\",\"message\":\"boom\",\"type\":\"upstream_error\"}}\n\n"},
		{name: "anthropic", protocol: protocolconv.ProtocolAnthropic, want: "event: error\ndata: {\"error\":{\"message\":\"boom\",\"type\":\"upstream_error\"},\"type\":\"error\"}\n\n"},
		{name: "google", protocol: protocolconv.ProtocolGoogleGenAI, want: "data: {\"error\":{\"code\":502,\"message\":\"boom\",\"status\":\"INTERNAL\"}}\n\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)

			require.True(t, writeProtocolStreamError(c, test.protocol, http.StatusBadGateway, "upstream_error", "upstream_error", "boom"))

			require.Equal(t, test.want, recorder.Body.String())
		})
	}
}

func TestProductionErrorHelpersUseProtocolRenderer(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*gin.Context)
		want   string
	}{
		{
			name: "gateway chat",
			invoke: func(c *gin.Context) {
				(&GatewayHandler{}).chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "bad input")
			},
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "gateway responses",
			invoke: func(c *gin.Context) {
				(&GatewayHandler{}).responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "bad input")
			},
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "gateway anthropic",
			invoke: func(c *gin.Context) {
				(&GatewayHandler{}).errorResponse(c, http.StatusBadRequest, "invalid_request_error", "bad input")
			},
			want: `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`,
		},
		{
			name: "openai responses",
			invoke: func(c *gin.Context) {
				(&OpenAIGatewayHandler{}).errorResponse(c, http.StatusBadRequest, "invalid_request_error", "bad input")
			},
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "openai anthropic",
			invoke: func(c *gin.Context) {
				(&OpenAIGatewayHandler{}).anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "bad input")
			},
			want: `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`,
		},
		{
			name: "opencode chat",
			invoke: func(c *gin.Context) {
				(&OpenCodeGoGatewayHandler{}).errorResponse(c, http.StatusBadRequest, openCodeGoHandlerErrorChat, "invalid_request_error", "bad input")
			},
			want: `{"error":{"message":"bad input","type":"invalid_request_error","code":"invalid_request_error"}}`,
		},
		{
			name: "opencode anthropic",
			invoke: func(c *gin.Context) {
				(&OpenCodeGoGatewayHandler{}).errorResponse(c, http.StatusBadRequest, openCodeGoHandlerErrorAnthropic, "invalid_request_error", "bad input")
			},
			want: `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`,
		},
		{
			name: "google",
			invoke: func(c *gin.Context) {
				googleError(c, http.StatusBadRequest, "bad input")
			},
			want: `{"error":{"code":400,"message":"bad input","status":"INVALID_ARGUMENT"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

			test.invoke(c)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var body any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.JSONEq(t, test.want, recorder.Body.String())
		})
	}
}
