package handler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type accountObserverAccountDeleter interface {
	DeleteAccount(context.Context, int64) error
}

type AccountObserverHandler struct {
	service      *service.AccountObserverService
	adminService accountObserverAccountDeleter
}

func NewAccountObserverHandler(
	observerService *service.AccountObserverService,
	adminService service.AdminService,
) *AccountObserverHandler {
	return &AccountObserverHandler{service: observerService, adminService: adminService}
}

type createAccountObserverTokenRequest struct {
	Name         string     `json:"name" binding:"required"`
	AllowedCIDRs []string   `json:"allowed_cidrs"`
	ExpiresAt    *time.Time `json:"expires_at"`
	AllowDelete  bool       `json:"allow_delete"`
}

func (h *AccountObserverHandler) CreateToken(c *gin.Context) {
	var request createAccountObserverTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	created, err := h.service.CreateToken(c.Request.Context(), service.CreateAccountObserverTokenInput{
		Name: request.Name, AllowedCIDRs: request.AllowedCIDRs,
		ExpiresAt: request.ExpiresAt, AllowDelete: request.AllowDelete,
	})
	if err != nil {
		if errors.Is(err, service.ErrAccountObserverInvalidInput) {
			response.BadRequest(c, err.Error())
		} else {
			response.InternalError(c, "Failed to create integration token")
		}
		return
	}
	response.Created(c, created)
}

func (h *AccountObserverHandler) ListTokens(c *gin.Context) {
	tokens, err := h.service.ListTokens(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to list integration tokens")
		return
	}
	response.Success(c, tokens)
}

func (h *AccountObserverHandler) RevokeToken(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid integration token ID")
		return
	}
	if err := h.service.RevokeToken(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrAccountObserverNotFound) {
			response.NotFound(c, "Integration token not found")
			return
		}
		response.InternalError(c, "Failed to revoke integration token")
		return
	}
	response.Success(c, gin.H{"revoked": true})
}

func (h *AccountObserverHandler) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(authorization, "Bearer ") {
			response.Unauthorized(c, "Invalid integration token")
			c.Abort()
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
		remoteIP := net.ParseIP(c.ClientIP())
		scope, err := h.service.Authenticate(c.Request.Context(), token, remoteIP)
		if err != nil {
			if errors.Is(err, service.ErrAccountObserverForbidden) {
				response.Forbidden(c, "Integration token is not allowed from this address")
			} else if errors.Is(err, service.ErrAccountObserverUnauthorized) {
				response.Unauthorized(c, "Invalid integration token")
			} else {
				response.InternalError(c, "Integration authentication failed")
			}
			c.Abort()
			return
		}
		c.Set("integration_scope", scope)
		c.Header("X-Integration-Scope", scope)
		c.Next()
	}
}

func (h *AccountObserverHandler) GetAccounts(c *gin.Context) {
	var updatedSince *time.Time
	if raw := strings.TrimSpace(c.Query("updated_since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.BadRequest(c, "Invalid updated_since")
			return
		}
		utc := parsed.UTC()
		updatedSince = &utc
	}
	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid limit")
			return
		}
		limit = parsed
	}

	page, err := h.service.ListAccounts(c.Request.Context(), service.ObserverListParams{
		Cursor: c.Query("cursor"), UpdatedSince: updatedSince, Limit: limit,
	})
	if err != nil {
		if errors.Is(err, service.ErrAccountObserverInvalidCursor) {
			response.BadRequest(c, "Invalid cursor")
			return
		}
		response.InternalError(c, "Failed to list observed accounts")
		return
	}
	c.Header("Cache-Control", "private, no-cache")
	c.Header("ETag", page.ETag)
	if c.GetHeader("If-None-Match") == page.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *AccountObserverHandler) DeleteAccount(c *gin.Context) {
	scope, _ := c.Get("integration_scope")
	if !service.AccountObserverScopeAllows(fmt.Sprint(scope), service.AccountObserverDeleteScope) {
		response.Forbidden(c, "Integration token cannot delete accounts")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if err := h.adminService.DeleteAccount(c.Request.Context(), accountID); err != nil &&
		!errors.Is(err, service.ErrAccountNotFound) {
		response.ErrorFrom(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AccountObserverHandler) RejectWrite(c *gin.Context) {
	response.Forbidden(c, "Account observer token does not allow this operation")
}
