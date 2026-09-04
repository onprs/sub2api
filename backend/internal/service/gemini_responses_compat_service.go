package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/gin-gonic/gin"
)

// ForwardAsResponses serves OpenAI Responses clients through Gemini accounts
// while retaining Gemini authentication, retry, failover, and usage policy in
// the shared Google provider executor.
func (s *GeminiMessagesCompatService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*ForwardResult, error) {
	return s.forwardAsResponsesWithClientModel(ctx, c, account, body, "", time.Now())
}

// ForwardAsResponsesWithClientModel preserves the inbound model when the
// handler has already applied a channel-level model mapping to body.
func (s *GeminiMessagesCompatService) ForwardAsResponsesWithClientModel(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
) (*ForwardResult, error) {
	return s.forwardAsResponsesWithClientModel(ctx, c, account, body, clientModel, time.Now())
}

func (s *GeminiMessagesCompatService) forwardAsResponsesWithClientModel(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	startTime time.Time,
) (*ForwardResult, error) {
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, writeGoogleResponsesError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, writeGoogleResponsesError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	originalModel := strings.TrimSpace(clientModel)
	if originalModel == "" {
		originalModel = req.Model
	}
	mappedModel := req.Model
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		mappedModel = account.GetMappedModel(req.Model)
	}
	pipelineConfig := protocolconv.PipelineConfig{
		Route: protocolconv.Route{
			Source: protocolconv.ProtocolOpenAIResponses, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
			ClientModel: originalModel, UpstreamModel: mappedModel, Provider: account.Platform, AccountID: account.ID,
		},
		Options: protocolconv.Options{SourceModel: mappedModel, LossPolicy: protocolconv.LossError},
	}
	s.configureGoogleMetadataBridge(ctx, account, &pipelineConfig)
	pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, pipelineConfig)
	if err != nil {
		return nil, fmt.Errorf("create responses Google pipeline: %w", err)
	}
	convertedRequest, err := pipeline.ConvertRequest(body)
	if err != nil {
		return nil, writeGoogleResponsesError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	googleBody := ensureGeminiFunctionCallThoughtSignatures(convertedRequest.Body)
	writeError := func(status int, errorType, message string) error {
		return writeGoogleResponsesError(c, status, errorType, message)
	}
	return s.forwardGoogleProtocolRequest(ctx, c, account, googleProtocolRequest{
		Source: protocolconv.ProtocolOpenAIResponses, Pipeline: pipeline, GoogleBody: googleBody,
		OriginalBody: body, OriginalModel: originalModel, MappedModel: mappedModel,
		ClientStream: req.Stream, UpstreamStream: req.Stream, StartTime: startTime,
		WriteError: writeError,
		WriteMappedError: func(status int, requestID string, body []byte) error {
			return s.writeGeminiResponsesMappedError(c, account, status, requestID, body)
		},
	})
}

func (s *GeminiMessagesCompatService) writeGeminiResponsesMappedError(
	c *gin.Context,
	account *Account,
	upstreamStatus int,
	upstreamRequestID string,
	body []byte,
) error {
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	setOpsUpstreamError(c, upstreamStatus, upstreamMsg, "")
	if account != nil {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			UpstreamStatusCode: upstreamStatus, UpstreamRequestID: upstreamRequestID,
			Kind: "http_error", Message: upstreamMsg,
		})
	}
	if status, errorType, message, matched := applyErrorPassthroughRule(
		c, PlatformGemini, upstreamStatus, body,
		http.StatusBadGateway, "upstream_error", "Upstream request failed",
	); matched {
		return writeGoogleResponsesError(c, status, errorType, message)
	}

	status := http.StatusBadGateway
	errorType := "upstream_error"
	message := "Upstream request failed"
	if mapped := mapGeminiErrorBodyToClaudeError(body); mapped != nil {
		if mapped.StatusCode > 0 {
			status = mapped.StatusCode
		}
		if mapped.Type != "" {
			errorType = mapped.Type
		}
		if mapped.Message != "" {
			message = mapped.Message
		}
	}
	switch upstreamStatus {
	case http.StatusBadRequest:
		if status == http.StatusBadGateway {
			status = http.StatusBadRequest
		}
		if errorType == "upstream_error" {
			errorType = "invalid_request_error"
		}
	case http.StatusNotFound:
		status = http.StatusNotFound
		if errorType == "upstream_error" {
			errorType = "not_found_error"
		}
	case http.StatusTooManyRequests:
		status = http.StatusTooManyRequests
		if errorType == "upstream_error" {
			errorType = "rate_limit_error"
		}
	case 529:
		status = http.StatusServiceUnavailable
		if errorType == "upstream_error" {
			errorType = "overloaded_error"
		}
	}
	if upstreamMsg != "" && message == "Upstream request failed" {
		message = upstreamMsg
	}
	return writeGoogleResponsesError(c, status, errorType, message)
}

func writeGoogleResponsesError(c *gin.Context, status int, errorType, message string) error {
	renderer, err := protocolconv.NewRenderer(protocolconv.ProtocolOpenAIResponses)
	if err != nil {
		return err
	}
	MarkResponseCommitted(c)
	if err := renderer.RenderError(c.Writer, status, errorType, errorType, message); err != nil {
		return err
	}
	return fmt.Errorf("%s", message)
}
