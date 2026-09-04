package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStandardProtocolBedrockRoutesUseNativeTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		path          string
		body          []byte
		forward       func(*GatewayService, *gin.Context, *Account, []byte) (*ForwardResult, error)
		assertOutput  func(*testing.T, []byte)
		wantMaxTokens int64
	}{
		{
			name: "chat buffered", path: "/v1/chat/completions",
			body: []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
			},
			assertOutput: func(t *testing.T, body []byte) {
				require.Equal(t, "chat.completion", gjson.GetBytes(body, "object").String())
				require.Equal(t, "hello", gjson.GetBytes(body, "choices.0.message.content").String())
			},
		},
		{
			name: "responses buffered", path: "/v1/responses",
			body: []byte(`{"model":"client-model","input":"hi"}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsResponses(context.Background(), c, account, body, nil)
			},
			assertOutput: func(t *testing.T, body []byte) {
				require.Equal(t, "response", gjson.GetBytes(body, "object").String())
				require.Equal(t, "hello", gjson.GetBytes(body, "output.0.content.0.text").String())
			},
		},
		{
			name: "google buffered", path: "/v1beta/models/client-model:generateContent",
			body: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardGoogleGenAI(context.Background(), c, account, "client-model", "client-model", false, body)
			},
			assertOutput: func(t *testing.T, body []byte) {
				require.Equal(t, "hello", gjson.GetBytes(body, "candidates.0.content.parts.0.text").String())
				require.Equal(t, "client-model", gjson.GetBytes(body, "modelVersion").String())
			},
			wantMaxTokens: 8192,
		},
		{
			name: "chat stream", path: "/v1/chat/completions",
			body: []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
			},
			assertOutput: func(t *testing.T, body []byte) {
				wire := string(body)
				require.Contains(t, wire, `"content":"hello"`)
				require.Equal(t, 1, strings.Count(wire, "data: [DONE]"))
			},
		},
		{
			name: "responses stream", path: "/v1/responses",
			body: []byte(`{"model":"client-model","input":"hi","stream":true}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardAsResponses(context.Background(), c, account, body, nil)
			},
			assertOutput: func(t *testing.T, body []byte) {
				wire := string(body)
				require.Contains(t, wire, "event: response.completed")
				require.Contains(t, wire, `"text":"hello"`)
				require.Equal(t, 1, strings.Count(wire, "data: [DONE]"))
			},
		},
		{
			name: "google stream", path: "/v1beta/models/client-model:streamGenerateContent",
			body: []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
			forward: func(s *GatewayService, c *gin.Context, account *Account, body []byte) (*ForwardResult, error) {
				return s.ForwardGoogleGenAI(context.Background(), c, account, "client-model", "client-model", true, body)
			},
			assertOutput: func(t *testing.T, body []byte) {
				wire := string(body)
				require.NotContains(t, wire, "event:")
				require.NotContains(t, wire, "[DONE]")
				require.Contains(t, wire, `"text":"hello"`)
				require.Contains(t, wire, `"modelVersion":"client-model"`)
			},
			wantMaxTokens: 8192,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamBody := buildBedrockAnthropicEventStream(t,
				`{"type":"message_start","message":{"id":"msg_bedrock","type":"message","role":"assistant","content":[],"model":"bedrock-model","usage":{"input_tokens":4}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":4,"outputTokenCount":2}}`,
				`{"type":"message_stop"}`,
			)
			upstreamSource := &closeTrackingReadCloser{Reader: bytes.NewReader(upstreamBody)}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":     []string{"application/vnd.amazon.eventstream"},
					"Content-Length":   []string{"12345"},
					"Content-Encoding": []string{"gzip"},
					"Content-Md5":      []string{"upstream-digest"},
					"Digest":           []string{"sha-256=upstream"},
					"Etag":             []string{"upstream-etag"},
					"X-Amzn-Requestid": []string{"rid-bedrock-standard"},
				},
				ContentLength: 12345,
				Uncompressed:  true,
				Body:          upstreamSource,
			}}
			svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := standardBedrockAPIKeyAccount()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, test.path, bytes.NewReader(test.body))

			result, err := test.forward(svc, c, account, test.body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, protocolconv.ProtocolAnthropic, result.ActualProtocol)
			require.Equal(t, "client-model", result.Model)
			require.Equal(t, "us.anthropic.claude-sonnet-4-6-v1", result.UpstreamModel)
			require.Equal(t, 4, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, "rid-bedrock-standard", result.RequestID)

			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "bedrock-runtime.us-east-1.amazonaws.com", upstream.lastReq.URL.Host)
			require.Equal(t, "/model/us.anthropic.claude-sonnet-4-6-v1/invoke-with-response-stream", upstream.lastReq.URL.Path)
			require.Equal(t, "Bearer test-bedrock-key", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(upstream.lastBody, "anthropic_version").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "model").Exists())
			require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Exists())
			if test.wantMaxTokens > 0 {
				require.Equal(t, test.wantMaxTokens, gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
			}
			test.assertOutput(t, recorder.Body.Bytes())
			for _, key := range []string{"Content-Length", "Content-Encoding", "Content-MD5", "Digest", "ETag"} {
				require.Empty(t, recorder.Header().Get(key))
			}
			require.Equal(t, 1, upstreamSource.closeCount)
			if strings.Contains(test.name, "stream") {
				require.True(t, result.Stream)
				require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
			} else {
				require.False(t, result.Stream)
			}
		})
	}
}

func TestStandardProtocolBedrockSigV4Authentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-sigv4"}},
		Body: io.NopCloser(bytes.NewReader(buildBedrockAnthropicEventStream(t,
			`{"type":"message_start","message":{"id":"msg_sigv4","type":"message","role":"assistant","content":[],"model":"bedrock-model","usage":{"input_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		))),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := standardBedrockAPIKeyAccount()
	account.Credentials["auth_mode"] = "sigv4"
	delete(account.Credentials, "api_key")
	account.Credentials["aws_access_key_id"] = "test-access-key"
	account.Credentials["aws_secret_access_key"] = "test-secret-key"
	account.Credentials["aws_session_token"] = "test-session-token"
	body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Contains(t, upstream.lastReq.Header.Get("Authorization"), "AWS4-HMAC-SHA256")
	require.Contains(t, upstream.lastReq.Header.Get("Authorization"), "Credential=test-access-key/")
	require.Equal(t, "test-session-token", upstream.lastReq.Header.Get("X-Amz-Security-Token"))
	require.NotContains(t, upstream.lastReq.Header.Get("Authorization"), "Bearer ")
}

func TestStandardProtocolBedrockNetworkErrorReturnsFailoverWithoutCommittingResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{err: errors.New("dial failed")}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, standardBedrockAPIKeyAccount(), requestBody, nil)
	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.JSONEq(t, string(gatewayTransportFailoverBody), string(failoverErr.ResponseBody))
	require.False(t, c.Writer.Written())
}

func TestStandardProtocolBedrockImmediate400UsesSourceEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &closeTrackingReadCloser{Reader: strings.NewReader(`{"message":"invalid bedrock request"}`)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"Content-Type":     []string{"application/json"},
			"X-Amzn-Requestid": []string{"rid-bedrock-400"},
		},
		Body: body,
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, standardBedrockAPIKeyAccount(), requestBody, nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "server_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Equal(t, "invalid bedrock request", gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
	require.NotContains(t, recorder.Body.String(), "data:")
	require.NotContains(t, recorder.Body.String(), "event:")
	require.Equal(t, 1, body.closeCount)
}

func TestStandardProtocolBedrockImmediate400UsesResponsesEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responseBody := &closeTrackingReadCloser{Reader: strings.NewReader(`{"message":"invalid bedrock request"}`)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-responses-400"}},
		Body:       responseBody,
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	body := []byte(`{"model":"client-model","input":"hi","stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	result, err := svc.ForwardAsResponses(context.Background(), c, standardBedrockAPIKeyAccount(), body, nil)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "server_error", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	require.Equal(t, "invalid bedrock request", gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
	require.NotContains(t, recorder.Body.String(), "event:")
	require.Equal(t, 1, responseBody.closeCount)
}

func TestStandardProtocolBedrockImmediate400UsesGoogleEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responseBody := &closeTrackingReadCloser{Reader: strings.NewReader(`{"message":"invalid bedrock request"}`)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-google-400"}},
		Body:       responseBody,
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/client-model:streamGenerateContent", bytes.NewReader(body))

	result, err := svc.ForwardGoogleGenAI(context.Background(), c, standardBedrockAPIKeyAccount(), "client-model", "client-model", true, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, int64(http.StatusBadRequest), gjson.GetBytes(recorder.Body.Bytes(), "error.code").Int())
	require.Equal(t, "INVALID_ARGUMENT", gjson.GetBytes(recorder.Body.Bytes(), "error.status").String())
	require.Equal(t, "invalid bedrock request", gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
	require.NotContains(t, recorder.Body.String(), "data:")
	require.Equal(t, 1, responseBody.closeCount)
}

func TestStandardProtocolBedrockStreamFailureBeforeOutputFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frame := buildBedrockAnthropicEventStream(t, `{"type":"message_stop"}`)
	frame[8] ^= 0xff
	body := &closeTrackingReadCloser{Reader: bytes.NewReader(frame)}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-stream-failure"}},
		Body:       body,
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, standardBedrockAPIKeyAccount(), requestBody, nil)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Equal(t, "rid-bedrock-stream-failure", failoverErr.ResponseHeaders.Get("x-request-id"))
	require.JSONEq(t, `{"type":"error","error":{"type":"upstream_disconnected","message":"Bedrock response stream failed before output"}}`, string(failoverErr.ResponseBody))
	require.False(t, recorder.Result().Header.Get("Content-Type") == "text/event-stream")
	require.Empty(t, recorder.Body.String())
	require.Equal(t, 1, body.closeCount)
}

func TestStandardProtocolBedrockMalformedChunkBeforeOutputFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	frame := buildBedrockEventStreamFrame(t, "chunk", []byte(`{"not_bytes":"bad"}`))
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-malformed"}},
		Body:       io.NopCloser(bytes.NewReader(frame)),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := []byte(`{"model":"client-model","input":"hi"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(requestBody))

	result, err := svc.ForwardAsResponses(context.Background(), c, standardBedrockAPIKeyAccount(), requestBody, nil)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, "rid-bedrock-malformed", failoverErr.ResponseHeaders.Get("x-request-id"))
	require.Empty(t, recorder.Body.String())
}

func TestStandardProtocolBedrockStreamFailureAfterOutputDoesNotFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := buildBedrockAnthropicEventStream(t,
		`{"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","content":[],"model":"bedrock-model","usage":{"input_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
	)
	stream = append(stream, []byte("corrupt-frame")...)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Amzn-Requestid": []string{"rid-bedrock-partial"}},
		Body:       io.NopCloser(bytes.NewReader(stream)),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	requestBody := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(requestBody))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, standardBedrockAPIKeyAccount(), requestBody, nil)
	require.Error(t, err)
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Contains(t, recorder.Body.String(), `"content":"partial"`)
}

func TestStandardProtocolBedrockFailoverPreservesStructuredError(t *testing.T) {
	svc := &GatewayService{}
	account := standardBedrockAPIKeyAccount()
	upstream := protocoltransport.Response{
		StatusCode: http.StatusTooManyRequests,
		Headers: http.Header{
			"x-request-id": []string{"rid-bedrock-429"},
			"Retry-After":  []string{"7"},
		},
		Body:           []byte(`{"message":"rate limited"}`),
		ActualProtocol: protocolconv.ProtocolAnthropic,
		RequestID:      "rid-bedrock-429",
	}

	resp, err := svc.handleStandardBedrockError(context.Background(), nil, account, "bedrock-model", upstream, nil)
	require.Nil(t, resp)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.JSONEq(t, `{"message":"rate limited"}`, string(failoverErr.ResponseBody))
	require.Equal(t, "7", failoverErr.ResponseHeaders.Get("Retry-After"))

	upstream.Headers.Set("Retry-After", "mutated")
	require.Equal(t, "7", failoverErr.ResponseHeaders.Get("Retry-After"))
}

func TestStandardProtocolBedrockAcceptsNilSuccessHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewReader(buildBedrockAnthropicEventStream(t,
			`{"type":"message_start","message":{"id":"msg_nil_headers","type":"message","role":"assistant","content":[],"model":"bedrock-model","usage":{"input_tokens":1}}}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		))),
	}}
	svc := &GatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, standardBedrockAPIKeyAccount(), body, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, protocolconv.ProtocolAnthropic, result.ActualProtocol)
}

func TestBedrockAnthropicSSEReadCloserClosesSourceOnce(t *testing.T) {
	source := &closeTrackingReadCloser{Reader: bytes.NewReader(buildBedrockAnthropicEventStream(t, `{"type":"message_stop"}`))}
	adapted := newBedrockAnthropicSSEReadCloser(source, nil)
	body, err := io.ReadAll(adapted)
	require.NoError(t, err)
	require.Contains(t, string(body), "event: message_stop")
	require.NoError(t, adapted.Close())
	require.NoError(t, adapted.Close())
	require.Equal(t, 1, source.closeCount)
}

func standardBedrockAPIKeyAccount() *Account {
	return &Account{
		ID: 901, Name: "bedrock-standard", Platform: PlatformAnthropic, Type: AccountTypeBedrock, Concurrency: 1,
		Credentials: map[string]any{
			"auth_mode":  "apikey",
			"api_key":    "test-bedrock-key",
			"aws_region": "us-east-1",
			"model_mapping": map[string]any{
				"client-model": "us.anthropic.claude-sonnet-4-6-v1",
			},
		},
	}
}

type closeTrackingReadCloser struct {
	io.Reader
	closeCount int
}

func (r *closeTrackingReadCloser) Close() error {
	r.closeCount++
	return nil
}

func buildBedrockAnthropicEventStream(t *testing.T, events ...string) []byte {
	t.Helper()
	var stream bytes.Buffer
	for _, event := range events {
		envelope := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(event)) + `"}`
		_, _ = stream.Write(buildBedrockEventStreamFrame(t, "chunk", []byte(envelope)))
	}
	return stream.Bytes()
}

func buildBedrockEventStreamFrame(t *testing.T, eventType string, payload []byte) []byte {
	t.Helper()
	var headers bytes.Buffer
	writeEventStreamStringHeader(t, &headers, ":event-type", eventType)
	writeEventStreamStringHeader(t, &headers, ":message-type", "event")

	totalLength := uint32(12 + headers.Len() + len(payload) + 4)
	var frame bytes.Buffer
	require.NoError(t, binary.Write(&frame, binary.BigEndian, totalLength))
	require.NoError(t, binary.Write(&frame, binary.BigEndian, uint32(headers.Len())))
	prelude := append([]byte(nil), frame.Bytes()...)
	require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(prelude)))
	_, err := frame.Write(headers.Bytes())
	require.NoError(t, err)
	_, err = frame.Write(payload)
	require.NoError(t, err)
	require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
	return frame.Bytes()
}

func writeEventStreamStringHeader(t *testing.T, dst *bytes.Buffer, name, value string) {
	t.Helper()
	require.NoError(t, dst.WriteByte(byte(len(name))))
	_, err := dst.WriteString(name)
	require.NoError(t, err)
	require.NoError(t, dst.WriteByte(7))
	require.NoError(t, binary.Write(dst, binary.BigEndian, uint16(len(value))))
	_, err = dst.WriteString(value)
	require.NoError(t, err)
}
