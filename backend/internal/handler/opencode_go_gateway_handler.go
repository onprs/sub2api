package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// OpenCodeGoGatewayHandler handles OpenCode Go gateway requests.
type OpenCodeGoGatewayHandler struct {
	openCodeGoService        *service.OpenCodeGoGatewayService
	gatewayService           *service.GatewayService
	billingCacheService      *service.BillingCacheService
	apiKeyService            *service.APIKeyService
	usageRecordWorkerPool    *service.UsageRecordWorkerPool
	errorPassthroughService  *service.ErrorPassthroughService
	contentModerationService *service.ContentModerationService
	concurrencyHelper        *ConcurrencyHelper
	maxAccountSwitches       int
	cfg                      *config.Config
}

// NewOpenCodeGoGatewayHandler creates an OpenCode Go gateway handler.
func NewOpenCodeGoGatewayHandler(
	openCodeGoService *service.OpenCodeGoGatewayService,
	gatewayService *service.GatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	cfg *config.Config,
) *OpenCodeGoGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	return &OpenCodeGoGatewayHandler{
		openCodeGoService:        openCodeGoService,
		gatewayService:           gatewayService,
		billingCacheService:      billingCacheService,
		apiKeyService:            apiKeyService,
		usageRecordWorkerPool:    usageRecordWorkerPool,
		errorPassthroughService:  errorPassthroughService,
		contentModerationService: contentModerationService,
		concurrencyHelper:        NewConcurrencyHelper(concurrencyService, SSEPingFormatClaude, pingInterval),
		maxAccountSwitches:       maxAccountSwitches,
		cfg:                      cfg,
	}
}

// ChatCompletions handles POST /v1/chat/completions for OpenCode Go groups.
func (h *OpenCodeGoGatewayHandler) ChatCompletions(c *gin.Context) {
	h.handle(c, openCodeGoInboundChat)
}

// Messages handles POST /v1/messages for OpenCode Go groups.
func (h *OpenCodeGoGatewayHandler) Messages(c *gin.Context) {
	h.handle(c, openCodeGoInboundMessages)
}

// Models handles GET /v1/models for OpenCode Go groups.
func (h *OpenCodeGoGatewayHandler) Models(c *gin.Context) {
	modelIDs := []string(nil)
	if h != nil && h.gatewayService != nil {
		modelIDs = h.gatewayService.GetAvailableModels(c.Request.Context(), apiKeyGroupIDFromContext(c), service.PlatformOpenCodeGo)
	}

	if apiKey, ok := middleware2.GetAPIKeyFromContext(c); ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.CustomModelsListEnabled() {
		modelIDs = filterModelsByCustomList(modelIDs, defaultModelIDsForPlatform(service.PlatformOpenCodeGo), apiKey.Group.ModelsListConfig.Models)
		writeModelsList(c, modelIDs)
		return
	}

	if len(modelIDs) == 0 {
		modelIDs = service.OpenCodeGoDefaultModelIDs()
	}
	writeModelsList(c, modelIDs)
}

type openCodeGoInboundProtocol string

const (
	openCodeGoInboundChat     openCodeGoInboundProtocol = "chat_completions"
	openCodeGoInboundMessages openCodeGoInboundProtocol = "messages"
)

func (h *OpenCodeGoGatewayHandler) handle(c *gin.Context, inbound openCodeGoInboundProtocol) {
	streamStarted := false
	requestStart := time.Now()
	errorFormat := resolveOpenCodeGoHandlerErrorFormat(inbound)
	component := "handler.opencode_go_gateway." + string(inbound)

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, errorFormat, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, errorFormat, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		component,
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureDependencies(c, errorFormat, reqLog) {
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, errorFormat, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, errorFormat, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, errorFormat, "invalid_request_error", "Request body is empty")
		return
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, errorFormat, "invalid_request_error", "Failed to parse request body")
		return
	}
	reqModel := gjson.GetBytes(body, "model").String()
	if reqModel == "" {
		h.errorResponse(c, http.StatusBadRequest, errorFormat, "invalid_request_error", "model is required")
		return
	}
	reqStream := gjson.GetBytes(body, "stream").Bool()
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	moderationProtocol := service.ContentModerationProtocolOpenAIChat
	if inbound == openCodeGoInboundMessages {
		moderationProtocol = service.ContentModerationProtocolAnthropicMessages
	}
	if decision := h.checkContentModeration(c, reqLog, apiKey, subject, moderationProtocol, reqModel, body); decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), errorFormat, contentModerationErrorCode(decision), decision.Message)
		return
	}

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog, errorFormat)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("opencode_go.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, errorFormat, code, message, streamStarted)
		return
	}

	parsedReq := h.buildParsedRequest(body, reqModel, reqStream, apiKey, inbound, c)
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	if inbound == openCodeGoInboundChat {
		sessionHash = h.gatewayService.GenerateOpenCodeGoCacheAffinityHash(parsedReq)
	}
	fs := NewFailoverState(h.maxAccountSwitches, false)
	routingStart := time.Now()

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, "", subject.UserID)
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				h.errorResponse(c, http.StatusServiceUnavailable, errorFormat, "api_error", "No available accounts: "+err.Error())
				return
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleFailoverExhausted(c, fs.LastFailoverErr, errorFormat, streamStarted)
				} else {
					h.errorResponse(c, http.StatusBadGateway, errorFormat, "server_error", "All available accounts exhausted")
				}
				return
			}
		}
		account := selection.Account
		if account == nil {
			markOpsRoutingCapacityLimited(c)
			h.errorResponse(c, http.StatusServiceUnavailable, errorFormat, "api_error", "No available accounts")
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		if err := h.gatewayService.ValidateGatewayTokenPricingAvailable(c.Request.Context(), apiKey, account, reqModel, channelMapping); err != nil {
			reqLog.Error("opencode_go.billing_pricing_unavailable",
				zap.Int64("account_id", account.ID),
				zap.String("model", reqModel),
				zap.Error(err),
			)
			h.errorResponse(c, http.StatusServiceUnavailable, errorFormat, "api_error", "Model pricing is not configured for this model")
			return
		}

		accountReleaseFunc, acquired := h.acquireAccountSlot(c, selection, reqStream, &streamStarted, reqLog, errorFormat)
		if !acquired {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}

		writerSizeBeforeForward := c.Writer.Size()
		var result *service.ForwardResult
		result, err = func() (*service.ForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			if inbound == openCodeGoInboundMessages {
				return h.openCodeGoService.ForwardMessages(c.Request.Context(), c, account, forwardBody)
			}
			return h.openCodeGoService.ForwardChatCompletions(c.Request.Context(), c, account, forwardBody)
		}()

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, errorFormat, true)
					return
				}
				action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					return
				default:
					h.handleFailoverExhausted(c, fs.LastFailoverErr, errorFormat, streamStarted)
					return
				}
			}
			wrote := h.ensureForwardErrorResponse(c, errorFormat, streamStarted)
			reqLog.Error("opencode_go.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wrote),
				zap.Error(err),
			)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		if quotaPlatform == "" {
			quotaPlatform = service.PlatformOpenCodeGo
		}

		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				QuotaPlatform:      quotaPlatform,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
			}); err != nil {
				logger.L().With(
					zap.String("component", component),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("opencode_go.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("opencode_go.request_completed", zap.Int64("account_id", account.ID), zap.Int("switch_count", fs.SwitchCount))
		return
	}
}

func (h *OpenCodeGoGatewayHandler) buildParsedRequest(body []byte, model string, stream bool, apiKey *service.APIKey, inbound openCodeGoInboundProtocol, c *gin.Context) *service.ParsedRequest {
	bodyRef := service.NewRequestBodyRef(body)
	protocol := "chat_completions"
	if inbound == openCodeGoInboundMessages {
		protocol = "anthropic"
	}
	parsed, _ := service.ParseGatewayRequest(bodyRef, protocol)
	if parsed == nil {
		parsed = &service.ParsedRequest{Model: model, Stream: stream, Body: bodyRef}
	}
	parsed.GroupID = apiKey.GroupID
	parsed.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	return parsed
}

func (h *OpenCodeGoGatewayHandler) acquireUserSlot(c *gin.Context, userID int64, concurrency int, stream bool, streamStarted *bool, reqLog *zap.Logger, format openCodeGoHandlerErrorFormat) (func(), bool) {
	maxWait := service.CalculateMaxWait(concurrency)
	canWait, err := h.concurrencyHelper.IncrementWaitCount(c.Request.Context(), userID, maxWait)
	waitCounted := false
	if err != nil {
		reqLog.Warn("opencode_go.user_wait_counter_increment_failed", zap.Error(err))
	} else if !canWait {
		h.errorResponse(c, http.StatusTooManyRequests, format, "rate_limit_error", "Too many pending requests, please retry later")
		return nil, false
	}
	if err == nil && canWait {
		waitCounted = true
	}
	defer func() {
		if waitCounted {
			h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), userID)
		}
	}()
	release, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, userID, concurrency, stream, streamStarted)
	if err != nil {
		reqLog.Warn("opencode_go.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted != nil && *streamStarted, format)
		return nil, false
	}
	if waitCounted {
		h.concurrencyHelper.DecrementWaitCount(c.Request.Context(), userID)
		waitCounted = false
	}
	return wrapReleaseOnDone(c.Request.Context(), release), true
}

func (h *OpenCodeGoGatewayHandler) acquireAccountSlot(c *gin.Context, selection *service.AccountSelectionResult, stream bool, streamStarted *bool, reqLog *zap.Logger, format openCodeGoHandlerErrorFormat) (func(), bool) {
	account := selection.Account
	release := selection.ReleaseFunc
	if selection.Acquired {
		return wrapReleaseOnDone(c.Request.Context(), release), true
	}
	if selection.WaitPlan == nil {
		markOpsRoutingCapacityLimited(c)
		h.errorResponse(c, http.StatusServiceUnavailable, format, "api_error", "No available accounts")
		return nil, false
	}
	accountWaitCounted := false
	canWait, err := h.concurrencyHelper.IncrementAccountWaitCount(c.Request.Context(), account.ID, selection.WaitPlan.MaxWaiting)
	if err != nil {
		reqLog.Warn("opencode_go.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	} else if !canWait {
		h.errorResponse(c, http.StatusTooManyRequests, format, "rate_limit_error", "Too many pending requests, please retry later")
		return nil, false
	}
	if err == nil && canWait {
		accountWaitCounted = true
	}
	releaseWait := func() {
		if accountWaitCounted {
			h.concurrencyHelper.DecrementAccountWaitCount(c.Request.Context(), account.ID)
			accountWaitCounted = false
		}
	}
	release, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(c, account.ID, selection.WaitPlan.MaxConcurrency, selection.WaitPlan.Timeout, stream, streamStarted)
	if err != nil {
		releaseWait()
		reqLog.Warn("opencode_go.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		h.handleConcurrencyError(c, err, "account", streamStarted != nil && *streamStarted, format)
		return nil, false
	}
	releaseWait()
	return wrapReleaseOnDone(c.Request.Context(), release), true
}

func (h *OpenCodeGoGatewayHandler) ensureDependencies(c *gin.Context, format openCodeGoHandlerErrorFormat, reqLog *zap.Logger) bool {
	missing := make([]string, 0, 4)
	if h == nil {
		missing = append(missing, "handler")
	} else {
		if h.openCodeGoService == nil {
			missing = append(missing, "openCodeGoService")
		}
		if h.gatewayService == nil {
			missing = append(missing, "gatewayService")
		}
		if h.billingCacheService == nil {
			missing = append(missing, "billingCacheService")
		}
		if h.concurrencyHelper == nil || h.concurrencyHelper.concurrencyService == nil {
			missing = append(missing, "concurrencyHelper")
		}
	}
	if len(missing) == 0 {
		return true
	}
	if reqLog != nil {
		reqLog.Error("opencode_go.handler_dependencies_missing", zap.Strings("missing_dependencies", missing))
	}
	h.errorResponse(c, http.StatusServiceUnavailable, format, "api_error", "Service temporarily unavailable")
	return false
}

func (h *OpenCodeGoGatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool, format openCodeGoHandlerErrorFormat) {
	status, errType, message := concurrencyErrorResponse(err, slotType)
	h.handleStreamingAwareError(c, status, format, errType, message, streamStarted)
}

func (h *OpenCodeGoGatewayHandler) handleFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, format openCodeGoHandlerErrorFormat, streamStarted bool) {
	if failoverErr == nil {
		h.handleStreamingAwareError(c, http.StatusBadGateway, format, "upstream_error", "Upstream request failed", streamStarted)
		return
	}
	status, errType, message := h.mapUpstreamError(failoverErr.StatusCode)
	service.SetOpsUpstreamError(c, failoverErr.StatusCode, service.ExtractUpstreamErrorMessage(failoverErr.ResponseBody), "")
	h.handleStreamingAwareError(c, status, format, errType, message, streamStarted)
}

func (h *OpenCodeGoGatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
	switch statusCode {
	case http.StatusTooManyRequests:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 529, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}

func (h *OpenCodeGoGatewayHandler) ensureForwardErrorResponse(c *gin.Context, format openCodeGoHandlerErrorFormat, streamStarted bool) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if c.Writer.Written() {
		streamStarted = true
	}
	h.handleStreamingAwareError(c, http.StatusBadGateway, format, "upstream_error", "Upstream request failed", streamStarted)
	return true
}

func (h *OpenCodeGoGatewayHandler) handleStreamingAwareError(c *gin.Context, status int, format openCodeGoHandlerErrorFormat, errType string, message string, streamStarted bool) {
	if streamStarted || (c != nil && c.Writer != nil && c.Writer.Written()) {
		protocol := protocolconv.ProtocolOpenAIChat
		if format == openCodeGoHandlerErrorAnthropic {
			protocol = protocolconv.ProtocolAnthropic
		}
		writeProtocolStreamError(c, protocol, status, errType, errType, message)
		return
	}
	h.errorResponse(c, status, format, errType, message)
}

func (h *OpenCodeGoGatewayHandler) errorResponse(c *gin.Context, status int, format openCodeGoHandlerErrorFormat, errType string, message string) {
	protocol := protocolconv.ProtocolOpenAIChat
	if format == openCodeGoHandlerErrorAnthropic {
		protocol = protocolconv.ProtocolAnthropic
	}
	writeProtocolError(c, protocol, status, errType, errType, message)
}

func (h *OpenCodeGoGatewayHandler) submitUsageRecordTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if h.usageRecordWorkerPool != nil {
		h.usageRecordWorkerPool.Submit(task)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task(ctx)
}

type openCodeGoHandlerErrorFormat string

const (
	openCodeGoHandlerErrorChat      openCodeGoHandlerErrorFormat = "chat"
	openCodeGoHandlerErrorAnthropic openCodeGoHandlerErrorFormat = "anthropic"
)

func resolveOpenCodeGoHandlerErrorFormat(inbound openCodeGoInboundProtocol) openCodeGoHandlerErrorFormat {
	if inbound == openCodeGoInboundMessages {
		return openCodeGoHandlerErrorAnthropic
	}
	return openCodeGoHandlerErrorChat
}
