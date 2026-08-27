package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) forwardGeminiIngressToStandardProvider(
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
	platform := ""
	if apiKey != nil && apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	if !isStandardGeminiIngressProvider(platform) {
		googleError(c, http.StatusBadRequest, "API key group platform does not support standard Gemini ingress")
		return
	}
	if platform == service.PlatformOpenAI && h.openAIGatewayService == nil {
		googleError(c, http.StatusBadGateway, "OpenAI compatibility service is not configured")
		return
	}
	if h.gatewayService == nil || h.concurrencyHelper == nil || h.billingCacheService == nil {
		googleError(c, http.StatusInternalServerError, "Gateway dependencies are not configured")
		return
	}

	var subscription *service.UserSubscription
	streamStarted := false
	googleConcurrency := NewConcurrencyHelper(h.concurrencyHelper.concurrencyService, SSEPingFormatNone, 0)
	userRelease, err := googleConcurrency.AcquireUserSlotWithWait(c, userID, userConcurrency, stream, &streamStarted)
	if err != nil {
		reqLog.Warn("gemini.standard.user_slot_acquire_failed", zap.String("platform", platform), zap.Error(err))
		googleError(c, http.StatusTooManyRequests, err.Error())
		return
	}
	userRelease = wrapReleaseOnDone(c.Request.Context(), userRelease)
	if userRelease != nil {
		defer userRelease()
	}

	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	if subscription, err = resolveLatestBillingEligibility(c, h.billingEligibilityService, apiKey, userID, quotaPlatform); err != nil {
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
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, routedModel, routedModel, platform)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available " + platform + " accounts: " + err.Error()
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
				h.handleGeminiIngressFailoverExhausted(c, platform, fs.LastFailoverErr)
				return
			}
		}
		account := selection.Account
		if account == nil || account.Platform != platform {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			googleError(c, http.StatusBadGateway, "Selected account does not support "+platform+" generation")
			return
		}
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountRelease := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				googleError(c, http.StatusServiceUnavailable, "No available "+platform+" accounts")
				return
			}
			accountRelease, err = googleConcurrency.AcquireAccountSlotWithWaitTimeout(
				c, account.ID, selection.WaitPlan.MaxConcurrency, selection.WaitPlan.Timeout, stream, &streamStarted,
			)
			if err != nil {
				reqLog.Warn("gemini.standard.account_slot_acquire_failed", zap.String("platform", platform), zap.Int64("account_id", account.ID), zap.Error(err))
				googleError(c, http.StatusTooManyRequests, err.Error())
				return
			}
		}
		accountRelease = wrapReleaseOnDone(c.Request.Context(), accountRelease)
		freshAccount, revalidateErr := h.gatewayService.RevalidateSelectedAccount(c.Request.Context(), account.ID, service.AccountEligibilityRequest{
			GroupID:          apiKey.GroupID,
			SessionHash:      sessionHash,
			RequestedModel:   reqModel,
			ExpectedPlatform: platform,
		})
		if revalidateErr != nil {
			if accountRelease != nil {
				accountRelease()
			}
			if fs.HandlePostAcquireRevalidationFailure(account.ID) == FailoverExhausted {
				googleError(c, http.StatusServiceUnavailable, "No available "+platform+" accounts")
				return
			}
			continue
		}
		account = freshAccount

		writerSizeBefore := c.Writer.Size()
		var openAIResult *service.OpenAIForwardResult
		var anthropicResult *service.ForwardResult
		var forwardErr error
		if platform == service.PlatformOpenAI {
			openAIResult, forwardErr = h.openAIGatewayService.ForwardGoogleGenAI(
				c.Request.Context(), c, account, reqModel, routedModel, stream, body,
			)
		} else {
			anthropicResult, forwardErr = h.gatewayService.ForwardGoogleGenAI(
				c.Request.Context(), c, account, reqModel, routedModel, stream, body,
			)
		}
		if accountRelease != nil {
			accountRelease()
		}
		if forwardErr != nil {
			var conversionErr *protocolconv.Error
			if errors.As(forwardErr, &conversionErr) && c.Writer.Size() == writerSizeBefore {
				googleError(c, http.StatusBadRequest, "Request cannot be represented for the selected provider")
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(forwardErr, &failoverErr) && c.Writer.Size() == writerSizeBefore {
				if platform == service.PlatformOpenAI {
					h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
				}
				switch fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr) {
				case FailoverContinue:
					continue
				case FailoverCanceled:
					return
				default:
					h.handleGeminiIngressFailoverExhausted(c, platform, fs.LastFailoverErr)
					return
				}
			}
			if c.Writer.Size() == writerSizeBefore {
				googleError(c, http.StatusBadGateway, "Upstream request failed")
			}
			reqLog.Error("gemini.standard.forward_failed", zap.String("platform", platform), zap.Int64("account_id", account.ID), zap.Error(forwardErr))
			return
		}

		if openAIResult == nil && anthropicResult == nil {
			googleError(c, http.StatusBadGateway, "Upstream returned an empty result")
			return
		}
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		actualProtocol := protocolconv.Protocol("")
		if openAIResult != nil {
			actualProtocol = openAIResult.ActualProtocol
		} else {
			actualProtocol = anthropicResult.ActualProtocol
		}
		upstreamEndpoint := GetUpstreamEndpointForActualProtocol(c, account.Platform, actualProtocol)
		if openAIResult != nil {
			h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, openAIResult.FirstTokenMs)
			cyberBlocked := service.GetOpsCyberPolicy(c) != nil
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := h.openAIGatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
					Result: openAIResult, QuotaPlatform: quotaPlatform, APIKey: apiKey, User: apiKey.User,
					Account: account, Subscription: subscription, InboundEndpoint: inboundEndpoint,
					UpstreamEndpoint: upstreamEndpoint, UserAgent: userAgent, IPAddress: clientIP,
					RequestPayloadHash: requestPayloadHash, APIKeyService: h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, openAIResult.UpstreamModel),
					CyberBlocked:       cyberBlocked,
				}); err != nil {
					reqLog.Error("gemini.openai.record_usage_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			})
		} else {
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
					Result: anthropicResult, QuotaPlatform: quotaPlatform, APIKey: apiKey, User: apiKey.User,
					Account: account, Subscription: subscription, InboundEndpoint: inboundEndpoint,
					UpstreamEndpoint: upstreamEndpoint, UserAgent: userAgent, IPAddress: clientIP,
					RequestPayloadHash: requestPayloadHash, APIKeyService: h.apiKeyService,
					ChannelUsageFields: channelMapping.ToUsageFields(reqModel, anthropicResult.UpstreamModel),
				}); err != nil {
					reqLog.Error("gemini.anthropic.record_usage_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				}
			})
		}
		reqLog.Debug("gemini.standard.request_completed", zap.String("platform", platform), zap.Int64("account_id", account.ID), zap.Int("switch_count", fs.SwitchCount))
		return
	}
}
