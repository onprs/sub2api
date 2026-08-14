package geminicli

import "strings"

// Model 表示管理界面和账户测试可选择的 Gemini 模型。
// JSON 字段需与现有前端约定保持一致。
type Model struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

// AIStudioFreeModels 是 Google AI Studio Free Tier 模型目录，快照日期：2026-08-14。
// ID 是 Gemini generateContent API 接受的实际值，不包含 models/ 前缀。
var AIStudioFreeModels = []Model{
	{ID: "gemini-3-flash-preview", Type: "model", DisplayName: "Gemini 3 Flash", CreatedAt: ""},
	{ID: "gemini-2.5-flash", Type: "model", DisplayName: "Gemini 2.5 Flash", CreatedAt: ""},
	{ID: "gemini-2.5-flash-lite", Type: "model", DisplayName: "Gemini 2.5 Flash-Lite", CreatedAt: ""},
	{ID: "gemini-3.1-flash-lite", Type: "model", DisplayName: "Gemini 3.1 Flash-Lite", CreatedAt: ""},
	{ID: "gemini-3.5-flash", Type: "model", DisplayName: "Gemini 3.5 Flash", CreatedAt: ""},
	{ID: "gemini-3.5-flash-lite", Type: "model", DisplayName: "Gemini 3.5 Flash-Lite", CreatedAt: ""},
	{ID: "gemini-3.6-flash", Type: "model", DisplayName: "Gemini 3.6 Flash", CreatedAt: ""},
	{ID: "gemini-3.7-flash", Type: "model", DisplayName: "Gemini 3.7 Flash", CreatedAt: ""},
	{ID: "gemma-4-26b-a4b-it", Type: "model", DisplayName: "Gemma 4 26B", CreatedAt: ""},
	{ID: "gemma-4-31b-it", Type: "model", DisplayName: "Gemma 4 31B", CreatedAt: ""},
}

// DefaultModels 保留非目标账户原有的 OAuth、付费和 Vertex 通用兜底目录。
var DefaultModels = []Model{
	{ID: "gemini-2.0-flash", Type: "model", DisplayName: "Gemini 2.0 Flash", CreatedAt: ""},
	{ID: "gemini-2.5-flash", Type: "model", DisplayName: "Gemini 2.5 Flash", CreatedAt: ""},
	{ID: "gemini-2.5-flash-image", Type: "model", DisplayName: "Gemini 2.5 Flash Image", CreatedAt: ""},
	{ID: "gemini-2.5-pro", Type: "model", DisplayName: "Gemini 2.5 Pro", CreatedAt: ""},
	{ID: "gemini-3.5-flash", Type: "model", DisplayName: "Gemini 3.5 Flash", CreatedAt: ""},
	{ID: "gemini-3-flash-preview", Type: "model", DisplayName: "Gemini 3 Flash Preview", CreatedAt: ""},
	{ID: "gemini-3-pro-preview", Type: "model", DisplayName: "Gemini 3 Pro Preview", CreatedAt: ""},
	{ID: "gemini-3.1-pro-preview", Type: "model", DisplayName: "Gemini 3.1 Pro Preview", CreatedAt: ""},
	{ID: "gemini-3.1-flash-image", Type: "model", DisplayName: "Gemini 3.1 Flash Image", CreatedAt: ""},
}

// ModelsForAIStudioTier 对规范值和历史 free 值返回严格的 Free Tier 目录。
// 未保存 tier 的 API Key 账户按历史 Free Tier 处理；其他非空未知值保留扩展兜底目录。
func ModelsForAIStudioTier(tierID string) []Model {
	switch strings.ToLower(strings.TrimSpace(tierID)) {
	case "", "free", "aistudio_free":
		return AIStudioFreeModels
	default:
		return DefaultModels
	}
}

// DefaultModelForAIStudioTier 返回该等级未指定测试模型时使用的请求 ID。
func DefaultModelForAIStudioTier(tierID string) string {
	models := ModelsForAIStudioTier(tierID)
	if len(models) == 0 {
		return DefaultTestModel
	}
	return models[0].ID
}

// DefaultTestModel 是非 Free Tier 账户测试未指定模型时使用的原有默认模型。
const DefaultTestModel = "gemini-2.0-flash"
