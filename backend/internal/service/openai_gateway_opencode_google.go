package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// forwardOpenCodeGoogleGenAI sends a native Google GenAI request to the
// OpenCode official Gemini endpoint. OpenCode serves Gemini models only on its
// Google SDK path, so the generic Google->Responses bridge used for OpenAI
// accounts cannot be reused here.
func (s *OpenAIGatewayService) forwardOpenCodeGoogleGenAI(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	clientModel string,
	routingModel string,
	stream bool,
	body []byte,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	upstreamModel := account.GetMappedModel(routingModel)
	if strings.TrimSpace(upstreamModel) == "" {
		return nil, errors.New("OpenCode Google model is empty")
	}
	if !IsSafeGeminiModelPathSegment(upstreamModel) {
		return nil, errors.New("OpenCode Google model contains an unsafe path segment")
	}
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenCodeGoogleBaseURL())
	if err != nil {
		return nil, fmt.Errorf("invalid OpenCode Google base_url: %w", err)
	}
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	targetURL := strings.TrimRight(baseURL, "/") + "/v1beta/models/" + url.PathEscape(upstreamModel) + ":" + action
	if stream {
		targetURL += "?alt=sse"
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build OpenCode Google request: %w", err)
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		return nil, fmt.Errorf("account %d missing api_key", account.ID)
	}
	// OpenCode authenticates its own Gemini gateway with the OpenCode API key.
	// Do not send x-goog-api-key: the upstream Google request must use the
	// credential that OpenCode injects, not the client gateway token.
	req.Header.Set("Authorization", "Bearer "+apiKey)
	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if openaiCCRawAllowedHeaders[lowerKey] {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	if customUA := account.GetOpenAIUserAgent(); customUA != "" {
		req.Header.Set("user-agent", customUA)
	}
	applyOpenCodeUpstreamUserAgent(account, targetURL, req.Header)
	applyOpenCodeSessionHeader(c, account, targetURL, req.Header, body)
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.doOpenAIUpstream(req, resolveAccountProxyURL(account), account)
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
		return nil, fmt.Errorf("OpenCode Google upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		respBody := s.readUpstreamErrorBody(resp)
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if s.shouldFailoverOpenAIUpstreamResponse(account, resp.StatusCode, upstreamMsg, respBody) {
			shouldDisable := s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, upstreamModel)
			retryableOnSameAccount := !shouldDisable && account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode)
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: retryableOnSameAccount,
			}
		}
		writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
		return nil, fmt.Errorf("OpenCode Google upstream returned status %d", resp.StatusCode)
	}

	result := &OpenAIForwardResult{
		RequestID:       firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("request-id"), resp.Header.Get("x-goog-request-id")),
		UpstreamHeaders: resp.Header,
		ActualProtocol:  protocolconv.ProtocolGoogleGenAI,
		Model:           clientModel,
		BillingModel:    routingModel,
		UpstreamModel:   upstreamModel,
		Stream:          stream,
	}
	if stream {
		if err := s.writeOpenCodeGoogleStream(c, resp, result, startTime); err != nil {
			return nil, err
		}
	} else {
		respBody, readErr := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
		if readErr != nil {
			if !errors.Is(readErr, ErrUpstreamResponseBodyTooLarge) {
				writeOpenAIEmbeddingsError(c, http.StatusBadGateway, "api_error", "Failed to read upstream response")
			}
			return nil, fmt.Errorf("read OpenCode Google response: %w", readErr)
		}
		writeOpenAIEmbeddingsUpstreamResponse(c, resp, respBody, s.responseHeaderFilter)
		result.Usage = openCodeGoogleUsageFromBody(respBody)
		result.ResponseID = gjson.GetBytes(respBody, "responseId").String()
		result.UpstreamResponseModel = gjson.GetBytes(respBody, "modelVersion").String()
	}
	result.Duration = time.Since(startTime)
	return result, nil
}

func (s *OpenAIGatewayService) writeOpenCodeGoogleStream(c *gin.Context, resp *http.Response, result *OpenAIForwardResult, startTime time.Time) error {
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	contentType := resp.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "text/event-stream"
	}
	c.Writer.Header().Set("Content-Type", contentType)
	c.Writer.WriteHeader(resp.StatusCode)
	flusher, _ := c.Writer.(http.Flusher)
	clientDisconnected := false
	var firstTokenMs *int
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !clientDisconnected {
				if _, writeErr := c.Writer.Write(line); writeErr != nil {
					clientDisconnected = true
				} else if flusher != nil {
					flusher.Flush()
				}
			}
			if payload := openCodeGoogleStreamPayload(line); payload != nil {
				if result.ResponseID == "" {
					result.ResponseID = gjson.GetBytes(payload, "responseId").String()
				}
				if result.UpstreamResponseModel == "" {
					result.UpstreamResponseModel = gjson.GetBytes(payload, "modelVersion").String()
				}
				if usage := openCodeGoogleUsageFromBody(payload); usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadInputTokens > 0 {
					result.Usage = usage
				}
				if firstTokenMs == nil && openCodeGoogleHasOutput(payload) {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				// The client may still have received a complete terminal event.
				// Keep the collected usage so the request is billed accurately.
				result.ClientDisconnect = clientDisconnected
				result.FirstTokenMs = firstTokenMs
				return nil
			}
			break
		}
	}
	if !clientDisconnected && flusher != nil {
		flusher.Flush()
	}
	result.ClientDisconnect = clientDisconnected
	result.FirstTokenMs = firstTokenMs
	return nil
}

func openCodeGoogleStreamPayload(line []byte) []byte {
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "data:") {
		return nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "" || payload == "[DONE]" || !gjson.Valid(payload) {
		return nil
	}
	return []byte(payload)
}

func openCodeGoogleHasOutput(payload []byte) bool {
	result := gjson.GetBytes(payload, "candidates")
	if !result.Exists() || !result.IsArray() {
		return false
	}
	found := false
	result.ForEach(func(_, candidate gjson.Result) bool {
		parts := candidate.Get("content.parts")
		if parts.Exists() && parts.IsArray() {
			parts.ForEach(func(_, part gjson.Result) bool {
				if part.Get("text").String() != "" || part.Get("functionCall").Exists() || part.Get("inlineData").Exists() {
					found = true
					return false
				}
				return true
			})
		}
		return !found
	})
	return found
}

func openCodeGoogleUsageFromBody(body []byte) OpenAIUsage {
	claudeUsage := extractGeminiUsage(body)
	if claudeUsage == nil {
		return OpenAIUsage{}
	}
	// Gemini promptTokenCount includes cachedContentTokenCount, while
	// OpenAIUsage.InputTokens is the total before RecordUsage splits cache read
	// out again. Add it back to keep the final non-cached input bucket intact.
	return OpenAIUsage{
		InputTokens:          claudeUsage.InputTokens + claudeUsage.CacheReadInputTokens,
		OutputTokens:         claudeUsage.OutputTokens,
		CacheReadInputTokens: claudeUsage.CacheReadInputTokens,
		ImageOutputTokens:    claudeUsage.ImageOutputTokens,
	}
}
