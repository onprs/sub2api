package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

const (
	// antigravityVersionSyncInterval 自动同步官方 agy CLI 版本号的周期。
	antigravityVersionSyncInterval = time.Hour
	// antigravityVersionSyncTimeout 单次同步请求的整体超时。
	antigravityVersionSyncTimeout = 15 * time.Second
	// antigravityManifestMaxBodyBytes 官方清单 JSON 读取上限（正常仅数百字节）。
	antigravityManifestMaxBodyBytes = 64 * 1024
)

// AntigravityManifestFetcher 拉取官方 agy CLI 版本清单中的最新版本号（便于测试注入）。
type AntigravityManifestFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

type httpAntigravityManifestFetcher struct {
	httpClient  *http.Client
	manifestURL func() string
}

type antigravityCLIManifest struct {
	Version string `json:"version"`
}

func newHTTPAntigravityManifestFetcher() *httpAntigravityManifestFetcher {
	return &httpAntigravityManifestFetcher{
		httpClient: &http.Client{
			Timeout: antigravityVersionSyncTimeout,
		},
		manifestURL: antigravity.ManifestURL,
	}
}

func (f *httpAntigravityManifestFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	urlStr := strings.TrimSpace(f.manifestURL())
	if urlStr == "" {
		return "", errors.New("antigravity manifest url is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create manifest request: %w", err)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, antigravityManifestMaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}
	var manifest antigravityCLIManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("decode manifest: %w", err)
	}
	version := antigravity.NormalizeUserAgentVersion(manifest.Version)
	if version == "" {
		return "", fmt.Errorf("invalid manifest version %q", manifest.Version)
	}
	return version, nil
}

// AntigravityVersionSyncService 周期性从官方 agy CLI 自动更新清单拉取最新稳定版本号，
// 写入 SettingKeyAntigravityUserAgentVersionSynced（本服务独占写入），避免为了跟进官方 CLI
// 版本而频繁发版。管理员手填的 SettingKeyAntigravityUserAgentVersion 始终优先。
type AntigravityVersionSyncService struct {
	settingRepo    SettingRepository
	settingService *SettingService
	fetcher        AntigravityManifestFetcher
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
}

func NewAntigravityVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	fetcher AntigravityManifestFetcher,
	interval time.Duration,
) *AntigravityVersionSyncService {
	if fetcher == nil {
		fetcher = newHTTPAntigravityManifestFetcher()
	}
	return &AntigravityVersionSyncService{
		settingRepo:    settingRepo,
		settingService: settingService,
		fetcher:        fetcher,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *AntigravityVersionSyncService) Start() {
	if s == nil || s.settingRepo == nil || s.fetcher == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runInitial()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *AntigravityVersionSyncService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AntigravityVersionSyncService) runInitial() {
	if s.syncedWithinInterval() {
		return
	}
	s.runOnce()
}

func (s *AntigravityVersionSyncService) syncedWithinInterval() bool {
	if s.interval <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), antigravityVersionSyncTimeout)
	defer cancel()

	setting, err := s.settingRepo.Get(ctx, SettingKeyAntigravityUserAgentVersionSynced)
	if err != nil || setting == nil || setting.UpdatedAt.IsZero() {
		return false
	}
	if antigravity.NormalizeUserAgentVersion(setting.Value) == "" {
		return false
	}
	return time.Since(setting.UpdatedAt) < s.interval
}

func (s *AntigravityVersionSyncService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), antigravityVersionSyncTimeout)
	defer cancel()

	if !s.autoSyncEnabled(ctx) {
		return
	}

	latest, err := s.fetcher.FetchLatestVersion(ctx)
	if err != nil {
		slog.Warn("antigravity_version_sync_fetch_failed", "error", err)
		return
	}
	latest = antigravity.NormalizeUserAgentVersion(latest)
	if latest == "" {
		return
	}
	// 不低于内置默认版本，避免异常清单导致版本号回退。
	if CompareVersions(latest, antigravity.DefaultUserAgentVersion) < 0 {
		return
	}

	current, err := s.settingRepo.GetValue(ctx, SettingKeyAntigravityUserAgentVersionSynced)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		slog.Warn("antigravity_version_sync_current_read_failed", "error", err)
		return
	}
	current = antigravity.NormalizeUserAgentVersion(current)
	if current != "" && CompareVersions(latest, current) <= 0 {
		return
	}
	if err := s.settingRepo.Set(ctx, SettingKeyAntigravityUserAgentVersionSynced, latest); err != nil {
		slog.Warn("antigravity_version_sync_persist_failed", "version", latest, "error", err)
		return
	}
	s.settingService.InvalidateAntigravityUserAgentVersionCache()
	slog.Info("antigravity_version_synced", "previous", current, "version", latest)
}

func (s *AntigravityVersionSyncService) autoSyncEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAntigravityVersionAutoSyncEnabled)
	if err != nil {
		return true
	}
	if strings.TrimSpace(value) == "" {
		return true
	}
	return strings.TrimSpace(value) == "true"
}
