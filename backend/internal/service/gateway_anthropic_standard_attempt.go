package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
)

type standardProtocolErrorWriter func(statusCode int, errorType, message string)

func resolveStandardAnthropicTargetModel(account *Account, requestedModel string) (string, error) {
	if account == nil {
		return "", errors.New("resolve Anthropic target model: nil account")
	}
	if account.IsBedrock() {
		modelID, ok := ResolveBedrockModelID(account, requestedModel)
		if !ok {
			return "", fmt.Errorf("unsupported bedrock model: %s", requestedModel)
		}
		return modelID, nil
	}

	mappedModel := requestedModel
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(requestedModel)
	}
	if mappedModel != requestedModel || account.Platform != PlatformAnthropic {
		return mappedModel, nil
	}
	if account.Type == AccountTypeServiceAccount {
		return normalizeVertexAnthropicModelID(claude.NormalizeModelID(requestedModel)), nil
	}
	if account.Type != AccountTypeAPIKey {
		return claude.NormalizeModelID(requestedModel), nil
	}
	return mappedModel, nil
}

// forwardStandardProtocolToAnthropic executes one already-converted Anthropic
// Messages attempt. Account selection/failover, billing, and source wire
// rendering remain with the caller.
func (s *GatewayService) forwardStandardProtocolToAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	anthropicBody []byte,
	system []byte,
	mappedModel string,
	writeError standardProtocolErrorWriter,
) (*http.Response, error) {
	if account.IsBedrock() {
		return s.forwardStandardProtocolToBedrock(ctx, c, account, anthropicBody, mappedModel, writeError)
	}

	shouldMimicClaudeCode := account.IsOAuth()
	if shouldMimicClaudeCode {
		anthropicBody = s.applyClaudeCodeOAuthMimicryToBody(ctx, c, account, anthropicBody, system, mappedModel)
	}
	anthropicBody = enforceCacheControlLimit(anthropicBody)

	token, tokenType, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, true)
	upstreamReq, _, err := s.buildUpstreamRequest(upstreamCtx, c, account, anthropicBody, token, tokenType, mappedModel, true, shouldMimicClaudeCode)
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}

	resp, err := s.httpUpstream.DoWithTLS(upstreamReq, proxyURL, account.ID, account.Concurrency, s.tlsFPProfileService.ResolveTLSProfile(account))
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: 0, Kind: "request_error", Message: safeErr,
		})
		if writeError != nil {
			writeError(http.StatusBadGateway, "server_error", "Upstream request failed")
		}
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	if resp == nil {
		if writeError != nil {
			writeError(http.StatusBadGateway, "server_error", "Upstream returned an empty response")
		}
		return nil, errors.New("upstream returned an empty response")
	}
	if resp.StatusCode < http.StatusBadRequest {
		return resp, nil
	}

	upstream, collectErr := s.collectGatewayStructuredUpstreamError(resp, protocolconv.ProtocolAnthropic, true)
	if collectErr != nil {
		return nil, collectErr
	}
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(upstream.Body)))
	if s.shouldFailoverUpstreamError(upstream.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: upstream.StatusCode, UpstreamRequestID: upstream.RequestID,
			Kind: "failover", Message: upstreamMsg,
		})
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, upstream.StatusCode, upstream.Headers, upstream.Body, mappedModel)
		}
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

func (s *GatewayService) collectGatewayStructuredUpstreamError(
	resp *http.Response,
	actualProtocol protocolconv.Protocol,
	streamRequested bool,
) (protocoltransport.Response, error) {
	if resp == nil || resp.Body == nil {
		return protocoltransport.Response{}, errors.New("collect structured upstream error: empty response")
	}
	headers := protocoltransport.CloneHeaders(resp.Header)
	requestID := resp.Header.Get("x-request-id")
	statusCode := resp.StatusCode
	bodyReader := resp.Body
	closeBody := bodyReader.Close
	resp.Body = http.NoBody

	if streamRequested {
		stream := &protocoltransport.Stream{
			StatusCode:     statusCode,
			Headers:        headers,
			ActualProtocol: actualProtocol,
			RequestID:      requestID,
			ErrorBody:      bodyReader,
		}
		if err := stream.Validate(); err != nil {
			_ = stream.Close()
			return protocoltransport.Response{}, fmt.Errorf("validate upstream error stream: %w", err)
		}
		bodyReader = stream.ErrorBody
		closeBody = stream.Close
	}

	body, _ := s.readUpstreamErrorBody(&http.Response{Body: bodyReader})
	_ = closeBody()
	upstream := protocoltransport.Response{
		StatusCode:     statusCode,
		Headers:        headers,
		Body:           body,
		ActualProtocol: actualProtocol,
		RequestID:      requestID,
	}
	if err := upstream.Validate(); err != nil {
		return protocoltransport.Response{}, fmt.Errorf("validate structured upstream error response: %w", err)
	}
	return upstream, nil
}
