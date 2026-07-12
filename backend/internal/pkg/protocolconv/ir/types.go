// Package ir defines the provider-neutral representation used by protocolconv.
package ir

import "encoding/json"

// Role identifies the semantic author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentType identifies a content part without relying on provider field names.
type ContentType string

const (
	ContentText       ContentType = "text"
	ContentImage      ContentType = "image"
	ContentFile       ContentType = "file"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
	ContentReasoning  ContentType = "reasoning"
	ContentRefusal    ContentType = "refusal"
	ContentCitation   ContentType = "citation"
	ContentAudio      ContentType = "audio"
)

// Request is the canonical request representation. Extensions are opaque values
// owned by the source protocol and are only emitted by a target that understands
// the associated namespace.
type Request struct {
	Model             string
	Messages          []Message
	SystemInstruction []ContentPart
	Tools             []ToolDefinition
	ToolChoice        *ToolChoice
	ToolConfig        *ToolConfig
	Generation        GenerationConfig
	Reasoning         *ReasoningConfig
	ResponseFormat    *ResponseFormat
	Stream            StreamConfig
	Cache             *CacheConfig
	Extensions        map[string]json.RawMessage
}

// Message retains content-part ordering. ProviderMetadata stores opaque replay
// data such as signatures without assigning it cross-provider semantics.
type Message struct {
	Role             Role
	Content          []ContentPart
	Name             string
	ProviderMetadata map[string]json.RawMessage
}

// ContentPart is a tagged union represented as one struct to keep conversion
// code allocation-conscious. Fields are interpreted according to Type.
type ContentPart struct {
	Type ContentType

	Text string

	URL       string
	Data      string
	MediaType string
	Name      string
	Detail    string

	ToolCallID string
	ToolName   string
	ToolInput  json.RawMessage
	ToolResult json.RawMessage
	IsError    bool

	Reasoning string
	Signature string
	Status    string
	Refusal   string
	Citation  json.RawMessage

	CacheHint        json.RawMessage
	ProviderMetadata map[string]json.RawMessage
}

// ToolDefinition describes a callable tool. ProviderType retains standard
// hosted-tool categories that cannot be represented by every target.
type ToolDefinition struct {
	Type             string
	Name             string
	Description      string
	Parameters       json.RawMessage
	Strict           *bool
	ProviderType     string
	CacheHint        json.RawMessage
	ProviderMetadata map[string]json.RawMessage
}

// ToolChoice normalizes provider-specific tool selection controls.
type ToolChoice struct {
	Mode string
	Name string
}

// ToolConfig contains controls separate from tool definitions.
type ToolConfig struct {
	DisableParallel *bool
	MaxCalls        *int
}

// GenerationConfig contains controls that affect generated candidates.
type GenerationConfig struct {
	Temperature      *float64
	TopP             *float64
	TopK             *int
	MaxTokens        *int
	StopSequences    []string
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Seed             *int64
	CandidateCount   *int
}

// ReasoningConfig describes requested reasoning behavior. A nil config means
// the client did not request a reasoning policy.
type ReasoningConfig struct {
	Mode         string
	Effort       string
	BudgetTokens *int
	Summary      string
}

// ResponseFormat describes text or structured output.
type ResponseFormat struct {
	Type       string
	MIMEType   string
	JSONSchema json.RawMessage
}

// StreamConfig preserves both streaming and terminal usage preferences.
type StreamConfig struct {
	Enabled      bool
	IncludeUsage bool
}

// CacheConfig contains portable cache controls where available.
type CacheConfig struct {
	Key       string
	Retention string
}

// Response is the canonical complete response representation.
type Response struct {
	ID               string
	Model            string
	Created          int64
	Status           string
	Choices          []Choice
	Usage            *Usage
	Error            *ErrorInfo
	ProviderMetadata map[string]json.RawMessage
}

// Choice is one generated result.
type Choice struct {
	Index        int
	Message      Message
	FinishReason FinishReason
	Logprobs     json.RawMessage
}

// FinishReason has a normalized reason and optional source detail.
type FinishReason struct {
	Reason         string
	StopSequence   string
	ProviderReason string
}

// Usage stores only provider-reported values. Conversion code must not estimate
// absent usage values.
type Usage struct {
	InputTokens         int
	OutputTokens        int
	TotalTokens         int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int
	InputTokenDetails   map[string]int
	OutputTokenDetails  map[string]int
}

// ErrorInfo represents a protocol-level failed response.
type ErrorInfo struct {
	Code    string
	Message string
	Param   string
}

// StreamEventType identifies a normalized stream lifecycle event.
type StreamEventType string

const (
	EventStreamStart       StreamEventType = "stream_start"
	EventStreamEnd         StreamEventType = "stream_end"
	EventContentBlockStart StreamEventType = "content_block_start"
	EventContentBlockEnd   StreamEventType = "content_block_end"
	EventTextDelta         StreamEventType = "text_delta"
	EventReasoningDelta    StreamEventType = "reasoning_delta"
	EventToolCallStart     StreamEventType = "tool_call_start"
	EventToolCallDelta     StreamEventType = "tool_call_delta"
	EventToolCallEnd       StreamEventType = "tool_call_end"
	EventFinish            StreamEventType = "finish"
	EventUsage             StreamEventType = "usage"
	EventError             StreamEventType = "error"
)

// StreamEvent is emitted in lifecycle order by a source converter and consumed
// by a target converter.
type StreamEvent struct {
	Type StreamEventType

	ResponseID string
	Model      string
	Created    int64

	BlockIndex    int
	BlockType     ContentType
	ChoiceIndex   int
	ToolCallIndex int

	Text           string
	Reasoning      string
	Signature      string
	ToolCallID     string
	ToolName       string
	ArgumentsDelta string

	FinishReason *FinishReason
	Usage        *Usage
	Error        *ErrorInfo

	ProviderMetadata map[string]json.RawMessage
}
