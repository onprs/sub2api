package service

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ChannelMonitor 全局常量。
// 这些是 MVP 阶段的硬编码值，按需可以提到 config 中。
const (
	// monitorRequestTimeout 单次模型请求总超时（含 Body 读取）。
	monitorRequestTimeout = 120 * time.Second
	// monitorPingTimeout HEAD 请求 endpoint origin 的超时。
	monitorPingTimeout = 8 * time.Second
	// monitorDegradedThreshold 主请求成功但耗时超过该阈值视为 degraded。
	monitorDegradedThreshold = 6 * time.Second
	// monitorHistoryRetentionDays 明细历史保留天数。
	// 60s 默认间隔 * 30 天 ≈ 43200 行/monitor/model，一般部署总量 <= 2M 行，
	// PG 无压力；所以直接保留完整明细一个月，可用率查询可以全走原始行不依赖聚合。
	// 聚合表 channel_monitor_daily_rollups 仍然保留，作为长期历史回填/降级查询的兜底。
	monitorHistoryRetentionDays = 30
	// monitorRollupRetentionDays 日聚合保留天数。
	// 日聚合行由 RunDailyMaintenance 在超过该窗口后软删。
	monitorRollupRetentionDays = 30
	// monitorMaintenanceMaxDaysPerRun 单次维护任务最多聚合的天数。
	// 用于限制首次上线回填（30 天）+ 少量余量，避免长事务。
	monitorMaintenanceMaxDaysPerRun = 35
	// monitorWorkerConcurrency 调度器并发执行的监控数（pond 池容量）。
	monitorWorkerConcurrency = 5
	// monitorStartupLoadTimeout Start 时一次性加载所有 enabled monitor 的总超时。
	monitorStartupLoadTimeout = 10 * time.Second
	// monitorMinIntervalSeconds / monitorMaxIntervalSeconds 用户配置的检测间隔上下限。
	monitorMinIntervalSeconds = 15
	monitorMaxIntervalSeconds = 3600
	// monitorMessageMaxBytes message 字段最大字节数（与 schema/migration 一致）。
	monitorMessageMaxBytes = 500
	// monitorResponseMaxBytes 单次模型响应最大读取字节，防止 OOM。
	monitorResponseMaxBytes = 64 * 1024
	// monitorErrorBodySnippetMaxBytes 非 2xx 响应时保留上游 body 片段的最大字节数。
	// 留 300 字节足够覆盖典型结构化错误（如 `{"error":{"message":"..."}}`），
	// 又给 "upstream HTTP <status>: " 前缀留出余量，避免最终被 monitorMessageMaxBytes (500) 截得太狠。
	monitorErrorBodySnippetMaxBytes = 300
	// monitorChallengeMin / monitorChallengeMax challenge 操作数范围。
	monitorChallengeMin = 1
	monitorChallengeMax = 50

	// providerOpenAIPath OpenAI Chat Completions 路径。
	providerOpenAIPath = "/v1/chat/completions"
	// providerOpenAIResponsesPath OpenAI Responses API 路径。
	providerOpenAIResponsesPath = "/v1/responses"
	// providerAnthropicPath Anthropic Messages 路径。
	providerAnthropicPath = "/v1/messages"
	// providerGeminiPathTemplate Gemini generateContent 路径模板（含 model 占位）。
	providerGeminiPathTemplate = "/v1beta/models/%s:generateContent"
	// providerOpenCodeGoChatPath OpenCode Go OpenAI-compatible Chat Completions path.
	providerOpenCodeGoChatPath = "/chat/completions"
	// providerOpenCodeGoMessagesPath OpenCode Go Anthropic-style Messages path.
	providerOpenCodeGoMessagesPath = "/messages"
	// providerOpenCodeGoResponsesPath OpenCode Go OpenAI Responses path.
	providerOpenCodeGoResponsesPath = "/responses"
	// providerClinePassChatPath works with both the local gateway root and Cline's /api/v1 root.
	providerClinePassChatPath = "/chat/completions"
	// providerOpenRouterChatPath works with both the local gateway root and OpenRouter's /api/v1 root.
	providerOpenRouterChatPath = "/chat/completions"
	// providerCommandCodeChatPath Command Code Provider API Chat Completions path.
	providerCommandCodeChatPath = "/provider/v1/chat/completions"

	// MonitorProvider* provider 字符串常量（也是 ent enum 的实际值）。
	MonitorProviderOpenAI            = "openai"
	MonitorProviderAnthropic         = "anthropic"
	MonitorProviderGemini            = "gemini"
	MonitorProviderOpenCodeGo        = "opencode_go"
	MonitorProviderClinePass         = "clinepass"
	MonitorProviderOpenRouter        = "openrouter"
	MonitorProviderCommandCode       = "commandcode"
	MonitorProviderAntigravityClaude = "antigravity_claude"
	MonitorProviderAntigravityGemini = "antigravity_gemini"

	// MonitorStatusOperational 等监控状态字符串常量（与 ent enum 一致）。
	MonitorStatusOperational = "operational"
	MonitorStatusDegraded    = "degraded"
	MonitorStatusFailed      = "failed"
	MonitorStatusError       = "error"

	// monitorAvailability7Days / 15 / 30 用于聚合查询窗口。
	monitorAvailability7Days  = 7
	monitorAvailability15Days = 15
	monitorAvailability30Days = 30

	// MonitorHistoryDefaultLimit 历史查询默认返回条数（handler 层共享）。
	MonitorHistoryDefaultLimit = 100
	// MonitorHistoryMaxLimit 历史查询最大返回条数（handler 层共享）。
	MonitorHistoryMaxLimit = 1000

	// monitorTimelineMaxPoints 用户视图 timeline 每个监控最多返回的历史点数。
	monitorTimelineMaxPoints = 60

	// monitorEndpointResolveTimeout validateEndpoint 解析 hostname 的最长耗时。
	monitorEndpointResolveTimeout = 5 * time.Second

	// ---- checker / runner 行为参数（消除 magic 值）----

	// monitorAnthropicAPIVersion Anthropic Messages API 版本头。
	monitorAnthropicAPIVersion = "2023-06-01"
	// monitorChallengeMaxTokens 普通模型单次 challenge 请求的输出上限。
	monitorChallengeMaxTokens = 50
	// monitorGemma4ChallengeMaxTokens 为 Gemma 4 的可见分析和最终答案预留空间。
	monitorGemma4ChallengeMaxTokens = 512
	// monitorGemma4ThinkingLevel 关闭 Gemma 4 的扩展思考，降低探活延迟和输出消耗。
	monitorGemma4ThinkingLevel = "MINIMAL"
	// monitorOpenCodeGoChallengeMaxTokens 为 OpenCode Go 推理/转换链路保留更宽的输出预算，避免 final content 偶发为空。
	monitorOpenCodeGoChallengeMaxTokens = 512
	// monitorClinePassChallengeMaxTokens 为 ClinePass reasoning 模型保留最终答案预算。
	// 官方 SDK 默认输出上限为 32k；探活使用 4k，在控制额度消耗的同时避免 reasoning 挤占全部输出。
	monitorClinePassChallengeMaxTokens = 4096
	// monitorOpenRouterChallengeMaxTokens 为 OpenRouter reasoning 模型保留充足的输出预算，避免思考过程挤占全部 output 导致最终 content 被截断。
	monitorOpenRouterChallengeMaxTokens = 4096
	// monitorCommandCodeChallengeMaxTokens 为 Command Code 推理模型（如 ox-alpha）保留输出预算。
	monitorCommandCodeChallengeMaxTokens = 4096
	// monitorAntigravityGeminiChallengeMaxTokens 为 Antigravity Gemini thinking/agent 模型保留更多输出预算。
	monitorAntigravityGeminiChallengeMaxTokens = 1024
	// monitorAntigravityGeminiThinkingLevel 渠道监控只做极简探活，Gemini 3 用 low 降低 thinking 消耗与首 token 延迟。
	monitorAntigravityGeminiThinkingLevel = "low"

	// monitorRunOneBuffer runOne 的总超时缓冲（除请求超时与 ping 超时外的额外裕量）。
	monitorRunOneBuffer = 10 * time.Second

	// monitorIdleConnTimeout HTTP transport 空闲连接关闭超时。
	monitorIdleConnTimeout = 30 * time.Second
	// monitorTLSHandshakeTimeout HTTP transport TLS 握手超时。
	monitorTLSHandshakeTimeout = 10 * time.Second
	// monitorResponseHeaderTimeout HTTP transport 等待响应头超时。
	monitorResponseHeaderTimeout = 120 * time.Second
	// monitorPingDiscardMaxBytes ping 时丢弃响应体的最大字节数。
	monitorPingDiscardMaxBytes = 1024

	// monitorDialTimeout 自定义 dialer 单次连接超时。
	monitorDialTimeout = 10 * time.Second
	// monitorDialKeepAlive 自定义 dialer keep-alive 间隔。
	monitorDialKeepAlive = 30 * time.Second
)

// 业务错误（统一在此声明，避免散落）。
var (
	ErrChannelMonitorNotFound = infraerrors.NotFound(
		"CHANNEL_MONITOR_NOT_FOUND", "channel monitor not found",
	)
	ErrChannelMonitorInvalidTargetType = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_TARGET_TYPE", "target_type must be local or external",
	)
	ErrChannelMonitorMissingGroup = infraerrors.BadRequest(
		"CHANNEL_MONITOR_MISSING_GROUP", "group_id is required for a local monitor",
	)
	ErrChannelMonitorGroupPlatformMismatch = infraerrors.BadRequest(
		"CHANNEL_MONITOR_GROUP_PLATFORM_MISMATCH", "the selected group platform does not match the monitor provider",
	)
	ErrChannelMonitorGroupUnavailable = infraerrors.BadRequest(
		"CHANNEL_MONITOR_GROUP_UNAVAILABLE", "the selected group is unavailable",
	)
	ErrChannelMonitorGroupAlreadyMonitored = infraerrors.Conflict(
		"CHANNEL_MONITOR_GROUP_ALREADY_MONITORED", "the selected group already has a local monitor",
	)
	ErrChannelMonitorInvalidProvider = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_PROVIDER", "provider must be one of openai/anthropic/gemini/opencode_go/clinepass/openrouter/commandcode/antigravity_claude/antigravity_gemini",
	)
	ErrChannelMonitorInvalidAPIMode = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_API_MODE", "api_mode must be chat_completions, messages, or responses; responses is supported for openai/opencode_go and messages is supported for opencode_go/commandcode",
	)
	ErrChannelMonitorInvalidRequestBody = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_REQUEST_BODY", "replace-mode body_override must include non-empty messages for chat_completions/messages or non-empty instructions and input for responses",
	)
	ErrChannelMonitorInvalidInterval = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_INTERVAL", "interval_seconds must be in [15, 3600]",
	)
	ErrChannelMonitorInvalidJitter = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_JITTER", "jitter_seconds must be >= 0 and interval_seconds - jitter_seconds must be >= 15",
	)
	ErrChannelMonitorInvalidEndpoint = infraerrors.BadRequest(
		"CHANNEL_MONITOR_INVALID_ENDPOINT", "endpoint must be a valid https URL",
	)
	ErrChannelMonitorEndpointScheme = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_SCHEME", "endpoint must use https scheme",
	)
	ErrChannelMonitorEndpointPath = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_PATH", "endpoint must be base origin only (no path/query/fragment)",
	)
	ErrChannelMonitorEndpointPrivate = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_PRIVATE", "endpoint must be a public host",
	)
	ErrChannelMonitorEndpointUnreachable = infraerrors.BadRequest(
		"CHANNEL_MONITOR_ENDPOINT_UNREACHABLE", "endpoint hostname could not be resolved",
	)
	ErrChannelMonitorMissingAPIKey = infraerrors.BadRequest(
		"CHANNEL_MONITOR_MISSING_API_KEY", "api_key is required for an external monitor",
	)
	ErrChannelMonitorMissingPrimaryModel = infraerrors.BadRequest(
		"CHANNEL_MONITOR_MISSING_PRIMARY_MODEL", "primary_model is required",
	)
	ErrChannelMonitorAPIKeyDecryptFailed = infraerrors.InternalServer(
		"CHANNEL_MONITOR_KEY_DECRYPT_FAILED", "api key decryption failed; please re-edit the monitor with a fresh key",
	)
)
