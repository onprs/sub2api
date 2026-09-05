package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

// DefaultAntigravityRouteModelIDs 返回聚合思考程度后的公开模型 ID。
// 历史 raw wire 和兼容 alias 仍可路由，但不再进入默认白名单或用户目录。
func DefaultAntigravityRouteModelIDs() []string {
	routes := domain.AntigravityPublicModelRoutes()
	ids := make([]string, 0, len(routes))
	for _, route := range routes {
		ids = append(ids, route.ModelID)
	}
	return ids
}
