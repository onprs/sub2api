package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// 白名单按注册路由匹配；新增管理接口默认不授予只读密钥权限。
func adminReadOnlyAPIKeyAllows(c *gin.Context) bool {
	if isWebSocketUpgradeRequest(c) {
		return false
	}
	if c.Request.Method == http.MethodPost {
		return c.FullPath() == "/api/v1/admin/accounts/usage/batch"
	}
	if c.Request.Method != http.MethodGet {
		return false
	}
	switch c.FullPath() {
	case "/api/v1/admin/groups", "/api/v1/admin/groups/all", "/api/v1/admin/groups/:id",
		"/api/v1/admin/accounts", "/api/v1/admin/accounts/:id",
		"/api/v1/admin/accounts/:id/usage", "/api/v1/admin/accounts/:id/claude/reset-credits":
		return true
	default:
		return false
	}
}

func validateAdminReadOnlyAPIKey(c *gin.Context, key string, settings *service.SettingService, users *service.UserService) bool {
	valid, err := settings.ValidateAdminReadOnlyAPIKey(c.Request.Context(), key)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return false
	}
	if !valid {
		AbortWithError(c, http.StatusUnauthorized, "INVALID_ADMIN_KEY", "Invalid admin API key")
		return false
	}
	if !adminReadOnlyAPIKeyAllows(c) {
		AbortWithError(c, http.StatusForbidden, "ADMIN_READ_ONLY_FORBIDDEN", "只读密钥仅允许查询分组、账号、用量和重置次数")
		return false
	}
	admin, err := users.GetFirstAdmin(c.Request.Context())
	if err != nil || admin == nil || !admin.IsActive() || !admin.IsAdmin() {
		AbortWithError(c, http.StatusUnauthorized, "INVALID_ADMIN_KEY", "Invalid admin API key")
		return false
	}
	c.Set(string(ContextKeyUser), AuthSubject{UserID: admin.ID, Concurrency: admin.Concurrency})
	c.Set(string(ContextKeyUserRole), admin.Role)
	c.Set(ContextKeyAuthEmail, admin.Email)
	c.Set("auth_method", service.AdminReadOnlyAPIKeyAuthMethod)
	return true
}
