package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// ZCode 是智谱账号能力；所有入口继承原有管理员鉴权。
func registerZCodeRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	group := admin.Group("/zhipu/oauth")
	group.POST("/start", h.Admin.Account.ZCodeLogin)
	group.POST("/poll", h.Admin.Account.ZCodePoll)
	accounts := admin.Group("/accounts/:id/zcode")
	accounts.GET("/status", h.Admin.Account.ZCodeStatus)
	accounts.GET("/quota", h.Admin.Account.ZCodeQuota)
	accounts.GET("/claim/preview", h.Admin.Account.ZCodePreview)
	accounts.POST("/claim", h.Admin.Account.ZCodeClaim)
}
