package service

import (
	"net/http"
	"testing"
)

func TestIsUpstreamModelNotFoundError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       bool
	}{
		{
			name:       "404 model not found message",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error":{"message":"model not found"}}`),
			want:       true,
		},
		{
			name:       "404 model_not_found code",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error":{"code":"model_not_found","message":"The requested model was not found"}}`),
			want:       true,
		},
		{
			name:       "404 unknown model message",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error":{"message":"unknown model gpt-5.4"}}`),
			want:       true,
		},
		{
			name:       "404 endpoint not found is not model specific",
			statusCode: http.StatusNotFound,
			body:       []byte(`{"error":{"message":"endpoint not found"}}`),
			want:       false,
		},
		{
			name:       "404 arbitrary body is not model specific",
			statusCode: http.StatusNotFound,
			body:       []byte(`404 page not found`),
			want:       false,
		},
		{
			name:       "400 ChatGPT Codex model unsupported matches",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"Model \"gpt-5.6-sol\" is not supported when using Codex with a ChatGPT account.","type":"invalid_request_error"}}`),
			want:       true,
		},
		{
			name:       "400 model not found does not match without ChatGPT Codex signal",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"model not found"}}`),
			want:       false,
		},
		{
			name:       "400 unsupported parameter does not match",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"Parameter temperature is not supported by this model"}}`),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpstreamModelNotFoundError(tt.statusCode, tt.body); got != tt.want {
				t.Fatalf("isUpstreamModelNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUpstreamModelNotFoundErrorForAccount_OpenCodeGoModelUnsupported(t *testing.T) {
	account := &Account{Platform: PlatformOpenCodeGo}
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       bool
	}{
		{
			name:       "OpenCode Go 模型不受支持",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"type":"error","error":{"type":"ModelError","message":"Model minimax-m2-7 not supported"}}`),
			want:       true,
		},
		{
			name:       "OpenCode Go 未回显模型名",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"type":"error","error":{"type":"ModelError","message":"Model is not supported"}}`),
			want:       true,
		},
		{
			name:       "OpenCode Go 协议格式不受支持",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"type":"error","error":{"type":"ModelError","message":"Model qwen3.7-max is not supported for format oa-compat"}}`),
			want:       true,
		},
		{
			name:       "OpenCode Go 403 模型不受支持",
			statusCode: http.StatusForbidden,
			body:       []byte(`{"type":"error","error":{"type":"ModelError","message":"Model deepseek-v4-flash is not supported"}}`),
			want:       true,
		},
		{
			name:       "普通鉴权失败",
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`),
			want:       false,
		},
		{
			name:       "参数不受支持不是模型能力错误",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"Parameter temperature is not supported by this model"}}`),
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUpstreamModelNotFoundErrorForAccount(account, tt.statusCode, tt.body); got != tt.want {
				t.Fatalf("isUpstreamModelNotFoundErrorForAccount() = %v, want %v", got, tt.want)
			}
		})
	}

	otherPlatform := &Account{Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"type":"ModelError","message":"Model qwen3.7-max not supported"}}`)
	if isUpstreamModelNotFoundErrorForAccount(otherPlatform, http.StatusUnauthorized, body) {
		t.Fatal("OpenCode Go 特有的 401 模型错误不能影响其他平台")
	}
}

func TestAntigravityModelNotFoundKeepsBare404Fallback(t *testing.T) {
	if !isModelNotFoundError(http.StatusNotFound, []byte(`endpoint not found`)) {
		t.Fatal("antigravity model-not-found helper should keep bare 404 fallback")
	}
}
