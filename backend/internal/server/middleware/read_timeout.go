package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ReadTimeout bounds request-body reads without imposing a timeout on the
// handler or response. This keeps long-lived gateway responses unaffected when
// the middleware is applied only to ordinary API route groups.
func ReadTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if timeout <= 0 {
			c.Next()
			return
		}

		controller := http.NewResponseController(c.Writer)
		deadlineSet := controller.SetReadDeadline(time.Now().Add(timeout)) == nil
		if deadlineSet {
			defer func() {
				_ = controller.SetReadDeadline(time.Time{})
			}()
		}

		// Request.Body must support concurrent Read and Close. Closing it is the
		// fallback for response writers that do not expose SetReadDeadline.
		if c.Request.Body != nil {
			body := c.Request.Body
			timer := time.AfterFunc(timeout, func() {
				_ = body.Close()
			})
			defer timer.Stop()
		}

		c.Next()
	}
}
