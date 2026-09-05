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
	converted, warnings, err := convertRequest(body, source, options)
	if err != nil {
		return converted, warnings, err
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
		thinking["thinkingLevel"] = strings.ToUpper(effort)
	}
	converted, err = json.Marshal(envelope)
	return converted, warnings, err
}

func convertRequest(body []byte, source protocolconv.Protocol, options Options) ([]byte, []protocolconv.Warning, error) {
	if err := validateOptions(options); err != nil {
		return nil, nil, err
	}
	registry, err := standard.NewRegistry()
	if err != nil {
		return nil, nil, err
	}

	switch options.Family {
	case FamilyClaude:
		standardBody, warnings, err := registry.ConvertRequest(body, source, protocolconv.ProtocolAnthropic, protocolconv.Options{SourceModel: options.SourceModel, LossPolicy: protocolconv.LossError})
		if err != nil {
			return nil, warnings, err
		}
		var request vendor.ClaudeRequest
		if err := json.Unmarshal(standardBody, &request); err != nil {
			return nil, warnings, fmt.Errorf("decode standard Anthropic request: %w", err)
		}
		request.Model = options.SourceModel
		transform := options.TransformOptions
		if transform == (vendor.TransformOptions{}) {
			transform = vendor.DefaultTransformOptions()
		}
		converted, err := vendor.TransformClaudeToGeminiWithOptions(&request, options.ProjectID, options.WireModel, transform)
		if err != nil || source != protocolconv.ProtocolGoogleGenAI || options.RectifySignatures {
			return converted, warnings, err
		}
		converted, err = restoreGoogleToolCallSignatures(body, converted)
		return converted, warnings, err
	case FamilyGemini:
		standardBody, warnings, err := registry.ConvertRequest(body, source, protocolconv.ProtocolGoogleGenAI, protocolconv.Options{SourceModel: options.SourceModel, LossPolicy: protocolconv.LossError})
		if err != nil {
			return nil, warnings, err
		}
		converted, err := adaptGemini(standardBody, options)
		return converted, warnings, err
	default:
		return nil, nil, fmt.Errorf("unsupported Antigravity family %q", options.Family)
	}
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
			if id != "" && signature != "" {
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
	if identity == "" {
		identity = vendor.GetDefaultIdentityPatch()
	}
	instruction, _ := request["systemInstruction"].(map[string]any)
	if instruction == nil {
		instruction = map[string]any{"role": "user"}
	}
	parts, _ := instruction["parts"].([]any)
	for _, raw := range parts {
		if part, ok := raw.(map[string]any); ok {
			if text, _ := part["text"].(string); strings.Contains(text, "[IDENTITY_PATCH]") || strings.Contains(text, "<identity>") {
				request["systemInstruction"] = instruction
				return
			}
		}
	}
	instruction["parts"] = append([]any{map[string]any{"text": identity}}, parts...)
	request["systemInstruction"] = instruction
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
