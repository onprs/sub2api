package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

const publicRequestBodyLimit int64 = 1 << 20

func usePublicRequestGuards(group *gin.RouterGroup, readTimeout time.Duration) {
	group.Use(middleware.ReadTimeout(readTimeout))
	group.Use(middleware.RequestBodyLimit(publicRequestBodyLimit))
}
