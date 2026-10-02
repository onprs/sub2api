package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func isAdminReadOnlyAPIKey(c *gin.Context) bool {
	return c.GetString("auth_method") == service.AdminReadOnlyAPIKeyAuthMethod
}

func adminReadOnlyGroups(groups []service.Group, simpleMode bool) []dto.AdminReadOnlyGroup {
	result := make([]dto.AdminReadOnlyGroup, 0, len(groups))
	for i := range groups {
		if simpleMode && !service.IsGroupBindableInSimpleMode(&groups[i]) {
			continue
		}
		result = append(result, *dto.AdminReadOnlyGroupFromService(&groups[i]))
	}
	return result
}
