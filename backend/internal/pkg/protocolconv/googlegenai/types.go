package googlegenai

import "encoding/json"

type requestWire struct {
	SystemInstruction *contentWire    `json:"systemInstruction,omitempty"`
	Contents          []contentWire   `json:"contents"`
	Tools             []toolGroupWire `json:"tools,omitempty"`
	ToolConfig        *toolConfigWire `json:"toolConfig,omitempty"`
	GenerationConfig  *generationWire `json:"generationConfig,omitempty"`
	SafetySettings    json.RawMessage `json:"safetySettings,omitempty"`
	CachedContent     string          `json:"cachedContent,omitempty"`
}

type contentWire struct {
	Role  string     `json:"role,omitempty"`
	Parts []partWire `json:"parts"`
}

type partWire struct {
	Text             string                `json:"text,omitempty"`
	InlineData       *blobWire             `json:"inlineData,omitempty"`
	FileData         *fileDataWire         `json:"fileData,omitempty"`
	FunctionCall     *functionCallWire     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponseWire `json:"functionResponse,omitempty"`
	Thought          bool                  `json:"thought,omitempty"`
	ThoughtSignature string                `json:"thoughtSignature,omitempty"`
	CacheControl     json.RawMessage       `json:"cache_control,omitempty"`
}

type blobWire struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type fileDataWire struct {
	MIMEType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

type functionCallWire struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponseWire struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type toolGroupWire struct {
	FunctionDeclarations []functionDeclarationWire `json:"functionDeclarations,omitempty"`
	GoogleSearch         json.RawMessage           `json:"googleSearch,omitempty"`
}

type functionDeclarationWire struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type toolConfigWire struct {
	FunctionCallingConfig *functionCallingConfigWire `json:"functionCallingConfig,omitempty"`
}

type functionCallingConfigWire struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type generationWire struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"topP,omitempty"`
	TopK             *int            `json:"topK,omitempty"`
	CandidateCount   *int            `json:"candidateCount,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	PresencePenalty  *float64        `json:"presencePenalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequencyPenalty,omitempty"`
	Seed             *int64          `json:"seed,omitempty"`
	ResponseMIMEType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
	ThinkingConfig   *thinkingWire   `json:"thinkingConfig,omitempty"`
}

type thinkingWire struct {
	IncludeThoughts *bool  `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type responseWire struct {
	Candidates     []candidateWire `json:"candidates,omitempty"`
	UsageMetadata  *usageWire      `json:"usageMetadata,omitempty"`
	ModelVersion   string          `json:"modelVersion,omitempty"`
	ResponseID     string          `json:"responseId,omitempty"`
	PromptFeedback json.RawMessage `json:"promptFeedback,omitempty"`
}

type candidateWire struct {
	Content           contentWire     `json:"content"`
	FinishReason      string          `json:"finishReason,omitempty"`
	FinishMessage     string          `json:"finishMessage,omitempty"`
	Index             int             `json:"index,omitempty"`
	SafetyRatings     json.RawMessage `json:"safetyRatings,omitempty"`
	CitationMetadata  json.RawMessage `json:"citationMetadata,omitempty"`
	GroundingMetadata json.RawMessage `json:"groundingMetadata,omitempty"`
}

type usageWire struct {
	PromptTokenCount        int               `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int               `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         int               `json:"totalTokenCount,omitempty"`
	CachedContentTokenCount int               `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount      int               `json:"thoughtsTokenCount,omitempty"`
	ToolUsePromptTokenCount int               `json:"toolUsePromptTokenCount,omitempty"`
	PromptTokensDetails     []tokenDetailWire `json:"promptTokensDetails,omitempty"`
	CandidatesTokensDetails []tokenDetailWire `json:"candidatesTokensDetails,omitempty"`
	CacheTokensDetails      []tokenDetailWire `json:"cacheTokensDetails,omitempty"`
}

type tokenDetailWire struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}
