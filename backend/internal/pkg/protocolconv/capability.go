package protocolconv

// Capability identifies a semantic feature independently of provider field names.
type Capability string

const (
	CapabilityText           Capability = "text"
	CapabilitySystem         Capability = "system_instruction"
	CapabilityDeveloper      Capability = "developer_instruction"
	CapabilityImageURL       Capability = "image_url"
	CapabilityImageData      Capability = "image_data"
	CapabilityFile           Capability = "file"
	CapabilityAudio          Capability = "audio"
	CapabilityTools          Capability = "tools"
	CapabilityParallelTools  Capability = "parallel_tools"
	CapabilityReasoning      Capability = "reasoning"
	CapabilitySignature      Capability = "reasoning_signature"
	CapabilityResponseFormat Capability = "response_format"
	CapabilityCacheControl   Capability = "cache_control"
	CapabilityCacheUsage     Capability = "cache_usage"
	CapabilityStreaming      Capability = "streaming"
	CapabilityStreamUsage    Capability = "stream_usage"
	CapabilityCitation       Capability = "citation"
	CapabilityRefusal        Capability = "refusal"
)

// SupportLevel describes whether a standard protocol can express a capability.
type SupportLevel uint8

const (
	SupportNone SupportLevel = iota
	SupportLossy
	SupportFull
)

// CapabilitySet is immutable by convention after converter registration.
type CapabilitySet map[Capability]SupportLevel

// Support returns SupportNone for undeclared capabilities.
func (s CapabilitySet) Support(capability Capability) SupportLevel {
	return s[capability]
}

// LossPolicy controls what happens when the target cannot express a field.
type LossPolicy uint8

const (
	// LossError rejects semantic loss.
	LossError LossPolicy = iota
	// LossWarn allows the converter to drop or normalize a field and requires a warning.
	LossWarn
)

// ChatExtensions enables explicitly negotiated non-standard Chat Completions
// fields. The zero value is fully disabled so existing providers keep standard
// protocol behavior.
type ChatExtensions struct {
	AnthropicCacheControl bool
}

// Options apply to one conversion. Strict loss handling is the default because
// the zero value uses LossError.
type Options struct {
	LossPolicy     LossPolicy
	ChatExtensions ChatExtensions
	// PreserveInstructionMessages 仅在来源和目标都能表达对应角色时，
	// 保留 system/developer 消息，不折叠到 SystemInstruction。
	PreserveInstructionMessages bool
	// PreserveChatReasoningText 保留 Chat reasoning_content 的带标签文本表示，
	// 供不接受 Responses reasoning item 历史输入的上游使用。
	PreserveChatReasoningText bool
	// SourceModel supplies model metadata carried outside the JSON body, as in
	// Google GenAI model-action URLs. It is never inferred by the converter.
	SourceModel string
	// ResponseModel restores the client-visible model on response/stream paths.
	ResponseModel string
	// GenerateAnthropicResponseID replaces an upstream response ID with an
	// Anthropic-compatible synthetic message ID during cross-protocol rendering.
	GenerateAnthropicResponseID bool
	// ToolRoutes is immutable request-scoped metadata populated by Pipeline
	// after request conversion. Direct registry callers normally leave it nil.
	ToolRoutes map[string]ToolRoute
}

func checkCapability(protocol Protocol, capabilities CapabilitySet, capability Capability, path string, options Options) ([]Warning, error) {
	switch capabilities.Support(capability) {
	case SupportFull:
		return nil, nil
	case SupportLossy:
		return []Warning{{
			Code:       WarningNormalizedField,
			Protocol:   protocol,
			Capability: capability,
			Path:       path,
			Message:    "target protocol requires semantic normalization",
		}}, nil
	default:
		if options.LossPolicy == LossWarn {
			return []Warning{{
				Code:       WarningUnsupportedCapability,
				Protocol:   protocol,
				Capability: capability,
				Path:       path,
				Message:    "target protocol cannot express this capability",
			}}, nil
		}
		return nil, &Error{
			Code:       ErrorUnsupportedCapability,
			Protocol:   protocol,
			Capability: capability,
			Path:       path,
			Message:    "target protocol cannot express this capability",
		}
	}
}
