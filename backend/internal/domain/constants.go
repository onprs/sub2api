package domain

import "strings"

// Status constants
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusError    = "error"
	StatusUnused   = "unused"
	StatusUsed     = "used"
	StatusExpired  = "expired"
)

// Role constants
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Platform constants
const (
	PlatformAnthropic   = "anthropic"
	PlatformOpenAI      = "openai"
	PlatformOpenCodeGo  = "opencode_go"
	PlatformClinePass   = "clinepass"
	PlatformOpenRouter  = "openrouter"
	PlatformCommandCode = "commandcode"
	PlatformGemini      = "gemini"
	PlatformAntigravity = "antigravity"
	PlatformGrok        = "grok"
	PlatformComposite   = "composite"
)

// Account type constants
const (
	AccountTypeOAuth          = "oauth"           // OAuth类型账号（full scope: profile + inference）
	AccountTypeSetupToken     = "setup-token"     // Setup Token类型账号（inference only scope）
	AccountTypeAPIKey         = "apikey"          // API Key类型账号
	AccountTypeUpstream       = "upstream"        // 上游透传类型账号（通过 Base URL + API Key 连接上游）
	AccountTypeBedrock        = "bedrock"         // AWS Bedrock 类型账号（通过 SigV4 签名或 API Key 连接 Bedrock，由 credentials.auth_mode 区分）
	AccountTypeServiceAccount = "service_account" // Google Service Account 类型账号（用于 Vertex AI）
)

// Redeem type constants
const (
	RedeemTypeBalance      = "balance"
	RedeemTypeConcurrency  = "concurrency"
	RedeemTypeSubscription = "subscription"
	RedeemTypeInvitation   = "invitation"
)

// PromoCode status constants
const (
	PromoCodeStatusActive   = "active"
	PromoCodeStatusDisabled = "disabled"
)

// Admin adjustment type constants
const (
	AdjustmentTypeAdminBalance     = "admin_balance"     // 管理员调整余额
	AdjustmentTypeAdminConcurrency = "admin_concurrency" // 管理员调整并发数
)

// Group subscription type constants
const (
	SubscriptionTypeStandard     = "standard"     // 标准计费模式（按余额扣费）
	SubscriptionTypeSubscription = "subscription" // 订阅模式（按限额控制）
)

// Subscription status constants
const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusSuspended = "suspended"
)

// AntigravityGemini31ProAgentModel 是 Gemini 3.1 Pro High 的历史 Cloud Code wire ID。
const AntigravityGemini31ProAgentModel = "gemini-pro-agent"

// AntigravityModelRoute 将 agy 用户模型与 Cloud Code 目录、wire 和诊断指纹分开。
// InternalModel 只用于观测，MODEL_PLACEHOLDER_* 可能随服务端版本漂移，绝不能作为请求 model。
type AntigravityModelRoute struct {
	ModelID           string
	DisplayName       string
	CatalogIDs        []string
	WireModel         string
	InternalModel     string
	ResponseModel     string
	BackendModel      string
	ThinkingBudget    int
	HasThinkingBudget bool
	TierGroup         string
}

var antigravityUserModelRoutes = []AntigravityModelRoute{
	{ModelID: "gemini-3.7-flash-high", DisplayName: "Gemini 3.7 Flash (High)", CatalogIDs: []string{"gemini-3.7-flash-tiered"}, WireModel: "gemini-3.7-flash-high", InternalModel: "MODEL_PLACEHOLDER_M298", ResponseModel: "gemini-3.7-flash", ThinkingBudget: -1, HasThinkingBudget: true, TierGroup: "flash"},
	{ModelID: "gemini-3.7-flash-medium", DisplayName: "Gemini 3.7 Flash (Medium)", CatalogIDs: []string{"gemini-3.7-flash-tiered"}, WireModel: "gemini-3.7-flash-medium", InternalModel: "MODEL_PLACEHOLDER_M299", ResponseModel: "gemini-3.7-flash", ThinkingBudget: 4000, HasThinkingBudget: true, TierGroup: "flash"},
	{ModelID: "gemini-3.7-flash-low", DisplayName: "Gemini 3.7 Flash (Low)", CatalogIDs: []string{"gemini-3.7-flash-tiered"}, WireModel: "gemini-3.7-flash-low", InternalModel: "MODEL_PLACEHOLDER_M300", ResponseModel: "gemini-3.7-flash", ThinkingBudget: 1000, HasThinkingBudget: true, TierGroup: "flash"},
	{ModelID: "gemini-3.6-flash-high", DisplayName: "Gemini 3.6 Flash (High)", CatalogIDs: []string{"gemini-3.6-flash-high"}, WireModel: "gemini-3.6-flash-high", InternalModel: "MODEL_PLACEHOLDER_M71", ThinkingBudget: -1, HasThinkingBudget: true},
	{ModelID: "gemini-3.6-flash-medium", DisplayName: "Gemini 3.6 Flash (Medium)", CatalogIDs: []string{"gemini-3.6-flash-medium"}, WireModel: "gemini-3.6-flash-medium", InternalModel: "MODEL_PLACEHOLDER_M72", ThinkingBudget: 4000, HasThinkingBudget: true},
	{ModelID: "gemini-3.6-flash-low", DisplayName: "Gemini 3.6 Flash (Low)", CatalogIDs: []string{"gemini-3.6-flash-low"}, WireModel: "gemini-3.6-flash-low", InternalModel: "MODEL_PLACEHOLDER_M73", ThinkingBudget: 1000, HasThinkingBudget: true},
	{ModelID: "gemini-3.5-flash-high", DisplayName: "Gemini 3.5 Flash (High)", CatalogIDs: []string{"gemini-3-flash-agent"}, WireModel: "gemini-3-flash-agent", InternalModel: "MODEL_PLACEHOLDER_M84", ResponseModel: "gemini-default", ThinkingBudget: -1, HasThinkingBudget: true},
	{ModelID: "gemini-3.5-flash-medium", DisplayName: "Gemini 3.5 Flash (Medium)", CatalogIDs: []string{"gemini-3.5-flash-low"}, WireModel: "gemini-3.5-flash-low", InternalModel: "MODEL_PLACEHOLDER_M20", ThinkingBudget: 4000, HasThinkingBudget: true},
	{ModelID: "gemini-3.5-flash-low", DisplayName: "Gemini 3.5 Flash (Low)", CatalogIDs: []string{"gemini-3.5-flash-extra-low"}, WireModel: "gemini-3.5-flash-extra-low", InternalModel: "MODEL_PLACEHOLDER_M187", ThinkingBudget: 1000, HasThinkingBudget: true},
	{ModelID: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro (High)", CatalogIDs: []string{"gemini-pro-agent", "gemini-3.1-pro-high"}, WireModel: AntigravityGemini31ProAgentModel, InternalModel: "MODEL_PLACEHOLDER_M16", ResponseModel: "gemini-pro-default", ThinkingBudget: 10001, HasThinkingBudget: true},
	{ModelID: "gemini-3.1-pro-low", DisplayName: "Gemini 3.1 Pro (Low)", CatalogIDs: []string{"gemini-3.1-pro-low"}, WireModel: "gemini-3.1-pro-low", InternalModel: "MODEL_PLACEHOLDER_M36", ThinkingBudget: 1001, HasThinkingBudget: true},
	{ModelID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6 (Thinking)", CatalogIDs: []string{"claude-sonnet-4-6"}, WireModel: "claude-sonnet-4-6", InternalModel: "MODEL_PLACEHOLDER_M35", BackendModel: "claude-sonnet-4-6@default", ThinkingBudget: 1024, HasThinkingBudget: true},
	{ModelID: "claude-opus-4-6-thinking", DisplayName: "Claude Opus 4.6 (Thinking)", CatalogIDs: []string{"claude-opus-4-6-thinking"}, WireModel: "claude-opus-4-6-thinking", InternalModel: "MODEL_PLACEHOLDER_M26", BackendModel: "claude-opus-4-6@default", ThinkingBudget: 1024, HasThinkingBudget: true},
	{ModelID: "gpt-oss-120b-medium", DisplayName: "GPT-OSS 120B (Medium)", CatalogIDs: []string{"gpt-oss-120b-medium"}, WireModel: "gpt-oss-120b-medium", InternalModel: "MODEL_OPENAI_GPT_OSS_120B_MEDIUM", BackendModel: "openai/gpt-oss-120b-maas", ThinkingBudget: 8192, HasThinkingBudget: true},
}

// AntigravityUserModelRoutes 返回 agy 用户目录定义的深拷贝。
func AntigravityUserModelRoutes() []AntigravityModelRoute {
	routes := make([]AntigravityModelRoute, len(antigravityUserModelRoutes))
	for i, route := range antigravityUserModelRoutes {
		route.CatalogIDs = append([]string(nil), route.CatalogIDs...)
		routes[i] = route
	}
	return routes
}

var antigravityCompatibilityModelMapping = map[string]string{
	// Claude 兼容别名。
	"claude-fable-5":             "claude-fable-5",
	"claude-opus-4-8":            "claude-opus-4-8",
	"claude-opus-4-7":            "claude-opus-4-7",
	"claude-opus-4-6":            "claude-opus-4-6-thinking",
	"claude-opus-4-5-thinking":   "claude-opus-4-6-thinking",
	"claude-sonnet-4-5":          "claude-sonnet-4-5",
	"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
	"claude-opus-4-5-20251101":   "claude-opus-4-6-thinking",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-5",
	"claude-haiku-4-5":           "claude-sonnet-4-6",
	"claude-haiku-4-5-20251001":  "claude-sonnet-4-6",

	// 旧 Gemini、图片和辅助模型仍可作为隐藏兼容输入。
	"gemini-2.5-flash":               "gemini-2.5-flash",
	"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
	"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
	"gemini-2.5-flash-lite":          "gemini-2.5-flash-lite",
	"gemini-2.5-flash-thinking":      "gemini-2.5-flash-thinking",
	"gemini-2.5-pro":                 "gemini-2.5-pro",
	"gemini-3-flash":                 "gemini-3-flash",
	"gemini-3-flash-preview":         "gemini-3-flash",
	"gemini-3-pro-high":              "gemini-3-pro-high",
	"gemini-3-pro-low":               "gemini-3-pro-low",
	"gemini-3-pro-preview":           "gemini-3-pro-high",
	AntigravityGemini31ProAgentModel: AntigravityGemini31ProAgentModel,
	"gemini-3.1-pro":                 AntigravityGemini31ProAgentModel,
	"gemini-3.1-pro-preview":         AntigravityGemini31ProAgentModel,
	"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
	"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
	"gemini-3.1-flash-lite":          "gemini-3.1-flash-lite",
	"gemini-3.6-flash-tiered":        "gemini-3.6-flash-tiered",
	"gemini-3.7-flash-tiered":        "gemini-3.7-flash-tiered",
	"gemini-3-flash-agent":           "gemini-3-flash-agent",
	"gemini-3.5-flash-extra-low":     "gemini-3.5-flash-extra-low",
	"gemini-3-pro-image":             "gemini-3.1-flash-image",
	"gemini-3-pro-image-preview":     "gemini-3.1-flash-image",
	"tab_flash_lite_preview":         "tab_flash_lite_preview",
}

// DefaultAntigravityModelMapping 是官方 Cloud Code OAuth/Setup Token 账号的内置路由表。
// 14 个公开用户 ID 覆盖同名历史 raw key；例如 gemini-3.5-flash-low 现在明确表示 Low。
var DefaultAntigravityModelMapping = buildDefaultAntigravityModelMapping()

func buildDefaultAntigravityModelMapping() map[string]string {
	mapping := make(map[string]string, len(antigravityCompatibilityModelMapping)+len(antigravityUserModelRoutes)*2)
	for model, wireModel := range antigravityCompatibilityModelMapping {
		mapping[model] = wireModel
	}
	for _, route := range antigravityUserModelRoutes {
		for _, catalogID := range route.CatalogIDs {
			if _, exists := mapping[catalogID]; !exists {
				mapping[catalogID] = catalogID
			}
		}
	}
	// 用户目录契约优先，解决 3.5 Low 与历史 Medium raw key 的同名冲突。
	for _, route := range antigravityUserModelRoutes {
		mapping[route.ModelID] = route.WireModel
	}
	return mapping
}

// ResolveDefaultAntigravityModelRoute 返回官方路由的结构化结果。
func ResolveDefaultAntigravityModelRoute(model string) (AntigravityModelRoute, bool) {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if model == "" {
		return AntigravityModelRoute{}, false
	}
	for _, route := range antigravityUserModelRoutes {
		if route.ModelID == model {
			return route, true
		}
	}
	wireModel, ok := DefaultAntigravityModelMapping[model]
	if !ok || strings.TrimSpace(wireModel) == "" {
		return AntigravityModelRoute{}, false
	}
	for _, route := range antigravityUserModelRoutes {
		if route.ModelID == wireModel || route.WireModel == wireModel {
			return route, true
		}
	}
	return AntigravityModelRoute{ModelID: wireModel, WireModel: wireModel}, true
}

// DefaultBedrockModelMapping 是 AWS Bedrock 平台的默认模型映射
// 将 Anthropic 标准模型名映射到 Bedrock 模型 ID
// 注意：此处的 "us." 前缀仅为默认值，ResolveBedrockModelID 会根据账号配置的
// aws_region 自动调整为匹配的区域前缀（如 eu.、apac.、jp. 等）
var DefaultBedrockModelMapping = map[string]string{
	// Claude Fable
	"claude-fable-5": "anthropic.claude-fable-5",
	// Claude Opus
	"claude-opus-4-8":          "us.anthropic.claude-opus-4-8-v1",
	"claude-opus-4-7":          "us.anthropic.claude-opus-4-7-v1",
	"claude-opus-4-6-thinking": "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-6":          "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-5-thinking": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-5-20251101": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-1":          "us.anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-opus-4-20250514":   "us.anthropic.claude-opus-4-20250514-v1:0",
	// Claude Sonnet
	"claude-sonnet-5":            "us.anthropic.claude-sonnet-5-v1",
	"claude-sonnet-4-6-thinking": "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-6":          "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-5":          "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-thinking": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-20250929": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-20250514":   "us.anthropic.claude-sonnet-4-20250514-v1:0",
	// Claude Haiku
	"claude-haiku-4-5":          "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-haiku-4-5-20251001": "us.anthropic.claude-haiku-4-5-20251001-v1:0",
}
