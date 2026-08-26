package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestChannelMonitorCommandCodeProviderAdapters 校验 Command Code 监控适配器：
// chat 走 /provider/v1/chat/completions，messages 走 /provider/v1/messages。
func TestChannelMonitorCommandCodeProviderAdapters(t *testing.T) {
	chatAdapter, apiMode, ok := providerAdapterFor(MonitorProviderCommandCode, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, apiMode)
	require.Equal(t, "/provider/v1/chat/completions", chatAdapter.buildPath("deepseek/deepseek-v4-flash"))
	require.Equal(t, "Bearer cc-key", chatAdapter.buildHeaders("cc-key")["Authorization"])
	require.Equal(t, "choices.0.message.content", chatAdapter.textPath)

	messagesAdapter, apiMode, ok := providerAdapterFor(MonitorProviderCommandCode, MonitorAPIModeMessages)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeMessages, apiMode)
	require.Equal(t, "/provider/v1/messages", messagesAdapter.buildPath("claude-sonnet-5"))
	headers := messagesAdapter.buildHeaders("cc-key")
	require.Equal(t, "cc-key", headers["x-api-key"])
	require.Equal(t, monitorAnthropicAPIVersion, headers["anthropic-version"])
	require.Equal(t, "content.0.text", messagesAdapter.textPath)
}

// commandCodeMonitorEchoDoer 从请求体中提取 challenge 的 6 位码并以不同响应格式回显，
// 用于验证 Command Code 监控对不同 content 结构的文本提取。
type commandCodeMonitorEchoDoer struct {
	responseBody func(code string) []byte
}

func (d *commandCodeMonitorEchoDoer) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	// 从 prompt 中提取 6 位检查码（跳过 max_tokens、模型名等数字）。
	code := ""
	const marker = "exact check code:"
	if idx := bytes.Index(body, []byte(marker)); idx >= 0 {
		matches := monitorChallengeNumberRegex.FindAllString(string(body[idx:]), -1)
		if len(matches) > 0 {
			code = matches[0]
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(d.responseBody(code))),
	}, nil
}

func TestChannelMonitorCommandCodeChatExtractsArrayContent(t *testing.T) {
	doer := &commandCodeMonitorEchoDoer{
		responseBody: func(code string) []byte {
			return []byte(`{
				"id": "chatcmpl-cc-test",
				"choices": [{
					"index": 0,
					"message": {"role": "assistant", "content": [{"type": "text", "text": "` + code + `"}]},
					"finish_reason": "stop"
				}]
			}`)
		},
	}
	res := runCheckForModelWithClient(
		context.Background(),
		MonitorProviderCommandCode,
		"https://api.commandcode.ai",
		"cc-key",
		"deepseek/deepseek-v4-flash",
		&CheckOptions{},
		doer,
	)
	require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
}

func TestChannelMonitorCommandCodeChatExtractsStringContent(t *testing.T) {
	doer := &commandCodeMonitorEchoDoer{
		responseBody: func(code string) []byte {
			return []byte(`{
				"id": "chatcmpl-cc-test",
				"choices": [{
					"index": 0,
					"message": {"role": "assistant", "content": "` + code + `"},
					"finish_reason": "stop"
				}]
			}`)
		},
	}
	res := runCheckForModelWithClient(
		context.Background(),
		MonitorProviderCommandCode,
		"https://api.commandcode.ai",
		"cc-key",
		"deepseek/deepseek-v4-flash",
		&CheckOptions{},
		doer,
	)
	require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
}

func TestChannelMonitorCommandCodeMessagesExtractsText(t *testing.T) {
	doer := &commandCodeMonitorEchoDoer{
		responseBody: func(code string) []byte {
			return []byte(`{
				"id": "msg_cc_test",
				"type": "message",
				"role": "assistant",
				"content": [{"type": "text", "text": "` + code + `"}]
			}`)
		},
	}
	res := runCheckForModelWithClient(
		context.Background(),
		MonitorProviderCommandCode,
		"https://api.commandcode.ai",
		"cc-key",
		"claude-sonnet-5",
		&CheckOptions{APIMode: MonitorAPIModeMessages},
		doer,
	)
	require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
}

func TestChannelMonitorCommandCodeReportsUpstreamHTTPError(t *testing.T) {
	errDoer := monitorDoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error": {"message": "invalid model"}}`))),
		}, nil
	})
	res := runCheckForModelWithClient(
		context.Background(),
		MonitorProviderCommandCode,
		"https://api.commandcode.ai",
		"cc-key",
		"deepseek/deepseek-v4-flash",
		&CheckOptions{},
		errDoer,
	)
	require.Equal(t, MonitorStatusError, res.Status)
	require.Contains(t, res.Message, "invalid model")
}

type monitorDoerFunc func(*http.Request) (*http.Response, error)

func (f monitorDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }
