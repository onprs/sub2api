package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
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
	openAIModelDatePattern      = regexp.MustCompile(`-\d{8}$`)
	openAIModelBasePattern      = regexp.MustCompile(`^(gpt-\d+(?:\.\d+)?)(?:-|$)`)
	htmlTableRowPattern         = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	htmlTableCellPattern        = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	htmlTagPattern              = regexp.MustCompile(`(?is)<[^>]+>`)
	openCodeGoPricePattern      = regexp.MustCompile(`^\$([0-9]+(?:\.[0-9]+)?)$`)
	openCodeGoThresholdPattern  = regexp.MustCompile(`(?i)(?:<=|≤|>|>=|≥)\s*([0-9]+)\s*([km])\b`)
	openCodeGoModelIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	openCodeGoUsageOfferPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*x\s+usage(?:\s+limits?)?$`)
	openAIGPT54FallbackPricing  = &LiteLLMModelPricing{
		InputCostPerToken:               2.5e-06, // $2.5 per MTok
		OutputCostPerToken:              1.5e-05, // $15 per MTok
		CacheReadInputTokenCost:         2.5e-07, // $0.25 per MTok
		LongContextInputTokenThreshold:  272000,
		LongContextInputCostMultiplier:  2.0,
		LongContextOutputCostMultiplier: 1.5,
		LiteLLMProvider:                 "openai",
		Mode:                            "chat",
		SupportsPromptCaching:           true,
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
		LongContextInputTokenThreshold:      openAIGPT54LongContextInputThreshold,
		LongContextInputCostMultiplier:      openAIGPT54LongContextInputMultiplier,
		LongContextOutputCostMultiplier:     openAIGPT54LongContextOutputMultiplier,
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
		LongContextInputTokenThreshold:      openAIGPT54LongContextInputThreshold,
		LongContextInputCostMultiplier:      openAIGPT54LongContextInputMultiplier,
		LongContextOutputCostMultiplier:     openAIGPT54LongContextOutputMultiplier,
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
		LongContextInputTokenThreshold:      openAIGPT54LongContextInputThreshold,
		LongContextInputCostMultiplier:      openAIGPT54LongContextInputMultiplier,
		LongContextOutputCostMultiplier:     openAIGPT54LongContextOutputMultiplier,
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
	// 定期检查哈希更新
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
				if err := s.syncWithRemote(); err != nil {
					logger.LegacyPrintf("service.pricing", "[Pricing] Sync failed: %v", err)
				}
			case <-usageOfferTicker.C:
				s.refreshOpenCodeGoUsageOffersBestEffortWithTimeout()
			case <-s.stopCh:
				return
			}
		}
	}()

	logger.LegacyPrintf("service.pricing", "[Pricing] Update scheduler started (pricing every %v, usage offers every %v)", hashInterval, openCodeGoUsageOfferRefreshInterval)
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

	// 解析JSON数据（使用灵活的解析方式）
	data, err := s.parsePricingData(body)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}
	data = s.mergeFallbackPricingData(data)

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
	s.pricingData = data
	s.lastUpdated = time.Now()
	s.localHash = syncHash
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

	result := make(map[string]*LiteLLMModelPricing)
	skipped := 0

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
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil && entry.OutputCostPerImage == nil && entry.OutputCostPerImageToken == nil {
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

		result[modelName] = pricing
	}

	if skipped > 0 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Skipped %d invalid entries", skipped)
	}

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

// loadPricingData 从本地文件加载价格数据
func (s *PricingService) loadPricingData(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file failed: %w", err)
	}

	// 使用灵活的解析方式
	pricingData, err := s.parsePricingData(data)
	if err != nil {
		return fmt.Errorf("parse pricing data: %w", err)
	}
	pricingData = s.mergeFallbackPricingData(pricingData)

	// 计算哈希
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	s.mu.Lock()
	s.pricingData = pricingData
	s.localHash = hashStr

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
	if err := os.WriteFile(pricingFile, data, 0644); err != nil {
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

func (s *PricingService) buildModelLookupCandidates(modelLower string) []string {
	rawCandidates := []string{
		modelLower,
		strings.TrimPrefix(modelLower, "models/"),
		lastSegment(modelLower),
		lastSegment(strings.TrimPrefix(modelLower, "models/")),
	}
	normalized := normalizeModelNameForPricing(modelLower)

	// 平台计费 alias 的精确定价优先；其他规范化（例如 OpenAI 拼写）保持原有优先级。
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
