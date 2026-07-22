package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStandardGatewayForwardErrorAlreadyCommunicated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("structured client error", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		before := c.Writer.Size()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model"})

		require.True(t, standardGatewayForwardErrorAlreadyCommunicated(c, before))
	})

	t.Run("partial success stream still needs terminal error", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		before := c.Writer.Size()
		c.Status(http.StatusOK)
		_, err := c.Writer.WriteString("data: {}\n\n")
		require.NoError(t, err)

		require.False(t, standardGatewayForwardErrorAlreadyCommunicated(c, before))
	})

	t.Run("no response still needs fallback", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)

		require.False(t, standardGatewayForwardErrorAlreadyCommunicated(c, c.Writer.Size()))
	})
}
