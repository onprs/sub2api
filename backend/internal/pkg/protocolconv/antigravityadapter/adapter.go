// Package antigravityadapter applies Antigravity v1internal vendor policy
// outside the standard protocol converters.
package antigravityadapter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	vendor "github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv/standard"
)

// Family is selected from the resolved account/model mapping, never inferred
// from the client endpoint.
type Family string

const (
	FamilyClaude Family = "claude"
	FamilyGemini Family = "gemini"
)

const dummyThoughtSignature = "skip_thought_signature_validator"

// Options contains vendor-owned envelope and compatibility policy.
type Options struct {
	Family      Family
	ProjectID   string
	SourceModel string
	WireModel   string

	TransformOptions  vendor.TransformOptions
	IdentityPatch     string
	RectifySignatures bool
	RequestID         string
	UserAgent         string
}

// ConvertRequest converts a standard source request and wraps the Antigravity
// v1internal request. It does not select accounts or perform transport.
func ConvertRequest(body []byte, source protocolconv.Protocol, options Options) ([]byte, []protocolconv.Warning, error) {
	converted, warnings, thinkingDisabled, err := convertRequest(body, source, options)
	if err != nil {
		return converted, warnings, err
	}
	if thinkingDisabled {
		return converted, warnings, nil
	}
	route, ok := domain.ResolveAntigravityReasoningRoute(options.SourceModel, "")
	if !ok || route.WireModel != options.WireModel {
		return converted, warnings, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(converted, &envelope); err != nil {
		return nil, warnings, err
	}
	// web_search 等厂商降级可能已更换实际模型，不能再附加原档位的配置。
	if envelope["model"] != route.WireModel {
		return converted, warnings, nil
	}
	request, _ := envelope["request"].(map[string]any)
	config, _ := request["generationConfig"].(map[string]any)
	if config == nil {
		config = make(map[string]any)
		request["generationConfig"] = config
	}
	thinking, _ := config["thinkingConfig"].(map[string]any)
	if thinking == nil {
		thinking = make(map[string]any)
		config["thinkingConfig"] = thinking
	}
	// 档位选择是厂商策略，不让标准协议转换生成的预算与物理档位相互冲突。
	delete(thinking, "thinkingBudget")
	delete(thinking, "thinkingLevel")
	if route.HasThinkingBudget {
		thinking["thinkingBudget"] = route.ThinkingBudget
	} else {
		_, effort := domain.AntigravityReasoningModel(route.ModelID)
		// Claude 的 max 档在 v1internal 中使用数值枚举 200；其余档位使用大写字符串。
		if effort == "max" {
			thinking["thinkingLevel"] = 200
		} else {
			thinking["thinkingLevel"] = strings.ToUpper(effort)
		}
	}
	converted, err = json.Marshal(envelope)
	return converted, warnings, err
}

func convertRequest(body []byte, source protocolconv.Protocol, options Options) ([]byte, []protocolconv.Warning, bool, error) {
	if err := validateOptions(options); err != nil {
		return nil, nil, false, err
	}
	registry, err := standard.NewRegistry()
	if err != nil {
		return nil, nil, false, err
	}

	switch options.Family {
	case FamilyClaude:
		standardBody, warnings, err := registry.ConvertRequest(body, source, protocolconv.ProtocolAnthropic, protocolconv.Options{SourceModel: options.SourceModel, LossPolicy: protocolconv.LossError})
		if err != nil {
			return nil, warnings, false, err
		}
		var request vendor.ClaudeRequest
		if err := json.Unmarshal(standardBody, &request); err != nil {
			return nil, warnings, false, fmt.Errorf("decode standard Anthropic request: %w", err)
		}
		request.Model = options.SourceModel
		// googlegenai 解码未指定 thinkingBudget 的 thinkingConfig 时会生成 mode="auto"，
		// Antigravity Claude 转换器仅识别 "enabled" / "adaptive"，在此归一。
		if request.Thinking != nil && request.Thinking.Type == "auto" {
			if request.Thinking.BudgetTokens > 0 {
				request.Thinking.Type = "enabled"
			} else {
				request.Thinking.Type = "adaptive"
			}
		}
		rectifiedThinking := false
		if options.RectifySignatures {
			rectifiedThinking = rectifyClaudeRequestThinking(&request)
		}
		transform := options.TransformOptions
		if transform == (vendor.TransformOptions{}) {
			transform = vendor.DefaultTransformOptions()
		}
		if source == protocolconv.ProtocolGoogleGenAI {
			// Gemini 协议（含经过标准桥接的 Chat/Responses）与 FamilyGemini 对齐：
			// 客户端系统提示词移出 systemInstruction 放到首条 user content，避免第三方 harness 提示词触发 429。
			transform.MoveSystemToUserContent = true
		}
		converted, strippedThinking, err := vendor.TransformClaudeToGeminiWithOptionsDetailed(&request, options.ProjectID, options.WireModel, transform)
		thinkingDisabled := rectifiedThinking || strippedThinking
		if err != nil || source != protocolconv.ProtocolGoogleGenAI || options.RectifySignatures {
			return converted, warnings, thinkingDisabled, err
		}
		converted, err = restoreGoogleToolCallSignatures(body, converted)
		return converted, warnings, thinkingDisabled, err
	case FamilyGemini:
		standardBody, warnings, err := registry.ConvertRequest(body, source, protocolconv.ProtocolGoogleGenAI, protocolconv.Options{SourceModel: options.SourceModel, LossPolicy: protocolconv.LossError})
		if err != nil {
			return nil, warnings, false, err
		}
		converted, err := adaptGemini(standardBody, options)
		return converted, warnings, false, err
	default:
		return nil, nil, false, fmt.Errorf("unsupported Antigravity family %q", options.Family)
	}
}

// rectifyClaudeRequestThinking 在签名整流重试时将历史 thinking block 降级为普通文本，
// 丢弃 redacted_thinking 并关闭顶层 thinking，避免 Claude Vertex 再次校验 signature 失败。
func rectifyClaudeRequestThinking(req *vendor.ClaudeRequest) bool {
	if req == nil {
		return false
	}
	changed := false
	if req.Thinking != nil {
		req.Thinking = nil
		changed = true
	}
	for i := range req.Messages {
		raw := req.Messages[i].Content
		if len(raw) == 0 {
			continue
		}
		var str string
		if json.Unmarshal(raw, &str) == nil {
			continue
		}
		var blocks []map[string]any
		if json.Unmarshal(raw, &blocks) != nil {
			continue
		}
		filtered := make([]map[string]any, 0, len(blocks))
		modifiedMsg := false
		for _, block := range blocks {
			t, _ := block["type"].(string)
			switch t {
			case "thinking":
				if thinkingText, _ := block["thinking"].(string); strings.TrimSpace(thinkingText) != "" {
					filtered = append(filtered, map[string]any{
						"type": "text",
						"text": thinkingText,
					})
				}
				modifiedMsg = true
			case "redacted_thinking":
				modifiedMsg = true
			default:
				if _, hasThinking := block["thinking"].(string); t == "" && hasThinking {
					if thinkingText, _ := block["thinking"].(string); strings.TrimSpace(thinkingText) != "" {
						filtered = append(filtered, map[string]any{
							"type": "text",
							"text": thinkingText,
						})
					}
					modifiedMsg = true
				} else {
					filtered = append(filtered, block)
				}
			}
		}
		if !modifiedMsg {
			continue
		}
		if len(filtered) == 0 {
			filtered = append(filtered, map[string]any{
				"type": "text",
				"text": "(content removed)",
			})
		}
		if newRaw, err := json.Marshal(filtered); err == nil {
			req.Messages[i].Content = newRaw
			changed = true
		}
	}
	return changed
}

func restoreGoogleToolCallSignatures(sourceBody, vendorBody []byte) ([]byte, error) {
	var source map[string]any
	if err := json.Unmarshal(sourceBody, &source); err != nil {
		return nil, fmt.Errorf("decode standard Google request for signature restoration: %w", err)
	}
	signatures := make(map[string]string)
	for _, rawContent := range anySlice(source["contents"]) {
		content, _ := rawContent.(map[string]any)
		for _, rawPart := range anySlice(content["parts"]) {
			part, _ := rawPart.(map[string]any)
			call, _ := part["functionCall"].(map[string]any)
			id, _ := call["id"].(string)
			signature, _ := part["thoughtSignature"].(string)
			// Claude 模型绝不透传 Gemini 占位的 dummyThoughtSignature。
			if id != "" && signature != "" && signature != dummyThoughtSignature {
				signatures[id] = signature
			}
		}
	}
	if len(signatures) == 0 {
		return vendorBody, nil
	}

	var envelope map[string]any
	if err := json.Unmarshal(vendorBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode Antigravity vendor request for signature restoration: %w", err)
	}
	request, _ := envelope["request"].(map[string]any)
	for _, rawContent := range anySlice(request["contents"]) {
		content, _ := rawContent.(map[string]any)
		for _, rawPart := range anySlice(content["parts"]) {
			part, _ := rawPart.(map[string]any)
			call, _ := part["functionCall"].(map[string]any)
			id, _ := call["id"].(string)
			if signature := signatures[id]; signature != "" {
				part["thoughtSignature"] = signature
			}
		}
	}
	return json.Marshal(envelope)
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func validateOptions(options Options) error {
	if options.Family != FamilyClaude && options.Family != FamilyGemini {
		return fmt.Errorf("unsupported Antigravity family %q", options.Family)
	}
	if strings.TrimSpace(options.ProjectID) == "" {
		return fmt.Errorf("project ID is required for Antigravity")
	}
	if strings.TrimSpace(options.SourceModel) == "" {
		return fmt.Errorf("source model is required for Antigravity")
	}
	if strings.TrimSpace(options.WireModel) == "" {
		return fmt.Errorf("wire model is required for Antigravity")
	}
	return nil
}

func adaptGemini(body []byte, options Options) ([]byte, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("decode standard Google request: %w", err)
	}
	injectIdentity(request, options.IdentityPatch)
	cleanSchemas(request)
	dropIncompatibleBuiltins(request)
	if options.RectifySignatures {
		rectifySignatures(request)
	}
	filterEmptyContents(request)

	requestID := options.RequestID
	if requestID == "" {
		requestID = randomID()
	}
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = vendor.GetUserAgent()
	}
	wrapped := map[string]any{
		"project":     options.ProjectID,
		"requestId":   requestID,
		"userAgent":   userAgent,
		"requestType": "agent",
		"model":       options.WireModel,
		"request":     request,
	}
	return json.Marshal(wrapped)
}

func injectIdentity(request map[string]any, identity string) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		identity = vendor.GetDefaultIdentityPatch()
	}
	instruction, _ := request["systemInstruction"].(map[string]any)
	if hasAntigravityIdentity(instruction) {
		return
	}

	// 不识别具体 harness：客户端 system prompt 统一移到第一条 user content，
	// systemInstruction 只保留 Antigravity 身份。这样任何客户端身份句都不会
	// 进入上游的 systemInstruction，同时不丢失客户端指令。
	clientParts, clientText := splitClientSystemInstruction(instruction)
	if clientText != "" || len(clientParts) > 0 {
		userParts := make([]any, 0, len(clientParts)+3)
		userParts = append(userParts, map[string]any{"text": "<user_system_instructions>"})
		if clientText != "" {
			userParts = append(userParts, map[string]any{"text": clientText})
		}
		userParts = append(userParts, clientParts...)
		userParts = append(userParts, map[string]any{"text": "</user_system_instructions>"})
		contents, _ := request["contents"].([]any)
		request["contents"] = append([]any{map[string]any{"role": "user", "parts": userParts}}, contents...)
	}

	request["systemInstruction"] = map[string]any{
		"role":  "user",
		"parts": []any{map[string]any{"text": identity}},
	}
}

func hasAntigravityIdentity(instruction map[string]any) bool {
	if instruction == nil {
		return false
	}
	if text, _ := instruction["text"].(string); strings.Contains(text, "[IDENTITY_PATCH]") || strings.Contains(text, "You are Antigravity") {
		return true
	}
	parts, _ := instruction["parts"].([]any)
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		text, _ := part["text"].(string)
		if strings.Contains(text, "[IDENTITY_PATCH]") || strings.Contains(text, "You are Antigravity") {
			return true
		}
	}
	return false
}

// splitClientSystemInstruction 将客户端 systemInstruction 拆成文本和非文本部分。
// 文本会以单个 part 放入 user content，非文本 part（如图片）原样保留。
func splitClientSystemInstruction(instruction map[string]any) ([]any, string) {
	if instruction == nil {
		return nil, ""
	}
	if text, _ := instruction["text"].(string); strings.TrimSpace(text) != "" {
		return nil, text
	}
	parts, _ := instruction["parts"].([]any)
	if len(parts) == 0 {
		return nil, ""
	}
	texts := make([]string, 0, len(parts))
	nonText := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		text, _ := part["text"].(string)
		if strings.TrimSpace(text) != "" {
			texts = append(texts, text)
			continue
		}
		nonText = append(nonText, raw)
	}
	return nonText, strings.Join(texts, "\n")
}

func dropIncompatibleBuiltins(request map[string]any) {
	tools, _ := request["tools"].([]any)
	if len(tools) == 0 {
		return
	}

	hasFunctions := false
	for _, rawGroup := range tools {
		group, _ := rawGroup.(map[string]any)
		declarations, _ := group["functionDeclarations"].([]any)
		if len(declarations) > 0 {
			hasFunctions = true
			break
		}
	}
	if !hasFunctions {
		return
	}

	filtered := make([]any, 0, len(tools))
	for _, rawGroup := range tools {
		group, ok := rawGroup.(map[string]any)
		if !ok {
			filtered = append(filtered, rawGroup)
			continue
		}
		delete(group, "googleSearch")
		delete(group, "google_search")
		delete(group, "codeExecution")
		delete(group, "code_execution")
		if len(group) > 0 {
			filtered = append(filtered, group)
		}
	}
	request["tools"] = filtered
	if toolConfig, ok := request["toolConfig"].(map[string]any); ok {
		delete(toolConfig, "includeServerSideToolInvocations")
		delete(toolConfig, "include_server_side_tool_invocations")
		if len(toolConfig) == 0 {
			delete(request, "toolConfig")
		}
	}
}

func cleanSchemas(request map[string]any) {
	tools, _ := request["tools"].([]any)
	for _, rawGroup := range tools {
		group, _ := rawGroup.(map[string]any)
		declarations, _ := group["functionDeclarations"].([]any)
		for _, rawDeclaration := range declarations {
			declaration, _ := rawDeclaration.(map[string]any)
			schema, _ := declaration["parameters"].(map[string]any)
			if schema != nil {
				declaration["parameters"] = vendor.CleanJSONSchema(schema)
			}
		}
	}
}

func rectifySignatures(request map[string]any) {
	contents, _ := request["contents"].([]any)
	for _, rawContent := range contents {
		content, _ := rawContent.(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if _, exists := part["thoughtSignature"]; exists {
				part["thoughtSignature"] = dummyThoughtSignature
			}
		}
	}
}

func filterEmptyContents(request map[string]any) {
	contents, _ := request["contents"].([]any)
	filtered := make([]any, 0, len(contents))
	for _, raw := range contents {
		content, _ := raw.(map[string]any)
		parts, _ := content["parts"].([]any)
		if len(parts) > 0 {
			filtered = append(filtered, raw)
		}
	}
	request["contents"] = filtered
}

func randomID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "protocolconv"
	}
	return hex.EncodeToString(raw[:])
}
