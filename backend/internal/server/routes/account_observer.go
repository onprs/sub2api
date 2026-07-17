package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterAccountObserverRoutes(v1 *gin.RouterGroup, h *handler.Handlers) {
	observer := v1.Group("/integrations/account-observer")
	observer.Use(h.AccountObserver.Authenticate())
	{
		observer.GET("/v1/accounts", h.AccountObserver.GetAccounts)
		observer.POST("/*path", h.AccountObserver.RejectWrite)
		observer.PUT("/*path", h.AccountObserver.RejectWrite)
		observer.PATCH("/*path", h.AccountObserver.RejectWrite)
		observer.DELETE("/*path", h.AccountObserver.RejectWrite)
	}
}
