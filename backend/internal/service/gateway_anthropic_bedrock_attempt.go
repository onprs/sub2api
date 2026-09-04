package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// forwardStandardProtocolToBedrock sends an already-converted Anthropic
// Messages request through Bedrock and adapts AWS EventStream frames back to
// standard Anthropic SSE. Source conversion and rendering remain with the
// caller.
func (s *GatewayService) forwardStandardProtocolToBedrock(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	anthropicBody []byte,
	mappedModel string,
	writeError standardProtocolErrorWriter,
) (*http.Response, error) {
	modelID, ok := ResolveBedrockModelID(account, mappedModel)
	if !ok {
		return nil, fmt.Errorf("unsupported bedrock model: %s", mappedModel)
	}
	var groupID *int64
	if identity, exists := ProtocolMetadataIdentityFromContext(ctx); exists {
		value := identity.GroupID
		groupID = &value
	}
	anthropicBody = s.ApplyBedrockCCCompat(c, anthropicBody, modelID, account, groupID)
	region := bedrockRuntimeRegion(account)
	betaHeader := ""
	if c != nil && c.Request != nil {
		betaHeader = c.GetHeader("anthropic-beta")
	}
	betaTokens, err := s.resolveBedrockBetaTokensForRequest(ctx, account, betaHeader, anthropicBody, modelID)
	if err != nil {
		return nil, err
	}
	bedrockBody, err := PrepareBedrockRequestBodyWithTokens(anthropicBody, modelID, betaTokens, false)
	if err != nil {
		return nil, fmt.Errorf("prepare bedrock request body: %w", err)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var signer *BedrockSigner
	var apiKey string
	if account.IsBedrockAPIKey() {
		apiKey = account.GetCredential("api_key")
		if apiKey == "" {
			return nil, errors.New("api_key not found in bedrock credentials")
		}
	} else {
		signer, err = NewBedrockSignerFromAccount(account)
		if err != nil {
			return nil, fmt.Errorf("create bedrock signer: %w", err)
		}
	}

	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, true)
	resp, err := s.executeBedrockUpstream(
		upstreamCtx, c, account, bedrockBody, modelID, region, true,
		signer, apiKey, proxyURL,
	)
	releaseUpstreamCtx()
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		if writeError != nil {
			writeError(http.StatusBadGateway, "server_error", "Upstream returned an empty response")
		}
		return nil, errors.New("bedrock returned an empty response")
	}

	mapBedrockRequestID(resp.Header)
	if resp.StatusCode >= http.StatusBadRequest {
		upstream, collectErr := s.collectGatewayStructuredUpstreamError(resp, protocolconv.ProtocolAnthropic, true)
		if collectErr != nil {
			return nil, collectErr
		}
		return s.handleStandardBedrockError(ctx, c, account, modelID, upstream, writeError)
	}

	headers := protocoltransport.CloneHeaders(resp.Header)
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "text/event-stream")
	for _, key := range []string{"Content-Length", "Content-Encoding", "Content-MD5", "Digest", "ETag"} {
		headers.Del(key)
	}
	resp.Header = headers
	resp.ContentLength = -1
	resp.Uncompressed = false
	resp.Body = newBedrockAnthropicSSEReadCloser(resp.Body, headers)
	return resp, nil
}

func (s *GatewayService) handleStandardBedrockStreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	modelID string,
	result *ForwardResult,
	err error,
) (*ForwardResult, error) {
	if err == nil || !isBedrockAnthropicStreamError(err) || (c != nil && c.Writer.Written()) {
		return result, err
	}
	var streamErr *bedrockAnthropicStreamError
	_ = errors.As(err, &streamErr)
	responseHeaders := make(http.Header)
	if streamErr != nil {
		responseHeaders = protocoltransport.CloneHeaders(streamErr.headers)
	}
	body := []byte(`{"type":"error","error":{"type":"upstream_disconnected","message":"Bedrock response stream failed before output"}}`)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: http.StatusBadGateway, UpstreamRequestID: responseHeaders.Get("x-request-id"),
		Kind: "stream_read_error", Message: sanitizeStreamError(err),
	})
	if s.rateLimitService != nil {
		s.rateLimitService.HandleUpstreamError(ctx, account, http.StatusBadGateway, responseHeaders, body, modelID)
	}
	return nil, &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		ResponseBody:           body,
		ResponseHeaders:        responseHeaders,
		RetryableOnSameAccount: true,
	}
}

func (s *GatewayService) handleStandardBedrockError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	modelID string,
	upstream protocoltransport.Response,
	writeError standardProtocolErrorWriter,
) (*http.Response, error) {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
	if s.shouldFailoverUpstreamError(upstream.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: upstream.StatusCode, UpstreamRequestID: upstream.RequestID,
			Kind: "failover", Message: upstreamMsg,
		})
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body, modelID)
		}
		return nil, &UpstreamFailoverError{
			StatusCode:             upstream.StatusCode,
			ResponseBody:           upstream.Body,
			ResponseHeaders:        protocoltransport.CloneHeaders(upstream.Headers),
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(upstream.StatusCode),
		}
	}
	if s.rateLimitService != nil && s.rateLimitService.HandleUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body, modelID) {
		return nil, &UpstreamFailoverError{
			StatusCode:             upstream.StatusCode,
			ResponseBody:           upstream.Body,
			ResponseHeaders:        protocoltransport.CloneHeaders(upstream.Headers),
			RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(upstream.StatusCode),
		}
	}
	if writeError != nil {
		writeError(mapUpstreamStatusCode(upstream.StatusCode), "server_error", upstreamMsg)
	}
	return nil, fmt.Errorf("upstream error: %d %s", upstream.StatusCode, upstreamMsg)
}

func mapBedrockRequestID(header http.Header) {
	if header == nil || header.Get("x-request-id") != "" {
		return
	}
	if requestID := header.Get("x-amzn-requestid"); requestID != "" {
		header.Set("x-request-id", requestID)
	}
}

type bedrockAnthropicStreamError struct {
	cause   error
	headers http.Header
}

func (e *bedrockAnthropicStreamError) Error() string {
	return fmt.Sprintf("bedrock stream read error: %v", e.cause)
}

func (e *bedrockAnthropicStreamError) Unwrap() error { return e.cause }

func isBedrockAnthropicStreamError(err error) bool {
	var streamErr *bedrockAnthropicStreamError
	return errors.As(err, &streamErr)
}

type bedrockAnthropicSSEReadCloser struct {
	source   io.ReadCloser
	decoder  *bedrockEventStreamDecoder
	headers  http.Header
	pending  *bytes.Reader
	terminal bool
	closed   atomic.Bool
	once     sync.Once
	readErr  error
	closeErr error
}

func newBedrockAnthropicSSEReadCloser(source io.ReadCloser, headers http.Header) io.ReadCloser {
	return &bedrockAnthropicSSEReadCloser{
		source:  source,
		decoder: newBedrockEventStreamDecoder(source),
		headers: protocoltransport.CloneHeaders(headers),
		pending: bytes.NewReader(nil),
	}
}

func (r *bedrockAnthropicSSEReadCloser) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if r.closed.Load() {
			return 0, io.ErrClosedPipe
		}
		if r.pending.Len() > 0 {
			return r.pending.Read(p)
		}
		if r.readErr != nil {
			return 0, r.readErr
		}
		if r.terminal {
			r.readErr = io.EOF
			continue
		}
		payload, err := r.decoder.Decode()
		if err != nil {
			r.readErr = &bedrockAnthropicStreamError{cause: err, headers: protocoltransport.CloneHeaders(r.headers)}
			continue
		}
		data, err := extractBedrockChunkDataStrict(payload)
		if err != nil {
			r.readErr = &bedrockAnthropicStreamError{cause: err, headers: protocoltransport.CloneHeaders(r.headers)}
			continue
		}
		data = transformBedrockInvocationMetrics(data)
		eventType := strings.TrimSpace(gjson.GetBytes(data, "type").String())
		if eventType == "message_stop" {
			r.terminal = true
		}
		var framed []byte
		if eventType != "" {
			framed = append(framed, "event: "...)
			framed = append(framed, eventType...)
			framed = append(framed, '\n')
		}
		framed = append(framed, "data: "...)
		framed = append(framed, data...)
		framed = append(framed, '\n', '\n')
		r.pending.Reset(framed)
	}
}

func extractBedrockChunkDataStrict(payload []byte) ([]byte, error) {
	encoded := gjson.GetBytes(payload, "bytes")
	if !encoded.Exists() || strings.TrimSpace(encoded.String()) == "" {
		return nil, errors.New("bedrock chunk is missing bytes")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		return nil, fmt.Errorf("decode bedrock chunk bytes: %w", err)
	}
	if !gjson.ValidBytes(decoded) {
		return nil, errors.New("bedrock chunk bytes contain invalid JSON")
	}
	if strings.TrimSpace(gjson.GetBytes(decoded, "type").String()) == "" {
		return nil, errors.New("bedrock chunk bytes are missing Anthropic event type")
	}
	return decoded, nil
}

func (r *bedrockAnthropicSSEReadCloser) Close() error {
	r.once.Do(func() {
		r.closed.Store(true)
		r.closeErr = r.source.Close()
	})
	return r.closeErr
}
