package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountObserverRoutesIncludeScopedDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	v1 := engine.Group("/api/v1")
	handlers := &handler.Handlers{AccountObserver: &handler.AccountObserverHandler{}}

	require.NotPanics(t, func() {
		RegisterAccountObserverRoutes(v1, handlers)
	})

	registered := false
	for _, route := range engine.Routes() {
		if route.Method == "DELETE" &&
			route.Path == "/api/v1/integrations/account-observer/v1/accounts/:id" {
			registered = true
			break
		}
	}
	require.True(t, registered)
}
