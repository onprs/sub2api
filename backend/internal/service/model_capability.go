package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// modelCapabilityCatalogTTL 控制公开模型能力目录的刷新周期。能力元数据变化很慢，
	// 用比价格数据更长的 TTL 换取更少的 4MB 级下载与解析。
	modelCapabilityCatalogTTL = 6 * time.Hour
	// modelCapabilityCatalogRetryDelay 是加载失败后的最小重试间隔，避免目录不可用时
	// 每个用户请求都重新发起远程下载。
	modelCapabilityCatalogRetryDelay = 5 * time.Minute
	// modelCapabilityCatalogFetchTimeout 限制单次目录下载与解析的最长耗时。
	modelCapabilityCatalogFetchTimeout = 60 * time.Second
	// modelCapabilityCanonicalURL 是 models.dev 的模型级目录（按 lab + 模型名索引），
	// 与模型详情页同源；provider 级目录（cliImportModelsDevAPIURL）作为回退。
	modelCapabilityCanonicalURL = "https://models.dev/models.json"
)

// modelCapabilitySnapshot 是一次加载得到的目录快照。
type modelCapabilitySnapshot struct {
	// providers 来自 provider 级目录（api.json）：provider → 模型名 → 能力。
	providers map[string]map[string]ModelCapability
	// canonical 来自模型级目录（models.json）：lab → 模型名 → 能力。
	canonical map[string]map[string]ModelCapability
}

var errModelCapabilityCatalogEmpty = errors.New("model capability catalog contains no mapped provider")

// ModelCapability 是模型对用户可见的能力元数据，来源为 models.dev 公开目录。
// 只保留「图标 + 短标签」需要表达的字段；未收录的模型返回零值，由展示层省略。
type ModelCapability struct {
	// ContextTokens 是官方最大上下文 Token 数；0 表示目录未收录。
	ContextTokens int
	// MaxOutputTokens 是官方最大输出 Token 数；0 表示目录未收录。
	MaxOutputTokens int
	Reasoning       bool
	ToolCall        bool
	// Vision 表示输入模态包含图片。
	Vision bool
	// PDFInput 表示输入模态包含 PDF。
	PDFInput bool
	// ImageOutput 表示输出模态包含图片。
	ImageOutput bool
}

// modelCapabilityProviderIDs 是公开目录中需要解析的 provider 集合。除本站在用平台
// 的对应 provider 外，还包括 Command Code 模型命名空间所指向的模型厂商第一方 provider
// （见 commandCodeCapabilityProviderAliases）。只解析这些 provider，避免为全部目录
// 模型分配内存。
var modelCapabilityProviderIDs = []string{
	"openai",
	"anthropic",
	"google",
	"xai",
	"opencode-go",
	"cline-pass",
	"openrouter",
}

// commandCodeCapabilityLabAliases 把 Command Code 的模型命名空间（模型 ID 中 `/` 前的
// 一段）映射到 models.dev 模型级目录（models.json）里的 lab。
//
// Command Code 官方接口只发布 id / name / context_length，没有任何能力字段，因此能力
// 元数据只能从公开目录借用。模型级目录按「lab + 模型名」索引，与 Command Code 自带的
// 厂商命名空间天然对应，比逐个 provider 查找更准也更全。
var commandCodeCapabilityLabAliases = map[string]string{
	"deepseek":         "deepseek",
	"xai":              "xai",
	"meta":             "meta",
	"qwen":             "alibaba",
	"moonshotai":       "moonshotai",
	"zai-org":          "zhipuai",
	"z-ai":             "zhipuai",
	"minimaxai":        "minimax",
	"google":           "google",
	"stepfun":          "stepfun",
	"openai":           "openai",
	"nvidia":           "nvidia",
	"poolside":         "poolside",
	"thinkingmachines": "thinkingmachines",
	"xiaomi":           "xiaomi",
	"meituan":          "meituan",
	"tencent":          "tencent",
	"inclusionai":      "inclusionai",
}

// commandCodeCapabilityProviderAliases 是模型级目录未收录时的 provider 级回退。
// 只在模型级目录查不到该模型时使用，顺序即优先级。
var commandCodeCapabilityProviderAliases = map[string][]string{
	"deepseek":         {"deepseek"},
	"xai":              {"xai"},
	"meta":             {"meta"},
	"qwen":             {"alibaba", "alibaba-cn"},
	"moonshotai":       {"moonshotai", "moonshotai-cn", "kimi-for-coding"},
	"zai-org":          {"zai", "zhipuai"},
	"z-ai":             {"zai", "zhipuai"},
	"minimaxai":        {"minimax", "minimax-cn"},
	"google":           {"google"},
	"stepfun":          {"stepfun", "stepfun-ai"},
	"openai":           {"openai"},
	"nvidia":           {"nvidia"},
	"poolside":         {"poolside"},
	"thinkingmachines": {"thinkingmachines"},
	"xiaomi":           {"xiaomi", "xiaomi-token-plan-cn"},
	"meituan":          {"longcat"},
	"tencent":          {"tencent-tokenhub", "tencent-token-plan", "tencent-coding-plan"},
}

// commandCodeCapabilityProviderIDs 返回需要预加载的厂商第一方 provider（去重、稳定顺序）。
func commandCodeCapabilityProviderIDs() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(commandCodeCapabilityProviderAliases))
	// 按命名空间字典序遍历，保证 provider 列表顺序稳定、可复现。
	namespaces := make([]string, 0, len(commandCodeCapabilityProviderAliases))
	for namespace := range commandCodeCapabilityProviderAliases {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		for _, provider := range commandCodeCapabilityProviderAliases[namespace] {
			if _, ok := seen[provider]; ok {
				continue
			}
			seen[provider] = struct{}{}
			out = append(out, provider)
		}
	}
	return out
}

// commandCodeCapabilityModelAliases 是 Command Code 自有变体名到厂商第一方模型名的
// 归一表。只列出已核实同源的变体（上下文窗口与厂商目录一致或非常接近）；
// 未列出的变体不借用元数据。
var commandCodeCapabilityModelAliases = map[string]string{
	"deepseek-v4-flash-fast": "deepseek-v4-flash",
	"glm-5.2-fast":           "glm-5.2",
	"hy3-paid":               "hy3",
	"laguna-s-2.1-free":      "laguna-s-2.1",
}

// commandCodeContextConsistencyRatio 是借用能力前允许的上下文窗口偏差。
// 同一模型的窗口在两份目录间常因取整而不同（实测最大 4.9%），而不同模型的差异远
// 超该值（实测 290% 以上），因此用它作为“同一个模型”的判据。
const commandCodeContextConsistencyRatio = 1.25

// modelCapabilityProviderForPlatform 把本站平台映射到公开目录的 provider。
// 未映射的平台（例如 Command Code 的专有命名空间）返回空字符串，表示不展示能力。
func modelCapabilityProviderForPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case PlatformOpenAI:
		return "openai"
	case PlatformAnthropic:
		return "anthropic"
	case PlatformGemini, PlatformAntigravity:
		return "google"
	case PlatformGrok:
		return "xai"
	case PlatformOpenCodeGo:
		return "opencode-go"
	case PlatformClinePass:
		return "cline-pass"
	case PlatformOpenRouter:
		return "openrouter"
	default:
		return ""
	}
}

type modelCapabilityCatalogLimit struct {
	Context *int `json:"context"`
	Output  *int `json:"output"`
}

type modelCapabilityCatalogModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelCapabilityCatalogModel struct {
	Reasoning  *bool                             `json:"reasoning"`
	ToolCall   *bool                             `json:"tool_call"`
	Modalities *modelCapabilityCatalogModalities `json:"modalities"`
	Limit      *modelCapabilityCatalogLimit      `json:"limit"`
}

type modelCapabilityCatalogProvider struct {
	Models map[string]modelCapabilityCatalogModel `json:"models"`
}

// modelCapabilityCatalog 是公开模型能力目录的进程内缓存，同时持有两份快照：
// 模型级目录（canonical，按 lab → 模型名）与 provider 级目录（providers）。
// 目录整体只加载一次，并发请求共享同一次加载（singleflight），加载失败按
// modelCapabilityCatalogRetryDelay 节流重试，并且始终优先返回已有快照。
type modelCapabilityCatalog struct {
	client PricingRemoteClient
	// resolveURL 在配置启用 URL allowlist 时校验并规范化目录 URL；nil 表示按原样使用。
	resolveURL func(string) (string, error)
	now        func() time.Time
	ttl        time.Duration

	mu          sync.RWMutex
	providers   map[string]map[string]ModelCapability
	canonical   map[string]map[string]ModelCapability
	loadedAt    time.Time
	lastFailure time.Time
	flight      singleflight.Group
}

func newModelCapabilityCatalog(client PricingRemoteClient, resolveURL func(string) (string, error)) *modelCapabilityCatalog {
	return &modelCapabilityCatalog{
		client:     client,
		resolveURL: resolveURL,
		now:        time.Now,
		ttl:        modelCapabilityCatalogTTL,
	}
}

// fetch 按配置解析 URL 后下载目录内容。
func (c *modelCapabilityCatalog) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if c.resolveURL != nil {
		resolved, err := c.resolveURL(rawURL)
		if err != nil {
			return nil, err
		}
		rawURL = resolved
	}
	return c.client.FetchPricingJSON(ctx, rawURL)
}

// lookup 返回指定平台与模型的能力元数据；平台未映射、目录未收录或加载失败时返回 false。
func (c *modelCapabilityCatalog) lookup(ctx context.Context, platform, model string) (ModelCapability, bool) {
	providerID := modelCapabilityProviderForPlatform(platform)
	if providerID == "" {
		return ModelCapability{}, false
	}
	modelID := normalizeModelCapabilityKey(model)
	if modelID == "" {
		return ModelCapability{}, false
	}

	models := c.ensure(ctx).providers[providerID]
	if len(models) == 0 {
		return ModelCapability{}, false
	}
	capability, ok := models[modelID]
	return capability, ok
}

// ensure 返回能力目录快照：命中缓存直接返回，否则加载（并发请求共享同一次加载），
// 加载失败时返回上一次快照并进入重试冷却。
func (c *modelCapabilityCatalog) ensure(ctx context.Context) modelCapabilitySnapshot {
	if c == nil || c.client == nil {
		return modelCapabilitySnapshot{}
	}
	if cached, fresh := c.snapshot(); fresh {
		return cached
	}

	// 并发首个请求触发加载，其余请求由 singleflight 合并等待同一结果；
	// 加载期间任何请求都不会被“节流成空结果”。
	value, err, _ := c.flight.Do("load", func() (any, error) {
		if cached, fresh := c.snapshot(); fresh {
			return cached, nil
		}
		// 目录是进程级共享数据，不能被单个请求的取消打断，因此忽略调用方取消信号。
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelCapabilityCatalogFetchTimeout)
		defer cancel()

		// 两份目录互为备份：任一可用就能提供能力，两份都失败才算加载失败。
		var providers map[string]map[string]ModelCapability
		if providerBody, fetchErr := c.fetch(loadCtx, cliImportModelsDevAPIURL); fetchErr == nil {
			providers, _ = parseModelCapabilityCatalog(providerBody)
		}

		var canonical map[string]map[string]ModelCapability
		if canonicalBody, canonicalErr := c.fetch(loadCtx, modelCapabilityCanonicalURL); canonicalErr == nil {
			canonical = parseModelCapabilityCanonicalCatalog(canonicalBody)
		}

		if len(providers) == 0 && len(canonical) == 0 {
			c.markFailure()
			return nil, errModelCapabilityCatalogEmpty
		}

		next := modelCapabilitySnapshot{providers: providers, canonical: canonical}
		c.mu.Lock()
		c.providers = next.providers
		c.canonical = next.canonical
		c.loadedAt = c.now()
		c.lastFailure = time.Time{}
		c.mu.Unlock()
		return next, nil
	})
	if err == nil {
		if snapshot, ok := value.(modelCapabilitySnapshot); ok &&
			(len(snapshot.providers) > 0 || len(snapshot.canonical) > 0) {
			return snapshot
		}
	}

	c.mu.RLock()
	cached := modelCapabilitySnapshot{providers: c.providers, canonical: c.canonical}
	c.mu.RUnlock()
	return cached
}

// snapshot 返回当前快照，并报告它是否仍在 TTL 内或处于失败冷却期而可以直接复用。
func (c *modelCapabilityCatalog) snapshot() (modelCapabilitySnapshot, bool) {
	now := c.now()
	c.mu.RLock()
	current := modelCapabilitySnapshot{providers: c.providers, canonical: c.canonical}
	loadedAt, lastFailure := c.loadedAt, c.lastFailure
	c.mu.RUnlock()

	if current.providers != nil || current.canonical != nil {
		if now.Sub(loadedAt) < c.ttl {
			return current, true
		}
	}
	if !lastFailure.IsZero() && now.Sub(lastFailure) < modelCapabilityCatalogRetryDelay {
		return current, true
	}
	return current, false
}

func (c *modelCapabilityCatalog) markFailure() {
	c.mu.Lock()
	c.lastFailure = c.now()
	c.mu.Unlock()
}

// parseModelCapabilityCatalog 从 models.dev 目录中提取本站在用 provider 的模型能力。
// 未收录能力字段的模型同样保留零值，交由展示层按缺失处理。
func parseModelCapabilityCatalog(body []byte) (map[string]map[string]ModelCapability, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	out := make(map[string]map[string]ModelCapability, len(modelCapabilityProviderIDs))
	providerIDs := append(append([]string{}, modelCapabilityProviderIDs...), commandCodeCapabilityProviderIDs()...)
	for _, providerID := range providerIDs {
		payload, ok := raw[providerID]
		if !ok {
			continue
		}
		var provider modelCapabilityCatalogProvider
		if err := json.Unmarshal(payload, &provider); err != nil {
			continue
		}
		models := make(map[string]ModelCapability, len(provider.Models))
		for modelID, entry := range provider.Models {
			key := normalizeModelCapabilityKey(modelID)
			if key == "" {
				continue
			}
			models[key] = entry.toModelCapability()
		}
		if len(models) > 0 {
			out[providerID] = models
		}
	}
	return out, nil
}

// parseModelCapabilityCanonicalCatalog 解析 models.dev 的模型级目录（models.json）。
// 该目录按 "<lab>/<模型名>" 索引，与模型详情页同源，包含跨 provider 聚合后的能力与规格。
func parseModelCapabilityCanonicalCatalog(body []byte) map[string]map[string]ModelCapability {
	var raw map[string]modelCapabilityCatalogModel
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}

	out := make(map[string]map[string]ModelCapability, 8)
	for key, entry := range raw {
		// key 已经是 "lab/模型名"；lab 用于分类，模型名才是查找键。
		lab, modelName, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		lab = normalizeModelCapabilityKey(lab)
		modelName = normalizeModelCapabilityKey(modelName)
		if lab == "" || modelName == "" {
			continue
		}
		models := out[lab]
		if models == nil {
			models = make(map[string]ModelCapability)
			out[lab] = models
		}
		models[modelName] = entry.toModelCapability()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeModelCapabilityKey 归一目录键：小写并将分隔符统一为连字符。
// 不同目录对同一模型存在 qwen3.8-27b / Qwen3.8 27B 之类的写法差异。
func normalizeModelCapabilityKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "-", "_", "-").Replace(value)
	return value
}

// commandCodeCapabilityMatch 是 Command Code 模型从厂商目录借用到的能力。
// sameContext 表示厂商条目与 Command Code 官方目录的上下文窗口一致（同源），
// 此时数值限额可以一并借用；否则只保留模型固有能力，限额交给 Command Code 官方口径。
type commandCodeCapabilityMatch struct {
	capability  ModelCapability
	sameContext bool
}

// lookupCommandCode 查询 Command Code 模型在公开目录中的能力元数据。
//
// 查找顺序：
//  1. 模型级目录（models.json）按 lab + 模型名精确命中；
//  2. 已核实的变体名归一后重新命中模型级目录；
//  3. provider 级目录（api.json）作为回退。
//
// expectedContext 是 Command Code 官方发布的上下文窗口，用来在多个候选中挑选同源条目：
// 同源的候选可完整借用（含数值限额），否则只借用模型固有能力。
// 命名空间无映射或目录未收录时返回 false。
func (c *modelCapabilityCatalog) lookupCommandCode(
	ctx context.Context,
	model string,
	expectedContext int,
) (commandCodeCapabilityMatch, bool) {
	namespace, base := splitCommandCodeModelID(model)
	if base == "" {
		return commandCodeCapabilityMatch{}, false
	}

	lab, ok := commandCodeCapabilityLabAliases[namespace]
	if !ok {
		if namespace != "" || !isOpenAICompatibleBareModelID(base) {
			return commandCodeCapabilityMatch{}, false
		}
		lab = commandCodeCapabilityLabAliases["openai"]
	}

	catalog := c.ensure(ctx)
	// 模型级与 provider 级按同名候选择一，模型级优先。
	exact := make([]ModelCapability, 0, 2)
	candidates := make([]ModelCapability, 0, 4)
	for _, name := range commandCodeCapabilityLookupNames(base) {
		if models := catalog.canonical[lab]; len(models) > 0 {
			if capability, hit := models[name]; hit {
				exact = append(exact, capability)
			}
		}
	}

	// provider 级回退：只在模型级目录没有精确命中时使用。
	if len(exact) == 0 {
		for _, provider := range commandCodeCapabilityProviderAliases[namespace] {
			models := catalog.providers[provider]
			if len(models) == 0 {
				continue
			}
			for _, name := range commandCodeCapabilityLookupNames(base) {
				if capability, hit := models[name]; hit {
					exact = append(exact, capability)
				}
			}
			// 部分厂商目录把命名空间或变体写在 key 里
			// （如 nvidia/nemotron-3-ultra-550b-a55b、thinkingmachines/Inkling:peft:262144）。
			for key, capability := range models {
				if name := lastModelKeySegment(key); sliceContains(commandCodeCapabilityLookupNames(base), name) {
					candidates = append(candidates, capability)
				}
			}
		}
	}

	// 优先选与 Command Code 官方上下文同源的候选：同名变体（如 peft:262144）比基座
	// 条目更接近实际部署，能用上下文窗口区分开。
	for _, capability := range append(append([]ModelCapability{}, exact...), candidates...) {
		if contextsConsistent(expectedContext, capability.ContextTokens) {
			return commandCodeCapabilityMatch{capability: capability, sameContext: true}, true
		}
	}
	if len(exact) > 0 {
		return commandCodeCapabilityMatch{capability: exact[0]}, true
	}
	if len(candidates) > 0 {
		return commandCodeCapabilityMatch{capability: candidates[0]}, true
	}
	return commandCodeCapabilityMatch{}, false
}

// commandCodeCapabilityLookupNames 返回模型名的查找候选（原名与已核实的变体名归一）。
func commandCodeCapabilityLookupNames(base string) []string {
	names := []string{base}
	if aliased, ok := commandCodeCapabilityModelAliases[base]; ok && aliased != base {
		names = append(names, aliased)
	}
	return names
}

// lastModelKeySegment 取目录 key 的最后一段，并去掉冒号变体后缀。
// "nvidia/nemotron-3-ultra-550b-a55b" → "nemotron-3-ultra-550b-a55b"
// "thinkingmachines/Inkling:peft:262144" → "inkling"
func lastModelKeySegment(key string) string {
	segment := key
	if idx := strings.LastIndex(segment, "/"); idx >= 0 {
		segment = segment[idx+1:]
	}
	if idx := strings.Index(segment, ":"); idx > 0 {
		segment = segment[:idx]
	}
	return strings.ToLower(strings.TrimSpace(segment))
}

func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// capabilityWithoutLimits 丢弃与部署口径相关的数值限额，只保留模型固有能力。
func capabilityWithoutLimits(capability ModelCapability) ModelCapability {
	capability.ContextTokens = 0
	capability.MaxOutputTokens = 0
	return capability
}

// splitCommandCodeModelID 拆分 Command Code 模型 ID 为 (命名空间, 模型名)。
// 同时去掉 Command Code 的计费后缀（:free / :paid），使同一模型的免费与付费变体共享能力。
func splitCommandCodeModelID(model string) (namespace, base string) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "", ""
	}
	if idx := strings.Index(model, "/"); idx >= 0 {
		namespace = model[:idx]
		base = model[idx+1:]
	} else {
		base = model
	}
	if idx := strings.LastIndex(base, ":"); idx > 0 {
		base = base[:idx]
	}
	return namespace, strings.TrimSpace(base)
}

// isOpenAICompatibleBareModelID 判断无命名空间的模型名是否属于 OpenAI 命名习惯。
func isOpenAICompatibleBareModelID(model string) bool {
	if strings.HasPrefix(model, "gpt-") {
		return true
	}
	// o1 / o3 / o4 系列推理模型的裸名形式。
	if len(model) > 1 && model[0] == 'o' && model[1] >= '0' && model[1] <= '9' {
		return true
	}
	return false
}

func (m modelCapabilityCatalogModel) toModelCapability() ModelCapability {
	capability := ModelCapability{}
	if m.Reasoning != nil {
		capability.Reasoning = *m.Reasoning
	}
	if m.ToolCall != nil {
		capability.ToolCall = *m.ToolCall
	}
	if m.Limit != nil {
		if m.Limit.Context != nil && *m.Limit.Context > 0 {
			capability.ContextTokens = *m.Limit.Context
		}
		if m.Limit.Output != nil && *m.Limit.Output > 0 {
			capability.MaxOutputTokens = *m.Limit.Output
		}
	}
	if m.Modalities != nil {
		capability.Vision = containsFold(m.Modalities.Input, "image")
		capability.PDFInput = containsFold(m.Modalities.Input, "pdf")
		capability.ImageOutput = containsFold(m.Modalities.Output, "image")
	}
	return capability
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

// GetModelCapability 返回平台下某个模型的公开能力元数据；未收录时返回 false。
//
// 目录按需懒加载（与同一 models.dev 源的 cliImportCatalog 一致），避免启动期间
// 等待公网下载；首次访问后缓存 modelCapabilityCatalogTTL，后续请求不再产生网络开销。
func (s *PricingService) GetModelCapability(ctx context.Context, platform, model string) (ModelCapability, bool) {
	if s == nil || s.modelCapabilities == nil {
		return ModelCapability{}, false
	}
	if strings.EqualFold(strings.TrimSpace(platform), PlatformCommandCode) {
		return s.commandCodeModelCapability(ctx, model)
	}
	return s.modelCapabilities.lookup(ctx, platform, model)
}

// commandCodeModelCapability 借用公开目录的能力字段。
//
// Command Code 官方接口只发布上下文窗口与计费，没有任何能力字段，因此能力只能从公开
// 目录借用。借用分两级：
//   - 目录条目与 Command Code 官方上下文同源（偏差在 commandCodeContextConsistencyRatio
//     以内）时完整借用，包括输出上限；
//   - 模型级命名空间精确命中但上下文差异过大时，说明 Command Code 用的是另一种部署或
//     限额口径，此时只保留推理、工具调用、多模态这类模型固有能力，数值限额交给
//     Command Code 官方口径。
func (s *PricingService) commandCodeModelCapability(ctx context.Context, model string) (ModelCapability, bool) {
	expectedContext := 0
	if entry, ok := s.commandCodeCatalogEntry(model); ok {
		expectedContext = entry.ContextWindow
	}
	match, ok := s.modelCapabilities.lookupCommandCode(ctx, model, expectedContext)
	if !ok {
		return ModelCapability{}, false
	}
	if !match.sameContext {
		return capabilityWithoutLimits(match.capability), true
	}
	return match.capability, true
}

// commandCodeCatalogEntry 读取 Command Code 官方目录条目；目录未就绪时用内置目录。
func (s *PricingService) commandCodeCatalogEntry(model string) (commandCodeCatalogEntry, bool) {
	catalog := s.commandCodeCatalog
	if catalog == nil {
		catalog = defaultCommandCodeCatalog
	}
	return catalog.entry(model)
}

// resolveCapabilityCatalogURL 在配置启用 URL allowlist 时校验并规范化目录 URL。
// 无配置（如测试构造的 PricingService）时按原样使用。
func (s *PricingService) resolveCapabilityCatalogURL(raw string) (string, error) {
	if s == nil || s.cfg == nil {
		return raw, nil
	}
	return s.validateSupplementalPricingURL(raw, []string{"models.dev"})
}

// contextsConsistent 判断两个上下文窗口是否属于同一个模型。
// 任一值缺失时不能证明同源，返回 false。
func contextsConsistent(left, right int) bool {
	if left <= 0 || right <= 0 {
		return false
	}
	if left == right {
		return true
	}
	return float64(max(left, right))/float64(min(left, right)) <= commandCodeContextConsistencyRatio
}
