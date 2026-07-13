package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) forwardGeminiIngressToOpenAI(
	c *gin.Context,
	reqLog *zap.Logger,
	apiKey *service.APIKey,
	userID int64,
	userConcurrency int,
	reqModel string,
	routedModel string,
	stream bool,
	body []byte,
	channelMapping service.ChannelMappingResult,
) {
	if h.openAIGatewayService == nil {
		googleError(c, http.StatusBadGateway, "OpenAI compatibility service is not configured")
		return
	}
	if h.concurrencyHelper == nil || h.billingCacheService == nil {
		googleError(c, http.StatusInternalServerError, "Gateway dependencies are not configured")
		return
	}

	subscription, _ := middleware.GetSubscriptionFromContext(c)
	streamStarted := false
	googleConcurrency := NewConcurrencyHelper(h.concurrencyHelper.concurrencyService, SSEPingFormatNone, 0)
	userRelease, err := googleConcurrency.AcquireUserSlotWithWait(c, userID, userConcurrency, stream, &streamStarted)
	if err != nil {
		reqLog.Warn("gemini.openai.user_slot_acquire_failed", zap.Error(err))
		googleError(c, http.StatusTooManyRequests, err.Error())
		return
	}
	userRelease = wrapReleaseOnDone(c.Request.Context(), userRelease)
	if userRelease != nil {
		defer userRelease()
	}

	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, quotaPlatform); err != nil {
		status, _, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		googleError(c, status, message)
		return
	}

	parsedReq, _ := service.ParseGatewayRequest(service.NewRequestBodyRef(body), "gemini")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: routedModel, Stream: stream, Body: service.NewRequestBodyRef(body)}
	}
	parsedReq.Model = routedModel
	parsedReq.Stream = stream
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP: ip.GetClientIP(c), UserAgent: c.GetHeader("User-Agent"), APIKeyID: apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	fs := NewFailoverState(h.maxAccountSwitches, false)

	for {
		selection, err := h.gatewayService.SelectAccountWithLoadAwareness(
			c.Request.Context(), apiKey.GroupID, sessionHash, routedModel, fs.FailedAccountIDs, "", 0,
		)
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, routedModel, routedModel, service.PlatformOpenAI)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available OpenAI accounts: " + err.Error()
				}
				googleError(c, cls.Status, message)
				return
			}
			switch fs.HandleSelectionExhausted(c.Request.Context()) {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				return
			default:
				h.handleGeminiIngressFailoverExhausted(c, service.PlatformOpenAI, fs.LastFailoverErr)
				return
			}
		}
		account := selection.Account
		if account == nil || account.Platform != service.PlatformOpenAI {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			googleError(c, http.StatusBadGateway, "Selected account does not support OpenAI generation")
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountRelease := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				googleError(c, http.StatusServiceUnavailable, "No available OpenAI accounts")
				return
			}
			accountRelease, err = googleConcurrency.AcquireAccountSlotWithWaitTimeout(
				c, account.ID, selection.WaitPlan.MaxConcurrency, selection.WaitPlan.Timeout, stream, &streamStarted,
			)
			if err != nil {
				reqLog.Warn("gemini.openai.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				googleError(c, http.StatusTooManyRequests, err.Error())
				return
			}
		}
		accountRelease = wrapReleaseOnDone(c.Request.Context(), accountRelease)

		writerSizeBefore := c.Writer.Size()
		result, forwardErr := h.openAIGatewayService.ForwardGoogleGenAI(
			c.Request.Context(), c, account, reqModel, routedModel, stream, body,
		)
		if accountRelease != nil {
			accountRelease()
		}
		if forwardErr != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(forwardErr, &failoverErr) && c.Writer.Size() == writerSizeBefore {
				h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				switch fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, failoverErr) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					return
				default:
					h.handleGeminiIngressFailoverExhausted(c, service.PlatformOpenAI, fs.LastFailoverErr)
					return
				}
			}
			if c.Writer.Size() == writerSizeBefore {
				googleError(c, http.StatusBadGateway, "Upstream request failed")
			}
			reqLog.Error("gemini.openai.forward_failed", zap.Int64("account_id", account.ID), zap.Error(forwardErr))
			return
		}

		if result == nil {
			googleError(c, http.StatusBadGateway, "Upstream returned an empty result")
			return
		}
		h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs)
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		cyberBlocked := service.GetOpsCyberPolicy(c) != nil
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result: result, QuotaPlatform: quotaPlatform, APIKey: apiKey, User: apiKey.User,
				Account: account, Subscription: subscription, InboundEndpoint: inboundEndpoint,
				UpstreamEndpoint: upstreamEndpoint, UserAgent: userAgent, IPAddress: clientIP,
				RequestPayloadHash: requestPayloadHash, APIKeyService: h.apiKeyService,
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
				CyberBlocked:       cyberBlocked,
			}); err != nil {
				reqLog.Error("gemini.openai.record_usage_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		})
		reqLog.Debug("gemini.openai.request_completed", zap.Int64("account_id", account.ID), zap.Int("switch_count", fs.SwitchCount))
		return
	}
}
