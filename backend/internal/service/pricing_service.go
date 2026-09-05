package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"go.uber.org/zap"
	xhtml "golang.org/x/net/html"
)

var (
	openAIModelDatePattern = regexp.MustCompile(`-\d{8}$`)
	openAIModelBasePattern = regexp.MustCompile(`^(gpt-\d+(?:\.\d+)?)(?:-|$)`)
	// aboveTierPricePattern 匹配 LiteLLM 长上下文绝对价字段名。
	aboveTierPricePattern = regexp.MustCompile(`^(input|output)_cost_per_token_above_(\d+)k_tokens$`)
	// cacheTierPricePattern 匹配 cache 侧长上下文绝对价字段名及其时长、服务档变体。
	cacheTierPricePattern       = regexp.MustCompile(`^(cache_(?:creation|read)_input_token_cost)(_above_1hr)?_above_\d+k_tokens((?:_[a-z]+)?)$`)
	htmlTableRowPattern         = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	htmlTableCellPattern        = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	htmlTagPattern              = regexp.MustCompile(`(?is)<[^>]+>`)
	openCodeGoPricePattern      = regexp.MustCompile(`^\$([0-9]+(?:\.[0-9]+)?)$`)
	openCodeGoThresholdPattern  = regexp.MustCompile(`(?i)(?:<=|≤|>|>=|≥)\s*([0-9]+)\s*([km])\b`)
	openCodeGoModelIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	openCodeGoUsageOfferPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*x\s+usage(?:\s+limits?)?$`)
	openAIGPT54FallbackPricing  = &LiteLLMModelPricing{
		InputCostPerToken:       2.5e-06, // $2.5 per MTok
		OutputCostPerToken:      1.5e-05, // $15 per MTok
		CacheReadInputTokenCost: 2.5e-07, // $0.25 per MTok
		LiteLLMProvider:         "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
	openAIGPT6AstraFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   1e-05,
		InputCostPerTokenPriority:           2e-05,
		OutputCostPerToken:                  5e-05,
		OutputCostPerTokenPriority:          1e-04,
		CacheCreationInputTokenCost:         1.25e-05,
		CacheCreationInputTokenCostPriority: 2.5e-05,
		CacheReadInputTokenCost:             1e-06,
		CacheReadInputTokenCostPriority:     2e-06,
		LongContextInputTokenThreshold:      272_000,
		LongContextInputCostMultiplier:      2,
		LongContextOutputCostMultiplier:     1.5,
		SupportsServiceTier:                 true,
		LiteLLMProvider:                     "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT56SolFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   5e-06,
		InputCostPerTokenPriority:           1e-05,
		OutputCostPerToken:                  3e-05,
		OutputCostPerTokenPriority:          6e-05,
		CacheCreationInputTokenCost:         6.25e-06,
		CacheCreationInputTokenCostPriority: 1.25e-05,
		CacheReadInputTokenCost:             5e-07,
		CacheReadInputTokenCostPriority:     1e-06,
		SupportsServiceTier:                 true,
		LiteLLMProvider:                     "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT56TerraFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   2e-06,
		InputCostPerTokenPriority:           4e-06,
		OutputCostPerToken:                  1.2e-05,
		OutputCostPerTokenPriority:          2.4e-05,
		CacheCreationInputTokenCost:         2.5e-06,
		CacheCreationInputTokenCostPriority: 5e-06,
		CacheReadInputTokenCost:             2e-07,
		CacheReadInputTokenCostPriority:     4e-07,
		SupportsServiceTier:                 true,
		LiteLLMProvider:                     "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT56LunaFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:                   2e-07,
		InputCostPerTokenPriority:           4e-07,
		OutputCostPerToken:                  1.2e-06,
		OutputCostPerTokenPriority:          2.4e-06,
		CacheCreationInputTokenCost:         2.5e-07,
		CacheCreationInputTokenCostPriority: 5e-07,
		CacheReadInputTokenCost:             2e-08,
		CacheReadInputTokenCostPriority:     4e-08,
		SupportsServiceTier:                 true,
		LiteLLMProvider:                     "openai",
		Mode:                                "chat",
		SupportsPromptCaching:               true,
	}
	openAIGPT54MiniFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:       7.5e-07,
		OutputCostPerToken:      4.5e-06,
		CacheReadInputTokenCost: 7.5e-08,
		LiteLLMProvider:         "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
	openAIGPT54NanoFallbackPricing = &LiteLLMModelPricing{
		InputCostPerToken:       2e-07,
		OutputCostPerToken:      1.25e-06,
		CacheReadInputTokenCost: 2e-08,
		LiteLLMProvider:         "openai",
		Mode:                    "chat",
		SupportsPromptCaching:   true,
	}
)

const (
	openCodeGoUsageOfferRefreshInterval = 10 * time.Minute
	openCodeGoUsageOfferEvidenceTTL     = time.Hour
	openCodeGoZeroRateEvidenceTTL       = time.Hour
	openCodeGoPricingAuthorityOfficial  = "official"
	openCodeGoPricingAuthorityModelsDev = "models_dev"
)

type openCodeGoUsageOffer struct {
	usageMultiplier float64
	confirmedAt     time.Time
}

// LiteLLMModelPricing LiteLLM价格数据结构
// 只保留我们需要的字段，使用指针来处理可能缺失的值
type LiteLLMModelPricing struct {
	InputCostPerToken                        float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority                float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                       float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority               float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost              float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority      float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr      float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost                  float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority          float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold           int     `json:"long_context_input_token_threshold,omitempty"`
	LongContextInputCostMultiplier           float64 `json:"long_context_input_cost_multiplier,omitempty"`
	LongContextOutputCostMultiplier          float64 `json:"long_context_output_cost_multiplier,omitempty"`
	SupportsServiceTier                      bool    `json:"supports_service_tier"`
	LiteLLMProvider                          string  `json:"litellm_provider"`
	Mode                                     string  `json:"mode"`
	SupportsPromptCaching                    bool    `json:"supports_prompt_caching"`
	OutputCostPerImage                       float64 `json:"output_cost_per_image"`       // 图片生成模型每张图片价格
	OutputCostPerImageToken                  float64 `json:"output_cost_per_image_token"` // 图片输出 token 价格
	InputCostPerImageToken                   float64 `json:"input_cost_per_image_token"`  // 图片输入 token 价格（如 gpt-image-2 图片编辑）
	SupportsReasoning                        bool    `json:"supports_reasoning"`
	SupportsVision                           bool    `json:"supports_vision"`
	SupportsPDFInput                         bool    `json:"supports_pdf_input"`
	SupportsFunctionCalling                  bool    `json:"supports_function_calling"`
	SupportsToolChoice                       bool    `json:"supports_tool_choice"`
	MaxInputTokens                           int     `json:"max_input_tokens"`
	MaxOutputTokens                          int     `json:"max_output_tokens"`
	InputCostPerTokenKnown                   bool    `json:"-"`
	OutputCostPerTokenKnown                  bool    `json:"-"`
	CacheCreationInputTokenCostKnown         bool    `json:"-"`
	CacheCreationInputTokenCostAbove1hrKnown bool    `json:"-"`
	CacheReadInputTokenCostKnown             bool    `json:"-"`
	SupportsReasoningKnown                   bool    `json:"-"`
	SupportsVisionKnown                      bool    `json:"-"`
	SupportsPDFInputKnown                    bool    `json:"-"`
	SupportsFunctionCallingKnown             bool    `json:"-"`
	SupportsToolChoiceKnown                  bool    `json:"-"`
	MaxInputTokensKnown                      bool    `json:"-"`
	MaxOutputTokensKnown                     bool    `json:"-"`
	OpenCodeGoPricingAuthority               string  `json:"opencode_go_pricing_authority,omitempty"`
	OpenCodeGoMonthlyUsageUSD                float64 `json:"opencode_go_monthly_usage_usd,omitempty"`
	OpenCodeGoPeakPricingKnown               bool    `json:"opencode_go_peak_pricing_known,omitempty"`
	OpenCodeGoPeakInputCostPerToken          float64 `json:"opencode_go_peak_input_cost_per_token,omitempty"`
	OpenCodeGoPeakOutputCostPerToken         float64 `json:"opencode_go_peak_output_cost_per_token,omitempty"`
	OpenCodeGoPeakCacheCreationCostPerToken  float64 `json:"opencode_go_peak_cache_creation_cost_per_token,omitempty"`
	OpenCodeGoPeakCacheReadCostPerToken      float64 `json:"opencode_go_peak_cache_read_cost_per_token,omitempty"`
	// OpenCodeGoExplicitZeroRate 只由官方价格表中的明确零价生成，不能由缺失字段推导。
	OpenCodeGoExplicitZeroRate bool `json:"opencode_go_explicit_zero_rate,omitempty"`

	// TokenPricingAbsent 表示源数据中 input/output token 价格均缺失（仅有图片价）。
	// 此类条目只可用于图片计费，token 计费必须回退到 fallback 或 fail-closed，
	// 否则 token 流量会被按 $0 计费。零值（false）表示条目具备 token 价格。
	TokenPricingAbsent bool `json:"-"`
}

// PricingRemoteClient 远程价格数据获取接口
type PricingRemoteClient interface {
	FetchPricingJSON(ctx context.Context, url string) ([]byte, error)
	FetchHashText(ctx context.Context, url string) (string, error)
}

// LiteLLMRawEntry 用于解析原始JSON数据
type LiteLLMRawEntry struct {
	InputCostPerToken                   *float64 `json:"input_cost_per_token"`
	InputCostPerTokenPriority           *float64 `json:"input_cost_per_token_priority"`
	OutputCostPerToken                  *float64 `json:"output_cost_per_token"`
	OutputCostPerTokenPriority          *float64 `json:"output_cost_per_token_priority"`
	CacheCreationInputTokenCost         *float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostPriority *float64 `json:"cache_creation_input_token_cost_priority"`
	CacheCreationInputTokenCostAbove1hr *float64 `json:"cache_creation_input_token_cost_above_1hr"`
	CacheReadInputTokenCost             *float64 `json:"cache_read_input_token_cost"`
	CacheReadInputTokenCostPriority     *float64 `json:"cache_read_input_token_cost_priority"`
	LongContextInputTokenThreshold      *int     `json:"long_context_input_token_threshold"`
	LongContextInputCostMultiplier      *float64 `json:"long_context_input_cost_multiplier"`
	LongContextOutputCostMultiplier     *float64 `json:"long_context_output_cost_multiplier"`
	SupportsServiceTier                 bool     `json:"supports_service_tier"`
	LiteLLMProvider                     string   `json:"litellm_provider"`
	Mode                                string   `json:"mode"`
	SupportsPromptCaching               bool     `json:"supports_prompt_caching"`
	OutputCostPerImage                  *float64 `json:"output_cost_per_image"`
	OutputCostPerImageToken             *float64 `json:"output_cost_per_image_token"`
	InputCostPerImageToken              *float64 `json:"input_cost_per_image_token"`
	SupportsReasoning                   *bool    `json:"supports_reasoning"`
	SupportsVision                      *bool    `json:"supports_vision"`
	SupportsPDFInput                    *bool    `json:"supports_pdf_input"`
	SupportsFunctionCalling             *bool    `json:"supports_function_calling"`
	SupportsToolChoice                  *bool    `json:"supports_tool_choice"`
	MaxInputTokens                      *int     `json:"max_input_tokens"`
	MaxOutputTokens                     *int     `json:"max_output_tokens"`
}

// PricingService 动态价格服务
type PricingService struct {
	cfg                          *config.Config
	remoteClient                 PricingRemoteClient
	mu                           sync.RWMutex
	pricingData                  map[string]*LiteLLMModelPricing
	openCodeGoPricing            map[string]*LiteLLMModelPricing
	customFilesHash              string
	openCodeGoPricingConfirmedAt time.Time
	lastUpdated                  time.Time
	localHash                    string
	cliImportCatalogMu           sync.RWMutex
	cliImportCatalogLoaded       bool
	cliImportCatalog             map[string]map[string]CLIImportModelCapability
	usageOfferMu                 sync.RWMutex
	openCodeGoUsageOffers        map[string]openCodeGoUsageOffer
	commandCodeCatalog           *CommandCodeCatalog

	// 停止信号
	stopCh                   chan struct{}
	wg                       sync.WaitGroup
	commandCodeCatalogCtx    context.Context
	commandCodeCatalogCancel context.CancelFunc
}

// NewPricingService 创建价格服务
func NewPricingService(cfg *config.Config, remoteClient PricingRemoteClient) *PricingService {
	commandCodeCatalogCtx, commandCodeCatalogCancel := context.WithCancel(context.Background())
	s := &PricingService{
		cfg:                      cfg,
		remoteClient:             remoteClient,
		pricingData:              make(map[string]*LiteLLMModelPricing),
		openCodeGoPricing:        make(map[string]*LiteLLMModelPricing),
		openCodeGoUsageOffers:    make(map[string]openCodeGoUsageOffer),
		commandCodeCatalog:       defaultCommandCodeCatalog,
		stopCh:                   make(chan struct{}),
		commandCodeCatalogCtx:    commandCodeCatalogCtx,
		commandCodeCatalogCancel: commandCodeCatalogCancel,
	}
	return s
}

// Initialize 初始化价格服务
func (s *PricingService) Initialize() error {
	// 确保数据目录存在
	if err := os.MkdirAll(s.cfg.Pricing.DataDir, 0755); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to create data directory: %v", err)
	}

	// 首次加载价格数据
	if err := s.checkAndUpdatePricing(); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Initial load failed, using fallback: %v", err)
		if err := s.useFallbackPricing(); err != nil {
			return fmt.Errorf("failed to load pricing data: %w", err)
		}
	}
	// 启动时优先从本地缓存加载 OpenCode Go 定价，确保离线或网络波动时价格立即可用
	if err := s.loadOpenCodeGoPricingData(s.getOpenCodeGoPricingFilePath()); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go cache file not loaded (will fetch from upstream): %v", err)
	}
	s.refreshOpenCodeGoPricingBestEffortWithTimeout()
	s.refreshOpenCodeGoUsageOffersBestEffortWithTimeout()

	// Command Code 启动时先恢复最后成功快照，再由独立后台任务校验双官方源。
	commandCodeCatalogPath := s.getCommandCodeCatalogFilePath()
	commandCodeCatalog := s.commandCodeCatalog
	if commandCodeCatalog == nil {
		commandCodeCatalog = defaultCommandCodeCatalog
	}
	if err := commandCodeCatalog.LoadSnapshot(commandCodeCatalogPath); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Command Code cache file not loaded (will use built-in catalog until refresh): %v", err)
	}
	s.startCommandCodeCatalogScheduler()

	// 启动定时更新
	s.startUpdateScheduler()

	logger.LegacyPrintf("service.pricing", "[Pricing] Service initialized with %d models", len(s.pricingData))
	return nil
}

// Stop 停止价格服务
func (s *PricingService) Stop() {
	if s.commandCodeCatalogCancel != nil {
		s.commandCodeCatalogCancel()
	}
	close(s.stopCh)
	s.wg.Wait()
	logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Service stopped")
}

func (s *PricingService) getCommandCodeCatalogFilePath() string {
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Pricing.DataDir) == "" {
		return ""
	}
	return filepath.Join(s.cfg.Pricing.DataDir, "commandcode_goat_catalog.json")
}

func (s *PricingService) startCommandCodeCatalogScheduler() {
	if s == nil || s.commandCodeCatalogCtx == nil {
		return
	}
	filePath := s.getCommandCodeCatalogFilePath()
	if filePath == "" {
		return
	}
	catalog := s.commandCodeCatalog
	if catalog == nil {
		catalog = defaultCommandCodeCatalog
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		refresh := func(force bool) {
			ctx, cancel := context.WithTimeout(s.commandCodeCatalogCtx, commandCodeCatalogHTTPTimeout)
			defer cancel()
			if err := catalog.RefreshAndSave(ctx, filePath, force); err != nil && !errors.Is(err, context.Canceled) {
				logger.LegacyPrintf("service.pricing", "[Pricing] Command Code catalog refresh failed; keeping last-known-good data: %v", err)
			}
		}

		refresh(true)
		ticker := time.NewTicker(commandCodeCatalogTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refresh(false)
			case <-s.commandCodeCatalogCtx.Done():
				return
			}
		}
	}()
}

// startUpdateScheduler 启动定时更新调度器
func (s *PricingService) startUpdateScheduler() {
	if s == nil || s.cfg == nil {
		return
	}
	remoteEnabled := strings.TrimSpace(s.cfg.Pricing.RemoteURL) != ""
	watchCustom := s.hasCustomPricingFiles()
	if !remoteEnabled {
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Remote sync disabled: pricing remote URL is empty")
	}
	if !remoteEnabled && !watchCustom {
		return
	}

	hashInterval := time.Duration(s.cfg.Pricing.HashCheckIntervalMinutes) * time.Minute
	if hashInterval < time.Minute {
		hashInterval = 10 * time.Minute
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		pricingTicker := time.NewTicker(hashInterval)
		usageOfferTicker := time.NewTicker(openCodeGoUsageOfferRefreshInterval)
		defer pricingTicker.Stop()
		defer usageOfferTicker.Stop()

		for {
			select {
			case <-pricingTicker.C:
				if remoteEnabled {
					if err := s.syncWithRemote(); err != nil {
						logger.LegacyPrintf("service.pricing", "[Pricing] Sync failed: %v", err)
					}
				}
				if watchCustom {
					s.reloadIfCustomFilesChanged()
				}
			case <-usageOfferTicker.C:
				s.refreshOpenCodeGoUsageOffersBestEffortWithTimeout()
			case <-s.stopCh:
				return
			}
		}
	}()

	logger.LegacyPrintf("service.pricing", "[Pricing] Update scheduler started (check every %v, remote sync=%t, custom file watch=%t)", hashInterval, remoteEnabled, watchCustom)
}

// checkAndUpdatePricing 检查并更新价格数据
func (s *PricingService) checkAndUpdatePricing() error {
	pricingFile := s.getPricingFilePath()

	// 检查本地文件是否存在
	if _, err := os.Stat(pricingFile); os.IsNotExist(err) {
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Local pricing file not found, downloading...")
		return s.downloadPricingData()
	}

	// 先加载本地文件（确保服务可用），再检查是否需要更新
	if err := s.loadPricingData(pricingFile); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to load local file, downloading: %v", err)
		return s.downloadPricingData()
	}

	// 如果配置了哈希URL，通过远程哈希检查是否有更新
	if s.cfg.Pricing.HashURL != "" {
		remoteHash, err := s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash on startup: %v", err)
			return nil // 已加载本地文件，哈希获取失败不影响启动
		}

		s.mu.RLock()
		localHash := s.localHash
		s.mu.RUnlock()

		if localHash == "" || remoteHash != localHash {
			logger.LegacyPrintf("service.pricing", "[Pricing] Remote hash differs on startup (local=%s remote=%s), downloading...",
				localHash[:min(8, len(localHash))], remoteHash[:min(8, len(remoteHash))])
			if err := s.downloadPricingData(); err != nil {
				logger.LegacyPrintf("service.pricing", "[Pricing] Download failed, using existing file: %v", err)
			}
		}
		return nil
	}

	// 没有哈希URL时，基于文件年龄检查
	info, err := os.Stat(pricingFile)
	if err != nil {
		return nil // 已加载本地文件
	}

	fileAge := time.Since(info.ModTime())
	maxAge := time.Duration(s.cfg.Pricing.UpdateIntervalHours) * time.Hour

	if fileAge > maxAge {
		logger.LegacyPrintf("service.pricing", "[Pricing] Local file is %v old, updating...", fileAge.Round(time.Hour))
		if err := s.downloadPricingData(); err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Download failed, using existing file: %v", err)
		}
	}

	return nil
}

// syncWithRemote 与远程同步（基于哈希校验）
func (s *PricingService) syncWithRemote() error {
	// 如果配置了哈希URL，从远程获取哈希进行比对
	if s.cfg.Pricing.HashURL != "" {
		remoteHash, err := s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash: %v", err)
			s.refreshOpenCodeGoPricingBestEffortWithTimeout()
			return nil // 哈希获取失败不影响正常使用
		}

		s.mu.RLock()
		localHash := s.localHash
		s.mu.RUnlock()

		if localHash == "" || remoteHash != localHash {
			logger.LegacyPrintf("service.pricing", "[Pricing] Remote hash differs (local=%s remote=%s), downloading new version...",
				localHash[:min(8, len(localHash))], remoteHash[:min(8, len(remoteHash))])
			return s.downloadPricingDataAndRefreshOpenCodeGo()
		}
		logger.LegacyPrintf("service.pricing", "%s", "[Pricing] Hash check passed, no update needed")
		s.refreshOpenCodeGoPricingBestEffortWithTimeout()
		return nil
	}

	// 没有哈希URL时，基于时间检查
	pricingFile := s.getPricingFilePath()
	info, err := os.Stat(pricingFile)
	if err != nil {
		return s.downloadPricingDataAndRefreshOpenCodeGo()
	}

	fileAge := time.Since(info.ModTime())
	maxAge := time.Duration(s.cfg.Pricing.UpdateIntervalHours) * time.Hour

	if fileAge > maxAge {
		logger.LegacyPrintf("service.pricing", "[Pricing] File is %v old, downloading...", fileAge.Round(time.Hour))
		return s.downloadPricingDataAndRefreshOpenCodeGo()
	}
	s.refreshOpenCodeGoPricingBestEffortWithTimeout()

	return nil
}

// hasCustomPricingFiles 报告是否配置了 fallback/override 任一文件路径（不要求文件存在）。
func (s *PricingService) hasCustomPricingFiles() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return strings.TrimSpace(s.cfg.Pricing.FallbackFile) != "" || strings.TrimSpace(s.cfg.Pricing.OverrideFile) != ""
}

// customPricingFilesFingerprint 返回 fallback、override 两个文件当前内容的联合 sha256。
// 每个文件以"长度前缀 + 正文"参与计算，不可读的文件按空正文处理；未配置任何文件返回空串。
func (s *PricingService) customPricingFilesFingerprint() string {
	if !s.hasCustomPricingFiles() {
		return ""
	}
	h := sha256.New()
	for _, path := range []string{s.cfg.Pricing.FallbackFile, s.cfg.Pricing.OverrideFile} {
		var body []byte
		if p := strings.TrimSpace(path); p != "" {
			body, _ = os.ReadFile(p)
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(body)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// validateCustomPricingFiles 要求每个已配置且存在的 fallback/override 文件可读且为 JSON
// 对象，任一不满足即返回带路径的错误；文件不存在视为该层为空，属合法状态。
func (s *PricingService) validateCustomPricingFiles() error {
	for _, path := range []string{s.cfg.Pricing.FallbackFile, s.cfg.Pricing.OverrideFile} {
		p := strings.TrimSpace(path)
		if p == "" {
			continue
		}
		body, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(body, &entries); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// reloadIfCustomFilesChanged 比对 fallback/override 文件指纹，与最近一次重建时不同则从
// 本地目录缓存重建内存数据。文件被删除视为该层清空，照常重建；文件存在但不可读或不是
// JSON 对象时保留当前数据且不更新指纹，下一轮会再次尝试并重复告警。目录正文与远程同步
// 锚点(localHash)不受本路径影响。
func (s *PricingService) reloadIfCustomFilesChanged() {
	fingerprint := s.customPricingFilesFingerprint()
	s.mu.RLock()
	unchanged := fingerprint == s.customFilesHash
	s.mu.RUnlock()
	if unchanged {
		return
	}
	if err := s.reloadCustomPricingLayers(); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Custom pricing file changed but reload failed: %v", err)
	}
}

// reloadCustomPricingLayers 读取本地目录缓存并重新叠加 fallback/override，只替换内存数据
// 与叠加层指纹。
func (s *PricingService) reloadCustomPricingLayers() error {
	pricingFile := s.getPricingFilePath()
	// 定价层文件可能在读取期间被替换。只有构建前后指纹一致时才提交，
	// 否则丢弃这次混合快照并重试，避免短暂应用不匹配的 fallback/override。
	var data map[string]*LiteLLMModelPricing
	var fingerprint string
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if validateErr := s.validateCustomPricingFiles(); validateErr != nil {
			return fmt.Errorf("validate custom pricing files: %w", validateErr)
		}
		before := s.customPricingFilesFingerprint()
		body, readErr := os.ReadFile(pricingFile)
		if readErr != nil {
			return fmt.Errorf("read file failed: %w", readErr)
		}
		data, fingerprint, err = s.buildPricingData(body)
		if err != nil {
			return fmt.Errorf("parse pricing data: %w", err)
		}
		after := s.customPricingFilesFingerprint()
		if validateErr := s.validateCustomPricingFiles(); validateErr != nil {
			return fmt.Errorf("validate custom pricing files: %w", validateErr)
		}
		if before == after && after == fingerprint {
			break
		}
		if attempt == 2 {
			return fmt.Errorf("custom pricing files changed during reload")
		}
	}

	s.mu.Lock()
	warnDroppedLongContextLadders(s.pricingData, data)
	s.pricingData = data
	s.customFilesHash = fingerprint
	s.mu.Unlock()

	logger.LegacyPrintf("service.pricing", "[Pricing] Custom pricing files changed, reloaded %d models from %s", len(data), pricingFile)
	return nil
}

// downloadPricingData 从远程下载价格数据
func (s *PricingService) downloadPricingData() error {
	remoteURL, err := s.validatePricingURL(s.cfg.Pricing.RemoteURL)
	if err != nil {
		return err
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Downloading from %s", remoteURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 获取远程哈希（用于同步锚点，不作为完整性校验）
	var remoteHash string
	if strings.TrimSpace(s.cfg.Pricing.HashURL) != "" {
		remoteHash, err = s.fetchRemoteHash()
		if err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Failed to fetch remote hash (continuing): %v", err)
		}
	}

	body, err := s.remoteClient.FetchPricingJSON(ctx, remoteURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// 哈希校验：不匹配时仅告警，不阻止更新
	// 远程哈希文件可能与数据文件不同步（如维护者更新了数据但未更新哈希文件）
	dataHash := sha256.Sum256(body)
	dataHashStr := hex.EncodeToString(dataHash[:])
	if remoteHash != "" && !strings.EqualFold(remoteHash, dataHashStr) {
		logger.LegacyPrintf("service.pricing", "[Pricing] Hash mismatch warning: remote=%s data=%s (hash file may be out of sync)",
			remoteHash[:min(8, len(remoteHash))], dataHashStr[:8])
	}

	data, customFilesHash, err := s.buildPricingData(body)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}

	// 保存到本地文件
	pricingFile := s.getPricingFilePath()
	if err := os.WriteFile(pricingFile, body, 0644); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to save file: %v", err)
	}

	// 使用远程哈希作为同步锚点，防止重复下载
	// 当远程哈希不可用时，回退到数据本身的哈希
	syncHash := dataHashStr
	if remoteHash != "" {
		syncHash = remoteHash
	}
	hashFile := s.getHashFilePath()
	if err := os.WriteFile(hashFile, []byte(syncHash+"\n"), 0644); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to save hash: %v", err)
	}

	// 更新内存数据
	s.mu.Lock()
	warnDroppedLongContextLadders(s.pricingData, data)
	s.pricingData = data
	s.lastUpdated = time.Now()
	s.localHash = syncHash
	s.customFilesHash = customFilesHash
	s.mu.Unlock()

	logger.LegacyPrintf("service.pricing", "[Pricing] Downloaded %d models successfully", len(data))
	return nil
}

func (s *PricingService) downloadPricingDataAndRefreshOpenCodeGo() error {
	if err := s.downloadPricingData(); err != nil {
		return err
	}
	s.refreshOpenCodeGoPricingBestEffortWithTimeout()
	return nil
}

// parsePricingData 解析价格数据（处理各种格式）
func (s *PricingService) parsePricingData(body []byte) (map[string]*LiteLLMModelPricing, error) {
	// 首先解析为 map[string]json.RawMessage
	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, fmt.Errorf("parse raw JSON: %w", err)
	}
	rawData = s.applyPricingOverrides(rawData)

	result := make(map[string]*LiteLLMModelPricing)
	skipped := 0
	var orphanCacheTiers, lopsidedLadders []string

	for modelName, rawEntry := range rawData {
		// 跳过 sample_spec 等文档条目
		if modelName == "sample_spec" {
			continue
		}

		// 尝试解析每个条目
		var entry LiteLLMRawEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			skipped++
			continue
		}

		// 只保留有有效价格的条目
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil && entry.OutputCostPerImage == nil && entry.OutputCostPerImageToken == nil && entry.InputCostPerImageToken == nil {
			continue
		}

		pricing := &LiteLLMModelPricing{
			LiteLLMProvider:       entry.LiteLLMProvider,
			Mode:                  entry.Mode,
			SupportsPromptCaching: entry.SupportsPromptCaching,
			SupportsServiceTier:   entry.SupportsServiceTier,
			TokenPricingAbsent:    entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil,
		}

		if entry.InputCostPerToken != nil {
			pricing.InputCostPerToken = *entry.InputCostPerToken
			pricing.InputCostPerTokenKnown = true
		}
		if entry.InputCostPerTokenPriority != nil {
			pricing.InputCostPerTokenPriority = *entry.InputCostPerTokenPriority
		}
		if entry.OutputCostPerToken != nil {
			pricing.OutputCostPerToken = *entry.OutputCostPerToken
			pricing.OutputCostPerTokenKnown = true
		}
		if entry.OutputCostPerTokenPriority != nil {
			pricing.OutputCostPerTokenPriority = *entry.OutputCostPerTokenPriority
		}
		if entry.CacheCreationInputTokenCost != nil {
			pricing.CacheCreationInputTokenCost = *entry.CacheCreationInputTokenCost
			pricing.CacheCreationInputTokenCostKnown = true
		}
		if entry.CacheCreationInputTokenCostPriority != nil {
			pricing.CacheCreationInputTokenCostPriority = *entry.CacheCreationInputTokenCostPriority
		}
		if entry.CacheCreationInputTokenCostAbove1hr != nil {
			pricing.CacheCreationInputTokenCostAbove1hr = *entry.CacheCreationInputTokenCostAbove1hr
			pricing.CacheCreationInputTokenCostAbove1hrKnown = true
		}
		if entry.CacheReadInputTokenCost != nil {
			pricing.CacheReadInputTokenCost = *entry.CacheReadInputTokenCost
			pricing.CacheReadInputTokenCostKnown = true
		}
		if entry.CacheReadInputTokenCostPriority != nil {
			pricing.CacheReadInputTokenCostPriority = *entry.CacheReadInputTokenCostPriority
		}
		if entry.LongContextInputTokenThreshold != nil {
			pricing.LongContextInputTokenThreshold = *entry.LongContextInputTokenThreshold
		}
		if entry.LongContextInputCostMultiplier != nil {
			pricing.LongContextInputCostMultiplier = *entry.LongContextInputCostMultiplier
		}
		if entry.LongContextOutputCostMultiplier != nil {
			pricing.LongContextOutputCostMultiplier = *entry.LongContextOutputCostMultiplier
		}
		if entry.OutputCostPerImage != nil {
			pricing.OutputCostPerImage = *entry.OutputCostPerImage
		}
		if entry.OutputCostPerImageToken != nil {
			pricing.OutputCostPerImageToken = *entry.OutputCostPerImageToken
		}
		if entry.InputCostPerImageToken != nil {
			pricing.InputCostPerImageToken = *entry.InputCostPerImageToken
		}
		if entry.SupportsReasoning != nil {
			pricing.SupportsReasoning = *entry.SupportsReasoning
			pricing.SupportsReasoningKnown = true
		}
		if entry.SupportsVision != nil {
			pricing.SupportsVision = *entry.SupportsVision
			pricing.SupportsVisionKnown = true
		}
		if entry.SupportsPDFInput != nil {
			pricing.SupportsPDFInput = *entry.SupportsPDFInput
			pricing.SupportsPDFInputKnown = true
		}
		if entry.SupportsFunctionCalling != nil {
			pricing.SupportsFunctionCalling = *entry.SupportsFunctionCalling
			pricing.SupportsFunctionCallingKnown = true
		}
		if entry.SupportsToolChoice != nil {
			pricing.SupportsToolChoice = *entry.SupportsToolChoice
			pricing.SupportsToolChoiceKnown = true
		}
		if entry.MaxInputTokens != nil {
			pricing.MaxInputTokens = *entry.MaxInputTokens
			pricing.MaxInputTokensKnown = true
		}
		if entry.MaxOutputTokens != nil {
			pricing.MaxOutputTokens = *entry.MaxOutputTokens
			pricing.MaxOutputTokensKnown = true
		}

		hasExplicitLongContext := entry.LongContextInputTokenThreshold != nil ||
			entry.LongContextInputCostMultiplier != nil ||
			entry.LongContextOutputCostMultiplier != nil
		if !hasExplicitLongContext {
			deriveLongContextFromAboveTierFields(rawEntry, pricing)
			if isLopsidedLongContextLadder(pricing) {
				lopsidedLadders = append(lopsidedLadders, fmt.Sprintf("%s(input x%.2f, output x%.2f)", modelName,
					pricing.LongContextInputCostMultiplier, pricing.LongContextOutputCostMultiplier))
			}
		}
		if orphans := orphanCacheTierFields(rawEntry); len(orphans) > 0 {
			orphanCacheTiers = append(orphanCacheTiers, modelName+"("+strings.Join(orphans, ",")+")")
		}

		result[modelName] = pricing
	}

	if skipped > 0 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Skipped %d invalid entries", skipped)
	}
	warnOrphanCacheTierFields(orphanCacheTiers)
	warnLopsidedLongContextLadders(lopsidedLadders)

	if len(result) == 0 {
		return nil, fmt.Errorf("no valid pricing entries found")
	}

	return result, nil
}

type openCodeGoDocPriceRow struct {
	Name            string
	Key             string
	Input           float64
	Output          float64
	CacheRead       float64
	CacheWrite      float64
	MonthlyUsageUSD float64
	Threshold       int
	Above           bool
	Peak            bool
	ZeroTokenPrices bool
}

func parseOpenCodeGoPricingDocument(body []byte) (map[string]*LiteLLMModelPricing, error) {
	rows := extractHTMLTableRows(body)
	if len(rows) == 0 {
		return nil, fmt.Errorf("opencode go pricing document contains no tables")
	}

	modelIDs := make(map[string]string)
	baseRows := make(map[string]openCodeGoDocPriceRow)
	aboveRows := make(map[string]openCodeGoDocPriceRow)
	peakRows := make(map[string]openCodeGoDocPriceRow)
	explicitFreeModels := parseOpenCodeGoExplicitFreeModelKeys(body)
	priceRows := 0

	for _, cells := range rows {
		if len(cells) < 2 {
			continue
		}
		if len(cells) >= 3 && isOpenCodeGoModelID(cells[1]) && strings.Contains(cells[2], "opencode.ai/zen/go/v1") {
			modelIDs[normalizeOpenCodeGoDocModelKey(cells[0])] = strings.ToLower(strings.TrimSpace(cells[1]))
			continue
		}
		if len(cells) < 5 {
			continue
		}
		input, okInput := parseOpenCodeGoMillionTokenPrice(cells[1])
		output, okOutput := parseOpenCodeGoMillionTokenPrice(cells[2])
		if !okInput || !okOutput {
			continue
		}
		cacheRead, _ := parseOpenCodeGoMillionTokenPrice(cells[3])
		cacheWrite, _ := parseOpenCodeGoMillionTokenPrice(cells[4])
		monthlyUsageUSD := 0.0
		if len(cells) >= 6 {
			monthlyUsageUSD, _ = parseOpenCodeGoUsageUSD(cells[5])
		}
		threshold, above := parseOpenCodeGoContextThreshold(cells[0])
		row := openCodeGoDocPriceRow{
			Name:            cells[0],
			Key:             normalizeOpenCodeGoDocModelKey(cells[0]),
			Input:           input,
			Output:          output,
			CacheRead:       cacheRead,
			CacheWrite:      cacheWrite,
			MonthlyUsageUSD: monthlyUsageUSD,
			Threshold:       threshold,
			Above:           above,
			Peak:            isOpenCodeGoPeakPriceRow(cells[0]),
			ZeroTokenPrices: isOpenCodeGoExplicitZeroPrice(cells[1]) &&
				isOpenCodeGoExplicitZeroPrice(cells[2]) &&
				isOpenCodeGoExplicitZeroPrice(cells[3]) &&
				isOpenCodeGoExplicitZeroPrice(cells[4]),
		}
		if row.Key == "" {
			continue
		}
		priceRows++
		switch {
		case row.Peak:
			peakRows[row.Key] = row
		case row.Above:
			aboveRows[row.Key] = row
		default:
			baseRows[row.Key] = row
		}
	}
	if priceRows == 0 {
		return nil, fmt.Errorf("opencode go pricing document contains no model price rows")
	}

	result := make(map[string]*LiteLLMModelPricing, len(baseRows))
	for key, row := range baseRows {
		modelID := modelIDs[key]
		if modelID == "" {
			modelID = openCodeGoFallbackModelID(row.Name)
		}
		if modelID == "" {
			continue
		}
		pricing := &LiteLLMModelPricing{
			InputCostPerToken:                row.Input,
			OutputCostPerToken:               row.Output,
			CacheReadInputTokenCost:          row.CacheRead,
			CacheCreationInputTokenCost:      row.CacheWrite,
			LiteLLMProvider:                  PlatformOpenCodeGo,
			Mode:                             "chat",
			SupportsPromptCaching:            row.CacheRead > 0 || row.CacheWrite > 0,
			InputCostPerTokenKnown:           true,
			OutputCostPerTokenKnown:          true,
			CacheReadInputTokenCostKnown:     row.CacheRead > 0,
			CacheCreationInputTokenCostKnown: row.CacheWrite > 0,
			OpenCodeGoPricingAuthority:       openCodeGoPricingAuthorityOfficial,
			OpenCodeGoMonthlyUsageUSD:        row.MonthlyUsageUSD,
			OpenCodeGoExplicitZeroRate:       row.ZeroTokenPrices && explicitFreeModels[row.Key],
		}
		if peak, ok := peakRows[key]; ok {
			pricing.OpenCodeGoPeakPricingKnown = true
			pricing.OpenCodeGoPeakInputCostPerToken = peak.Input
			pricing.OpenCodeGoPeakOutputCostPerToken = peak.Output
			pricing.OpenCodeGoPeakCacheReadCostPerToken = peak.CacheRead
			pricing.OpenCodeGoPeakCacheCreationCostPerToken = peak.CacheWrite
		}
		if above, ok := aboveRows[key]; ok && row.Threshold > 0 {
			pricing.LongContextInputTokenThreshold = row.Threshold
			if row.Input > 0 && above.Input > row.Input {
				pricing.LongContextInputCostMultiplier = above.Input / row.Input
			}
			if row.Output > 0 && above.Output > row.Output {
				pricing.LongContextOutputCostMultiplier = above.Output / row.Output
			}
		}
		result[modelID] = pricing
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("opencode go pricing document contains no usable model prices")
	}
	return result, nil
}

func parseOpenCodeGoModelsDevPricingDocument(body []byte) (map[string]*LiteLLMModelPricing, error) {
	var raw map[string]cliImportModelsDevProvider
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	provider, ok := raw["opencode-go"]
	if !ok || len(provider.Models) == 0 {
		return nil, fmt.Errorf("models.dev catalog does not contain opencode-go provider")
	}

	result := make(map[string]*LiteLLMModelPricing, len(provider.Models))
	for modelKey, model := range provider.Models {
		modelID := strings.ToLower(strings.TrimSpace(model.ID))
		if modelID == "" {
			modelID = strings.ToLower(strings.TrimSpace(modelKey))
		}
		if modelID == "" || strings.Contains(modelID, "*") || !isOpenCodeGoModelID(modelID) {
			continue
		}
		if model.Cost == nil || model.Cost.Input == nil || model.Cost.Output == nil {
			continue
		}
		input := *model.Cost.Input / 1_000_000
		output := *model.Cost.Output / 1_000_000
		if input <= 0 && output <= 0 {
			continue
		}
		pricing := &LiteLLMModelPricing{
			InputCostPerToken:          input,
			OutputCostPerToken:         output,
			LiteLLMProvider:            PlatformOpenCodeGo,
			Mode:                       "chat",
			InputCostPerTokenKnown:     true,
			OutputCostPerTokenKnown:    true,
			OpenCodeGoPricingAuthority: openCodeGoPricingAuthorityModelsDev,
		}
		if model.Cost.CacheRead != nil {
			pricing.CacheReadInputTokenCost = *model.Cost.CacheRead / 1_000_000
			pricing.CacheReadInputTokenCostKnown = true
		}
		if model.Cost.CacheWrite != nil {
			pricing.CacheCreationInputTokenCost = *model.Cost.CacheWrite / 1_000_000
			pricing.CacheCreationInputTokenCostKnown = true
		}
		pricing.SupportsPromptCaching = pricing.CacheReadInputTokenCost > 0 || pricing.CacheCreationInputTokenCost > 0
		if model.Reasoning != nil {
			pricing.SupportsReasoning = *model.Reasoning
			pricing.SupportsReasoningKnown = true
		}
		if model.ToolCall != nil {
			pricing.SupportsFunctionCalling = *model.ToolCall
			pricing.SupportsToolChoice = *model.ToolCall
			pricing.SupportsFunctionCallingKnown = true
			pricing.SupportsToolChoiceKnown = true
		}
		if model.Modalities != nil {
			inputModalities := cleanCLIImportModalities(model.Modalities.Input)
			pricing.SupportsVision = containsString(inputModalities, "image")
			pricing.SupportsPDFInput = containsString(inputModalities, "pdf")
			pricing.SupportsVisionKnown = true
			pricing.SupportsPDFInputKnown = true
		}
		if model.Limit != nil {
			if model.Limit.Context != nil {
				pricing.MaxInputTokens = *model.Limit.Context
				pricing.MaxInputTokensKnown = true
			}
			if model.Limit.Output != nil {
				pricing.MaxOutputTokens = *model.Limit.Output
				pricing.MaxOutputTokensKnown = true
			}
		}
		result[modelID] = pricing
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("models.dev opencode-go provider contains no usable model prices")
	}
	return result, nil
}

func extractHTMLTableRows(body []byte) [][]string {
	rowMatches := htmlTableRowPattern.FindAllSubmatch(body, -1)
	rows := make([][]string, 0, len(rowMatches))
	for _, rowMatch := range rowMatches {
		if len(rowMatch) < 2 {
			continue
		}
		cellMatches := htmlTableCellPattern.FindAllSubmatch(rowMatch[1], -1)
		if len(cellMatches) == 0 {
			continue
		}
		cells := make([]string, 0, len(cellMatches))
		for _, cellMatch := range cellMatches {
			if len(cellMatch) < 2 {
				continue
			}
			cells = append(cells, cleanHTMLCellText(cellMatch[1]))
		}
		rows = append(rows, cells)
	}
	return rows
}

func cleanHTMLCellText(raw []byte) string {
	text := htmlTagPattern.ReplaceAllString(string(raw), "")
	text = stdhtml.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func parseOpenCodeGoMillionTokenPrice(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "-" {
		return 0, true
	}
	match := openCodeGoPricePattern.FindStringSubmatch(trimmed)
	if len(match) != 2 {
		return 0, false
	}
	price, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return price / 1_000_000, true
}

func parseOpenCodeGoUsageUSD(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "-" {
		return 0, false
	}
	match := openCodeGoPricePattern.FindStringSubmatch(trimmed)
	if len(match) != 2 {
		return 0, false
	}
	usageUSD, err := strconv.ParseFloat(match[1], 64)
	if err != nil || usageUSD <= 0 {
		return 0, false
	}
	return usageUSD, true
}

func isOpenCodeGoPeakPriceRow(name string) bool {
	normalized := strings.ToLower(stdhtml.UnescapeString(name))
	return strings.Contains(normalized, "(peak)")
}

func openCodeGoPricingAt(pricing *LiteLLMModelPricing, now time.Time) *LiteLLMModelPricing {
	if pricing == nil || !pricing.OpenCodeGoPeakPricingKnown {
		return pricing
	}
	if !isOpenCodeGoPeakTime(now) {
		return pricing
	}
	selected := *pricing
	selected.InputCostPerToken = pricing.OpenCodeGoPeakInputCostPerToken
	selected.OutputCostPerToken = pricing.OpenCodeGoPeakOutputCostPerToken
	selected.CacheCreationInputTokenCost = pricing.OpenCodeGoPeakCacheCreationCostPerToken
	selected.CacheReadInputTokenCost = pricing.OpenCodeGoPeakCacheReadCostPerToken
	return &selected
}

func parseOpenCodeGoExplicitFreeModelKeys(body []byte) map[string]bool {
	result := make(map[string]bool)
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return result
	}
	collectOpenCodeGoExplicitFreeModelKeys(doc, result)
	return result
}

func collectOpenCodeGoExplicitFreeModelKeys(node *xhtml.Node, result map[string]bool) {
	if node == nil {
		return
	}
	if node.Type == xhtml.ElementNode && (strings.EqualFold(node.Data, "p") || strings.EqualFold(node.Data, "li")) {
		const suffix = ": free for a limited time"
		text := strings.TrimSuffix(normalizeOpenCodeGoVisibleText(openCodeGoVisibleText(node)), ".")
		if strings.HasSuffix(text, suffix) {
			name := strings.TrimSpace(strings.TrimSuffix(text, suffix))
			if key := normalizeOpenCodeGoDocModelKey(name); key != "" {
				result[key] = true
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectOpenCodeGoExplicitFreeModelKeys(child, result)
	}
}

func isOpenCodeGoExplicitZeroPrice(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	price, ok := parseOpenCodeGoMillionTokenPrice(trimmed)
	return ok && price == 0
}

func parseOpenCodeGoContextThreshold(name string) (int, bool) {
	normalized := stdhtml.UnescapeString(strings.ToLower(name))
	match := openCodeGoThresholdPattern.FindStringSubmatch(normalized)
	if len(match) != 3 {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(match[2]) {
	case "m":
		value *= 1_000_000
	default:
		value *= 1_000
	}
	return value, strings.Contains(normalized, ">") || strings.Contains(normalized, "≥")
}

func normalizeOpenCodeGoDocModelKey(name string) string {
	normalized := strings.ToLower(stdhtml.UnescapeString(name))
	if idx := strings.Index(normalized, "("); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	if strings.HasPrefix(normalized, "kimi ") && strings.HasSuffix(normalized, " code") {
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, " code"))
	}
	return normalized
}

func isOpenCodeGoModelID(value string) bool {
	return openCodeGoModelIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func openCodeGoFallbackModelID(name string) string {
	key := normalizeOpenCodeGoDocModelKey(name)
	if key == "" {
		return ""
	}
	return strings.ReplaceAll(key, " ", "-")
}

func openCodeGoVisibleText(node *xhtml.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == xhtml.ElementNode {
		switch strings.ToLower(node.Data) {
		case "script", "style", "template":
			return ""
		}
	}
	var parts []string
	if node.Type == xhtml.TextNode {
		if text := strings.TrimSpace(node.Data); text != "" {
			parts = append(parts, text)
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if text := openCodeGoVisibleText(child); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func normalizeOpenCodeGoVisibleText(text string) string {
	text = stdhtml.UnescapeString(text)
	text = strings.ReplaceAll(text, "×", "x")
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func openCodeGoHTMLAttribute(node *xhtml.Node, name string) (string, bool) {
	if node == nil || node.Type != xhtml.ElementNode {
		return "", false
	}
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func isOpenCodeGoHiddenUsageOfferNode(node *xhtml.Node) bool {
	if node == nil || node.Type != xhtml.ElementNode {
		return false
	}
	switch strings.ToLower(node.Data) {
	case "script", "style", "template":
		return true
	default:
		return false
	}
}

func findOpenCodeGoUsageOffersMain(node *xhtml.Node) *xhtml.Node {
	if node == nil || isOpenCodeGoHiddenUsageOfferNode(node) {
		return nil
	}
	if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "main") {
		if page, ok := openCodeGoHTMLAttribute(node, "data-page"); ok && strings.EqualFold(strings.TrimSpace(page), "go") {
			return node
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if match := findOpenCodeGoUsageOffersMain(child); match != nil {
			return match
		}
	}
	return nil
}

func openCodeGoFindDescendantWithAttribute(node *xhtml.Node, name string) *xhtml.Node {
	if node == nil || isOpenCodeGoHiddenUsageOfferNode(node) {
		return nil
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if isOpenCodeGoHiddenUsageOfferNode(child) {
			continue
		}
		if _, ok := openCodeGoHTMLAttribute(child, name); ok {
			return child
		}
		if match := openCodeGoFindDescendantWithAttribute(child, name); match != nil {
			return match
		}
	}
	return nil
}

func parseOpenCodeGoUsageOffersDocument(body []byte) (map[string]float64, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("opencode go usage offers page is empty")
	}
	doc, err := xhtml.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse opencode go usage offers page: %w", err)
	}
	main := findOpenCodeGoUsageOffersMain(doc)
	if main == nil {
		return nil, fmt.Errorf("opencode go usage offers page marker is missing")
	}

	offers := make(map[string]float64)
	var parseErr error
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node == nil || parseErr != nil || isOpenCodeGoHiddenUsageOfferNode(node) {
			return
		}
		model, hasModel := openCodeGoHTMLAttribute(node, "data-model")
		bonus := openCodeGoFindDescendantWithAttribute(node, "data-bonus")
		if hasModel && bonus != nil {
			model = billingModelAliasLookupKey(model)
			if model != "" && openCodeGoModelIDPattern.MatchString(model) {
				text := normalizeOpenCodeGoVisibleText(openCodeGoVisibleText(bonus))
				match := openCodeGoUsageOfferPattern.FindStringSubmatch(text)
				if len(match) == 2 {
					multiplier, multiplierErr := strconv.ParseFloat(match[1], 64)
					if multiplierErr == nil && multiplier > 1 && !math.IsNaN(multiplier) && !math.IsInf(multiplier, 0) {
						if existing, exists := offers[model]; exists && existing != multiplier {
							parseErr = fmt.Errorf("opencode go usage offer for %s is ambiguous", model)
							return
						}
						offers[model] = multiplier
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(main)
	if parseErr != nil {
		return nil, parseErr
	}
	return offers, nil
}

func (s *PricingService) replaceOpenCodeGoUsageOffers(offers map[string]float64, confirmedAt time.Time) {
	if s == nil {
		return
	}
	if confirmedAt.IsZero() {
		confirmedAt = time.Now()
	}
	next := make(map[string]openCodeGoUsageOffer, len(offers))
	for model, multiplier := range offers {
		model = billingModelAliasLookupKey(model)
		if model == "" || multiplier <= 1 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			continue
		}
		next[model] = openCodeGoUsageOffer{usageMultiplier: multiplier, confirmedAt: confirmedAt}
	}
	s.usageOfferMu.Lock()
	s.openCodeGoUsageOffers = next
	s.usageOfferMu.Unlock()
}

func (s *PricingService) expireOpenCodeGoUsageOffers(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.usageOfferMu.Lock()
	for model, offer := range s.openCodeGoUsageOffers {
		age := now.Sub(offer.confirmedAt)
		if offer.confirmedAt.IsZero() || age < 0 || age > openCodeGoUsageOfferEvidenceTTL {
			delete(s.openCodeGoUsageOffers, model)
		}
	}
	s.usageOfferMu.Unlock()
}

func (s *PricingService) OpenCodeGoUsageOfferMultiplier(model string, now time.Time) float64 {
	if s == nil {
		return 1
	}
	model = strings.ToLower(strings.TrimSpace(model))
	model = trimOpenCodeGoModelProviderPrefix(model)
	model = strings.TrimPrefix(model, "models/")
	model = trimOpenCodeGoModelProviderPrefix(model)
	if model == "" {
		return 1
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.usageOfferMu.RLock()
	offer, ok := s.openCodeGoUsageOffers[model]
	s.usageOfferMu.RUnlock()
	age := now.Sub(offer.confirmedAt)
	if !ok || offer.usageMultiplier <= 1 || math.IsNaN(offer.usageMultiplier) || math.IsInf(offer.usageMultiplier, 0) ||
		offer.confirmedAt.IsZero() || age < 0 || age > openCodeGoUsageOfferEvidenceTTL {
		return 1
	}
	return offer.usageMultiplier
}

func (s *PricingService) refreshOpenCodeGoUsageOffersBestEffortWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.refreshOpenCodeGoUsageOffersBestEffort(ctx)
}

func (s *PricingService) refreshOpenCodeGoUsageOffersBestEffort(ctx context.Context) {
	if s == nil {
		return
	}
	if s.cfg == nil || s.remoteClient == nil {
		s.expireOpenCodeGoUsageOffers(time.Now())
		return
	}
	promotionsURL := strings.TrimSpace(s.cfg.Pricing.OpenCodeGoPromotionsURL)
	if promotionsURL == "" {
		s.replaceOpenCodeGoUsageOffers(nil, time.Now())
		return
	}
	validatedURL, err := s.validatePricingURL(promotionsURL)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go usage offers URL invalid: %v", err)
		s.expireOpenCodeGoUsageOffers(time.Now())
		return
	}
	body, err := s.remoteClient.FetchPricingJSON(ctx, validatedURL)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go usage offers fetch failed: %v", err)
		s.expireOpenCodeGoUsageOffers(time.Now())
		return
	}
	offers, err := parseOpenCodeGoUsageOffersDocument(body)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go usage offers parse failed: %v", err)
		s.expireOpenCodeGoUsageOffers(time.Now())
		return
	}
	s.replaceOpenCodeGoUsageOffers(offers, time.Now())
	logger.LegacyPrintf("service.pricing", "[Pricing] Refreshed %d OpenCode Go official usage offers", len(offers))
}

func (s *PricingService) mergeOpenCodeGoPricingBestEffort(ctx context.Context, pricingData map[string]*LiteLLMModelPricing) int {
	if s == nil || s.cfg == nil || s.remoteClient == nil || pricingData == nil {
		return 0
	}
	docsURL := strings.TrimSpace(s.cfg.Pricing.OpenCodeGoDocsURL)
	if docsURL == "" {
		return 0
	}
	merged := 0
	validatedURL, err := s.validatePricingURL(docsURL)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go pricing URL invalid: %v", err)
		return 0
	}
	body, err := s.remoteClient.FetchPricingJSON(ctx, validatedURL)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go pricing fetch failed: %v", err)
	} else {
		if openCodeGoPricing, pricingErr := parseOpenCodeGoPricingDocument(body); pricingErr != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go pricing parse failed: %v", pricingErr)
		} else {
			for model, pricing := range openCodeGoPricing {
				pricingData[model] = pricing
				merged++
			}
			logger.LegacyPrintf("service.pricing", "[Pricing] Merged %d OpenCode Go official prices", len(openCodeGoPricing))
		}
	}

	modelsDevURL, err := s.validateSupplementalPricingURL(cliImportModelsDevAPIURL, []string{"models.dev"})
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go models.dev pricing URL invalid: %v", err)
		return merged
	}
	body, err = s.remoteClient.FetchPricingJSON(ctx, modelsDevURL)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go models.dev pricing fetch failed: %v", err)
		return merged
	}
	modelsDevPricing, err := parseOpenCodeGoModelsDevPricingDocument(body)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenCode Go models.dev pricing parse failed: %v", err)
		return merged
	}
	modelsDevMerged := 0
	for model, pricing := range modelsDevPricing {
		if !isOpenCodeGoModelsDevSupplementalModel(model) {
			continue
		}
		if _, exists := pricingData[model]; exists {
			continue
		}
		pricingData[model] = pricing
		modelsDevMerged++
	}
	merged += modelsDevMerged
	logger.LegacyPrintf("service.pricing", "[Pricing] Merged %d OpenCode Go models.dev supplemental prices", modelsDevMerged)
	return merged
}

func (s *PricingService) refreshOpenCodeGoPricingBestEffortWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.refreshOpenCodeGoPricingBestEffort(ctx)
}

func (s *PricingService) refreshOpenCodeGoPricingBestEffort(ctx context.Context) {
	if s == nil {
		return
	}
	current := make(map[string]*LiteLLMModelPricing)
	if s.mergeOpenCodeGoPricingBestEffort(ctx, current) == 0 || !hasOpenCodeGoOfficialPricing(current) {
		// 若抓取失败且当前内存中尚无定价数据，尝试从本地缓存文件恢复
		s.mu.RLock()
		hasMemoryPricing := len(s.openCodeGoPricing) > 0
		s.mu.RUnlock()
		if !hasMemoryPricing {
			_ = s.loadOpenCodeGoPricingData(s.getOpenCodeGoPricingFilePath())
		}
		return
	}
	confirmedAt := time.Now()
	s.mu.Lock()
	s.openCodeGoPricing = current
	s.openCodeGoPricingConfirmedAt = confirmedAt
	s.lastUpdated = confirmedAt
	s.mu.Unlock()
	_ = s.saveOpenCodeGoPricingData(current)
}

func (s *PricingService) getOpenCodeGoPricingFilePath() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return filepath.Join(s.cfg.Pricing.DataDir, "opencode_go_pricing.json")
}

func (s *PricingService) loadOpenCodeGoPricingData(filePath string) error {
	if filePath == "" {
		return fmt.Errorf("empty file path")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	confirmedAt := time.Time{}
	if info, statErr := os.Stat(filePath); statErr == nil {
		confirmedAt = info.ModTime()
	}
	var pricingData map[string]*LiteLLMModelPricing
	if err := json.Unmarshal(data, &pricingData); err != nil {
		return fmt.Errorf("unmarshal opencode_go pricing: %w", err)
	}
	if len(pricingData) == 0 {
		return fmt.Errorf("no opencode_go pricing entries in cache file")
	}
	for _, p := range pricingData {
		if p != nil {
			if p.LiteLLMProvider == "" {
				p.LiteLLMProvider = PlatformOpenCodeGo
			}
			if p.InputCostPerToken > 0 {
				p.InputCostPerTokenKnown = true
			}
			if p.OutputCostPerToken > 0 {
				p.OutputCostPerTokenKnown = true
			}
			if p.CacheCreationInputTokenCost > 0 {
				p.CacheCreationInputTokenCostKnown = true
			}
			if p.CacheReadInputTokenCost > 0 {
				p.CacheReadInputTokenCostKnown = true
			}
		}
	}
	s.mu.Lock()
	if len(s.openCodeGoPricing) == 0 {
		s.openCodeGoPricing = pricingData
		s.openCodeGoPricingConfirmedAt = confirmedAt
	}
	s.mu.Unlock()
	logger.LegacyPrintf("service.pricing", "[Pricing] Loaded %d OpenCode Go cached models from %s", len(pricingData), filePath)
	return nil
}

func (s *PricingService) saveOpenCodeGoPricingData(data map[string]*LiteLLMModelPricing) error {
	if s == nil || len(data) == 0 {
		return nil
	}
	filePath := s.getOpenCodeGoPricingFilePath()
	if filePath == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to marshal OpenCode Go pricing: %v", err)
		return err
	}
	if err := os.WriteFile(filePath, encoded, 0644); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to save OpenCode Go pricing file: %v", err)
		return err
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Saved %d OpenCode Go models to %s", len(data), filePath)
	return nil
}

func hasOpenCodeGoOfficialPricing(data map[string]*LiteLLMModelPricing) bool {
	for _, pricing := range data {
		if pricing != nil && isOpenCodeGoPricingPlatform(pricing.LiteLLMProvider) &&
			pricing.OpenCodeGoPricingAuthority == openCodeGoPricingAuthorityOfficial {
			return true
		}
	}
	return false
}

// deriveLongContextFromAboveTierFields 把 LiteLLM 目录的 *_above_XXXk_tokens 绝对价字段
// 折算成 long_context_* 阈值+倍率（sub2api 计费机制的内部表达）：阈值取自字段名，
// 倍率 = above 价 ÷ 基础价。条目显式携带任一 long_context_* 字段（含显式 0）时由
// 调用方跳过折算，以显式配置为准——显式写 threshold=0 或 multiplier=1 均可关闭该
// 模型的阶梯。多个阈值并存时取最小阈值。
// cache_read/cache_creation 的 above 档在计费中统一跟随输入倍率，不单独折算：
// 目录条目的 cache above 档须恰为基础价 × 输入倍率；缺基础价的 cache above 字段
// 无法参与计费，由 orphanCacheTierFields 哨兵告警。
func deriveLongContextFromAboveTierFields(rawEntry json.RawMessage, pricing *LiteLLMModelPricing) {
	if pricing == nil ||
		pricing.LongContextInputTokenThreshold > 0 ||
		pricing.LongContextInputCostMultiplier > 0 ||
		pricing.LongContextOutputCostMultiplier > 0 {
		return
	}
	if !bytes.Contains(rawEntry, []byte("_above_")) {
		return
	}
	var fields map[string]any
	if err := json.Unmarshal(rawEntry, &fields); err != nil {
		return
	}
	type tierPrices struct{ input, output float64 }
	tiers := make(map[int]*tierPrices)
	for key, value := range fields {
		m := aboveTierPricePattern.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		price, ok := value.(float64)
		if !ok || price <= 0 {
			continue
		}
		thousands, err := strconv.Atoi(m[2])
		if err != nil || thousands <= 0 {
			continue
		}
		threshold := thousands * 1000
		tp := tiers[threshold]
		if tp == nil {
			tp = &tierPrices{}
			tiers[threshold] = tp
		}
		if m[1] == "input" {
			tp.input = price
		} else {
			tp.output = price
		}
	}
	if len(tiers) == 0 {
		return
	}
	threshold := 0
	for t := range tiers {
		if threshold == 0 || t < threshold {
			threshold = t
		}
	}
	tp := tiers[threshold]
	inputMultiplier, outputMultiplier := 1.0, 1.0
	if tp.input > 0 && pricing.InputCostPerToken > 0 {
		inputMultiplier = tp.input / pricing.InputCostPerToken
	}
	if tp.output > 0 && pricing.OutputCostPerToken > 0 {
		outputMultiplier = tp.output / pricing.OutputCostPerToken
	}
	// above 价不高于基础价时视为无附加费，不生成阶梯。
	if inputMultiplier <= 1 && outputMultiplier <= 1 {
		return
	}
	pricing.LongContextInputTokenThreshold = threshold
	pricing.LongContextInputCostMultiplier = inputMultiplier
	pricing.LongContextOutputCostMultiplier = outputMultiplier
}

// isLopsidedLongContextLadder 判断折算出的阶梯是否只有一侧带附加费。官方阶梯（OpenAI、
// Google、Anthropic、xAI）都同时抬高 input 与 output；单侧附加费意味着条目的基础价与
// above 档来自不同价格版本（如基础价被手工 pin、above 档随上游更新），折算出的倍率失真。
func isLopsidedLongContextLadder(pricing *LiteLLMModelPricing) bool {
	if pricing == nil || pricing.LongContextInputTokenThreshold <= 0 {
		return false
	}
	return (pricing.LongContextInputCostMultiplier > 1) != (pricing.LongContextOutputCostMultiplier > 1)
}

// warnLopsidedLongContextLadders 对单侧附加费的折算阶梯打 WARN：应成组修正该条目的
// 基础价与 above 档（目录或 pricing.override_file）。
func warnLopsidedLongContextLadders(entries []string) {
	if len(entries) == 0 {
		return
	}
	sort.Strings(entries)
	total := len(entries)
	if total > 20 {
		entries = append(entries[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: %d model(s) derive a one-sided long-context ladder (surcharge on only input or only output); base prices and above-tier prices likely come from different price versions: %s", total, strings.Join(entries, ", "))
}

// orphanCacheTierFields 返回条目中没有对应基础价的 cache 侧 above 档字段名。
// cache 侧 above 档不参与计费取值，计费按"基础价 × 输入倍率"；基础价缺失或为 0 时，
// 该缓存分项在整个阶梯上都按 0 计。计费对变体有回落：服务档变体（_priority/_flex）
// 缺自身基础价时用标准基础价，1h 缓存写入缺 above_1hr 价时全部按 5m 价——因此沿
// 回落链任一基础价存在即不算孤儿。
func orphanCacheTierFields(rawEntry json.RawMessage) []string {
	if !bytes.Contains(rawEntry, []byte("_above_")) {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(rawEntry, &fields); err != nil {
		return nil
	}
	positive := func(key string) bool {
		price, ok := fields[key].(float64)
		return ok && price > 0
	}
	var orphans []string
	for key := range fields {
		m := cacheTierPricePattern.FindStringSubmatch(key)
		if m == nil || !positive(key) {
			continue
		}
		stem, hourly, tier := m[1], m[2], m[3]
		if positive(stem+hourly+tier) || positive(stem+hourly) || positive(stem+tier) || positive(stem) {
			continue
		}
		orphans = append(orphans, key)
	}
	sort.Strings(orphans)
	return orphans
}

// warnOrphanCacheTierFields 对带 cache 侧 above 档却没有基础价的条目打 WARN：
// 该缓存分项按 0 计费，目录或 pricing.override_file 补上基础价即可消除。
func warnOrphanCacheTierFields(entries []string) {
	if len(entries) == 0 {
		return
	}
	sort.Strings(entries)
	total := len(entries)
	if total > 20 {
		entries = append(entries[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: %d model(s) carry cache above-tier prices without a base cache price; that cache item bills at $0 until the catalog/override supplies the base: %s", total, strings.Join(entries, ", "))
}

// applyPricingOverrides 把 override 文件的条目逐字段修补进原始目录数据。目录与回退
// 文件的解析都经过 parsePricingData，因此 override 是最高优先级的数据源。这里只修补
// 已存在的条目：目录/回退里都没有的模型由 mergeOverrideOnlyModels 在两层数据合并后
// 统一并入——若在此处抢先建条目，纯 override 条目会挡住回退文件中同名完整条目的合并。
func (s *PricingService) applyPricingOverrides(rawData map[string]json.RawMessage) map[string]json.RawMessage {
	overrides := s.loadPricingOverrideEntries()
	if len(overrides) == 0 {
		return rawData
	}
	for name, patch := range overrides {
		base, ok := rawData[name]
		if !ok {
			continue
		}
		merged, valid := mergePricingOverrideEntry(base, patch)
		if !valid {
			logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override entry %q skipped: not a JSON object", name)
			continue
		}
		rawData[name] = merged
	}
	return rawData
}

// loadPricingOverrideEntries 读取 override 文件的原始条目。未配置返回 nil；
// 读取或解析失败打日志并跳过，不影响目录加载。
func (s *PricingService) loadPricingOverrideEntries() map[string]json.RawMessage {
	if s == nil || s.cfg == nil {
		return nil
	}
	path := strings.TrimSpace(s.cfg.Pricing.OverrideFile)
	if path == "" {
		return nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override merge skipped: %v", err)
		return nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override merge skipped: %v", err)
		return nil
	}
	return entries
}

// mergePricingOverrideEntry 在 JSON 字段层浅合并：patch 字段覆盖 base 同名字段，
// 值为 null 的 patch 字段从结果中删除，base 为空时结果即 patch 本身。
// patch 不是 JSON 对象时返回 ok=false。
func mergePricingOverrideEntry(base, patch json.RawMessage) (json.RawMessage, bool) {
	var patchFields map[string]any
	if err := json.Unmarshal(patch, &patchFields); err != nil || patchFields == nil {
		return nil, false
	}
	merged := make(map[string]any, len(patchFields))
	if len(base) > 0 {
		// base 非对象时忽略，仅以 patch 为准。
		if err := json.Unmarshal(base, &merged); err != nil {
			merged = make(map[string]any, len(patchFields))
		}
	}
	for k, v := range patchFields {
		if v == nil {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, false
	}
	return out, true
}

// mergeOverrideOnlyModels 把 override 中目录/回退两层都不存在的模型作为独立条目并入
// （条目须自带价格字段才能通过有效性过滤），并对最终仍未生效的条目打 WARN：
// 模型名拼错、或纯补丁条目落在不存在的模型上时会被静默丢弃，让"已改价/已关阶梯"
// 的运营预期与实际计费脱节，这里是唯一的哨兵。
func (s *PricingService) mergeOverrideOnlyModels(data map[string]*LiteLLMModelPricing) map[string]*LiteLLMModelPricing {
	overrides := s.loadPricingOverrideEntries()
	if len(overrides) == 0 {
		return data
	}
	if data == nil {
		data = make(map[string]*LiteLLMModelPricing)
	}
	leftover := make(map[string]json.RawMessage)
	for name, patch := range overrides {
		if _, ok := data[name]; !ok {
			leftover[name] = patch
		}
	}
	if len(leftover) == 0 {
		return data
	}
	// 复用主解析路径（含 above_XXXk 折算与有效性过滤）；applyPricingOverrides
	// 对已存在条目做的自我修补是幂等的，不会二次改值。
	if body, err := json.Marshal(leftover); err == nil {
		if parsed, err := s.parsePricingData(body); err == nil {
			maps.Copy(data, parsed)
		}
	}
	var missing []string
	for name := range leftover {
		if _, ok := data[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return data
	}
	sort.Strings(missing)
	logger.LegacyPrintf("service.pricing", "[Pricing] Warning: override had no effect for %d model(s): %s (unknown model name, or patch-only entry without price fields)", len(missing), strings.Join(missing, ", "))
	return data
}

// buildPricingData 解析目录正文并依次叠加 fallback、override 两层，返回合并结果与
// 叠加层文件指纹。指纹在合并读取之前采样：并发改文件只会让存下的指纹落后于实际
// 合并的数据、不会领先，下一轮定时比对因此会再次重建。
func (s *PricingService) buildPricingData(body []byte) (map[string]*LiteLLMModelPricing, string, error) {
	fingerprint := s.customPricingFilesFingerprint()
	data, err := s.parsePricingData(body)
	if err != nil {
		return nil, "", err
	}
	data = s.mergeFallbackPricingData(data)
	data = s.mergeOverrideOnlyModels(data)
	return data, fingerprint, nil
}

// loadPricingData 从本地文件加载价格数据
func (s *PricingService) loadPricingData(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file failed: %w", err)
	}

	pricingData, customFilesHash, err := s.buildPricingData(data)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}

	// 计算哈希
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	s.mu.Lock()
	warnDroppedLongContextLadders(s.pricingData, pricingData)
	s.pricingData = pricingData
	s.localHash = hashStr
	s.customFilesHash = customFilesHash

	info, _ := os.Stat(filePath)
	if info != nil {
		s.lastUpdated = info.ModTime()
	} else {
		s.lastUpdated = time.Now()
	}
	s.mu.Unlock()

	logger.LegacyPrintf("service.pricing", "[Pricing] Loaded %d models from %s", len(pricingData), filePath)
	return nil
}

func (s *PricingService) mergeFallbackPricingData(data map[string]*LiteLLMModelPricing) map[string]*LiteLLMModelPricing {
	if data == nil {
		data = make(map[string]*LiteLLMModelPricing)
	}
	if s == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Pricing.FallbackFile) == "" {
		return data
	}
	fallbackBody, err := os.ReadFile(s.cfg.Pricing.FallbackFile)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Fallback merge skipped: %v", err)
		return data
	}
	fallbackData, err := s.parsePricingData(fallbackBody)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Fallback merge parse skipped: %v", err)
		return data
	}
	merged := 0
	for modelName, pricing := range fallbackData {
		if _, ok := data[modelName]; ok {
			continue
		}
		data[modelName] = pricing
		merged++
	}
	if merged > 0 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Merged %d fallback-only models", merged)
	}
	return data
}

// warnDroppedLongContextLadders 对比新旧目录数据：原本带长上下文阶梯的条目在新数据里
// 丢失阈值时打 WARN。阶梯已完全数据驱动（无代码兜底），数据源一次误提交就会把阶梯
// 静默变成基础价少收（07-14~08-21 漏收事故的形态），这里是唯一的哨兵。
// 调用方需持有 s.mu 写锁。
func warnDroppedLongContextLadders(old, next map[string]*LiteLLMModelPricing) {
	if len(old) == 0 {
		return
	}
	var dropped []string
	for name, prev := range old {
		if prev == nil || prev.LongContextInputTokenThreshold <= 0 {
			continue
		}
		if cur, ok := next[name]; ok && (cur == nil || cur.LongContextInputTokenThreshold <= 0) {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) == 0 {
		return
	}
	sort.Strings(dropped)
	total := len(dropped)
	if total > 20 {
		dropped = append(dropped[:20], "...")
	}
	logger.LegacyPrintf("service.pricing", "[Pricing] Long-context ladder dropped for %d model(s) after reload: %s (verify catalog/override data if unintended)", total, strings.Join(dropped, ", "))
}

// useFallbackPricing 使用回退价格文件
func (s *PricingService) useFallbackPricing() error {
	fallbackFile := s.cfg.Pricing.FallbackFile

	if _, err := os.Stat(fallbackFile); os.IsNotExist(err) {
		return fmt.Errorf("fallback file not found: %s", fallbackFile)
	}

	logger.LegacyPrintf("service.pricing", "[Pricing] Using fallback file: %s", fallbackFile)

	// 复制到数据目录
	data, err := os.ReadFile(fallbackFile)
	if err != nil {
		return fmt.Errorf("read fallback failed: %w", err)
	}

	pricingFile := s.getPricingFilePath()
	if err := os.WriteFile(pricingFile, data, 0644); err != nil { //nolint:gosec // G703: 路径为配置的数据目录 + 硬编码文件名，非请求输入
		logger.LegacyPrintf("service.pricing", "[Pricing] Failed to copy fallback: %v", err)
	}

	return s.loadPricingData(fallbackFile)
}

// fetchRemoteHash 从远程获取哈希值
func (s *PricingService) fetchRemoteHash() (string, error) {
	hashURL, err := s.validatePricingURL(s.cfg.Pricing.HashURL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hash, err := s.remoteClient.FetchHashText(ctx, hashURL)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

func (s *PricingService) validatePricingURL(raw string) (string, error) {
	return s.validateSupplementalPricingURL(raw, nil)
}

func (s *PricingService) validateSupplementalPricingURL(raw string, extraHosts []string) (string, error) {
	if s.cfg != nil && !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid pricing url: %w", err)
		}
		return normalized, nil
	}
	allowedHosts := append([]string{}, s.cfg.Security.URLAllowlist.PricingHosts...)
	allowedHosts = append(allowedHosts, extraHosts...)
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     allowedHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid pricing url: %w", err)
	}
	return normalized, nil
}

// GetModelPricingExact 仅在通用价格目录中按标准化候选键精确读取，
// 不执行版本、系列或厂商回落。
func (s *PricingService) GetModelPricingExact(modelName string) *LiteLLMModelPricing {
	if s == nil || strings.TrimSpace(modelName) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getModelPricingExactLocked(s.pricingData, modelName)
}

// GetOpenCodeGoModelPricingExact 从平台隔离快照精确读取 OpenCode Go 价格。
func openCodeGoZeroRateEvidenceFresh(confirmedAt, now time.Time) bool {
	if confirmedAt.IsZero() {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(confirmedAt)
	return age >= 0 && age <= openCodeGoZeroRateEvidenceTTL
}

func (s *PricingService) GetOpenCodeGoModelPricingExact(modelName string) *LiteLLMModelPricing {
	if s == nil || strings.TrimSpace(modelName) == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	pricing := s.getModelPricingExactLocked(s.openCodeGoPricing, modelName)
	// 旧缓存会把 DeepSeek 的 Peak 行覆盖为全天价格。缺少完整两档证据时拒绝该条目，
	// 让调用方回退到经过核实的静态 Peak/Off-Peak 目录。
	if pricing != nil && pricing.OpenCodeGoPricingAuthority == openCodeGoPricingAuthorityOfficial &&
		openCodeGoRequiresTimeBandPricing(modelName) && !pricing.OpenCodeGoPeakPricingKnown {
		return nil
	}
	if pricing != nil && pricing.OpenCodeGoExplicitZeroRate &&
		!openCodeGoZeroRateEvidenceFresh(s.openCodeGoPricingConfirmedAt, time.Now()) {
		return nil
	}
	return pricing
}

func (s *PricingService) getModelPricingExactLocked(data map[string]*LiteLLMModelPricing, modelName string) *LiteLLMModelPricing {
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	for _, candidate := range s.buildModelLookupCandidates(modelLower) {
		if pricing, ok := data[candidate]; ok {
			return pricing
		}
	}
	return nil
}

// GetModelPricing 获取模型价格（带模糊匹配）
func (s *PricingService) GetModelPricing(modelName string) *LiteLLMModelPricing {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if modelName == "" {
		return nil
	}

	// 标准化模型名称（同时兼容 "models/xxx"、VertexAI 资源名等前缀）
	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	lookupCandidates := s.buildModelLookupCandidates(modelLower)

	// 1~3. 确定性识别（精确名 / 已知拼写变体 / 去掉日期版本后缀）
	if pricing := s.lookupIdentifiedModelPricingLocked(lookupCandidates); pricing != nil {
		return pricing
	}

	// 4. 基于模型系列匹配（Claude）
	if pricing := s.matchByModelFamily(lookupCandidates[0]); pricing != nil {
		return pricing
	}

	// 5. OpenAI 模型回退策略
	if strings.HasPrefix(lookupCandidates[0], "gpt-") {
		return s.matchOpenAIModel(lookupCandidates[0])
	}

	return nil
}

// lookupIdentifiedModelPricingLocked 只做"确定性识别"的三步查找：精确键、已知拼写
// 变体、去掉日期/版本后缀后的同名条目。它刻意不包含 matchByModelFamily /
// matchOpenAIModel 这类按子串猜系列的兜底——那些兜底会给任意名字都返回一个价格。
// 调用方必须持有 s.mu 读锁。
func (s *PricingService) lookupIdentifiedModelPricingLocked(lookupCandidates []string) *LiteLLMModelPricing {
	if len(lookupCandidates) == 0 {
		return nil
	}

	// 1. 精确匹配
	for _, candidate := range lookupCandidates {
		if candidate == "" {
			continue
		}
		if pricing, ok := s.pricingData[candidate]; ok {
			return pricing
		}
	}

	// 2. 处理常见的模型名称变体
	// claude-opus-4-5-20251101 -> claude-opus-4.5-20251101
	for _, candidate := range lookupCandidates {
		normalized := strings.ReplaceAll(candidate, "-4-5-", "-4.5-")
		if pricing, ok := s.pricingData[normalized]; ok {
			return pricing
		}
	}

	// 3. 尝试模糊匹配（去掉版本号后缀）
	// claude-opus-4-5-20251101 -> claude-opus-4.5
	baseName := s.extractBaseName(lookupCandidates[0])
	for key, pricing := range s.pricingData {
		keyBase := s.extractBaseName(strings.ToLower(key))
		if keyBase == baseName {
			return pricing
		}
	}

	return nil
}

// GetIdentifiedModelPricing 在价格表中确定性地识别模型，识别不到时返回 nil。
// 与 GetModelPricing 的区别：不会退化成按 "opus"/"haiku" 之类子串猜出的系列兜底价。
// 用于必须区分"这是价格表里已知的模型"和"这只是名字里带某个关键词"的场景。
func (s *PricingService) GetIdentifiedModelPricing(modelName string) *LiteLLMModelPricing {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return nil
	}
	return s.lookupIdentifiedModelPricingLocked(s.buildModelLookupCandidates(modelLower))
}

func (s *PricingService) buildModelLookupCandidates(modelLower string) []string {
	rawCandidates := []string{
		modelLower,
		strings.TrimPrefix(modelLower, "models/"),
		lastSegment(modelLower),
		lastSegment(strings.TrimPrefix(modelLower, "models/")),
	}
	normalized := normalizeModelNameForPricing(modelLower)

	// 平台计费 alias 的精确定价优先；规范化值只作为 alias 的 fallback。
	candidates := rawCandidates
	rawLastSegment := lastSegment(strings.TrimPrefix(modelLower, "models/"))
	if canonicalBillingModelForPricing(rawLastSegment) != rawLastSegment {
		candidates = append(candidates, normalized)
	} else {
		candidates = append([]string{normalized}, candidates...)
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return []string{modelLower}
	}
	return out
}

func normalizeModelNameForPricing(model string) string {
	// Common Gemini/VertexAI forms:
	// - models/gemini-2.0-flash-exp
	// - publishers/google/models/gemini-2.5-pro
	// - projects/.../locations/.../publishers/google/models/gemini-2.5-pro
	model = strings.TrimSpace(model)
	model = strings.TrimLeft(model, "/")
	model = strings.TrimPrefix(model, "models/")
	model = strings.TrimPrefix(model, "publishers/google/models/")

	if idx := strings.LastIndex(model, "/publishers/google/models/"); idx != -1 {
		model = model[idx+len("/publishers/google/models/"):]
	}
	if idx := strings.LastIndex(model, "/models/"); idx != -1 {
		model = model[idx+len("/models/"):]
	}

	model = strings.TrimLeft(model, "/")
	if canonical := canonicalizeOpenAIModelAliasSpelling(model); canonical != "" {
		if canonical == "gpt-6" {
			return "gpt-6-astra"
		}
		if canonical == "gpt-5.6" {
			return "gpt-5.6-sol"
		}
		if suffix, ok := strings.CutPrefix(canonical, "gpt-5.6-"); ok && (suffix == "max" || isKnownCodexModelSuffix(suffix)) {
			return "gpt-5.6-sol"
		}
		return canonical
	}
	return canonicalBillingModelForPricing(model)
}

func lastSegment(model string) string {
	if idx := strings.LastIndex(model, "/"); idx != -1 {
		return model[idx+1:]
	}
	return model
}

// extractBaseName 提取基础模型名称（去掉日期版本号）
func (s *PricingService) extractBaseName(model string) string {
	// 移除日期后缀 (如 -20251101, -20241022)
	parts := strings.Split(model, "-")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		// 跳过看起来像日期的部分（8位数字）
		if len(part) == 8 && isNumeric(part) {
			continue
		}
		// 跳过版本号（如 v1:0）
		if strings.Contains(part, ":") {
			continue
		}
		result = append(result, part)
	}
	return strings.Join(result, "-")
}

// matchByModelFamily 基于模型系列匹配
func (s *PricingService) matchByModelFamily(model string) *LiteLLMModelPricing {
	// modelFamily 定义一个模型系列的匹配和定价查找规则。
	type modelFamily struct {
		name    string   // 系列名称
		match   []string // 用于将模型归类到此系列的模式（strings.Contains 匹配）
		pricing []string // 用于在定价数据中查找价格的模式（nil 则复用 match；可包含低版本 fallback）
	}

	// 按特异性降序排列：高版本号在前，避免 "claude-opus-4"（opus-4 系列）
	// 因子串关系误匹配 "claude-opus-4-7"（opus-4.7 系列）。
	// 注意：原 map 实现存在 Go map 迭代随机性导致的同类 bug，此处改为有序切片修复。
	families := []modelFamily{
		// Opus 5 与 Opus 4.8 同价（$5/$25 per MTok）。定价数据缺失 claude-opus-5 时
		// 必须回退到 4.8，否则会掉进 "opus-4" 系列按 $15/$75 计费（3 倍超收）。
		{name: "opus-5", match: []string{"claude-opus-5"}, pricing: []string{"claude-opus-5", "claude-opus-4-8"}},
		{name: "opus-4.8", match: []string{"claude-opus-4-8", "claude-opus-4.8"}, pricing: []string{"claude-opus-4-8", "claude-opus-4.8", "claude-opus-4-7"}},
		{name: "opus-4.7", match: []string{"claude-opus-4-7", "claude-opus-4.7"}, pricing: []string{"claude-opus-4-7", "claude-opus-4.7", "claude-opus-4-6"}},
		{name: "opus-4.6", match: []string{"claude-opus-4-6", "claude-opus-4.6"}},
		{name: "opus-4.5", match: []string{"claude-opus-4-5", "claude-opus-4.5"}},
		{name: "opus-4", match: []string{"claude-opus-4", "claude-3-opus"}},
		{name: "sonnet-4.5", match: []string{"claude-sonnet-4-5", "claude-sonnet-4.5"}},
		{name: "sonnet-4", match: []string{"claude-sonnet-4", "claude-3-5-sonnet"}},
		{name: "sonnet-3.5", match: []string{"claude-3-5-sonnet", "claude-3.5-sonnet"}},
		{name: "sonnet-3", match: []string{"claude-3-sonnet"}},
		{name: "haiku-3.5", match: []string{"claude-3-5-haiku", "claude-3.5-haiku"}},
		{name: "haiku-3", match: []string{"claude-3-haiku"}},
	}

	// Phase 1: 按有序切片归类（最具体的系列优先匹配）
	var matched *modelFamily
	for i := range families {
		for _, pattern := range families[i].match {
			if strings.Contains(model, pattern) || strings.Contains(model, strings.ReplaceAll(pattern, "-", "")) {
				matched = &families[i]
				break
			}
		}
		if matched != nil {
			break
		}
	}

	// Phase 2: 二次兜底——当模型 ID 不含已知模式串时，按关键字粗分
	if matched == nil {
		var fallbackName string
		switch {
		case strings.Contains(model, "opus"):
			switch {
			// "opus-5" 必须先判：不能用裸 "5" 匹配，否则 claude-opus-4-5 会被误判。
			case strings.Contains(model, "opus-5") || strings.Contains(model, "opus5"):
				fallbackName = "opus-5"
			case strings.Contains(model, "4.8") || strings.Contains(model, "4-8"):
				fallbackName = "opus-4.8"
			case strings.Contains(model, "4.7") || strings.Contains(model, "4-7"):
				fallbackName = "opus-4.7"
			case strings.Contains(model, "4.6") || strings.Contains(model, "4-6"):
				fallbackName = "opus-4.6"
			case strings.Contains(model, "4.5") || strings.Contains(model, "4-5"):
				fallbackName = "opus-4.5"
			default:
				fallbackName = "opus-4"
			}
		case strings.Contains(model, "sonnet"):
			switch {
			case strings.Contains(model, "4.5") || strings.Contains(model, "4-5"):
				fallbackName = "sonnet-4.5"
			case strings.Contains(model, "3-5") || strings.Contains(model, "3.5"):
				fallbackName = "sonnet-3.5"
			default:
				fallbackName = "sonnet-4"
			}
		case strings.Contains(model, "haiku"):
			switch {
			case strings.Contains(model, "3-5") || strings.Contains(model, "3.5"):
				fallbackName = "haiku-3.5"
			default:
				fallbackName = "haiku-3"
			}
		}
		if fallbackName != "" {
			for i := range families {
				if families[i].name == fallbackName {
					matched = &families[i]
					break
				}
			}
		}
	}

	if matched == nil {
		return nil
	}

	// Phase 3: 在定价数据中查找该系列的价格
	lookups := matched.pricing
	if lookups == nil {
		lookups = matched.match
	}
	for _, pattern := range lookups {
		for key, pricing := range s.pricingData {
			keyLower := strings.ToLower(key)
			if strings.Contains(keyLower, pattern) {
				logger.LegacyPrintf("service.pricing", "[Pricing] Fuzzy matched %s -> %s", model, key)
				return pricing
			}
		}
	}

	return nil
}

// matchOpenAIModel OpenAI 模型回退匹配策略
// 回退顺序：
// 1. gpt-5.3-codex-spark* -> gpt-5.1-codex（按业务要求固定计费）
// 2. gpt-5.2-codex -> gpt-5.2（去掉后缀如 -codex, -mini, -max 等）
// 3. gpt-5.2-20251222 -> gpt-5.2（去掉日期版本号）
// 4. gpt-5.3-codex -> gpt-5.2-codex
// 5. gpt-5.4* -> 业务静态兜底价
// 6. 最终回退到 DefaultTestModel (gpt-5.1-codex)
func (s *PricingService) matchOpenAIModel(model string) *LiteLLMModelPricing {
	if strings.HasPrefix(model, "gpt-5.3-codex-spark") {
		if pricing, ok := s.pricingData["gpt-5.1-codex"]; ok {
			logger.LegacyPrintf("service.pricing", "[Pricing][SparkBilling] %s -> %s billing", model, "gpt-5.1-codex")
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.1-codex"))
			return pricing
		}
	}

	// 尝试的回退变体
	variants := s.generateOpenAIModelVariants(model, openAIModelDatePattern)

	for _, variant := range variants {
		if pricing, ok := s.pricingData[variant]; ok {
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, variant))
			return pricing
		}
	}

	if strings.HasPrefix(model, "gpt-5.3-codex") {
		if pricing, ok := s.pricingData["gpt-5.2-codex"]; ok {
			logger.With(zap.String("component", "service.pricing")).
				Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.2-codex"))
			return pricing
		}
	}

	if isOpenAIGPT6AstraModel(model) {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-6-astra(static)"))
		return openAIGPT6AstraFallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.6-sol") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-sol(static)"))
		return openAIGPT56SolFallbackPricing
	}
	if strings.HasPrefix(model, "gpt-5.6-terra") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-terra(static)"))
		return openAIGPT56TerraFallbackPricing
	}
	if strings.HasPrefix(model, "gpt-5.6-luna") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.6-luna(static)"))
		return openAIGPT56LunaFallbackPricing
	}

	// GPT-5.5 回退到 GPT-5.4 定价
	if strings.HasPrefix(model, "gpt-5.5") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4(static)"))
		return openAIGPT54FallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4-mini") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4-mini(static)"))
		return openAIGPT54MiniFallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4-nano") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4-nano(static)"))
		return openAIGPT54NanoFallbackPricing
	}

	if strings.HasPrefix(model, "gpt-5.4") {
		logger.With(zap.String("component", "service.pricing")).
			Info(fmt.Sprintf("[Pricing] OpenAI fallback matched %s -> %s", model, "gpt-5.4(static)"))
		return openAIGPT54FallbackPricing
	}

	if isOpenAIImageGenerationModel(model) {
		for _, candidate := range []string{"gpt-image-2", "gpt-image-1.5", "gpt-image-1"} {
			if pricing, ok := s.pricingData[candidate]; ok {
				logger.LegacyPrintf("service.pricing", "[Pricing] OpenAI image fallback matched %s -> %s", model, candidate)
				return pricing
			}
		}
		return nil
	}

	// 最终回退到 DefaultTestModel
	defaultModel := strings.ToLower(openai.DefaultTestModel)
	if pricing, ok := s.pricingData[defaultModel]; ok {
		logger.LegacyPrintf("service.pricing", "[Pricing] OpenAI fallback to default model %s -> %s", model, defaultModel)
		return pricing
	}

	return nil
}

// generateOpenAIModelVariants 生成 OpenAI 模型的回退变体列表
func (s *PricingService) generateOpenAIModelVariants(model string, datePattern *regexp.Regexp) []string {
	seen := make(map[string]bool)
	var variants []string

	addVariant := func(v string) {
		if v != model && !seen[v] {
			seen[v] = true
			variants = append(variants, v)
		}
	}

	// 1. 去掉日期版本号: gpt-5.2-20251222 -> gpt-5.2
	withoutDate := datePattern.ReplaceAllString(model, "")
	if withoutDate != model {
		addVariant(withoutDate)
	}

	// 2. 提取基础版本号: gpt-5.2-codex -> gpt-5.2
	// 只匹配纯数字版本号格式 gpt-X 或 gpt-X.Y，不匹配 gpt-4o 这种带字母后缀的
	if matches := openAIModelBasePattern.FindStringSubmatch(model); len(matches) > 1 {
		addVariant(matches[1])
	}

	// 3. 同时去掉日期后再提取基础版本号
	if withoutDate != model {
		if matches := openAIModelBasePattern.FindStringSubmatch(withoutDate); len(matches) > 1 {
			addVariant(matches[1])
		}
	}

	return variants
}

// GetStatus 获取服务状态
func (s *PricingService) GetStatus() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]any{
		"model_count":  len(s.pricingData),
		"last_updated": s.lastUpdated,
		"local_hash":   s.localHash[:min(8, len(s.localHash))],
	}
}

// ForceUpdate 强制更新
func (s *PricingService) ForceUpdate() error {
	return s.downloadPricingDataAndRefreshOpenCodeGo()
}

// getPricingFilePath 获取价格文件路径
func (s *PricingService) getPricingFilePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "model_pricing.json")
}

// getHashFilePath 获取哈希文件路径
func (s *PricingService) getHashFilePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "model_pricing.sha256")
}

// ListModelNamesByProvider returns all model names in the catalog whose
// LiteLLMProvider matches the given provider string (case-insensitive).
// The returned slice is sorted alphabetically.
func (s *PricingService) ListModelNamesByProvider(provider string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provider = strings.ToLower(strings.TrimSpace(provider))
	names := make([]string, 0)
	for name, p := range s.pricingData {
		if strings.ToLower(p.LiteLLMProvider) == provider {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// isNumeric 检查字符串是否为纯数字
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
