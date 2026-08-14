package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/antigravityadapter"
	protocoltransport "github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/transport"
	"github.com/gin-gonic/gin"
)

// ForwardAsChatCompletions routes a Chat Completions request through the
// Antigravity Google wire adapter while preserving the client protocol.
func (s *AntigravityGatewayService) ForwardAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	isStickySession bool,
) (*ForwardResult, error) {
	return s.forwardStandardProtocol(ctx, c, account, body, clientModel, protocolconv.ProtocolOpenAIChat, isStickySession)
}

// ForwardAsResponses routes a Responses request through the Antigravity Google
// wire adapter while preserving the client protocol.
func (s *AntigravityGatewayService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	isStickySession bool,
) (*ForwardResult, error) {
	return s.forwardStandardProtocol(ctx, c, account, body, clientModel, protocolconv.ProtocolOpenAIResponses, isStickySession)
}

func (s *AntigravityGatewayService) forwardStandardProtocol(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	clientModel string,
	source protocolconv.Protocol,
	isStickySession bool,
) (*ForwardResult, error) {
	var request struct {
		Model         string `json:"model"`
		Stream        bool   `json:"stream"`
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, s.writeStandardProtocolError(c, source, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return nil, s.writeStandardProtocolError(c, source, http.StatusBadRequest, "invalid_request_error", "model is required")
	}
	if strings.TrimSpace(clientModel) == "" {
		clientModel = request.Model
	}
	mappedModel := s.getMappedModel(account, request.Model)
	if mappedModel == "" {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalFeatureGate)
		return nil, s.writeStandardProtocolError(c, source, http.StatusForbidden, "permission_error", fmt.Sprintf("model %s not in whitelist", request.Model))
	}

	pipelineFactory := func(sourceModel, wireModel string) (*protocolconv.Pipeline, []byte, error) {
		pipelineConfig := protocolconv.PipelineConfig{
			Route: protocolconv.Route{
				Source: source, IntendedTarget: protocolconv.ProtocolGoogleGenAI,
				ClientModel: clientModel, UpstreamModel: wireModel,
				Provider: account.Platform, AccountID: account.ID,
			},
			Options: protocolconv.Options{SourceModel: sourceModel, ResponseModel: clientModel, LossPolicy: protocolconv.LossError},
		}
		s.configureStandardMetadataBridge(ctx, account, &pipelineConfig)
		pipeline, err := protocolconv.NewPipeline(standardProtocolRegistry, pipelineConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("create Antigravity standard pipeline: %w", err)
		}
		converted, err := pipeline.ConvertRequest(body)
		if err != nil {
			return nil, nil, err
		}
		return pipeline, converted.Body, nil
	}
	includeUsage := request.StreamOptions != nil && request.StreamOptions.IncludeUsage
	result, err := s.ForwardGemini(
		ctx, c, account, request.Model, "streamGenerateContent", request.Stream,
		body, isStickySession,
		WithForwardGeminiProtocol(source, pipelineFactory, includeUsage),
	)
	if result != nil {
		result.Model = clientModel
		result.ActualProtocol = protocolconv.ProtocolGoogleGenAI
	}
	return result, err
}

func (s *AntigravityGatewayService) forwardGeminiAdapterOptions(
	ctx context.Context,
	opts forwardGeminiOptions,
	projectID string,
	sourceModel string,
	wireModel string,
) antigravityadapter.Options {
	family := antigravityadapter.FamilyGemini
	if opts.pipelineFactory != nil {
		family = antigravityFamilyForMappedModel(wireModel)
	}
	adapterOptions := antigravityadapter.Options{
		Family: family, ProjectID: projectID, SourceModel: sourceModel, WireModel: wireModel, UserAgent: "antigravity",
	}
	if family == antigravityadapter.FamilyClaude {
		adapterOptions.TransformOptions = s.getClaudeTransformOptions(ctx)
	}
	return adapterOptions
}

func antigravityFamilyForMappedModel(mappedModel string) antigravityadapter.Family {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mappedModel)), "claude-") {
		return antigravityadapter.FamilyClaude
	}
	return antigravityadapter.FamilyGemini
}

func (s *AntigravityGatewayService) configureStandardMetadataBridge(
	ctx context.Context,
	account *Account,
	pipelineConfig *protocolconv.PipelineConfig,
) {
	if s == nil || s.providerMetadataStore == nil || account == nil || pipelineConfig == nil {
		return
	}
	identity, ok := ProtocolMetadataIdentityFromContext(ctx)
	if !ok {
		return
	}
	pipelineConfig.MetadataStore = s.providerMetadataStore
	pipelineConfig.MetadataScope = protocolconv.MetadataScope{
		TenantID: identity.TenantID, APIKeyID: identity.APIKeyID, GroupID: identity.GroupID,
		AccountID: account.ID, Protocol: protocolconv.ProtocolGoogleGenAI,
	}
}

func (s *AntigravityGatewayService) writeForwardGeminiError(c *gin.Context, opts forwardGeminiOptions, status int, message string) error {
	if opts.pipelineFactory == nil {
		return s.writeGoogleError(c, status, message)
	}
	errorType := "upstream_error"
	switch status {
	case http.StatusBadRequest:
		errorType = "invalid_request_error"
	case http.StatusForbidden:
		errorType = "permission_error"
	case http.StatusNotFound:
		errorType = "not_found_error"
	case http.StatusTooManyRequests:
		errorType = "rate_limit_error"
	}
	return s.writeStandardProtocolError(c, opts.source, status, errorType, message)
}

func (s *AntigravityGatewayService) writeStandardProtocolError(
	c *gin.Context,
	protocol protocolconv.Protocol,
	status int,
	errorType string,
	message string,
) error {
	renderer, err := protocolconv.NewRenderer(protocol)
	if err != nil {
		return err
	}
	MarkResponseCommitted(c)
	if err := renderer.RenderError(c.Writer, status, errorType, errorType, message); err != nil {
		return err
	}
	return errors.New(message)
}

func (s *AntigravityGatewayService) handleStandardGeminiResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
	stream bool,
	includeUsage bool,
	startTime time.Time,
) (*antigravityStreamResult, error) {
	if stream {
		return s.handleStandardGeminiStreamingResponse(c, resp, pipeline, source, includeUsage, startTime)
	}
	return s.handleStandardGeminiBufferedResponse(c, resp, pipeline, source, startTime)
}

func (s *AntigravityGatewayService) handleStandardGeminiBufferedResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
	startTime time.Time,
) (*antigravityStreamResult, error) {
	parser := protocoltransport.NewSSEParser(resp.Body, s.maxStandardSSERecordSize())
	defer func() { _ = parser.Close() }()

	usage := &ClaudeUsage{}
	var firstTokenMs *int
	var last map[string]any
	var lastWithParts map[string]any
	var collectedParts []map[string]any

	for {
		record, err := parser.Next(context.Background())
		if errors.Is(err, io.EOF) || errors.Is(err, protocoltransport.ErrSSEDone) {
			break
		}
		if err != nil {
			return nil, s.standardGenerationFailover("read Antigravity stream", err)
		}
		inner, err := s.unwrapV1InternalResponse(record.Data)
		if err != nil || !json.Valid(inner) {
			return nil, s.standardGenerationFailover("decode Antigravity stream envelope", err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(inner, &parsed); err != nil {
			return nil, s.standardGenerationFailover("decode Antigravity Google response", err)
		}
		if firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		last = parsed
		if parts := extractGeminiParts(parsed); len(parts) > 0 {
			lastWithParts = parsed
			collectedParts = append(collectedParts, parts...)
		}
		if current := extractGeminiUsage(inner); current != nil {
			usage = current
		}
	}

	if last == nil && lastWithParts == nil {
		return nil, s.standardGenerationFailover("empty Antigravity stream", nil)
	}
	finalResponse := pickGeminiCollectResult(last, lastWithParts)
	if len(collectedParts) > 0 {
		finalResponse = mergeCollectedPartsToResponse(finalResponse, collectedParts)
	}
	googleBody, err := json.Marshal(finalResponse)
	if err != nil {
		return nil, s.standardGenerationFailover("marshal Antigravity Google response", err)
	}
	converted, err := pipeline.ConvertResponse(googleBody, protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, s.standardGenerationFailover("convert Antigravity Google response", err)
	}
	renderer, err := protocolconv.NewRenderer(source)
	if err != nil {
		return nil, err
	}
	if err := renderer.RenderJSON(c.Writer, http.StatusOK, resp.Header, converted.Body); err != nil {
		return nil, err
	}
	return &antigravityStreamResult{usage: usage, firstTokenMs: firstTokenMs}, nil
}

func (s *AntigravityGatewayService) handleStandardGeminiStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	pipeline *protocolconv.Pipeline,
	source protocolconv.Protocol,
	includeUsage bool,
	startTime time.Time,
) (*antigravityStreamResult, error) {
	parser := protocoltransport.NewSSEParser(resp.Body, s.maxStandardSSERecordSize())
	defer func() { _ = parser.Close() }()
	session, err := pipeline.NewStreamProcessor(protocolconv.ProtocolGoogleGenAI)
	if err != nil {
		return nil, err
	}
	renderer, err := protocolconv.NewRenderer(source)
	if err != nil {
		return nil, err
	}

	usage := &ClaudeUsage{}
	var firstTokenMs *int
	headersWritten := false
	clientDisconnected := false
	writePayloads := func(payloads [][]byte) error {
		if clientDisconnected {
			return nil
		}
		for _, payload := range payloads {
			if source == protocolconv.ProtocolOpenAIChat && !includeUsage && isOpenAIChatUsageOnlyStreamChunk(string(payload)) {
				continue
			}
			if !headersWritten {
				if err := renderer.WriteStreamHeaders(c.Writer, http.StatusOK, resp.Header); err != nil {
					return err
				}
				headersWritten = true
			}
			framed, err := renderer.FrameStreamEvent(payload)
			if err != nil {
				return err
			}
			if _, err := c.Writer.Write(framed); err != nil {
				clientDisconnected = true
				return nil
			}
			c.Writer.Flush()
		}
		return nil
	}

	for {
		record, readErr := parser.Next(context.Background())
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, protocoltransport.ErrSSEDone) {
			break
		}
		if readErr != nil {
			if !headersWritten {
				return nil, s.standardGenerationFailover("read Antigravity stream", readErr)
			}
			return nil, readErr
		}
		inner, unwrapErr := s.unwrapV1InternalResponse(record.Data)
		if unwrapErr != nil || !json.Valid(inner) {
			if !headersWritten {
				return nil, s.standardGenerationFailover("decode Antigravity stream envelope", unwrapErr)
			}
			return nil, fmt.Errorf("decode Antigravity stream envelope: %w", unwrapErr)
		}
		if current := extractGeminiUsage(inner); current != nil {
			usage = current
		}
		payloads, _, convertErr := session.Convert(inner)
		if convertErr != nil {
			if !headersWritten {
				return nil, s.standardGenerationFailover("convert Antigravity stream event", convertErr)
			}
			return nil, convertErr
		}
		if len(payloads) > 0 && firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		if err := writePayloads(payloads); err != nil {
			return nil, err
		}
	}

	finalPayloads, _, err := session.Finalize()
	if err != nil {
		if !headersWritten {
			return nil, s.standardGenerationFailover("finalize Antigravity stream", err)
		}
		return nil, err
	}
	if err := writePayloads(finalPayloads); err != nil {
		return nil, err
	}
	if !headersWritten && !clientDisconnected {
		return nil, s.standardGenerationFailover("empty Antigravity stream", nil)
	}
	if !clientDisconnected {
		if terminal := renderer.StreamTerminal(); len(terminal) > 0 {
			if _, err := c.Writer.Write(terminal); err != nil {
				clientDisconnected = true
			} else {
				c.Writer.Flush()
			}
		}
	}
	return &antigravityStreamResult{usage: usage, firstTokenMs: firstTokenMs, clientDisconnect: clientDisconnected}, nil
}

func (s *AntigravityGatewayService) maxStandardSSERecordSize() int {
	if s != nil && s.settingService != nil && s.settingService.cfg != nil && s.settingService.cfg.Gateway.MaxLineSize > 0 {
		return s.settingService.cfg.Gateway.MaxLineSize
	}
	return protocoltransport.DefaultMaxSSERecordBytes
}

func (s *AntigravityGatewayService) standardGenerationFailover(message string, cause error) error {
	if cause != nil {
		message += ": " + cause.Error()
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"type": "upstream_error", "message": message}})
	return &UpstreamFailoverError{
		StatusCode:             http.StatusBadGateway,
		ResponseBody:           body,
		RetryableOnSameAccount: true,
	}
}
