package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/antigravityadapter"
	protocolmetadata "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/metadata"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAntigravityForwardAsChatCompletionsUsesVendorWire(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-chat","modelVersion":"claude-sonnet-4-6","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2,"totalTokenCount":9}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	var upstreamRequest *http.Request
	upstream := &queuedHTTPUpstreamStub{
		responses: []*http.Response{{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-ag-chat"}},
			Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		}},
		onCall: func(req *http.Request, _ *queuedHTTPUpstreamStub) { upstreamRequest = req.Clone(req.Context()) },
	}
	svc := newAntigravityStandardTestService(upstream)
	account := antigravityStandardTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "claude-sonnet-4-6", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, protocolconv.ProtocolGoogleGenAI, result.ActualProtocol)
	require.Equal(t, "claude-sonnet-4-6", result.Model)
	require.Equal(t, "claude-sonnet-4-6", result.UpstreamModel)
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)

	require.NotNil(t, upstreamRequest)
	require.Equal(t, "daily-cloudcode-pa.googleapis.com", upstreamRequest.URL.Host)
	require.Equal(t, "/v1internal:streamGenerateContent", upstreamRequest.URL.Path)
	require.Equal(t, "alt=sse", upstreamRequest.URL.RawQuery)
	require.Equal(t, "Bearer antigravity-token", upstreamRequest.Header.Get("Authorization"))
	require.NotEqual(t, "api.anthropic.com", upstreamRequest.URL.Host)
	require.Equal(t, "project-1", gjson.GetBytes(upstream.requestBodies[0], "project").String())
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(upstream.requestBodies[0], "model").String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.requestBodies[0], "request.contents.0.parts.0.text").String())
	require.NotEmpty(t, gjson.GetBytes(upstream.requestBodies[0], "request.sessionId").String())
	require.Contains(t, string(upstream.requestBodies[0]), "Claude Sonnet 4.6")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "claude-sonnet-4-6", gjson.GetBytes(recorder.Body.Bytes(), "model").String())
	require.Equal(t, "hello", gjson.GetBytes(recorder.Body.Bytes(), "choices.0.message.content").String())
	require.Equal(t, int64(7), gjson.GetBytes(recorder.Body.Bytes(), "usage.prompt_tokens").Int())
}

func TestAntigravityForwardAsChatCompletionsRetryBuildsFreshAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	successBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-retry","modelVersion":"claude-sonnet-4-6","candidates":[{"content":{"role":"model","parts":[{"text":"retry-ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}}`,
		"", "data: [DONE]", "",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{
		{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"retry"}}`))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(successBody))},
	}}
	svc := newAntigravityStandardTestService(upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, antigravityStandardTestAccount(), body, "claude-sonnet-4-6", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "retry-ok", gjson.GetBytes(recorder.Body.Bytes(), "choices.0.message.content").String())
	require.Len(t, upstream.requestBodies, 2)
	firstID := gjson.GetBytes(upstream.requestBodies[0], "requestId").String()
	secondID := gjson.GetBytes(upstream.requestBodies[1], "requestId").String()
	require.NotEmpty(t, firstID)
	require.NotEmpty(t, secondID)
	require.NotEqual(t, firstID, secondID)
}

func TestAntigravityForwardAsResponsesStreamsToolCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-response","modelVersion":"gemini-upstream","candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call_ag","name":"lookup","args":{"q":"x"}},"thoughtSignature":"sig-ag"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":1,"totalTokenCount":6}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-ag-responses"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	account := antigravityStandardTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body, "claude-sonnet-4-6", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, protocolconv.ProtocolGoogleGenAI, result.ActualProtocol)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)

	wire := recorder.Body.String()
	require.Contains(t, wire, "event: response.created\n")
	require.Contains(t, wire, `"model":"claude-sonnet-4-6"`)
	require.Contains(t, wire, "event: response.output_item.added\n")
	require.Contains(t, wire, `"call_id":"call_ag"`)
	require.Contains(t, wire, `"name":"lookup"`)
	require.Contains(t, wire, `"arguments":"{\"q\":\"x\"}"`)
	require.Contains(t, wire, "event: response.completed\n")
	require.True(t, strings.HasSuffix(wire, "data: [DONE]\n\n"))
}

func TestAntigravityForwardAsResponsesStreamsCompleteTextLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-response-text","modelVersion":"gemini-pro-agent","candidates":[{"content":{"role":"model","parts":[{"text":"hello "}]}}]}}`,
		"",
		`data: {"response":{"responseId":"ag-response-text","modelVersion":"gemini-pro-agent","candidates":[{"content":{"role":"model","parts":[{"text":"world"}]}}]}}`,
		"",
		`data: {"response":{"responseId":"ag-response-text","modelVersion":"gemini-pro-agent","candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2,"totalTokenCount":10}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-ag-response-text"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gemini-3.1-pro-high","stream":true,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, antigravityStandardTestAccount(), body, "gemini-3.1-pro-high", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, protocolconv.ProtocolGoogleGenAI, result.ActualProtocol)
	require.Equal(t, 8, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, "gemini-pro-agent", gjson.GetBytes(upstream.requestBodies[0], "model").String())
	require.Equal(t, "hello", gjson.GetBytes(upstream.requestBodies[0], "request.contents.0.parts.0.text").String())

	wire := recorder.Body.String()
	orderedEvents := []string{
		"event: response.created\n",
		"event: response.output_item.added\n",
		"event: response.content_part.added\n",
		"event: response.output_text.delta\n",
		"event: response.output_text.done\n",
		"event: response.content_part.done\n",
		"event: response.output_item.done\n",
		"event: response.completed\n",
	}
	previous := -1
	for _, event := range orderedEvents {
		index := strings.Index(wire, event)
		require.Greater(t, index, previous, "event order for %s", event)
		previous = index
	}
	require.Contains(t, wire, `"delta":"hello "`)
	require.Contains(t, wire, `"delta":"world"`)
	require.Contains(t, wire, `"text":"hello world"`)
	require.Contains(t, wire, `"model":"gemini-3.1-pro-high"`)
	require.True(t, strings.HasSuffix(wire, "data: [DONE]\n\n"))
}

func TestAntigravityForwardAsResponsesBuffersText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-response-buffered","modelVersion":"gemini-pro-agent","candidates":[{"content":{"role":"model","parts":[{"text":"buffered text"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}}`,
		"", "data: [DONE]", "",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gemini-3.1-pro-high","stream":false,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, antigravityStandardTestAccount(), body, "gemini-3.1-pro-high", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "buffered text", gjson.GetBytes(recorder.Body.Bytes(), "output.0.content.0.text").String())
	require.Equal(t, "gemini-3.1-pro-high", gjson.GetBytes(recorder.Body.Bytes(), "model").String())
	require.Equal(t, int64(4), gjson.GetBytes(recorder.Body.Bytes(), "usage.input_tokens").Int())
	require.Equal(t, int64(2), gjson.GetBytes(recorder.Body.Bytes(), "usage.output_tokens").Int())
}

func TestAntigravityForwardAsResponsesDrainsUsageAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := strings.Join([]string{
		`data: {"response":{"responseId":"ag-disconnect","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}}`,
		"",
		`data: {"response":{"responseId":"ag-disconnect","candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13}}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
	body := []byte(`{"model":"claude-sonnet-4-6","stream":true,"input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, antigravityStandardTestAccount(), body, "claude-sonnet-4-6", false)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

func TestAntigravityForwardAsResponsesUsesSourceErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad vendor request"}}`)),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	account := antigravityStandardTestAccount()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-6","input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body, "claude-sonnet-4-6", false)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "invalid_request_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Equal(t, "invalid_request_error", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	require.NotEmpty(t, gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
}

func TestAntigravityForwardAsChatCompletionsRejectsMalformedSuccessBeforeCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: not-json\n\n")),
	}}}
	svc := newAntigravityStandardTestService(upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, antigravityStandardTestAccount(), body, "claude-sonnet-4-6", false)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.Equal(t, 0, recorder.Body.Len())
}

func TestAntigravityStandardMetadataBridgeRestoresSignatureWithinScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstResponse := strings.Join([]string{
		`data: {"response":{"responseId":"ag-tool","candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"x"}},"thoughtSignature":"real-ag-signature"}]},"finishReason":"STOP"}]}}`,
		"", "data: [DONE]", "",
	}, "\n")
	secondResponse := strings.Join([]string{
		`data: {"response":{"responseId":"ag-final","candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}]}}`,
		"", "data: [DONE]", "",
	}, "\n")
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(firstResponse))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(secondResponse))},
	}}
	svc := newAntigravityStandardTestService(upstream)
	account := antigravityStandardTestAccount()
	ctx := WithProtocolMetadataIdentity(context.Background(), 10, 20, 30)

	firstBody := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"call lookup"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
	firstRecorder := httptest.NewRecorder()
	firstContext, _ := gin.CreateTestContext(firstRecorder)
	firstContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(firstBody))
	_, err := svc.ForwardAsChatCompletions(ctx, firstContext, account, firstBody, "claude-sonnet-4-6", false)
	require.NoError(t, err)

	secondBody := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},{"role":"tool","tool_call_id":"call-1","content":"found"}]}`)
	secondRecorder := httptest.NewRecorder()
	secondContext, _ := gin.CreateTestContext(secondRecorder)
	secondContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(secondBody))
	_, err = svc.ForwardAsChatCompletions(ctx, secondContext, account, secondBody, "claude-sonnet-4-6", false)
	require.NoError(t, err)
	require.Contains(t, string(upstream.requestBodies[1]), "real-ag-signature")
	require.Equal(t, "real-ag-signature", gjson.GetBytes(upstream.requestBodies[1], "request.contents.0.parts.0.thoughtSignature").String())
}

func TestAntigravityStandardMetadataBridgeDoesNotCrossAccountScope(t *testing.T) {
	svc := &AntigravityGatewayService{providerMetadataStore: protocolmetadata.NewStore(time.Minute, 32)}
	baseCtx := WithProtocolMetadataIdentity(context.Background(), 11, 21, 31)
	baseAccount := &Account{ID: 41, Platform: PlatformAntigravity}
	config := protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		AccountID: baseAccount.ID,
	}}
	svc.configureStandardMetadataBridge(baseCtx, baseAccount, &config)
	require.Same(t, svc.providerMetadataStore, config.MetadataStore)
	require.Equal(t, protocolconv.MetadataScope{
		TenantID: 11, APIKeyID: 21, GroupID: 31, AccountID: 41, Protocol: protocolconv.ProtocolGoogleGenAI,
	}, config.MetadataScope)

	otherConfig := protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		AccountID: 99,
	}}
	svc.configureStandardMetadataBridge(baseCtx, &Account{ID: 99, Platform: PlatformAntigravity}, &otherConfig)
	require.NotEqual(t, config.MetadataScope, otherConfig.MetadataScope)

	missingConfig := protocolconv.PipelineConfig{Route: protocolconv.Route{
		Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
		AccountID: baseAccount.ID,
	}}
	svc.configureStandardMetadataBridge(context.Background(), baseAccount, &missingConfig)
	require.Nil(t, missingConfig.MetadataStore)
	require.Equal(t, protocolconv.MetadataScope{}, missingConfig.MetadataScope)
}

func newAntigravityStandardTestService(upstream HTTPUpstream) *AntigravityGatewayService {
	return &AntigravityGatewayService{
		settingService:        NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}),
		tokenProvider:         &AntigravityTokenProvider{},
		httpUpstream:          upstream,
		providerMetadataStore: protocolmetadata.NewStore(time.Minute, 32),
	}
}

func antigravityStandardTestAccount() *Account {
	return &Account{
		ID: 901, Name: "ag-standard", Platform: PlatformAntigravity, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "antigravity-token",
			"project_id":   "project-1",
			"model_mapping": map[string]any{
				"claude-sonnet-4-6":   "claude-sonnet-4-6",
				"gemini-3.1-pro-high": "gemini-pro-agent",
			},
		},
	}
}

func TestAntigravityRetryLoopUsesFreshPipelinePerHTTPAttempt(t *testing.T) {
	upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{
		{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"retry"}}`))},
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))},
	}}
	account := antigravityStandardTestAccount()
	var pipelines []*protocolconv.Pipeline
	factory := func() (*antigravityRetryAttempt, error) {
		pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, protocolconv.PipelineConfig{
			Route: protocolconv.Route{
				Source: protocolconv.ProtocolOpenAIChat, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
				ClientModel: "claude-sonnet-4-6", UpstreamModel: "claude-sonnet-4-6",
				Provider: PlatformAntigravity, AccountID: account.ID,
			},
			Options: protocolconv.Options{SourceModel: "claude-sonnet-4-6", LossPolicy: protocolconv.LossError},
		})
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, pipeline)
		body := []byte(fmt.Sprintf(`{"requestId":"attempt-%d"}`, len(pipelines)))
		return &antigravityRetryAttempt{body: body, pipeline: pipeline}, nil
	}

	result, err := (&AntigravityGatewayService{httpUpstream: upstream}).antigravityRetryLoop(antigravityRetryLoopParams{
		ctx: context.Background(), prefix: "[test]", account: account,
		accessToken: "token", action: "streamGenerateContent", bodyFactory: factory,
		httpUpstream: upstream, requestedModel: "claude-sonnet-4-6",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, pipelines, 2)
	require.Same(t, pipelines[1], result.pipeline)
	require.Len(t, upstream.requestBodies, 2)
	require.Equal(t, "attempt-1", gjson.GetBytes(upstream.requestBodies[0], "requestId").String())
	require.Equal(t, "attempt-2", gjson.GetBytes(upstream.requestBodies[1], "requestId").String())
}

func TestAntigravityFamilyForMappedModel(t *testing.T) {
	require.Equal(t, antigravityadapter.FamilyClaude, antigravityFamilyForMappedModel(" claude-sonnet-4-6 "))
	require.Equal(t, antigravityadapter.FamilyClaude, antigravityFamilyForMappedModel("CLAUDE-OPUS-4-6"))
	require.Equal(t, antigravityadapter.FamilyGemini, antigravityFamilyForMappedModel("gemini-3.1-pro"))
	require.Equal(t, antigravityadapter.FamilyGemini, antigravityFamilyForMappedModel(""))
}

func TestAntigravityStandardTestResponseFixtureIsValidJSON(t *testing.T) {
	// Keep the v1internal fixture shape explicit so test failures distinguish
	// malformed fixtures from protocol conversion regressions.
	fixture := []byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}}`)
	require.True(t, json.Valid(fixture))
	require.Equal(t, antigravity.BaseURLs[0], resolveAntigravityForwardBaseURL())
}
