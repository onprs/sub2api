package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	CLIImportOSWindows = "windows"
	CLIImportOSLinux   = "linux"

	cliImportModelsDevAPIURL = "https://models.dev/api.json"
	cliImportProviderName    = "OnprsCodexApi"
)

var (
	ErrCLIImportInvalidOS              = infraerrors.BadRequest("CLI_IMPORT_INVALID_OS", "unsupported cli import script os")
	ErrCLIImportAPIKeyForbidden        = infraerrors.Forbidden("CLI_IMPORT_API_KEY_FORBIDDEN", "api key does not belong to current user")
	ErrCLIImportAPIKeyInactive         = infraerrors.Forbidden("CLI_IMPORT_API_KEY_INACTIVE", "api key is not active")
	ErrCLIImportAPIKeyExpired          = infraerrors.Forbidden("CLI_IMPORT_API_KEY_EXPIRED", "api key has expired")
	ErrCLIImportAPIKeyQuotaExhausted   = infraerrors.Forbidden("CLI_IMPORT_API_KEY_QUOTA_EXHAUSTED", "api key quota is exhausted")
	ErrCLIImportAPIKeyNoGroup          = infraerrors.BadRequest("CLI_IMPORT_API_KEY_NO_GROUP", "api key is not bound to a group")
	ErrCLIImportAPIKeyGroupInactive    = infraerrors.Forbidden("CLI_IMPORT_API_KEY_GROUP_INACTIVE", "api key group is not active")
	ErrCLIImportMissingAPIBaseURL      = infraerrors.BadRequest("CLI_IMPORT_MISSING_API_BASE_URL", "api base url is required")
	ErrCLIImportNoImportableModel      = infraerrors.BadRequest("CLI_IMPORT_NO_IMPORTABLE_MODEL", "no concrete model is available for cli import")
	ErrCLIImportMissingAPIKeySensitive = infraerrors.BadRequest("CLI_IMPORT_MISSING_API_KEY", "api key secret is missing")
	ErrCLIImportModelCapabilityUnknown = infraerrors.BadRequest("CLI_IMPORT_MODEL_CAPABILITY_UNKNOWN", "OpenCode model capability metadata is incomplete")
)

type CLIImportScriptInput struct {
	OS           string
	APIBaseURL   string
	APIKey       *APIKey
	Models       []string
	Capabilities map[string]CLIImportModelCapability
}

type CLIImportScriptResult struct {
	Filename    string
	ContentType string
	Body        []byte
}

type CLIImportModelCapability struct {
	Name                         string
	Family                       string
	ReleaseDate                  string
	Status                       string
	Attachment                   bool
	SupportsReasoning            bool
	SupportsVision               bool
	SupportsPDFInput             bool
	SupportsFunctionCalling      bool
	SupportsToolChoice           bool
	MaxInputTokens               int
	MaxOutputTokens              int
	InputModalities              []string
	OutputModalities             []string
	Mode                         string
	InputCostPerToken            *float64
	OutputCostPerToken           *float64
	CacheReadCostPerToken        *float64
	CacheWriteCostPerToken       *float64
	OutputCostPerImage           *float64
	OutputCostPerImageToken      *float64
	ReasoningKnown               bool
	AttachmentKnown              bool
	ToolCallKnown                bool
	ModalitiesKnown              bool
	LimitKnown                   bool
	CostKnown                    bool
	Temperature                  bool
	TemperatureKnown             bool
	SupportsVisionKnown          bool
	SupportsPDFInputKnown        bool
	SupportsFunctionCallingKnown bool
	SupportsToolChoiceKnown      bool
}

type CLIImportAvailableModelsProvider interface {
	GetAvailableModels(ctx context.Context, groupID *int64, platform string) []string
	GetAvailableModelPricingCandidates(ctx context.Context, groupID *int64, platform string, models []string) map[string][]string
}

type CLIImportCapabilityProvider interface {
	GetCLIImportModelCapability(ctx context.Context, platform, model string) (CLIImportModelCapability, bool)
}

type cliImportPayload struct {
	KeyID          int64                `json:"key_id"`
	KeyName        string               `json:"key_name"`
	GroupID        int64                `json:"group_id"`
	GroupName      string               `json:"group_name"`
	Platform       string               `json:"platform"`
	APIKey         string               `json:"api_key"`
	BaseURL        string               `json:"base_url"`
	EnvName        string               `json:"env_name"`
	ProviderID     string               `json:"provider_id"`
	ProviderName   string               `json:"provider_name"`
	DefaultModel   string               `json:"default_model"`
	CodexSupported bool                 `json:"codex_supported"`
	Models         []cliImportModelSpec `json:"models"`
}

type cliImportModelSpec struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Family      string                   `json:"family,omitempty"`
	Reasoning   bool                     `json:"reasoning"`
	Attachment  bool                     `json:"attachment"`
	ToolCall    bool                     `json:"tool_call"`
	Temperature *bool                    `json:"temperature,omitempty"`
	Modalities  cliImportModelModalities `json:"modalities"`
	Limit       cliImportModelLimit      `json:"limit"`
	Cost        cliImportModelCost       `json:"cost"`
}

type cliImportModelModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type cliImportModelLimit struct {
	Context int  `json:"context"`
	Input   *int `json:"input,omitempty"`
	Output  int  `json:"output"`
}

type cliImportModelCost struct {
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

type cliImportModelsDevProvider struct {
	Models map[string]cliImportModelsDevModel `json:"models"`
}

type cliImportModelsDevModel struct {
	ID          string                        `json:"id"`
	Name        string                        `json:"name"`
	Family      string                        `json:"family"`
	ReleaseDate string                        `json:"release_date"`
	Attachment  *bool                         `json:"attachment"`
	Reasoning   *bool                         `json:"reasoning"`
	Temperature *bool                         `json:"temperature"`
	ToolCall    *bool                         `json:"tool_call"`
	Modalities  *cliImportModelsDevModalities `json:"modalities"`
	Limit       *cliImportModelsDevLimit      `json:"limit"`
	Cost        *cliImportModelsDevCost       `json:"cost"`
	Status      string                        `json:"status"`
}

type cliImportModelsDevModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type cliImportModelsDevLimit struct {
	Context *int `json:"context"`
	Input   *int `json:"input"`
	Output  *int `json:"output"`
}

type cliImportModelsDevCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

func (s *APIKeyService) BuildCLIImportScript(ctx context.Context, input CLIImportScriptInput, userID int64, modelProvider CLIImportAvailableModelsProvider, capabilityProvider CLIImportCapabilityProvider) (*CLIImportScriptResult, error) {
	if s == nil {
		return nil, fmt.Errorf("api key service is nil")
	}
	if input.APIKey == nil {
		return nil, ErrAPIKeyNotFound
	}
	key, err := s.GetByID(ctx, input.APIKey.ID)
	if err != nil {
		return nil, err
	}
	input.APIKey = key
	if err := validateCLIImportAPIKey(key, userID); err != nil {
		return nil, err
	}
	groupID := key.Group.ID
	available := resolveCLIImportModelList(nil, key.Group)
	if modelProvider != nil {
		available = resolveCLIImportModelList(modelProvider.GetAvailableModels(ctx, &groupID, key.Group.Platform), key.Group)
	}
	input.Models = available
	input.Capabilities = resolveCLIImportCapabilities(ctx, &groupID, key.Group.Platform, available, modelProvider, capabilityProvider)
	return BuildCLIImportScript(input)
}

func BuildCLIImportScript(input CLIImportScriptInput) (*CLIImportScriptResult, error) {
	if err := validateCLIImportAPIKey(input.APIKey, input.APIKeyUserID()); err != nil {
		return nil, err
	}
	baseURL := normalizeCLIImportAPIBaseURL(input.APIBaseURL)
	if baseURL == "" {
		return nil, ErrCLIImportMissingAPIBaseURL
	}
	if strings.TrimSpace(input.APIKey.Key) == "" {
		return nil, ErrCLIImportMissingAPIKeySensitive
	}

	models := resolveCLIImportModelList(input.Models, input.APIKey.Group)
	models = ensureCLIImportDefaultModel(models, strings.TrimSpace(input.APIKey.Group.DefaultMappedModel))
	if len(models) == 0 {
		return nil, ErrCLIImportNoImportableModel
	}
	if err := validateCLIImportOpenCodeCapabilities(models, input.Capabilities); err != nil {
		return nil, err
	}

	payload := buildCLIImportPayload(input.APIKey, baseURL, models, input.Capabilities)
	payloadJSON, err := marshalCLIImportJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal cli import payload: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(input.OS)) {
	case CLIImportOSWindows:
		return &CLIImportScriptResult{
			Filename:    "sub2api-cli-import.bat",
			ContentType: "application/octet-stream",
			Body:        []byte(renderCLIImportWindowsScript(payloadJSON)),
		}, nil
	case CLIImportOSLinux:
		return &CLIImportScriptResult{
			Filename:    "sub2api-cli-import.sh",
			ContentType: "application/octet-stream",
			Body:        []byte(renderCLIImportShellScript(payloadJSON)),
		}, nil
	default:
		return nil, ErrCLIImportInvalidOS
	}
}

func (input CLIImportScriptInput) APIKeyUserID() int64 {
	if input.APIKey == nil {
		return 0
	}
	return input.APIKey.UserID
}

func validateCLIImportAPIKey(key *APIKey, userID int64) error {
	if key == nil {
		return ErrAPIKeyNotFound
	}
	if key.UserID != userID {
		return ErrCLIImportAPIKeyForbidden
	}
	switch key.Status {
	case StatusAPIKeyQuotaExhausted:
		return ErrCLIImportAPIKeyQuotaExhausted
	case StatusAPIKeyExpired:
		return ErrCLIImportAPIKeyExpired
	case StatusAPIKeyActive:
	default:
		return ErrCLIImportAPIKeyInactive
	}
	if key.IsExpired() {
		return ErrCLIImportAPIKeyExpired
	}
	if key.IsQuotaExhausted() {
		return ErrCLIImportAPIKeyQuotaExhausted
	}
	if key.GroupID == nil || key.Group == nil {
		return ErrCLIImportAPIKeyNoGroup
	}
	if key.Group.Status != "" && key.Group.Status != StatusActive {
		return ErrCLIImportAPIKeyGroupInactive
	}
	return nil
}

func normalizeCLIImportAPIBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	baseURL = strings.TrimRight(baseURL, "/")
	for {
		lower := strings.ToLower(baseURL)
		if strings.HasSuffix(lower, "/v1") {
			baseURL = strings.TrimRight(baseURL[:len(baseURL)-3], "/")
			continue
		}
		if strings.HasSuffix(lower, "/v1beta") {
			baseURL = strings.TrimRight(baseURL[:len(baseURL)-7], "/")
			continue
		}
		return baseURL
	}
}

func resolveCLIImportModelList(available []string, group *Group) []string {
	if group != nil && group.CustomModelsListEnabled() {
		return cleanCLIImportModelList(group.ModelsListConfig.Models)
	}
	if cleaned := cleanCLIImportModelList(available); len(cleaned) > 0 {
		return cleaned
	}
	if group == nil {
		return nil
	}
	return cleanCLIImportModelList(DefaultModelIDsForPlatform(group.Platform))
}

func cleanCLIImportModelList(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || strings.Contains(model, "*") {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ensureCLIImportDefaultModel(models []string, defaultModel string) []string {
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel == "" || strings.Contains(defaultModel, "*") {
		return models
	}
	for _, model := range models {
		if model == defaultModel {
			return models
		}
	}
	return append([]string{defaultModel}, models...)
}

func DefaultModelIDsForPlatform(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return openai.DefaultModelIDs()
	case PlatformGemini:
		ids := make([]string, 0, len(geminicli.DefaultModels))
		for _, model := range geminicli.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	case PlatformOpenCodeGo:
		return OpenCodeGoDefaultModelIDs()
	case PlatformClinePass:
		return ClinePassDefaultModelIDs()
	case PlatformAntigravity:
		models := antigravity.DefaultModels()
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		return ids
	default:
		ids := make([]string, 0, len(claude.DefaultModels))
		for _, model := range claude.DefaultModels {
			ids = append(ids, model.ID)
		}
		return ids
	}
}

func resolveCLIImportCapabilities(ctx context.Context, groupID *int64, platform string, models []string, modelProvider CLIImportAvailableModelsProvider, capabilityProvider CLIImportCapabilityProvider) map[string]CLIImportModelCapability {
	caps := make(map[string]CLIImportModelCapability, len(models))
	if len(models) == 0 || capabilityProvider == nil {
		return caps
	}
	candidates := make(map[string][]string, len(models))
	if modelProvider != nil {
		candidates = modelProvider.GetAvailableModelPricingCandidates(ctx, groupID, platform, models)
	}
	for _, model := range models {
		try := append([]string(nil), candidates[model]...)
		try = append(try, model)
		for _, candidate := range try {
			if cap, ok := capabilityProvider.GetCLIImportModelCapability(ctx, platform, candidate); ok {
				caps[model] = cap
				break
			}
		}
	}
	return caps
}

func (s *PricingService) getCLIImportModelsDevCapability(ctx context.Context, platform, model string) (CLIImportModelCapability, bool) {
	providerID := cliImportModelsDevProviderForPlatform(platform)
	if providerID == "" {
		return CLIImportModelCapability{}, false
	}
	catalog := s.getCLIImportModelsDevCatalog(ctx)
	if len(catalog) == 0 {
		return CLIImportModelCapability{}, false
	}
	providerModels := catalog[providerID]
	if len(providerModels) == 0 {
		return CLIImportModelCapability{}, false
	}
	cap, ok := providerModels[strings.ToLower(strings.TrimSpace(model))]
	return cap, ok && cap.openCodeComplete()
}

func (s *PricingService) getCLIImportModelsDevCatalog(ctx context.Context) map[string]map[string]CLIImportModelCapability {
	if s == nil {
		return nil
	}
	s.cliImportCatalogMu.RLock()
	if s.cliImportCatalogLoaded {
		catalog := s.cliImportCatalog
		s.cliImportCatalogMu.RUnlock()
		return catalog
	}
	s.cliImportCatalogMu.RUnlock()

	s.cliImportCatalogMu.Lock()
	defer s.cliImportCatalogMu.Unlock()
	if s.cliImportCatalogLoaded {
		return s.cliImportCatalog
	}
	if s.remoteClient == nil {
		return nil
	}
	body, err := s.remoteClient.FetchPricingJSON(ctx, cliImportModelsDevAPIURL)
	if err != nil {
		return nil
	}
	catalog, err := parseCLIImportModelsDevCatalog(body)
	if err != nil {
		return nil
	}
	s.cliImportCatalog = catalog
	s.cliImportCatalogLoaded = true
	return s.cliImportCatalog
}

func parseCLIImportModelsDevCatalog(body []byte) (map[string]map[string]CLIImportModelCapability, error) {
	var raw map[string]cliImportModelsDevProvider
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]CLIImportModelCapability, len(raw))
	for providerID, provider := range raw {
		providerID = strings.ToLower(strings.TrimSpace(providerID))
		if providerID == "" || len(provider.Models) == 0 {
			continue
		}
		models := make(map[string]CLIImportModelCapability, len(provider.Models))
		for modelID, model := range provider.Models {
			cap, ok := model.toCLIImportCapability()
			if !ok {
				continue
			}
			models[strings.ToLower(strings.TrimSpace(modelID))] = cap
			if strings.TrimSpace(model.ID) != "" {
				models[strings.ToLower(strings.TrimSpace(model.ID))] = cap
			}
		}
		if len(models) > 0 {
			out[providerID] = models
		}
	}
	return out, nil
}

func (m cliImportModelsDevModel) toCLIImportCapability() (CLIImportModelCapability, bool) {
	if m.Reasoning == nil || m.Attachment == nil || m.ToolCall == nil || m.Modalities == nil || m.Limit == nil || m.Cost == nil {
		return CLIImportModelCapability{}, false
	}
	if m.Limit.Context == nil || m.Limit.Output == nil || m.Cost.Input == nil || m.Cost.Output == nil {
		return CLIImportModelCapability{}, false
	}
	inputModalities := cleanCLIImportModalities(m.Modalities.Input)
	outputModalities := cleanCLIImportModalities(m.Modalities.Output)
	if len(inputModalities) == 0 || len(outputModalities) == 0 || *m.Limit.Context <= 0 || *m.Limit.Output <= 0 {
		return CLIImportModelCapability{}, false
	}
	cap := CLIImportModelCapability{
		Name:                         strings.TrimSpace(m.Name),
		Family:                       strings.TrimSpace(m.Family),
		ReleaseDate:                  strings.TrimSpace(m.ReleaseDate),
		Status:                       strings.TrimSpace(m.Status),
		Attachment:                   *m.Attachment,
		SupportsReasoning:            *m.Reasoning,
		SupportsFunctionCalling:      *m.ToolCall,
		MaxInputTokens:               *m.Limit.Context,
		MaxOutputTokens:              *m.Limit.Output,
		InputModalities:              inputModalities,
		OutputModalities:             outputModalities,
		InputCostPerToken:            cliImportFloat64Ptr(*m.Cost.Input),
		OutputCostPerToken:           cliImportFloat64Ptr(*m.Cost.Output),
		ReasoningKnown:               true,
		AttachmentKnown:              true,
		ToolCallKnown:                true,
		ModalitiesKnown:              true,
		LimitKnown:                   true,
		CostKnown:                    true,
		SupportsVision:               containsString(inputModalities, "image"),
		SupportsPDFInput:             containsString(inputModalities, "pdf"),
		SupportsVisionKnown:          true,
		SupportsPDFInputKnown:        true,
		SupportsFunctionCallingKnown: true,
	}
	if strings.TrimSpace(cap.Name) == "" {
		cap.Name = strings.TrimSpace(m.ID)
	}
	if m.Temperature != nil {
		cap.Temperature = *m.Temperature
		cap.TemperatureKnown = true
	}
	if m.Cost.CacheRead != nil {
		cap.CacheReadCostPerToken = cliImportFloat64Ptr(*m.Cost.CacheRead)
	}
	if m.Cost.CacheWrite != nil {
		cap.CacheWriteCostPerToken = cliImportFloat64Ptr(*m.Cost.CacheWrite)
	}
	return cap, true
}

func cliImportModelsDevProviderForPlatform(platform string) string {
	switch strings.TrimSpace(platform) {
	case PlatformOpenAI:
		return "openai"
	case PlatformAnthropic:
		return "anthropic"
	case PlatformGemini, PlatformAntigravity:
		return "google"
	case PlatformOpenCodeGo:
		return "opencode-go"
	default:
		return ""
	}
}

func cleanCLIImportModalities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *PricingService) GetCLIImportModelCapability(ctx context.Context, platform, model string) (CLIImportModelCapability, bool) {
	if s == nil {
		return CLIImportModelCapability{}, false
	}
	if cap, ok := s.getCLIImportModelsDevCapability(ctx, platform, model); ok {
		return cap, true
	}
	if cap, ok := getOpenCodeGoBuiltinCLIImportCapability(platform, model); ok {
		return cap, true
	}
	pricing := s.GetModelPricing(model)
	if pricing == nil {
		return CLIImportModelCapability{}, false
	}
	cap := CLIImportModelCapability{
		SupportsReasoning:            pricing.SupportsReasoning,
		Attachment:                   pricing.SupportsVision || pricing.SupportsPDFInput,
		SupportsVision:               pricing.SupportsVision,
		SupportsPDFInput:             pricing.SupportsPDFInput,
		SupportsFunctionCalling:      pricing.SupportsFunctionCalling,
		SupportsToolChoice:           pricing.SupportsToolChoice,
		MaxInputTokens:               pricing.MaxInputTokens,
		MaxOutputTokens:              pricing.MaxOutputTokens,
		Mode:                         pricing.Mode,
		ReasoningKnown:               pricing.SupportsReasoningKnown,
		AttachmentKnown:              pricing.openCodeAttachmentKnown(),
		ToolCallKnown:                pricing.openCodeToolCallKnown(),
		ModalitiesKnown:              pricing.openCodeModalitiesKnown(),
		LimitKnown:                   pricing.MaxInputTokensKnown && pricing.MaxOutputTokensKnown,
		CostKnown:                    pricing.InputCostPerTokenKnown && pricing.OutputCostPerTokenKnown,
		SupportsVisionKnown:          pricing.SupportsVisionKnown,
		SupportsPDFInputKnown:        pricing.SupportsPDFInputKnown,
		SupportsFunctionCallingKnown: pricing.SupportsFunctionCallingKnown,
		SupportsToolChoiceKnown:      pricing.SupportsToolChoiceKnown,
	}
	if cap.ModalitiesKnown {
		cap.InputModalities = []string{"text"}
		if pricing.SupportsVision {
			cap.InputModalities = append(cap.InputModalities, "image")
		}
		if pricing.SupportsPDFInput {
			cap.InputModalities = append(cap.InputModalities, "pdf")
		}
		cap.OutputModalities = []string{"text"}
	}
	if pricing.InputCostPerTokenKnown {
		cap.InputCostPerToken = cliImportFloat64Ptr(pricing.InputCostPerToken * 1000000)
	}
	if pricing.OutputCostPerTokenKnown {
		cap.OutputCostPerToken = cliImportFloat64Ptr(pricing.OutputCostPerToken * 1000000)
	}
	if pricing.CacheReadInputTokenCostKnown {
		cap.CacheReadCostPerToken = cliImportFloat64Ptr(pricing.CacheReadInputTokenCost * 1000000)
	}
	if pricing.CacheCreationInputTokenCostKnown {
		cap.CacheWriteCostPerToken = cliImportFloat64Ptr(pricing.CacheCreationInputTokenCost * 1000000)
	} else if pricing.CacheCreationInputTokenCostAbove1hrKnown {
		cap.CacheWriteCostPerToken = cliImportFloat64Ptr(pricing.CacheCreationInputTokenCostAbove1hr * 1000000)
	}
	if pricing.OutputCostPerImage != 0 {
		cap.OutputCostPerImage = cliImportFloat64Ptr(pricing.OutputCostPerImage)
	}
	if pricing.OutputCostPerImageToken != 0 {
		cap.OutputCostPerImageToken = cliImportFloat64Ptr(pricing.OutputCostPerImageToken)
	}
	return cap, true
}

var openCodeGoBuiltinCLIImportCapabilities = map[string]CLIImportModelCapability{
	"deepseek-v4-flash": newOpenCodeGoBuiltinCLIImportCapability(
		"DeepSeek V4 Flash", "deepseek-flash", false, true, true, true,
		[]string{"text"}, 1000000, 384000, 0.14, 0.28, cliImportFloat64Ptr(0.0028), nil,
	),
	"deepseek-v4-pro": newOpenCodeGoBuiltinCLIImportCapability(
		"DeepSeek V4 Pro", "deepseek-thinking", false, true, true, true,
		[]string{"text"}, 1000000, 384000, 1.74, 3.48, cliImportFloat64Ptr(0.0145), nil,
	),
	"glm-5": newOpenCodeGoBuiltinCLIImportCapability(
		"GLM-5", "glm", false, true, true, true,
		[]string{"text"}, 202752, 32768, 1, 3.2, cliImportFloat64Ptr(0.2), nil,
	),
	"glm-5.1": newOpenCodeGoBuiltinCLIImportCapability(
		"GLM-5.1", "glm", false, true, true, true,
		[]string{"text"}, 202752, 32768, 1.4, 4.4, cliImportFloat64Ptr(0.26), nil,
	),
	"glm-5.2": newOpenCodeGoBuiltinCLIImportCapability(
		"GLM-5.2", "glm", false, true, true, true,
		[]string{"text"}, 1000000, 131072, 1.4, 4.4, cliImportFloat64Ptr(0.26), nil,
	),
	"hy3-preview": newOpenCodeGoBuiltinCLIImportCapability(
		"HY3 Preview", "hy3", false, true, true, true,
		[]string{"text"}, 262144, 65536, 0, 0, nil, nil,
	),
	// Hy3 GA (graduated from preview). Pricing mirrors the OpenCode Go docs
	// ($0.14/$0.58/$0.035 per 1M tokens); limits/caps follow the Tencent Hy3
	// native spec published on models.dev.
	"hy3": newOpenCodeGoBuiltinCLIImportCapability(
		"Hy3", "hy3", false, true, true, true,
		[]string{"text"}, 262144, 65536, 0.14, 0.58, cliImportFloat64Ptr(0.035), nil,
	),
	// Grok 4.5: OpenCode Go serves it via /chat/completions at
	// $2/$6/$0.30 (cache read) per 1M tokens. Output spec mirrors the xAI
	// canonical models.dev entry (500000 input / 500000 output).
	"grok-4.5": newOpenCodeGoBuiltinCLIImportCapability(
		"Grok 4.5", "grok", true, true, true, true,
		[]string{"text", "image"}, 500000, 500000, 2.0, 6.0, cliImportFloat64Ptr(0.30), nil,
	),
	"kimi-k2.5": newOpenCodeGoBuiltinCLIImportCapability(
		"Kimi K2.5", "kimi-k2", true, true, true, true,
		[]string{"text", "image", "video"}, 262144, 65536, 0.6, 3, cliImportFloat64Ptr(0.1), nil,
	),
	"kimi-k2.6": newOpenCodeGoBuiltinCLIImportCapability(
		"Kimi K2.6", "kimi-k2", true, true, true, true,
		[]string{"text", "image", "video"}, 262144, 65536, 0.95, 4, cliImportFloat64Ptr(0.16), nil,
	),
	"kimi-k2.7": newOpenCodeGoBuiltinCLIImportCapability(
		"Kimi K2.7", "kimi-k2", true, true, false, true,
		[]string{"text", "image", "video"}, 262144, 262144, 0.95, 4, cliImportFloat64Ptr(0.19), nil,
	),
	"kimi-k2.7-code": newOpenCodeGoBuiltinCLIImportCapability(
		"Kimi K2.7 Code", "kimi-k2", true, true, false, true,
		[]string{"text", "image", "video"}, 262144, 262144, 0.95, 4, cliImportFloat64Ptr(0.19), nil,
	),
	// Kimi K3: OpenCode Go serves it via /chat/completions at
	// $3/$15/$0.30 (cache read) per 1M tokens. Native spec is 1M context /
	// 131072 output, multimodal text+image+video, reasoning toggle, no temperature.
	"kimi-k3": newOpenCodeGoBuiltinCLIImportCapability(
		"Kimi K3", "kimi-k3", true, true, false, true,
		[]string{"text", "image", "video"}, 1048576, 131072, 3.0, 15.0, cliImportFloat64Ptr(0.30), nil,
	),
	"mimo-v2.5": newOpenCodeGoBuiltinCLIImportCapability(
		"MiMo V2.5", "mimo-v2.5", true, true, true, true,
		[]string{"text", "image", "audio", "video"}, 1000000, 128000, 0.14, 0.28, cliImportFloat64Ptr(0.0028), nil,
	),
	"mimo-v2.5-pro": newOpenCodeGoBuiltinCLIImportCapability(
		"MiMo V2.5 Pro", "mimo-v2.5-pro", true, true, true, true,
		[]string{"text"}, 1048576, 128000, 1.74, 3.48, cliImportFloat64Ptr(0.0145), nil,
	),
	"mimo-v2-omni": newOpenCodeGoBuiltinCLIImportCapability(
		"MiMo V2 Omni", "mimo-v2-omni", true, true, true, true,
		[]string{"text", "image", "audio", "pdf"}, 262144, 128000, 0.4, 2, cliImportFloat64Ptr(0.08), nil,
	),
	"mimo-v2-pro": newOpenCodeGoBuiltinCLIImportCapability(
		"MiMo V2 Pro", "mimo-v2-pro", true, true, true, true,
		[]string{"text"}, 1048576, 128000, 1, 3, cliImportFloat64Ptr(0.2), nil,
	),
	"minimax-m2.5": newOpenCodeGoBuiltinCLIImportCapability(
		"MiniMax M2.5", "minimax-m2.5", false, true, true, true,
		[]string{"text"}, 204800, 65536, 0.3, 1.2, cliImportFloat64Ptr(0.03), nil,
	),
	"minimax-m2.7": newOpenCodeGoBuiltinCLIImportCapability(
		"MiniMax M2.7", "minimax-m2.7", false, true, true, true,
		[]string{"text"}, 204800, 131072, 0.3, 1.2, cliImportFloat64Ptr(0.06), nil,
	),
	"minimax-m3": newOpenCodeGoBuiltinCLIImportCapability(
		"MiniMax M3", "minimax-m3", false, true, true, true,
		[]string{"text", "image", "video"}, 512000, 131072, 0.1, 0.4, cliImportFloat64Ptr(0.02), nil,
	),
	"qwen3.6-plus": newOpenCodeGoBuiltinCLIImportCapability(
		"Qwen3.6 Plus", "qwen3.6", true, true, true, true,
		[]string{"text", "image", "video"}, 1000000, 65536, 0.5, 3, cliImportFloat64Ptr(0.05), cliImportFloat64Ptr(0.625),
	),
	"qwen3.5-plus": newOpenCodeGoBuiltinCLIImportCapability(
		"Qwen3.5 Plus", "qwen3.5", true, true, true, true,
		[]string{"text", "image", "video"}, 262144, 65536, 0.2, 1.2, cliImportFloat64Ptr(0.02), cliImportFloat64Ptr(0.25),
	),
	"qwen3.7-max": newOpenCodeGoBuiltinCLIImportCapability(
		"Qwen3.7 Max", "qwen3.7-max", false, true, true, true,
		[]string{"text"}, 1000000, 65536, 2.5, 7.5, cliImportFloat64Ptr(0.5), cliImportFloat64Ptr(3.125),
	),
	"qwen3.7-plus": newOpenCodeGoBuiltinCLIImportCapability(
		"Qwen3.7 Plus", "qwen3.7-plus", true, true, true, true,
		[]string{"text", "image", "video"}, 1000000, 65536, 0.4, 1.6, cliImportFloat64Ptr(0.04), cliImportFloat64Ptr(0.5),
	),
}

func getOpenCodeGoBuiltinCLIImportCapability(platform, model string) (CLIImportModelCapability, bool) {
	if strings.TrimSpace(platform) != PlatformOpenCodeGo {
		return CLIImportModelCapability{}, false
	}
	cap, ok := openCodeGoBuiltinCLIImportCapabilities[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return CLIImportModelCapability{}, false
	}
	cap.InputModalities = append([]string(nil), cap.InputModalities...)
	cap.OutputModalities = append([]string(nil), cap.OutputModalities...)
	return cap, true
}

func newOpenCodeGoBuiltinCLIImportCapability(name, family string, attachment, reasoning, temperature, toolCall bool, inputModalities []string, contextTokens, outputTokens int, inputCost, outputCost float64, cacheRead, cacheWrite *float64) CLIImportModelCapability {
	input := cleanCLIImportModalities(inputModalities)
	return CLIImportModelCapability{
		Name:                         name,
		Family:                       family,
		Attachment:                   attachment,
		SupportsReasoning:            reasoning,
		SupportsFunctionCalling:      toolCall,
		MaxInputTokens:               contextTokens,
		MaxOutputTokens:              outputTokens,
		InputModalities:              input,
		OutputModalities:             []string{"text"},
		InputCostPerToken:            cliImportFloat64Ptr(inputCost),
		OutputCostPerToken:           cliImportFloat64Ptr(outputCost),
		CacheReadCostPerToken:        cacheRead,
		CacheWriteCostPerToken:       cacheWrite,
		ReasoningKnown:               true,
		AttachmentKnown:              true,
		ToolCallKnown:                true,
		ModalitiesKnown:              true,
		LimitKnown:                   true,
		CostKnown:                    true,
		Temperature:                  temperature,
		TemperatureKnown:             true,
		SupportsVision:               containsString(input, "image"),
		SupportsPDFInput:             containsString(input, "pdf"),
		SupportsVisionKnown:          true,
		SupportsPDFInputKnown:        true,
		SupportsFunctionCallingKnown: true,
	}
}

func (p *LiteLLMModelPricing) openCodeAttachmentKnown() bool {
	if p == nil {
		return false
	}
	if p.SupportsVisionKnown && p.SupportsVision {
		return true
	}
	if p.SupportsPDFInputKnown && p.SupportsPDFInput {
		return true
	}
	return p.SupportsVisionKnown && p.SupportsPDFInputKnown
}

func (p *LiteLLMModelPricing) openCodeToolCallKnown() bool {
	if p == nil {
		return false
	}
	if p.SupportsFunctionCallingKnown && p.SupportsFunctionCalling {
		return true
	}
	if p.SupportsToolChoiceKnown && p.SupportsToolChoice {
		return true
	}
	return p.SupportsFunctionCallingKnown && p.SupportsToolChoiceKnown
}

func (p *LiteLLMModelPricing) openCodeModalitiesKnown() bool {
	if p == nil {
		return false
	}
	return p.SupportsVisionKnown && p.SupportsPDFInputKnown
}

func validateCLIImportOpenCodeCapabilities(models []string, caps map[string]CLIImportModelCapability) error {
	missing := make([]string, 0)
	for _, model := range models {
		cap, ok := caps[model]
		if !ok || !cap.openCodeComplete() {
			missing = append(missing, model)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return ErrCLIImportModelCapabilityUnknown.WithMetadata(map[string]string{
		"models": strings.Join(missing, ","),
	})
}

func (cap CLIImportModelCapability) openCodeComplete() bool {
	return cap.ReasoningKnown &&
		cap.AttachmentKnown &&
		cap.ToolCallKnown &&
		cap.ModalitiesKnown &&
		cap.LimitKnown &&
		cap.CostKnown &&
		cap.MaxInputTokens > 0 &&
		cap.MaxOutputTokens > 0 &&
		len(cap.InputModalities) > 0 &&
		len(cap.OutputModalities) > 0 &&
		cap.InputCostPerToken != nil &&
		cap.OutputCostPerToken != nil
}

func buildCLIImportPayload(key *APIKey, baseURL string, models []string, caps map[string]CLIImportModelCapability) cliImportPayload {
	group := key.Group
	defaultModel := strings.TrimSpace(group.DefaultMappedModel)
	if defaultModel == "" || strings.Contains(defaultModel, "*") || !containsString(models, defaultModel) {
		defaultModel = models[0]
	}
	providerID := fmt.Sprintf("sub2api_%d", key.ID)
	if group.Platform != "" {
		providerID = fmt.Sprintf("sub2api_%s_%d", safeCLIImportID(group.Platform), key.ID)
	}
	specs := make([]cliImportModelSpec, 0, len(models))
	for _, model := range models {
		specs = append(specs, buildCLIImportModelSpec(model, caps[model]))
	}
	return cliImportPayload{
		KeyID:          key.ID,
		KeyName:        strings.TrimSpace(key.Name),
		GroupID:        group.ID,
		GroupName:      strings.TrimSpace(group.Name),
		Platform:       strings.TrimSpace(group.Platform),
		APIKey:         key.Key,
		BaseURL:        baseURL + "/v1",
		EnvName:        fmt.Sprintf("SUB2API_KEY_%d", key.ID),
		ProviderID:     providerID,
		ProviderName:   cliImportProviderName,
		DefaultModel:   defaultModel,
		CodexSupported: group.Platform != PlatformOpenCodeGo,
		Models:         specs,
	}
}

func buildCLIImportModelSpec(model string, cap CLIImportModelCapability) cliImportModelSpec {
	name := normalizeCLIImportModelDisplayName(cap.Name)
	if name == "" {
		name = model
	}
	spec := cliImportModelSpec{
		ID:         model,
		Name:       name,
		Family:     strings.TrimSpace(cap.Family),
		Reasoning:  cap.SupportsReasoning,
		Attachment: cap.Attachment,
		ToolCall:   cap.SupportsFunctionCalling || cap.SupportsToolChoice,
		Modalities: cliImportModelModalities{
			Input:  append([]string(nil), cap.InputModalities...),
			Output: append([]string(nil), cap.OutputModalities...),
		},
		Limit: cliImportModelLimit{
			Context: cap.MaxInputTokens,
			Output:  cap.MaxOutputTokens,
		},
		Cost: cliImportModelCost{
			Input:      derefFloat64(cap.InputCostPerToken),
			Output:     derefFloat64(cap.OutputCostPerToken),
			CacheRead:  cap.CacheReadCostPerToken,
			CacheWrite: cap.CacheWriteCostPerToken,
		},
	}
	if cap.TemperatureKnown {
		spec.Temperature = &cap.Temperature
	}
	return spec
}

func normalizeCLIImportModelDisplayName(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{
		"(3x usage)",
	} {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSpace(strings.TrimSuffix(name, suffix))
		}
	}
	return name
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func safeCLIImportID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "provider"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			_, _ = b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			_ = b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "provider"
	}
	return out
}

func derefFloat64(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func cliImportFloat64Ptr(v float64) *float64 {
	return &v
}

func marshalCLIImportJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func renderCLIImportWindowsScript(payloadJSON string) string {
	return "@echo off\r\n" +
		"setlocal\r\n" +
		"powershell -NoProfile -ExecutionPolicy Bypass -Command \"$raw = Get-Content -Raw -LiteralPath '%~f0'; $marker = '### SUB2API_CLI_IMPORT_' + 'POWERSHELL ###'; $parts = $raw -split [regex]::Escape($marker), 2; if ($parts.Count -lt 2) { Write-Error 'PowerShell payload not found'; exit 1 }; Invoke-Expression $parts[1]\"\r\n" +
		"exit /b %ERRORLEVEL%\r\n" +
		"### SUB2API_CLI_IMPORT_POWERSHELL ###\r\n" +
		renderPowerShellHelper(payloadJSON)
}

func renderPowerShellHelper(payloadJSON string) string {
	return `$ErrorActionPreference = "Stop"
$payload = @'
` + payloadJSON + `
'@ | ConvertFrom-Json

function ConvertTo-Hashtable($InputObject) {
  if ($null -eq $InputObject) { return @{} }
  if ($InputObject -is [System.Collections.IDictionary]) {
    $h = [ordered]@{}
    foreach ($key in $InputObject.Keys) { $h[$key] = ConvertTo-Hashtable $InputObject[$key] }
    return $h
  }
  if ($InputObject -is [System.Collections.IEnumerable] -and -not ($InputObject -is [string]) -and -not ($InputObject -is [pscustomobject])) {
    $arr = @()
    foreach ($item in $InputObject) { $arr += ,(ConvertTo-Hashtable $item) }
    return ,$arr
  }
  if ($InputObject -is [pscustomobject]) {
    $h = [ordered]@{}
    foreach ($prop in $InputObject.PSObject.Properties) { $h[$prop.Name] = ConvertTo-Hashtable $prop.Value }
    return $h
  }
  return $InputObject
}

$payloadMap = [ordered]@{}
foreach ($prop in $payload.PSObject.Properties) {
  $payloadMap[$prop.Name] = $prop.Value
}

function Ensure-Directory($Path) {
  $dir = Split-Path -Parent $Path
  if ($dir -and -not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
}

function Backup-File($Path) {
  if (Test-Path -LiteralPath $Path) {
    $stamp = Get-Date -Format "yyyyMMddHHmmss"
    Copy-Item -LiteralPath $Path -Destination "$Path.bak.$stamp" -Force
    Write-Host "Backup: $Path.bak.$stamp"
  }
}

function Write-Utf8NoBom($Path, $Content) {
  $encoding = New-Object System.Text.UTF8Encoding($false)
  [System.IO.File]::WriteAllText($Path, [string]$Content, $encoding)
}

function Read-JsonConfig($Path) {
  if (-not (Test-Path -LiteralPath $Path)) { return [ordered]@{} }
  $raw = Get-Content -Raw -LiteralPath $Path
  if ([string]::IsNullOrWhiteSpace($raw)) { return [ordered]@{} }
  return ConvertTo-Hashtable ((ConvertFrom-JsoncText $raw))
}

function ConvertFrom-JsoncText($Text) {
  $sb = New-Object System.Text.StringBuilder
  $inString = $false
  $escape = $false
  $inLineComment = $false
  $inBlockComment = $false
  for ($i = 0; $i -lt $Text.Length; $i++) {
    $ch = $Text[$i]
    $next = [char]0
    if ($i + 1 -lt $Text.Length) { $next = $Text[$i + 1] }
    if ($inLineComment) {
      if ($ch -eq [char]10 -or $ch -eq [char]13) {
        [void]$sb.Append($ch)
        $inLineComment = $false
      }
      continue
    }
    if ($inBlockComment) {
      if ($ch -eq [char]42 -and $next -eq [char]47) {
        $i++
        $inBlockComment = $false
      } elseif ($ch -eq [char]10 -or $ch -eq [char]13) {
        [void]$sb.Append($ch)
      }
      continue
    }
    if ($inString) {
      [void]$sb.Append($ch)
      if ($escape) {
        $escape = $false
      } elseif ($ch -eq [char]92) {
        $escape = $true
      } elseif ($ch -eq [char]34) {
        $inString = $false
      }
      continue
    }
    if ($ch -eq [char]34) {
      [void]$sb.Append($ch)
      $inString = $true
      continue
    }
    if ($ch -eq [char]47 -and $next -eq [char]47) {
      $i++
      $inLineComment = $true
      continue
    }
    if ($ch -eq [char]47 -and $next -eq [char]42) {
      $i++
      $inBlockComment = $true
      continue
    }
    [void]$sb.Append($ch)
  }
  $clean = $sb.ToString()
  do {
    $previous = $clean
    $clean = [regex]::Replace($clean, ',(\s*[\]}])', '$1')
  } while ($clean -ne $previous)
  if ([string]::IsNullOrWhiteSpace($clean)) { return [pscustomobject]@{} }
  return $clean | ConvertFrom-Json
}

function Write-JsonConfig($Path, $Config) {
  Ensure-Directory $Path
  Backup-File $Path
  Write-Utf8NoBom $Path (($Config | ConvertTo-Json -Depth 100) + [Environment]::NewLine)
}

function Get-OpenCodeConfigPath() {
  $dir = Join-Path $HOME ".config\opencode"
  $jsonc = Join-Path $dir "opencode.jsonc"
  $json = Join-Path $dir "opencode.json"
  if (Test-Path -LiteralPath $jsonc) { return $jsonc }
  if (Test-Path -LiteralPath $json) { return $json }
  return $jsonc
}

function Get-OpenCodeAuthPath() {
  return (Join-Path $HOME ".local\share\opencode\auth.json")
}

function Get-OpenCodeKeyPath($ProviderID) {
  return (Join-Path (Join-Path $HOME ".config\opencode") ($ProviderID + ".key"))
}

function Get-OpenCodeKeyReference($ProviderID) {
  return ("{file:~/.config/opencode/" + $ProviderID + ".key}")
}

function Restart-OpenCodeDesktopIfNeeded() {
  if ($env:SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH -eq "1") { return }
  $processes = @(Get-Process -Name "OpenCode" -ErrorAction SilentlyContinue)
  if ($processes.Count -eq 0) { return }

  Write-Host ""
  Write-Host "OpenCode Desktop is currently running."
  Write-Host "OpenCode Desktop caches provider/auth data in its sidecar process; restart it before using the imported provider."
  $answer = Read-Host "Restart OpenCode Desktop now? [y/N]"
  if ($answer -notmatch '^(?i:y|yes)$') {
    Write-Host "Please fully quit and reopen OpenCode Desktop before using OnprsCodexApi."
    return
  }

  $main = $processes | Where-Object { $_.MainWindowHandle -ne 0 -and $_.Path } | Select-Object -First 1
  if (-not $main) { $main = $processes | Where-Object { $_.Path } | Select-Object -First 1 }
  $exe = $null
  if ($main) { $exe = $main.Path }

  $processes | Stop-Process -Force -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 2

  if ($exe -and (Test-Path -LiteralPath $exe)) {
    Start-Process -FilePath $exe | Out-Null
    Write-Host "OpenCode Desktop restarted. Start a new OpenCode chat/session if an old session still shows stale errors."
  } else {
    Write-Host "OpenCode Desktop was stopped. Reopen it manually, then start a new chat/session if needed."
  }
}

function Escape-TomlString($Value) {
  $text = (($Value | ForEach-Object { [string]$_ }) -join "").Trim()
  return ($text -replace '\\', '\\' -replace '"', '\"')
}

function New-TomlStringLine($Key, $Value) {
  return ('{0} = "{1}"' -f $Key, (Escape-TomlString $Value))
}

function Get-PayloadString($Name) {
  if ($payloadMap.Contains($Name)) {
    return (($payloadMap[$Name] | ForEach-Object { [string]$_ }) -join "").Trim()
  }
  return ""
}

function Set-TopLevelTomlString($Content, $Key, $Value) {
  $line = $Key + ' = "' + (Escape-TomlString $Value) + '"'
  $lines = @()
  if ($Content) { $lines = $Content -split '\r?\n' }
  $out = New-Object System.Collections.Generic.List[string]
  $done = $false
  $inTable = $false
  foreach ($existing in $lines) {
    if ($existing -match '^\s*\[') { $inTable = $true }
    if (-not $inTable -and $existing -match ("^\s*" + [regex]::Escape($Key) + "\s*=")) {
      if (-not $done) { $out.Add($line); $done = $true }
      continue
    }
    $out.Add($existing)
  }
  if (-not $done) { $out.Insert(0, $line) }
  return ($out -join [Environment]::NewLine)
}

function Upsert-TomlTable($Content, $Header, [string[]]$BlockLines) {
  $lines = @()
  if ($Content) { $lines = $Content -split '\r?\n' }
  $out = New-Object System.Collections.Generic.List[string]
  $i = 0
  while ($i -lt $lines.Count) {
    if ($lines[$i].Trim() -eq $Header) {
      $i++
      while ($i -lt $lines.Count -and $lines[$i] -notmatch '^\s*\[') { $i++ }
      continue
    }
    $out.Add($lines[$i])
    $i++
  }
  if ($out.Count -gt 0 -and $out[$out.Count - 1].Trim() -ne "") { $out.Add("") }
  foreach ($line in $BlockLines) { $out.Add($line) }
  return (($out -join [Environment]::NewLine).TrimEnd() + [Environment]::NewLine)
}

function Import-Codex($SetDefault) {
  if (-not $payload.codex_supported) {
    Write-Host "Codex CLI import is skipped: OpenCode Go groups do not support the Codex Responses API."
    return
  }
  $path = Join-Path $HOME ".codex\config.toml"
  Ensure-Directory $path
  $content = ""
  if (Test-Path -LiteralPath $path) { $content = Get-Content -Raw -LiteralPath $path }
  $providerID = Get-PayloadString "provider_id"
  $providerName = Get-PayloadString "provider_name"
  $baseURL = Get-PayloadString "base_url"
  $envName = Get-PayloadString "env_name"
  $defaultModel = Get-PayloadString "default_model"
  $header = "[model_providers.$providerID]"
  $block = @(
    $header,
    (New-TomlStringLine "name" $providerName),
    (New-TomlStringLine "base_url" $baseURL),
    (New-TomlStringLine "env_key" $envName),
    'wire_api = "responses"'
  )
  $content = Upsert-TomlTable $content $header $block
  if ($SetDefault) {
    $content = Set-TopLevelTomlString $content "model_provider" $providerID
    $content = Set-TopLevelTomlString $content "model" $defaultModel
  }
  Backup-File $path
  Write-Utf8NoBom $path $content
  Write-Host "Codex CLI config written: $path"
}

function Import-OpenCode($SetDefault) {
  $path = Get-OpenCodeConfigPath
  $config = Read-JsonConfig $path
  if (-not $config.Contains('$schema')) { $config['$schema'] = "https://opencode.ai/config.json" }
  if (-not $config.Contains("provider")) { $config["provider"] = [ordered]@{} }
  $providerID = Get-PayloadString "provider_id"
  $providerName = Get-PayloadString "provider_name"
  $baseURL = Get-PayloadString "base_url"
  $defaultModel = Get-PayloadString "default_model"
  $provider = [ordered]@{
    name = $providerName
    npm = "@ai-sdk/openai-compatible"
    options = [ordered]@{
      baseURL = $baseURL
      apiKey = Get-OpenCodeKeyReference $providerID
    }
    models = [ordered]@{}
  }
  foreach ($model in $payload.models) {
    $provider.models[$model.id] = ConvertTo-Hashtable $model
  }
  $config.provider[$providerID] = $provider
  if ($SetDefault) { $config.model = "$providerID/$defaultModel" }
  Write-JsonConfig $path $config
  Write-Host "OpenCode config written: $path"

  $keyPath = Get-OpenCodeKeyPath $providerID
  Ensure-Directory $keyPath
  Backup-File $keyPath
  Write-Utf8NoBom $keyPath (Get-PayloadString "api_key")
  Write-Host "OpenCode key file written: $keyPath"

  $authPath = Get-OpenCodeAuthPath
  $auth = Read-JsonConfig $authPath
  $auth[$providerID] = [ordered]@{
    type = "api"
    key = Get-PayloadString "api_key"
  }
  Write-JsonConfig $authPath $auth
  Write-Host "OpenCode auth written: $authPath"
  Restart-OpenCodeDesktopIfNeeded
}

function Import-ClaudeCode($SetDefault) {
  $path = Join-Path $HOME ".claude\settings.json"
  $config = Read-JsonConfig $path
  if (-not $config.Contains("env")) { $config["env"] = [ordered]@{} }
  $baseURL = Get-PayloadString "base_url"
  $apiKey = Get-PayloadString "api_key"
  $defaultModel = Get-PayloadString "default_model"
  $config.env["ANTHROPIC_BASE_URL"] = $baseURL
  $config.env["ANTHROPIC_AUTH_TOKEN"] = $apiKey
  $config.env["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"] = "1"
  if ($SetDefault) {
    $config.env["ANTHROPIC_MODEL"] = $defaultModel
    if ($defaultModel -notmatch '^claude-') {
      $config.env["ANTHROPIC_CUSTOM_MODEL_OPTION"] = $defaultModel
    }
  }
  Write-JsonConfig $path $config
  Write-Host "Claude Code settings written: $path"
}

Write-Host "Sub2API CLI import"
Write-Host "Key: $($payload.key_name)  Group: $($payload.group_name)  Endpoint: $($payload.base_url)"
Write-Host "1) Codex CLI"
Write-Host "2) OpenCode"
Write-Host "3) Claude Code"
Write-Host "4) All"
$choice = Read-Host "Select import target [1-4]"
$defaultAnswer = Read-Host "Set imported provider/model as default? [y/N]"
$setDefault = $defaultAnswer -match '^(?i:y|yes)$'

$envName = Get-PayloadString "env_name"
$apiKey = Get-PayloadString "api_key"
[Environment]::SetEnvironmentVariable($envName, $apiKey, "User")
Set-Item -Path ("Env:" + $envName) -Value $apiKey
Write-Host "User environment variable saved: $envName"

switch ($choice) {
  "1" { Import-Codex $setDefault }
  "2" { Import-OpenCode $setDefault }
  "3" { Import-ClaudeCode $setDefault }
  "4" { Import-Codex $setDefault; Import-OpenCode $setDefault; Import-ClaudeCode $setDefault }
  default { throw "Invalid selection" }
}
Write-Host "Sub2API CLI import finished. Restart terminals to pick up persistent environment changes."
`
}

func renderCLIImportShellScript(payloadJSON string) string {
	return `#!/usr/bin/env bash
set -Eeuo pipefail

# Targets: ~/.codex/config.toml ~/.config/opencode/opencode.jsonc ~/.claude/settings.json

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required. Please install Python 3 and run this script again." >&2
  exit 1
fi

tmp_py="$(mktemp "${TMPDIR:-/tmp}/sub2api-cli-import.XXXXXX.py")"
chmod 600 "$tmp_py"
cleanup() {
  rm -f "$tmp_py"
}
trap cleanup EXIT

cat > "$tmp_py" <<'PY'
import json
import os
import re
import shlex
import shutil
import stat
import subprocess
import sys
from pathlib import Path

PAYLOAD = json.loads(r'''` + payloadJSON + `''')

def backup(path: Path):
    if path.exists():
        stamp = __import__("datetime").datetime.now().strftime("%Y%m%d%H%M%S")
        target = path.with_name(path.name + ".bak." + stamp)
        shutil.copy2(path, target)
        print(f"Backup: {target}")

def ensure_parent(path: Path):
    path.parent.mkdir(parents=True, exist_ok=True)

def chmod_600(path: Path):
    try:
        path.chmod(stat.S_IRUSR | stat.S_IWUSR)
    except OSError:
        pass

def read_json(path: Path):
    if not path.exists() or not path.read_text(encoding="utf-8").strip():
        return {}
    return json.loads(strip_jsonc(path.read_text(encoding="utf-8")))

def strip_jsonc(text: str) -> str:
    out = []
    in_string = False
    escape = False
    in_line_comment = False
    in_block_comment = False
    i = 0
    while i < len(text):
        ch = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if in_line_comment:
            if ch in "\r\n":
                out.append(ch)
                in_line_comment = False
            i += 1
            continue
        if in_block_comment:
            if ch == "*" and nxt == "/":
                i += 2
                in_block_comment = False
                continue
            if ch in "\r\n":
                out.append(ch)
            i += 1
            continue
        if in_string:
            out.append(ch)
            if escape:
                escape = False
            elif ch == "\\":
                escape = True
            elif ch == '"':
                in_string = False
            i += 1
            continue
        if ch == '"':
            out.append(ch)
            in_string = True
            i += 1
            continue
        if ch == "/" and nxt == "/":
            in_line_comment = True
            i += 2
            continue
        if ch == "/" and nxt == "*":
            in_block_comment = True
            i += 2
            continue
        out.append(ch)
        i += 1
    cleaned = "".join(out)
    previous = None
    while previous != cleaned:
        previous = cleaned
        cleaned = re.sub(r",(\s*[\]}])", r"\1", cleaned)
    return cleaned

def write_json(path: Path, data):
    ensure_parent(path)
    backup(path)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    chmod_600(path)

def opencode_config_path() -> Path:
    directory = Path.home() / ".config" / "opencode"
    jsonc = directory / "opencode.jsonc"
    json_path = directory / "opencode.json"
    if jsonc.exists():
        return jsonc
    if json_path.exists():
        return json_path
    return jsonc

def opencode_auth_path() -> Path:
    return Path.home() / ".local" / "share" / "opencode" / "auth.json"

def opencode_key_path(provider_id: str) -> Path:
    return Path.home() / ".config" / "opencode" / (provider_id + ".key")

def opencode_key_reference(provider_id: str) -> str:
    return "{file:~/.config/opencode/" + provider_id + ".key}"

def running_opencode_desktop_pids():
    try:
        result = subprocess.run(
            ["ps", "-eo", "pid=,comm=,args="],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
        )
    except Exception:
        return []
    current = os.getpid()
    pids = []
    for line in result.stdout.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        parts = stripped.split(None, 2)
        if len(parts) < 2:
            continue
        try:
            pid = int(parts[0])
        except ValueError:
            continue
        if pid == current:
            continue
        command = parts[1]
        args = parts[2] if len(parts) > 2 else ""
        haystack = f"{command} {args}".lower()
        command_lower = command.lower()
        looks_like_desktop = (
            command == "OpenCode"
            or "ai.opencode.desktop" in haystack
            or ("opencode" in haystack and "app.asar" in haystack)
            or "opencode-desktop" in haystack
            or ("opencode" in command_lower and "server" in haystack)
        )
        if looks_like_desktop:
            pids.append(pid)
    return sorted(set(pids))

def refresh_opencode_desktop_if_needed():
    if os.environ.get("SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH") == "1":
        return
    pids = running_opencode_desktop_pids()
    if not pids:
        return
    print()
    print("OpenCode Desktop appears to be running.")
    print("OpenCode Desktop caches provider/auth data in its sidecar process; restart it before using the imported provider.")
    answer = input("Stop OpenCode Desktop now? [y/N]: ").strip().lower()
    if answer not in {"y", "yes"}:
        print("Please fully quit and reopen OpenCode Desktop before using OnprsCodexApi.")
        return
    for pid in pids:
        try:
            os.kill(pid, 15)
        except ProcessLookupError:
            pass
        except PermissionError:
            print(f"Could not stop process {pid}; please quit OpenCode Desktop manually.")
    if sys.platform == "darwin":
        subprocess.run(["open", "-a", "OpenCode"], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print("OpenCode Desktop restart requested. Start a new OpenCode chat/session if an old session still shows stale errors.")
    else:
        print("OpenCode Desktop stop requested. Reopen it manually, then start a new chat/session if needed.")

def toml_escape(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"')

def set_top_level_toml_string(content: str, key: str, value: str) -> str:
    new_line = f'{key} = "{toml_escape(value)}"'
    lines = content.splitlines()
    out = []
    done = False
    in_table = False
    for line in lines:
        if re.match(r"\s*\[", line):
            in_table = True
        if not in_table and re.match(rf"\s*{re.escape(key)}\s*=", line):
            if not done:
                out.append(new_line)
                done = True
            continue
        out.append(line)
    if not done:
        out.insert(0, new_line)
    return "\n".join(out) + "\n"

def upsert_toml_table(content: str, header: str, block_lines):
    lines = content.splitlines()
    out = []
    i = 0
    while i < len(lines):
        if lines[i].strip() == header:
            i += 1
            while i < len(lines) and not re.match(r"\s*\[", lines[i]):
                i += 1
            continue
        out.append(lines[i])
        i += 1
    if out and out[-1].strip():
        out.append("")
    out.extend(block_lines)
    return "\n".join(out).rstrip() + "\n"

def write_managed_env():
    env_file = Path.home() / ".sub2api-cli-import-env"
    block = f'export {PAYLOAD["env_name"]}={shlex.quote(PAYLOAD["api_key"])}\n'
    backup(env_file)
    env_file.write_text(block, encoding="utf-8")
    chmod_600(env_file)
    managed = (
        "# >>> sub2api cli import >>>\n"
        f'[ -f "{env_file}" ] && . "{env_file}"\n'
        "# <<< sub2api cli import <<<\n"
    )
    shell = os.environ.get("SHELL", "")
    profile_names = [".profile"]
    if shell.endswith("zsh"):
        profile_names.append(".zshrc")
    else:
        profile_names.append(".bashrc")
    for name in dict.fromkeys(profile_names):
        profile = Path.home() / name
        old = profile.read_text(encoding="utf-8") if profile.exists() else ""
        old = re.sub(r"# >>> sub2api cli import >>>\n.*?# <<< sub2api cli import <<<\n?", "", old, flags=re.S)
        ensure_parent(profile)
        profile.write_text(old.rstrip() + "\n\n" + managed, encoding="utf-8")
    print(f"Managed environment variable saved: {PAYLOAD['env_name']}")

def import_codex(set_default: bool):
    if not PAYLOAD["codex_supported"]:
        print("Codex CLI import is skipped: OpenCode Go groups do not support the Codex Responses API.")
        return
    path = Path.home() / ".codex" / "config.toml"
    content = path.read_text(encoding="utf-8") if path.exists() else ""
    header = f'[model_providers.{PAYLOAD["provider_id"]}]'
    block = [
        header,
        f'name = "{toml_escape(PAYLOAD["provider_name"])}"',
        f'base_url = "{toml_escape(PAYLOAD["base_url"])}"',
        f'env_key = "{PAYLOAD["env_name"]}"',
        'wire_api = "responses"',
    ]
    content = upsert_toml_table(content, header, block)
    if set_default:
        content = set_top_level_toml_string(content, "model_provider", PAYLOAD["provider_id"])
        content = set_top_level_toml_string(content, "model", PAYLOAD["default_model"])
    ensure_parent(path)
    backup(path)
    path.write_text(content, encoding="utf-8")
    chmod_600(path)
    print(f"Codex CLI config written: {path}")

def import_opencode(set_default: bool):
    path = opencode_config_path()
    config = read_json(path)
    config.setdefault("$schema", "https://opencode.ai/config.json")
    config.setdefault("provider", {})
    provider = {
        "name": PAYLOAD["provider_name"],
        "npm": "@ai-sdk/openai-compatible",
        "options": {
            "baseURL": PAYLOAD["base_url"],
            "apiKey": opencode_key_reference(PAYLOAD["provider_id"]),
        },
        "models": {},
    }
    for model in PAYLOAD["models"]:
        provider["models"][model["id"]] = dict(model)
    config["provider"][PAYLOAD["provider_id"]] = provider
    if set_default:
        config["model"] = PAYLOAD["provider_id"] + "/" + PAYLOAD["default_model"]
    write_json(path, config)
    print(f"OpenCode config written: {path}")

    key_path = opencode_key_path(PAYLOAD["provider_id"])
    ensure_parent(key_path)
    backup(key_path)
    key_path.write_text(PAYLOAD["api_key"], encoding="utf-8")
    chmod_600(key_path)
    print(f"OpenCode key file written: {key_path}")

    auth_path = opencode_auth_path()
    auth = read_json(auth_path)
    auth[PAYLOAD["provider_id"]] = {
        "type": "api",
        "key": PAYLOAD["api_key"],
    }
    write_json(auth_path, auth)
    print(f"OpenCode auth written: {auth_path}")
    refresh_opencode_desktop_if_needed()

def import_claude_code(set_default: bool):
    path = Path.home() / ".claude" / "settings.json"
    config = read_json(path)
    config.setdefault("env", {})
    config["env"]["ANTHROPIC_BASE_URL"] = PAYLOAD["base_url"]
    config["env"]["ANTHROPIC_AUTH_TOKEN"] = PAYLOAD["api_key"]
    config["env"]["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"] = "1"
    if set_default:
        config["env"]["ANTHROPIC_MODEL"] = PAYLOAD["default_model"]
        if not PAYLOAD["default_model"].startswith("claude-"):
            config["env"]["ANTHROPIC_CUSTOM_MODEL_OPTION"] = PAYLOAD["default_model"]
    write_json(path, config)
    print(f"Claude Code settings written: {path}")

print("Sub2API CLI import")
print(f"Key: {PAYLOAD['key_name']}  Group: {PAYLOAD['group_name']}  Endpoint: {PAYLOAD['base_url']}")
print("1) Codex CLI")
print("2) OpenCode")
print("3) Claude Code")
print("4) All")
choice = input("Select import target [1-4]: ").strip()
set_default = input("Set imported provider/model as default? [y/N]: ").strip().lower() in {"y", "yes"}
write_managed_env()

if choice == "1":
    import_codex(set_default)
elif choice == "2":
    import_opencode(set_default)
elif choice == "3":
    import_claude_code(set_default)
elif choice == "4":
    import_codex(set_default)
    import_opencode(set_default)
    import_claude_code(set_default)
else:
    raise SystemExit("Invalid selection")

print("Sub2API CLI import finished. Restart terminals or source your shell profile to pick up the environment change.")
PY
python3 "$tmp_py"
`
}

func SortCLIImportModels(models []string) []string {
	out := append([]string(nil), models...)
	sort.Strings(out)
	return out
}
