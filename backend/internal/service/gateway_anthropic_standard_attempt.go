package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type standardProtocolErrorWriter func(statusCode int, errorType, message string)

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

	respBody, _ := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
	if s.shouldFailoverUpstreamError(resp.StatusCode) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			Kind: "failover", Message: upstreamMsg,
		})
		if s.rateLimitService != nil {
			s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, mappedModel)
		}
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: respBody}
	}
	if writeError != nil {
		writeError(mapUpstreamStatusCode(resp.StatusCode), "server_error", upstreamMsg)
	}
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}
