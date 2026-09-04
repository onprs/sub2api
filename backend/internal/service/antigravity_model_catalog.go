package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

// DefaultAntigravityRouteModelIDs 返回 agy 对外公开的 15 个用户模型 ID。
// 历史 raw wire 和兼容 alias 仍可路由，但不再进入默认白名单或用户目录。
func DefaultAntigravityRouteModelIDs() []string {
	routes := domain.AntigravityUserModelRoutes()
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.ModelID)
	}
	return ids
}
