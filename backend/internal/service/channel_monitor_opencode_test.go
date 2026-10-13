//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// opencodeMonitorEchoDoer 从探活请求中提取 6 位检查码并按指定响应格式回显，
// 同时记录出站请求 URL，用于校验 OpenCode Go 三种 api_mode 的路径拼接。
type opencodeMonitorEchoDoer struct {
	responseBody func(code string) []byte
	capturedURL  string
}

func (d *opencodeMonitorEchoDoer) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	d.capturedURL = req.URL.String()

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

func opencodeChatResponse(code string) []byte {
	return []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"` + code + `"},"finish_reason":"stop"}]}`)
}

// OpenCode Go 支持探活：provider 表与 check_mode 组合矩阵必须放行三种模式。
func TestChannelMonitorOpenCodeGoCheckModeMatrix(t *testing.T) {
	require.True(t, providerSupportsProbe(MonitorProviderOpenCodeGo))
	for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuota, MonitorCheckModeQuotaProbe} {
		require.NoError(t, validateCheckMode(MonitorProviderOpenCodeGo, mode), mode)
	}
	// OpenCode Go 按模型分流到 Chat / Responses / Messages 三种上游协议。
	require.NoError(t, validateProvider(MonitorProviderOpenCodeGo))
	for _, apiMode := range []string{
		MonitorAPIModeChatCompletions, MonitorAPIModeResponses, MonitorAPIModeMessages,
	} {
		require.NoError(t, validateAPIMode(MonitorProviderOpenCodeGo, apiMode), apiMode)
	}
}

// 创建参数：OpenCode Go 探活模式走外站 endpoint + key，配额模式走关联账号。
func TestChannelMonitorOpenCodeGoValidateCreateParams(t *testing.T) {
	accountID := int64(9)
	base := ChannelMonitorCreateParams{
		Provider:        MonitorProviderOpenCodeGo,
		Endpoint:        "https://opencode.ai/zen/go/v1",
		APIKey:          "oc-key",
		PrimaryModel:    "glm-5.3",
		IntervalSeconds: 60,
	}

	probe := base
	probe.CheckMode = MonitorCheckModeProbe
	require.NoError(t, validateCreateParams(probe))

	quotaProbe := base
	quotaProbe.CheckMode = MonitorCheckModeQuotaProbe
	quotaProbe.AccountID = &accountID
	require.NoError(t, validateCreateParams(quotaProbe))

	quota := ChannelMonitorCreateParams{
		Provider:        MonitorProviderOpenCodeGo,
		CheckMode:       MonitorCheckModeQuota,
		AccountID:       &accountID,
		IntervalSeconds: 60,
	}
	require.NoError(t, validateCreateParams(quota))
}

// OpenCode Go 三种 api_mode 的适配器形态：路径、鉴权头与文本抽取路径。
func TestChannelMonitorOpenCodeGoProviderAdapters(t *testing.T) {
	chatAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeChatCompletions, apiMode)
	require.Equal(t, "/v1/chat/completions", chatAdapter.buildPath("glm-5.3"))
	require.Equal(t, "Bearer oc-key", chatAdapter.buildHeaders("oc-key")["Authorization"])
	require.Equal(t, "choices.0.message.content", chatAdapter.textPath)

	responsesAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeResponses)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeResponses, apiMode)
	require.Equal(t, "/v1/responses", responsesAdapter.buildPath("kimi-k2"))
	require.Equal(t, "Bearer oc-key", responsesAdapter.buildHeaders("oc-key")["Authorization"])

	messagesAdapter, apiMode, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeMessages)
	require.True(t, ok)
	require.Equal(t, MonitorAPIModeMessages, apiMode)
	require.Equal(t, "/v1/messages", messagesAdapter.buildPath("claude-sonnet-5"))
	headers := messagesAdapter.buildHeaders("oc-key")
	require.Equal(t, "Bearer oc-key", headers["Authorization"])
	require.Equal(t, "oc-key", headers["x-api-key"])
	require.Equal(t, monitorAnthropicAPIVersion, headers["anthropic-version"])

	// 探活请求体保留 OpenCode 转换链路所需的系统提示与输出预算。
	body, err := chatAdapter.buildBody("glm-5.3", "prompt")
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "glm-5.3", payload["model"])
	require.Equal(t, float64(monitorOpenCodeGoChallengeMaxTokens), payload["max_tokens"])
	require.Equal(t, false, payload["stream"])

	resBody, err := responsesAdapter.buildBody("kimi-k2", "prompt")
	require.NoError(t, err)
	var resPayload map[string]any
	require.NoError(t, json.Unmarshal(resBody, &resPayload))
	require.Equal(t, "prompt", resPayload["input"])
	require.NotEmpty(t, resPayload["instructions"])
}

// 外站探活：endpoint 已含 /v1 时不重复追加，三种 api_mode 都能拿到 operational。
func TestChannelMonitorOpenCodeGoProbeModes(t *testing.T) {
	cases := []struct {
		name     string
		apiMode  string
		endpoint string
		wantURL  string
		response func(code string) []byte
	}{
		{
			name:     "chat completions",
			apiMode:  MonitorAPIModeChatCompletions,
			endpoint: "https://opencode.ai/zen/go/v1",
			wantURL:  "https://opencode.ai/zen/go/v1/chat/completions",
			response: opencodeChatResponse,
		},
		{
			name:     "chat completions without v1 suffix",
			apiMode:  MonitorAPIModeChatCompletions,
			endpoint: "https://opencode.ai/zen/go",
			wantURL:  "https://opencode.ai/zen/go/v1/chat/completions",
			response: opencodeChatResponse,
		},
		{
			name:     "chat content array",
			apiMode:  MonitorAPIModeChatCompletions,
			endpoint: "https://opencode.ai/zen/go/v1",
			wantURL:  "https://opencode.ai/zen/go/v1/chat/completions",
			response: func(code string) []byte {
				return []byte(`{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"` + code + `"}]}}]}`)
			},
		},
		{
			name:     "responses",
			apiMode:  MonitorAPIModeResponses,
			endpoint: "https://opencode.ai/zen/go/v1",
			wantURL:  "https://opencode.ai/zen/go/v1/responses",
			response: func(code string) []byte {
				return []byte(`{"status":"completed","output":[{"type":"reasoning","content":[]},{"type":"message","content":[{"type":"output_text","text":"` + code + `"}]}]}`)
			},
		},
		{
			name:     "messages",
			apiMode:  MonitorAPIModeMessages,
			endpoint: "https://opencode.ai/zen/go",
			wantURL:  "https://opencode.ai/zen/go/v1/messages",
			response: func(code string) []byte {
				return []byte(`{"content":[{"type":"thinking","thinking":"..."},{"type":"text","text":"` + code + `"}]}`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doer := &opencodeMonitorEchoDoer{responseBody: tc.response}
			res := runCheckForModelWithClient(
				context.Background(),
				MonitorProviderOpenCodeGo,
				tc.endpoint,
				"oc-key",
				"glm-5.3",
				&CheckOptions{APIMode: tc.apiMode},
				doer,
			)
			require.Equal(t, tc.wantURL, doer.capturedURL)
			require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
		})
	}
}

// Responses 的 2xx failed 信封要给出上游错误原因，而不是「文本为空」。
func TestChannelMonitorOpenCodeGoResponsesFailedEnvelope(t *testing.T) {
	doer := &opencodeMonitorEchoDoer{
		responseBody: func(_ string) []byte {
			return []byte(`{"status":"failed","error":{"message":"No available accounts"}}`)
		},
	}
	res := runCheckForModelWithClient(
		context.Background(),
		MonitorProviderOpenCodeGo,
		"https://opencode.ai/zen/go/v1",
		"oc-key",
		"kimi-k2",
		&CheckOptions{APIMode: MonitorAPIModeResponses},
		doer,
	)
	require.Equal(t, MonitorStatusFailed, res.Status)
	require.Contains(t, res.Message, "OpenCode Go Responses failed: No available accounts")
}

// merge 模式下 model / messages / instructions / input 由探活流程掌控，不接受覆盖。
func TestChannelMonitorOpenCodeGoBodyMergeDenyList(t *testing.T) {
	chatAdapter, _, ok := providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeChatCompletions)
	require.True(t, ok)
	merged, err := buildRequestBody(chatAdapter, MonitorProviderOpenCodeGo, MonitorAPIModeChatCompletions, "glm-5.3", "prompt", &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride:     map[string]any{"model": "hijacked", "messages": "nope", "temperature": 1},
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(merged, &payload))
	require.Equal(t, "glm-5.3", payload["model"])
	require.IsType(t, []any{}, payload["messages"])
	require.Equal(t, float64(1), payload["temperature"])

	_, _, ok = providerAdapterFor(MonitorProviderOpenCodeGo, MonitorAPIModeResponses)
	require.True(t, ok)
	require.Equal(t,
		map[string]bool{"model": true, "instructions": true, "input": true, "stream": true},
		bodyMergeKeyDenyList[MonitorProviderOpenCodeGo+":"+MonitorAPIModeResponses],
	)
	require.NoError(t, validateReplaceRequestBody(MonitorProviderOpenCodeGo, MonitorAPIModeResponses, map[string]any{
		"instructions": "check", "input": "1 + 1",
	}))
	require.Error(t, validateReplaceRequestBody(MonitorProviderOpenCodeGo, MonitorAPIModeResponses, map[string]any{}))
	require.NoError(t, validateReplaceRequestBody(MonitorProviderOpenCodeGo, MonitorAPIModeMessages, map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}))
	require.Error(t, validateReplaceRequestBody(MonitorProviderOpenCodeGo, MonitorAPIModeMessages, map[string]any{}))
}

// 本站分组监控：OpenCode Go 分组走 /v1 网关路由，三种 api_mode 各自的入站路径与出站一致。
func TestChannelMonitorOpenCodeGoLocalGatewayPaths(t *testing.T) {
	groupID := int64(41)
	group := activeMonitorTargetGroup(groupID, "OpenCode", PlatformOpenCodeGo)
	svc := NewChannelMonitorService(&channelMonitorTargetRepoStub{}, &channelMonitorTargetEncryptorStub{})

	var seenPaths []string
	svc.SetLocalRequestHandler(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		seenPaths = append(seenPaths, request.URL.Path)
		require.Equal(t, "sub2api.internal", request.Host)
		apiKey, ok := InternalChannelMonitorAPIKey(request.Context())
		require.True(t, ok)
		require.Equal(t, groupID, *apiKey.GroupID)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		code := ""
		switch v := payload["messages"].(type) {
		case []any:
			for _, item := range v {
				msg, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if content, _ := msg["content"].(string); content != "" {
					if match := regexp.MustCompile(`\d{6}`).FindString(content); match != "" {
						code = match
					}
				}
			}
		}
		if code == "" {
			if input, _ := payload["input"].(string); input != "" {
				code = regexp.MustCompile(`\d{6}`).FindString(input)
			}
		}
		require.NotEmpty(t, code, "probe request must carry the check code")

		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]any{{"type": "text", "text": code}},
			})
		case "/v1/responses":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "completed",
				"output": []map[string]any{{"type": "message", "content": []map[string]any{{"type": "output_text", "text": code}}}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": code}}},
			})
		}
	}))

	for _, tc := range []struct {
		apiMode string
		want    string
	}{
		{MonitorAPIModeChatCompletions, "/v1/chat/completions"},
		{MonitorAPIModeResponses, "/v1/responses"},
		{MonitorAPIModeMessages, "/v1/messages"},
	} {
		monitor := &ChannelMonitor{
			ID:           51,
			Provider:     MonitorProviderOpenCodeGo,
			APIMode:      tc.apiMode,
			TargetType:   ChannelMonitorTargetLocal,
			GroupID:      &groupID,
			Group:        group,
			PrimaryModel: "glm-5.3",
			CheckMode:    MonitorCheckModeProbe,
		}
		res := svc.runLocalCheckForModel(context.Background(), monitor, monitor.PrimaryModel, &CheckOptions{APIMode: tc.apiMode})
		require.Equal(t, MonitorStatusOperational, res.Status, res.Message)
		require.Equal(t, tc.want, seenPaths[len(seenPaths)-1])
	}
}

// provider 表、探活能力表与探活适配器表必须一致：可探活的 provider 都要有 adapter，
// 有 adapter 的 provider 都要登记为可探活，剩下的只允许配额模式。
func TestChannelMonitorProbeTablesConsistency(t *testing.T) {
	for provider := range probeCapableProviders {
		require.Contains(t, monitorProviders, provider, provider)
		adapter, _, ok := providerAdapterFor(provider, MonitorAPIModeChatCompletions)
		require.True(t, ok, provider)
		require.NotNil(t, adapter.buildPath, provider)
		require.NotNil(t, adapter.buildHeaders, provider)
	}
	for provider := range providerAdapters {
		require.True(t, providerSupportsProbe(provider), provider)
	}
	for provider := range monitorProviders {
		if providerSupportsProbe(provider) {
			continue
		}
		// 不支持探活的 provider 只允许配额模式，错误码要与「模式取值非法」区分。
		require.ErrorIs(t, validateCheckMode(provider, MonitorCheckModeProbe), ErrChannelMonitorProbeUnsupported, provider)
		require.NoError(t, validateCheckMode(provider, MonitorCheckModeQuota), provider)
		_, _, ok := providerAdapterFor(provider, MonitorAPIModeChatCompletions)
		require.False(t, ok, provider)
	}
}
