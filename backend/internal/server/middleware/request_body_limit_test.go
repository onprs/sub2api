package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitOneMiBBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const limit = int64(1 << 20)
	router := gin.New()
	router.Use(RequestBodyLimit(limit))
	router.POST("/body", func(c *gin.Context) {
		_, err := io.Copy(io.Discard, c.Request.Body)
		if err == nil {
			c.Status(http.StatusNoContent)
			return
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusBadRequest)
	})

	tests := []struct {
		name       string
		size       int
		wantStatus int
	}{
		{name: "exactly at limit", size: int(limit), wantStatus: http.StatusNoContent},
		{name: "one byte over limit", size: int(limit) + 1, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/body",
				bytes.NewReader(make([]byte, test.size)),
			)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}
