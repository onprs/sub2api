package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetAdminReadOnlyAPIKey 查询脱敏密钥状态。
func (h *SettingHandler) GetAdminReadOnlyAPIKey(c *gin.Context) {
	masked, exists, err := h.settingService.GetAdminReadOnlyAPIKeyStatus(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"exists": exists, "masked_key": masked})
}

// RegenerateAdminReadOnlyAPIKey 完整密钥仅在本次生成响应中返回。
func (h *SettingHandler) RegenerateAdminReadOnlyAPIKey(c *gin.Context) {
	key, err := h.settingService.GenerateAdminReadOnlyAPIKey(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"key": key})
}

func (h *SettingHandler) DeleteAdminReadOnlyAPIKey(c *gin.Context) {
	if err := h.settingService.DeleteAdminReadOnlyAPIKey(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "只读 API Key 已删除"})
}
