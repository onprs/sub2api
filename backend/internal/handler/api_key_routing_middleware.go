package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// APIKeyRoutingMiddleware 在认证完成后、协议 Handler 执行前解析动态分组。
func (h *GatewayHandler) APIKeyRoutingMiddleware(googleErrors bool, retryPreflight ...gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := middleware2.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || !apiKey.UsesDynamicGroupRouting() || h == nil || h.gatewayService == nil {
			c.Next()
			return
		}

		input, shouldResolve, err := apiKeyRoutingInputFromRequest(c)
		if err != nil {
			writeAPIKeyRoutingError(c, googleErrors, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
			return
		}
		if !shouldResolve {
			c.Next()
			return
		}
		if forcePlatform, exists := middleware2.GetForcePlatformFromContext(c); exists {
			input.ForcePlatform = forcePlatform
		}

		effectiveKey, err := h.gatewayService.ResolveAPIKeyRoutingGroup(c.Request.Context(), apiKey, input)
		if err != nil || effectiveKey == nil || effectiveKey.Group == nil {
			writeAPIKeyRoutingError(c, googleErrors, http.StatusServiceUnavailable, "NO_AVAILABLE_ROUTING_GROUP", "No configured group can serve this request")
			return
		}

		h.serveAPIKeyRoutingAttempts(c, apiKey, effectiveKey, input, retryPreflight)
	}
}

func apiKeyRoutingInputFromRequest(c *gin.Context) (service.APIKeyRoutingResolveInput, bool, error) {
	input := service.APIKeyRoutingResolveInput{
		Capability:        service.APIKeyRoutingCapabilityText,
		CompositeEndpoint: apiKeyRoutingCompositeEndpoint(c.Request.URL.Path),
	}
	path := c.Request.URL.Path
	method := c.Request.Method

	switch {
	case method == http.MethodGet && strings.Contains(path, "/videos/"):
		requestID := strings.TrimSpace(c.Param("request_id"))
		if requestID == "" {
			return input, false, nil
		}
		apiKey, hasAPIKey := middleware2.GetAPIKeyFromContext(c)
		subject, hasSubject := middleware2.GetAuthSubjectFromContext(c)
		if !hasAPIKey || apiKey == nil || !hasSubject {
			return input, false, nil
		}
		input.Capability = service.APIKeyRoutingCapabilityVideo
		input.PreserveSession = true
		if apiKey.RoutingPlatformValue() == service.PlatformComposite {
			input.ForcePlatform = service.PlatformGrok
		}
		input.SessionKey = service.GrokMediaVideoRequestSessionHash(requestID, subject.UserID, apiKey.ID)
		if input.SessionKey == "" {
			return input, false, nil
		}
		return input, true, nil
	case method == http.MethodGet && (strings.HasSuffix(path, "/models") || strings.HasSuffix(path, "/usage")):
		if strings.HasSuffix(path, "/models") &&
			(strings.TrimSpace(c.Query("client_version")) != "" || strings.HasSuffix(path, "/backend-api/codex/models")) {
			return input, true, nil
		}
		return input, false, nil
	case method == http.MethodGet:
		if model := strings.TrimSpace(c.Param("model")); model != "" {
			input.Model = strings.TrimPrefix(model, "models/")
			return input, true, nil
		}
		return input, false, nil
	case strings.Contains(path, "/images/batches/"):
		// 已创建任务按 user/api_key 归属管理，不重新要求当前候选具备生成能力。
		return input, false, nil
	case strings.Contains(path, "/messages"):
		input.Capability = service.APIKeyRoutingCapabilityMessages
	case strings.Contains(path, "/images/batches"):
		input.Capability = service.APIKeyRoutingCapabilityBatchImage
	case strings.Contains(path, "/images/"):
		input.Capability = service.APIKeyRoutingCapabilityImage
	case strings.Contains(path, "/videos/"):
		input.Capability = service.APIKeyRoutingCapabilityVideo
	}

	var body []byte
	mediaType, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if c.Request.Body != nil && !strings.HasPrefix(mediaType, "multipart/") {
		var err error
		body, err = io.ReadAll(c.Request.Body)
		if err != nil {
			return input, false, err
		}
		c.Request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	input.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	input.MediaSize = apiKeyRoutingMediaSize(body)
	if input.Capability == service.APIKeyRoutingCapabilityText && service.IsImageGenerationIntent(path, input.Model, body) {
		input.Capability = service.APIKeyRoutingCapabilityImage
	}
	if modelAction := strings.TrimPrefix(c.Param("modelAction"), "/"); modelAction != "" {
		if model, _, err := parseGeminiModelAction(modelAction); err == nil {
			input.Model = model
		}
	}
	input.SessionKey = firstNonEmptyRoutingSessionKey(c, body)
	if previous := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()); previous != "" {
		input.SessionKey = previous
		input.PreserveSession = true
	}
	if conversation := strings.TrimSpace(gjson.GetBytes(body, "conversation").String()); conversation != "" {
		input.PreserveSession = true
	}
	applyAPIKeyRoutingAccountRequirements(c, body, &input)
	return input, true, nil
}

func apiKeyRoutingCompositeEndpoint(path string) string {
	switch {
	case strings.Contains(path, "/messages/count_tokens"):
		return service.CompositeRouteEndpointCountTokens
	case strings.Contains(path, "/messages"):
		return service.CompositeRouteEndpointMessages
	case strings.Contains(path, "/responses"):
		return service.CompositeRouteEndpointResponses
	case strings.Contains(path, "/chat/completions"):
		return service.CompositeRouteEndpointChatCompletions
	case strings.Contains(path, "/embeddings"):
		return service.CompositeRouteEndpointEmbeddings
	case strings.Contains(path, "/images/"):
		return service.CompositeRouteEndpointImages
	case strings.Contains(path, "/v1beta/"), strings.Contains(path, "/v1/models/"):
		return service.CompositeRouteEndpointGemini
	default:
		return service.CompositeRouteEndpointAny
	}
}

func applyAPIKeyRoutingAccountRequirements(c *gin.Context, body []byte, input *service.APIKeyRoutingResolveInput) {
	if c == nil || input == nil {
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		return
	}
	platform := apiKey.RoutingPlatformValue()
	if platform != service.PlatformOpenAI && platform != service.PlatformGrok {
		return
	}

	path := c.Request.URL.Path
	switch {
	case strings.Contains(path, "/embeddings"):
		input.RequiredEndpointCapability = service.OpenAIEndpointCapabilityEmbeddings
	case strings.Contains(path, "/images/"):
		if platform == service.PlatformOpenAI {
			input.RequiredImageCapability = service.OpenAIImagesCapabilityBasic
		}
	case strings.Contains(path, "/responses"):
		needsResponses := service.IsOpenAIResponsesCompactPath(c) ||
			(isBareOpenAIResponsesPath(c) && service.HasCompactionTriggerInInput(body))
		input.RequiredEndpointCapability = openAIResponsesRequiredCapabilityForRequest(
			input.Capability == service.APIKeyRoutingCapabilityImage,
			needsResponses,
			platform,
		)
	case strings.Contains(path, "/chat/completions"), strings.Contains(path, "/messages"):
		input.RequiredEndpointCapability = service.OpenAIEndpointCapabilityChatCompletions
	}
}

func apiKeyRoutingMediaSize(body []byte) string {
	return firstNonEmptyGJSONValue(
		body,
		"image_size",
		"size",
		"resolution",
		"generationConfig.imageConfig.imageSize",
		"generation_config.image_config.image_size",
		`tools.#(type=="image_generation").size`,
	)
}

func firstNonEmptyGJSONValue(body []byte, paths ...string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyRoutingSessionKey(c *gin.Context, body []byte) string {
	values := []string{
		strings.TrimSpace(c.GetHeader("x-claude-code-session-id")),
		strings.TrimSpace(c.GetHeader("x-session-id")),
		strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String()),
		strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()),
		strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()),
		strings.TrimSpace(gjson.GetBytes(body, "conversation_id").String()),
	}
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	anchor := gjson.GetBytes(body, "system").Raw
	if anchor == "" {
		messages := gjson.GetBytes(body, "messages")
		if messages.IsArray() {
			for _, message := range messages.Array() {
				if strings.EqualFold(message.Get("role").String(), "system") {
					anchor = message.Get("content").Raw
					break
				}
			}
		}
	}
	if anchor == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(anchor))
	return "system:" + hex.EncodeToString(hash[:])
}

func setEffectiveAPIKeyRoutingGroup(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil || apiKey.Group == nil {
		return
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	middleware2.SetOpsFallbackAPIKey(c, apiKey)
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, apiKey.Group)
	ctx = context.WithValue(ctx, ctxkey.APIKeyRoutingAPIKeyID, apiKey.ID)
	c.Request = c.Request.WithContext(ctx)
}

func apiKeyRoutingObservedLatencyMs(c *gin.Context, elapsed time.Duration) *int64 {
	if c != nil {
		for _, key := range []string{service.OpsTimeToFirstTokenMsKey, service.OpsUpstreamLatencyMsKey} {
			value, exists := c.Get(key)
			if !exists {
				continue
			}
			var latency int64
			switch typed := value.(type) {
			case int:
				latency = int64(typed)
			case int64:
				latency = typed
			default:
				continue
			}
			if latency >= 0 {
				return &latency
			}
		}
	}
	latency := elapsed.Milliseconds()
	if latency < 0 {
		latency = 0
	}
	return &latency
}

func writeAPIKeyRoutingError(c *gin.Context, googleErrors bool, status int, code, message string) {
	if googleErrors {
		c.AbortWithStatusJSON(status, gin.H{
			"error": gin.H{"code": status, "message": message, "status": "UNAVAILABLE"},
		})
		return
	}
	middleware2.AbortWithError(c, status, code, message)
}
