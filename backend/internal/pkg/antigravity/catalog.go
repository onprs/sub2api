package antigravity

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	CatalogSourceLive     = "live"
	CatalogSourceFallback = "fallback"
	CatalogSourceUpstream = "upstream"
	CatalogSourceMapping  = "mapping"
)

// CatalogModel 表示 agy 用户目录项，并显式保留与 raw 目录和请求路由的边界。
type CatalogModel struct {
	ID               string         `json:"id"`
	CatalogID        string         `json:"catalog_id,omitempty"`
	Type             string         `json:"type"`
	DisplayName      string         `json:"display_name"`
	CreatedAt        string         `json:"created_at"`
	WireModel        string         `json:"wire_model,omitempty"`
	InternalModel    string         `json:"internal_model,omitempty"`
	ResponseModel    string         `json:"response_model,omitempty"`
	BackendModel     string         `json:"backend_model,omitempty"`
	ThinkingBudget   *int           `json:"thinking_budget,omitempty"`
	Source           string         `json:"source,omitempty"`
	ReasoningEfforts []string       `json:"reasoning_efforts,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// FallbackCatalogModels 返回聚合思考程度后的用户目录。内部指纹不能用作 wire model。
func FallbackCatalogModels() []CatalogModel {
	routes := domain.AntigravityUserModelRoutes()
	models := make([]CatalogModel, 0, len(routes))
	for _, route := range routes {
		catalogID := ""
		if len(route.CatalogIDs) > 0 {
			catalogID = route.CatalogIDs[0]
		}
		models = append(models, catalogModelFromRoute(route, catalogID, route.InternalModel, nil, CatalogSourceFallback))
	}
	return aggregateCatalogReasoning(models)
}

// CatalogModelsFromResponse 使用 raw 目录的可用性证据生成 agy 用户目录。
// raw models 和 agentModelSorts 都不是用户菜单；具体档位聚合成一个公开模型。
func CatalogModelsFromResponse(response *FetchAvailableModelsResponse, raw map[string]any) []CatalogModel {
	if response == nil || len(response.Models) == 0 {
		return nil
	}

	rawModels, _ := raw["models"].(map[string]any)
	routes := domain.AntigravityUserModelRoutes()
	models := make([]CatalogModel, 0, len(routes))
	for _, route := range routes {
		catalogID, info, ok := findRouteCatalogEntry(response, route)
		if !ok || !routeTierIsAvailable(response, route, catalogID) {
			continue
		}

		internalModel := route.InternalModel
		// 仅同一物理 wire 的 raw 指纹可覆盖快照；tiered 入口和废弃别名不能冒充具体档位。
		if catalogID == route.WireModel && strings.TrimSpace(info.Model) != "" {
			internalModel = info.Model
		}
		metadata, _ := rawModels[catalogID].(map[string]any)
		models = append(models, catalogModelFromRoute(route, catalogID, internalModel, metadata, CatalogSourceLive))
	}
	return aggregateCatalogReasoning(models)
}

func aggregateCatalogReasoning(models []CatalogModel) []CatalogModel {
	result := make([]CatalogModel, 0, len(models))
	indices := make(map[string]int)
	for _, model := range models {
		base, effort := domain.AntigravityReasoningModel(model.ID)
		if base == "" {
			result = append(result, model)
			continue
		}
		if index, exists := indices[base]; exists {
			result[index].ReasoningEfforts = append(result[index].ReasoningEfforts, effort)
			continue
		}
		indices[base] = len(result)
		model.ID = base
		model.DisplayName = strings.Split(model.DisplayName, " (")[0]
		model.WireModel = ""
		model.InternalModel = ""
		model.ThinkingBudget = nil
		model.ResponseModel = base
		model.ReasoningEfforts = []string{effort}
		result = append(result, model)
	}
	return result
}

// RawCatalogModelsFromResponse 原样保留 fetchAvailableModels 的 raw 数据层。
// opaque、辅助和未来 ID 不进入公开菜单，但也不会在解析阶段丢失。
func RawCatalogModelsFromResponse(response *FetchAvailableModelsResponse, raw map[string]any) []CatalogModel {
	if response == nil || len(response.Models) == 0 {
		return nil
	}

	ids := make([]string, 0, len(response.Models))
	for id := range response.Models {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	rawModels, _ := raw["models"].(map[string]any)
	models := make([]CatalogModel, 0, len(ids))
	for _, id := range ids {
		info := response.Models[id]
		metadata, _ := rawModels[id].(map[string]any)
		models = append(models, CatalogModel{
			ID:            id,
			CatalogID:     id,
			Type:          "model",
			DisplayName:   info.DisplayName,
			InternalModel: info.Model,
			Source:        CatalogSourceLive,
			Metadata:      cloneCatalogMetadata(metadata),
		})
	}
	return models
}

func catalogModelFromRoute(route domain.AntigravityModelRoute, catalogID, internalModel string, metadata map[string]any, source string) CatalogModel {
	model := CatalogModel{
		ID:            route.ModelID,
		CatalogID:     catalogID,
		Type:          "model",
		DisplayName:   route.DisplayName,
		WireModel:     route.WireModel,
		InternalModel: internalModel,
		ResponseModel: route.ResponseModel,
		BackendModel:  route.BackendModel,
		Source:        source,
		Metadata:      cloneCatalogMetadata(metadata),
	}
	if route.HasThinkingBudget {
		budget := route.ThinkingBudget
		model.ThinkingBudget = &budget
	}
	return model
}

func findRouteCatalogEntry(response *FetchAvailableModelsResponse, route domain.AntigravityModelRoute) (string, ModelInfo, bool) {
	for _, catalogID := range route.CatalogIDs {
		if info, ok := response.Models[catalogID]; ok {
			return catalogID, info, true
		}
	}
	for _, catalogID := range route.CatalogIDs {
		deprecated, ok := response.DeprecatedModelIDs[catalogID]
		if !ok {
			continue
		}
		newModelID := strings.TrimSpace(deprecated.NewModelID)
		if info, exists := response.Models[newModelID]; exists {
			return newModelID, info, true
		}
	}
	return "", ModelInfo{}, false
}

func routeTierIsAvailable(response *FetchAvailableModelsResponse, route domain.AntigravityModelRoute, catalogID string) bool {
	if route.TierGroup == "" || len(response.TieredModelIDs) == 0 {
		return true
	}
	ids, exists := response.TieredModelIDs[route.TierGroup]
	if !exists {
		return false
	}
	for _, id := range ids {
		if id == catalogID {
			return true
		}
		for _, candidate := range route.CatalogIDs {
			if id == candidate {
				return true
			}
		}
	}
	return false
}

func cloneCatalogMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
