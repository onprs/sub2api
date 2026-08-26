package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	commandCodeProviderModelsURL     = "https://api.commandcode.ai/provider/v1/models"
	commandCodeGoatDocsURL           = "https://commandcode.ai/docs/plans/goat"
	commandCodeCatalogTTL            = time.Hour
	commandCodeCatalogHTTPTimeout    = 6 * time.Second
	commandCodeCatalogBodyLimit      = 2 << 20
	commandCodeCatalogMinModelCount  = 10
	commandCodeCatalogMinRetainedPct = 80
)

var (
	commandCodeTimeWindowPattern         = regexp.MustCompile(`(?i)([0-9]{1,2})(?::00)?\s*[–—-]\s*([0-9]{1,2})(?::00)?`)
	commandCodeListRatesReferencePattern = regexp.MustCompile(`:tiers:([0-9]+):listRates$`)
)

type commandCodeCatalogRates struct {
	Input         float64
	Output        float64
	CacheRead     float64
	CacheWrite    float64
	HasCacheWrite bool
}

type commandCodeCatalogTier struct {
	MinTokens int
	MaxTokens *int
	Rates     commandCodeCatalogRates
	ListRates *commandCodeCatalogRates
}

type commandCodeCatalogDeal struct {
	Code            string
	Label           string
	DiscountPercent float64
	Free            bool
	Term            string
	ExpiresAt       time.Time
}

type commandCodeCatalogTimeWindow struct {
	StartHourUTC int
	EndHourUTC   int
}

type commandCodeCatalogScheduledChange struct {
	Effective time.Time
	Rates     commandCodeCatalogRates
}

type commandCodeCatalogTimeOfDay struct {
	Effective time.Time
	Peak      commandCodeCatalogRates
	OffPeak   commandCodeCatalogRates
	Windows   []commandCodeCatalogTimeWindow
}

type commandCodeCatalogEntry struct {
	ID                string
	Name              string
	ContextWindow     int
	MonthlyCreditsUSD float64
	Tiers             []commandCodeCatalogTier
	Deal              *commandCodeCatalogDeal
	ScheduledChange   *commandCodeCatalogScheduledChange
	TimeOfDay         *commandCodeCatalogTimeOfDay
}

func (e commandCodeCatalogEntry) clone() commandCodeCatalogEntry {
	cloned := e
	if e.Tiers != nil {
		cloned.Tiers = make([]commandCodeCatalogTier, len(e.Tiers))
		for i := range e.Tiers {
			cloned.Tiers[i] = e.Tiers[i]
			if e.Tiers[i].MaxTokens != nil {
				value := *e.Tiers[i].MaxTokens
				cloned.Tiers[i].MaxTokens = &value
			}
			if e.Tiers[i].ListRates != nil {
				value := *e.Tiers[i].ListRates
				cloned.Tiers[i].ListRates = &value
			}
		}
	}
	if e.Deal != nil {
		value := *e.Deal
		cloned.Deal = &value
	}
	if e.ScheduledChange != nil {
		value := *e.ScheduledChange
		cloned.ScheduledChange = &value
	}
	if e.TimeOfDay != nil {
		value := *e.TimeOfDay
		value.Windows = append([]commandCodeCatalogTimeWindow(nil), e.TimeOfDay.Windows...)
		cloned.TimeOfDay = &value
	}
	return cloned
}

type commandCodeCatalogDiskSnapshot struct {
	Version int                                `json:"version"`
	SavedAt time.Time                          `json:"saved_at"`
	Entries map[string]commandCodeCatalogEntry `json:"entries"`
}

const commandCodeCatalogSnapshotVersion = 1

// CommandCodeCatalog 保存 Command Code 官方 GOAT 模型注册表的最后成功快照。
// GOAT 文档提供价格与活动，Provider API 则用于确认模型仍可调用并校验上下文上限。
type CommandCodeCatalog struct {
	mu                     sync.RWMutex
	entries                map[string]commandCodeCatalogEntry
	lastSuccess            time.Time
	lastAttempt            time.Time
	ttl                    time.Duration
	providerEndpoint       string
	goatEndpoint           string
	minimumModels          int
	minimumRetainedPercent int
	client                 *http.Client
	now                    func() time.Time
	flight                 singleflight.Group
}

var defaultCommandCodeCatalog = NewCommandCodeCatalog(nil)

func NewCommandCodeCatalog(client *http.Client) *CommandCodeCatalog {
	if client == nil {
		client = &http.Client{Timeout: commandCodeCatalogHTTPTimeout}
	}
	return &CommandCodeCatalog{
		entries:                commandCodeFallbackCatalogEntries(),
		ttl:                    commandCodeCatalogTTL,
		providerEndpoint:       commandCodeProviderModelsURL,
		goatEndpoint:           commandCodeGoatDocsURL,
		minimumModels:          commandCodeCatalogMinModelCount,
		minimumRetainedPercent: commandCodeCatalogMinRetainedPct,
		client:                 client,
		now:                    time.Now,
	}
}

func CommandCodeDefaultModelIDs() []string {
	return defaultCommandCodeCatalog.ModelIDs(context.Background())
}

func CommandCodeFallbackModelIDs() []string {
	return commandCodeModelIDsAt(commandCodeFallbackCatalogEntries(), time.Now())
}

func (c *CommandCodeCatalog) ModelIDs(ctx context.Context) []string {
	if c == nil {
		return CommandCodeFallbackModelIDs()
	}
	_ = c.refresh(ctx, false)
	c.mu.RLock()
	entries := cloneCommandCodeCatalogEntries(c.entries)
	c.mu.RUnlock()
	return commandCodeModelIDsAt(entries, c.now())
}

func (c *CommandCodeCatalog) ForceRefresh(ctx context.Context) ([]string, error) {
	if c == nil {
		return CommandCodeFallbackModelIDs(), fmt.Errorf("commandcode catalog is not configured")
	}
	err := c.refresh(ctx, true)
	c.mu.RLock()
	entries := cloneCommandCodeCatalogEntries(c.entries)
	c.mu.RUnlock()
	return commandCodeModelIDsAt(entries, c.now()), err
}

func (c *CommandCodeCatalog) RefreshAndSave(ctx context.Context, filePath string, force bool) error {
	if c == nil {
		return fmt.Errorf("commandcode catalog is not configured")
	}
	if err := c.refresh(ctx, force); err != nil {
		return err
	}
	return c.SaveSnapshot(filePath)
}

func (c *CommandCodeCatalog) SaveSnapshot(filePath string) error {
	if c == nil || strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("commandcode catalog snapshot path is empty")
	}
	c.mu.RLock()
	snapshot := commandCodeCatalogDiskSnapshot{
		Version: commandCodeCatalogSnapshotVersion,
		SavedAt: c.lastSuccess,
		Entries: cloneCommandCodeCatalogEntries(c.entries),
	}
	c.mu.RUnlock()
	if snapshot.SavedAt.IsZero() || len(snapshot.Entries) < c.minimumModels {
		return fmt.Errorf("commandcode catalog has no validated online snapshot")
	}
	encoded, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	temporaryPath := filePath + ".tmp"
	if err := os.WriteFile(temporaryPath, encoded, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, filePath); err != nil {
		// Windows 不允许 Rename 覆盖已有文件；缓存文件可安全地先替换旧版本。
		if removeErr := os.Remove(filePath); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(temporaryPath)
			return err
		}
		if renameErr := os.Rename(temporaryPath, filePath); renameErr != nil {
			_ = os.Remove(temporaryPath)
			return renameErr
		}
	}
	return nil
}

func (c *CommandCodeCatalog) LoadSnapshot(filePath string) error {
	if c == nil || strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("commandcode catalog snapshot path is empty")
	}
	body, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	var snapshot commandCodeCatalogDiskSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return err
	}
	if snapshot.Version != commandCodeCatalogSnapshotVersion || snapshot.SavedAt.IsZero() {
		return fmt.Errorf("unsupported commandcode catalog snapshot")
	}
	if snapshot.SavedAt.After(c.now().Add(5 * time.Minute)) {
		return fmt.Errorf("commandcode catalog snapshot timestamp is in the future")
	}
	if len(snapshot.Entries) < c.minimumModels {
		return fmt.Errorf("commandcode catalog snapshot contained only %d models", len(snapshot.Entries))
	}
	for key, entry := range snapshot.Entries {
		if key != strings.ToLower(strings.TrimSpace(entry.ID)) {
			return fmt.Errorf("invalid commandcode catalog snapshot key %q", key)
		}
		if err := validateCommandCodeCatalogEntry(entry); err != nil {
			return fmt.Errorf("invalid commandcode catalog snapshot entry %q: %w", entry.ID, err)
		}
	}
	c.mu.Lock()
	c.entries = cloneCommandCodeCatalogEntries(snapshot.Entries)
	c.lastSuccess = snapshot.SavedAt
	c.lastAttempt = time.Time{}
	c.mu.Unlock()
	return nil
}

func validateCommandCodeCatalogEntry(entry commandCodeCatalogEntry) error {
	if !isCommandCodeModelID(entry.ID) || entry.ContextWindow <= 0 || len(entry.Tiers) == 0 {
		return fmt.Errorf("missing model metadata")
	}
	if entry.MonthlyCreditsUSD < 0 || math.IsNaN(entry.MonthlyCreditsUSD) || math.IsInf(entry.MonthlyCreditsUSD, 0) {
		return fmt.Errorf("invalid monthly credits")
	}
	free := entry.Deal != nil && entry.Deal.Free
	if !free && entry.MonthlyCreditsUSD <= 0 {
		return fmt.Errorf("invalid monthly credits")
	}
	if entry.Deal != nil && (strings.TrimSpace(entry.Deal.Code) == "" || strings.TrimSpace(entry.Deal.Label) == "" ||
		entry.Deal.DiscountPercent <= 0 || entry.Deal.DiscountPercent > 100) {
		return fmt.Errorf("invalid promotion")
	}
	previousMax := 0
	for index, tier := range entry.Tiers {
		if tier.MinTokens != previousMax || !commandCodeCatalogRatesValid(tier.Rates) ||
			(!free && !commandCodeCatalogRatesHavePrice(tier.Rates)) {
			return fmt.Errorf("invalid pricing tier %d", index)
		}
		if tier.ListRates != nil && (!commandCodeCatalogRatesValid(*tier.ListRates) ||
			(!free && !commandCodeCatalogRatesHavePrice(*tier.ListRates))) {
			return fmt.Errorf("invalid list rates in tier %d", index)
		}
		if tier.MaxTokens == nil {
			if index != len(entry.Tiers)-1 {
				return fmt.Errorf("unbounded tier is not last")
			}
			continue
		}
		if *tier.MaxTokens <= tier.MinTokens {
			return fmt.Errorf("invalid tier boundary")
		}
		previousMax = *tier.MaxTokens
	}
	lastTier := entry.Tiers[len(entry.Tiers)-1]
	if lastTier.MaxTokens != nil && *lastTier.MaxTokens < entry.ContextWindow {
		return fmt.Errorf("pricing tiers do not cover the model context window")
	}
	if entry.Deal != nil && !entry.Deal.Free && !entry.Deal.ExpiresAt.IsZero() {
		for index, tier := range entry.Tiers {
			if tier.ListRates == nil {
				return fmt.Errorf("expiring promotion tier %d has no list rates", index)
			}
		}
	}
	if entry.ScheduledChange != nil && (entry.ScheduledChange.Effective.IsZero() ||
		!commandCodeCatalogRatesValid(entry.ScheduledChange.Rates) ||
		(!free && !commandCodeCatalogRatesHavePrice(entry.ScheduledChange.Rates))) {
		return fmt.Errorf("invalid scheduled price change")
	}
	if entry.TimeOfDay != nil {
		if entry.TimeOfDay.Effective.IsZero() || len(entry.TimeOfDay.Windows) == 0 ||
			!commandCodeCatalogRatesValid(entry.TimeOfDay.Peak) || !commandCodeCatalogRatesValid(entry.TimeOfDay.OffPeak) ||
			(!free && (!commandCodeCatalogRatesHavePrice(entry.TimeOfDay.Peak) || !commandCodeCatalogRatesHavePrice(entry.TimeOfDay.OffPeak))) {
			return fmt.Errorf("invalid time-of-day pricing")
		}
		lastEnd := 0
		for _, window := range entry.TimeOfDay.Windows {
			if window.StartHourUTC < lastEnd || window.StartHourUTC < 0 || window.EndHourUTC > 24 || window.StartHourUTC >= window.EndHourUTC {
				return fmt.Errorf("invalid time-of-day window")
			}
			lastEnd = window.EndHourUTC
		}
	}
	return nil
}

func commandCodeCatalogRatesValid(rates commandCodeCatalogRates) bool {
	if !rates.HasCacheWrite && rates.CacheWrite != 0 {
		return false
	}
	values := []float64{rates.Input, rates.Output, rates.CacheRead, rates.CacheWrite}
	for _, value := range values {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func commandCodeCatalogRatesHavePrice(rates commandCodeCatalogRates) bool {
	return rates.Input > 0 || rates.Output > 0 || rates.CacheRead > 0 ||
		(rates.HasCacheWrite && rates.CacheWrite > 0)
}

func (c *CommandCodeCatalog) entry(model string) (commandCodeCatalogEntry, bool) {
	if c == nil {
		return commandCodeCatalogEntry{}, false
	}
	key := commandCodeCanonicalModelID(model)
	if key == "" {
		return commandCodeCatalogEntry{}, false
	}
	c.mu.RLock()
	entry, ok := c.entries[key]
	if !ok {
		entry, ok = findCommandCodeCatalogEntry(c.entries, key)
	}
	c.mu.RUnlock()
	if !ok {
		return commandCodeCatalogEntry{}, false
	}
	return entry.clone(), true
}

func (c *CommandCodeCatalog) refresh(ctx context.Context, force bool) error {
	now := c.now()
	c.mu.RLock()
	fresh := !c.lastSuccess.IsZero() && now.Sub(c.lastSuccess) < c.ttl
	recentAttempt := !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) < time.Minute
	c.mu.RUnlock()
	if !force && (fresh || recentAttempt) {
		return nil
	}

	_, err, _ := c.flight.Do("refresh", func() (any, error) {
		now = c.now()
		c.mu.Lock()
		if !force && !c.lastSuccess.IsZero() && now.Sub(c.lastSuccess) < c.ttl {
			c.mu.Unlock()
			return nil, nil
		}
		c.lastAttempt = now
		c.mu.Unlock()

		entries, fetchErr := c.fetch(ctx)
		if fetchErr != nil {
			slog.Warn("commandcode_catalog_refresh_failed", "error", sanitizeUpstreamErrorMessage(fetchErr.Error()))
			return nil, fetchErr
		}
		c.mu.RLock()
		previousCount := len(c.entries)
		minimumRetainedPercent := c.minimumRetainedPercent
		c.mu.RUnlock()
		if minimumRetainedPercent > 0 && previousCount > 0 && len(entries)*100 < previousCount*minimumRetainedPercent {
			shrinkErr := fmt.Errorf("commandcode GOAT catalog shrank unexpectedly from %d to %d models", previousCount, len(entries))
			slog.Warn("commandcode_catalog_refresh_failed", "error", shrinkErr.Error())
			return nil, shrinkErr
		}
		c.mu.Lock()
		c.entries = entries
		c.lastSuccess = c.now()
		c.mu.Unlock()
		return nil, nil
	})
	return err
}

func (c *CommandCodeCatalog) fetch(ctx context.Context) (map[string]commandCodeCatalogEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandCodeCatalogHTTPTimeout)
	defer cancel()

	var providerBody, goatBody []byte
	group, fetchCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		body, err := c.fetchBytes(fetchCtx, c.providerEndpoint, "application/json")
		providerBody = body
		return err
	})
	group.Go(func() error {
		body, err := c.fetchBytes(fetchCtx, c.goatEndpoint, "text/html,application/xhtml+xml")
		goatBody = body
		return err
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}

	providerModels, err := parseCommandCodeProviderCatalog(providerBody)
	if err != nil {
		return nil, fmt.Errorf("parse commandcode provider models: %w", err)
	}
	entries, err := parseCommandCodeGoatDocument(goatBody)
	if err != nil {
		return nil, fmt.Errorf("parse commandcode GOAT document: %w", err)
	}
	if len(entries) < c.minimumModels {
		return nil, fmt.Errorf("commandcode GOAT catalog contained only %d models", len(entries))
	}

	for key, entry := range entries {
		provider, ok := providerModels[key]
		if !ok {
			return nil, fmt.Errorf("commandcode GOAT model %q is absent from Provider API", entry.ID)
		}
		if provider.ContextWindow <= 0 || entry.ContextWindow != provider.ContextWindow {
			return nil, fmt.Errorf(
				"commandcode context mismatch for %q: docs=%d provider=%d",
				entry.ID,
				entry.ContextWindow,
				provider.ContextWindow,
			)
		}
	}
	return entries, nil
}

func (c *CommandCodeCatalog) fetchBytes(ctx context.Context, endpoint, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "sub2api-commandcode-catalog/2.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("commandcode catalog %s returned HTTP %d", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, commandCodeCatalogBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > commandCodeCatalogBodyLimit {
		return nil, fmt.Errorf("commandcode catalog response from %s is too large", endpoint)
	}
	return body, nil
}

type commandCodeProviderModel struct {
	ID            string
	Name          string
	ContextWindow int
}

func parseCommandCodeProviderCatalog(body []byte) (map[string]commandCodeProviderModel, error) {
	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextWindow int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	models := make(map[string]commandCodeProviderModel, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if !isCommandCodeModelID(id) || item.ContextWindow <= 0 {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := models[key]; exists {
			return nil, fmt.Errorf("duplicate commandcode Provider API model %q", id)
		}
		models[key] = commandCodeProviderModel{
			ID:            id,
			Name:          strings.TrimSpace(item.Name),
			ContextWindow: item.ContextWindow,
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("commandcode Provider API contained no valid models")
	}
	return models, nil
}

type commandCodeDocumentRates struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cacheRead"`
	CacheWrite *float64 `json:"cacheWrite"`
}

type commandCodeDocumentTier struct {
	Context   string                   `json:"context"`
	Rates     commandCodeDocumentRates `json:"rates"`
	ListRates json.RawMessage          `json:"listRates"`
}

type commandCodeDocumentDeal struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	DiscountPercent float64 `json:"discountPercent"`
	Note            string  `json:"note"`
	Term            string  `json:"term"`
	Free            bool    `json:"free"`
	Expires         string  `json:"expires"`
}

type commandCodeDocumentScheduledChange struct {
	Effective string                   `json:"effective"`
	Rates     commandCodeDocumentRates `json:"rates"`
}

type commandCodeDocumentTimeOfDay struct {
	Effective string                   `json:"effective"`
	Peak      commandCodeDocumentRates `json:"peak"`
	OffPeak   commandCodeDocumentRates `json:"offPeak"`
	Windows   string                   `json:"windows"`
}

type commandCodeDocumentModel struct {
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	ContextWindow     int                       `json:"contextWindow"`
	MinPlanName       string                    `json:"minPlanName"`
	Tiers             []commandCodeDocumentTier `json:"tiers"`
	Deal              json.RawMessage           `json:"deal"`
	ScheduledChange   json.RawMessage           `json:"scheduledChange"`
	TimeOfDay         json.RawMessage           `json:"timeOfDay"`
	MonthlyCreditsRaw string                    `json:"credits"`
}

// commandCodeDocumentCreditRow 是 GOAT 页面模型表格组件的 rows 数据：
// 以显示名关联模型，并提供官方 Monthly credits（如 "$$70"）。
type commandCodeDocumentCreditRow struct {
	Name    string `json:"name"`
	Credits string `json:"credits"`
}

func parseCommandCodeGoatDocument(body []byte) (map[string]commandCodeCatalogEntry, error) {
	flight, err := extractCommandCodeNextFlight(body)
	if err != nil {
		return nil, err
	}

	var best map[string]commandCodeCatalogEntry
	var lastErr error
	creditRows, err := extractCommandCodeCreditRows(flight)
	if err != nil {
		return nil, err
	}
	searchFrom := 0
	for {
		relative := strings.Index(flight[searchFrom:], `"models":`)
		if relative < 0 {
			break
		}
		marker := searchFrom + relative + len(`"models":`)
		array, end, extractErr := extractCommandCodeJSONArray(flight, marker)
		if extractErr != nil {
			lastErr = extractErr
			searchFrom = marker
			continue
		}
		searchFrom = end

		var models []commandCodeDocumentModel
		if unmarshalErr := json.Unmarshal([]byte(array), &models); unmarshalErr != nil {
			lastErr = unmarshalErr
			continue
		}
		entries, normalizeErr := normalizeCommandCodeDocumentModels(models, creditRows)
		if normalizeErr != nil {
			lastErr = normalizeErr
			continue
		}
		if len(entries) > len(best) {
			best = entries
		}
	}
	if len(best) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("commandcode GOAT document contained no model registry")
	}
	planScope, err := extractCommandCodeGoatPlanScope(flight)
	if err != nil {
		return nil, err
	}
	if len(planScope) != len(best) {
		return nil, fmt.Errorf("commandcode GOAT plan scope contains %d models but registry selected %d", len(planScope), len(best))
	}
	for _, id := range planScope {
		if _, ok := best[strings.ToLower(strings.TrimSpace(id))]; !ok {
			return nil, fmt.Errorf("commandcode GOAT plan scope model %q is absent from registry", id)
		}
	}
	return best, nil
}

func extractCommandCodeGoatPlanScope(flight string) ([]string, error) {
	var best []string
	searchFrom := 0
	for {
		relative := strings.Index(flight[searchFrom:], `"planScope":`)
		if relative < 0 {
			break
		}
		marker := searchFrom + relative + len(`"planScope":`)
		object, end, err := extractCommandCodeJSONContainer(flight, marker, '{', '}')
		if err != nil {
			searchFrom = marker
			continue
		}
		searchFrom = end
		modelIDsMarker := strings.Index(object, `"modelIds":`)
		if modelIDsMarker < 0 {
			continue
		}
		array, _, err := extractCommandCodeJSONArray(object, modelIDsMarker+len(`"modelIds":`))
		if err != nil {
			continue
		}
		var modelIDs []string
		if json.Unmarshal([]byte(array), &modelIDs) == nil && len(modelIDs) > len(best) {
			best = modelIDs
		}
	}
	if len(best) == 0 {
		return nil, fmt.Errorf("commandcode GOAT document contained no plan scope")
	}
	seen := make(map[string]struct{}, len(best))
	for _, id := range best {
		key := strings.ToLower(strings.TrimSpace(id))
		if !isCommandCodeModelID(id) {
			return nil, fmt.Errorf("commandcode GOAT plan scope contains invalid model %q", id)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("commandcode GOAT plan scope contains duplicate model %q", id)
		}
		seen[key] = struct{}{}
	}
	return best, nil
}

func extractCommandCodeNextFlight(body []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	var chunks strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "script" && node.FirstChild != nil {
			text := node.FirstChild.Data
			const prefix = "self.__next_f.push("
			searchFrom := 0
			for {
				relative := strings.Index(text[searchFrom:], prefix)
				if relative < 0 {
					break
				}
				argumentStart := searchFrom + relative + len(prefix)
				decoder := json.NewDecoder(strings.NewReader(text[argumentStart:]))
				var frame []json.RawMessage
				if err := decoder.Decode(&frame); err != nil {
					searchFrom = argumentStart
					continue
				}
				searchFrom = argumentStart + int(decoder.InputOffset())
				if len(frame) < 2 {
					continue
				}
				var chunk string
				if json.Unmarshal(frame[1], &chunk) == nil {
					_, _ = chunks.WriteString(chunk)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if chunks.Len() == 0 {
		return "", fmt.Errorf("commandcode GOAT document contained no Next Flight payload")
	}
	return chunks.String(), nil
}

func extractCommandCodeJSONArray(source string, start int) (string, int, error) {
	return extractCommandCodeJSONContainer(source, start, '[', ']')
}

func extractCommandCodeJSONContainer(source string, start int, opening, closing byte) (string, int, error) {
	for start < len(source) && (source[start] == ' ' || source[start] == '\n' || source[start] == '\r' || source[start] == '\t') {
		start++
	}
	if start >= len(source) || source[start] != opening {
		return "", start, fmt.Errorf("commandcode Flight value has unexpected JSON type")
	}
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(source); index++ {
		char := source[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case opening:
			depth++
		case closing:
			depth--
			if depth == 0 {
				return source[start : index+1], index + 1, nil
			}
		}
	}
	return "", start, fmt.Errorf("commandcode Flight JSON value is truncated")
}

// extractCommandCodeCreditRows 从 GOAT 页面的模型表格组件 props 中提取
// 每个模型的显示名与官方 Monthly credits（如 "$$70"）。
// rows 的 name 可能带档位后缀（如 "Qwen 3.7 Flash (≤ 32k)"）或状态后缀
// （如 "(latest)"），统一规范化后与 models 数组的 name 匹配。
func extractCommandCodeCreditRows(flight string) (map[string]float64, error) {
	rows := make(map[string]float64)
	searchFrom := 0
	foundAny := false
	for {
		relative := strings.Index(flight[searchFrom:], `"rows":`)
		if relative < 0 {
			break
		}
		marker := searchFrom + relative + len(`"rows":`)
		array, end, err := extractCommandCodeJSONArray(flight, marker)
		if err != nil {
			searchFrom = marker
			continue
		}
		searchFrom = end

		var rowsData []commandCodeDocumentCreditRow
		if err := json.Unmarshal([]byte(array), &rowsData); err != nil {
			continue
		}
		if len(rowsData) == 0 {
			continue
		}
		for _, row := range rowsData {
			name := strings.ToLower(strings.TrimSpace(row.Name))
			if name == "" {
				continue
			}
			credits, parseErr := parseCommandCodeCreditsUSD(row.Credits)
			if parseErr != nil {
				continue
			}
			rows[commandCodeNormalizeCreditRowName(name)] = credits
			foundAny = true
		}
	}
	if !foundAny {
		return nil, fmt.Errorf("commandcode GOAT document contained no monthly credits rows")
	}
	return rows, nil
}

// commandCodeNormalizeCreditRowName 去掉 rows 显示名中的括号后缀
// （档位/状态说明），得到可与 models name 匹配的基础名。
func commandCodeNormalizeCreditRowName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for {
		open := strings.IndexByte(name, '(')
		if open < 0 {
			break
		}
		close := strings.IndexByte(name[open:], ')')
		if close < 0 {
			break
		}
		name = strings.TrimSpace(name[:open] + " " + name[open+close+1:])
	}
	return strings.TrimSpace(name)
}

func normalizeCommandCodeDocumentModels(models []commandCodeDocumentModel, creditRows map[string]float64) (map[string]commandCodeCatalogEntry, error) {
	entries := make(map[string]commandCodeCatalogEntry)
	for _, model := range models {
		plan := strings.ToLower(strings.TrimSpace(model.MinPlanName))
		if plan != "go" && plan != "goat" {
			continue
		}
		id := strings.TrimSpace(model.ID)
		if !isCommandCodeModelID(id) || model.ContextWindow <= 0 {
			return nil, fmt.Errorf("invalid commandcode GOAT model metadata for %q", id)
		}
		deal, err := parseCommandCodeDocumentDeal(model.Deal)
		if err != nil {
			return nil, fmt.Errorf("parse deal for %q: %w", id, err)
		}
		isFree := deal != nil && deal.Free
		monthlyCreditsUSD := 0.0
		if credits, ok := creditRows[commandCodeNormalizeCreditRowName(model.Name)]; ok {
			monthlyCreditsUSD = credits
		} else if !isFree {
			return nil, fmt.Errorf("missing monthly credits for %q", id)
		}
		tiers, err := normalizeCommandCodeDocumentTiers(model.Tiers, isFree)
		if err != nil {
			return nil, fmt.Errorf("parse pricing tiers for %q: %w", id, err)
		}
		scheduledChange, err := parseCommandCodeDocumentScheduledChange(model.ScheduledChange)
		if err != nil {
			return nil, fmt.Errorf("parse scheduled price change for %q: %w", id, err)
		}
		if scheduledChange != nil && len(tiers) != 1 {
			return nil, fmt.Errorf("scheduled price change with multiple context tiers is unsupported for %q", id)
		}
		timeOfDay, err := parseCommandCodeDocumentTimeOfDay(model.TimeOfDay)
		if err != nil {
			return nil, fmt.Errorf("parse time-of-day pricing for %q: %w", id, err)
		}
		if timeOfDay != nil && len(tiers) != 1 {
			return nil, fmt.Errorf("time-of-day pricing with multiple context tiers is unsupported for %q", id)
		}
		key := strings.ToLower(id)
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("duplicate commandcode GOAT model %q", id)
		}
		entry := commandCodeCatalogEntry{
			ID:                id,
			Name:              strings.TrimSpace(model.Name),
			ContextWindow:     model.ContextWindow,
			MonthlyCreditsUSD: monthlyCreditsUSD,
			Tiers:             tiers,
			Deal:              deal,
			ScheduledChange:   scheduledChange,
			TimeOfDay:         timeOfDay,
		}
		if err := validateCommandCodeCatalogEntry(entry); err != nil {
			return nil, fmt.Errorf("validate commandcode GOAT model %q: %w", id, err)
		}
		entries[key] = entry
	}
	return entries, nil
}

func normalizeCommandCodeDocumentTiers(tiers []commandCodeDocumentTier, free bool) ([]commandCodeCatalogTier, error) {
	if len(tiers) == 0 {
		return nil, fmt.Errorf("missing pricing tiers")
	}
	out := make([]commandCodeCatalogTier, 0, len(tiers))
	previousMax := 0
	for index, tier := range tiers {
		rates, ok := normalizeCommandCodeDocumentRates(tier.Rates)
		if !ok {
			return nil, fmt.Errorf("tier %d has incomplete rates", index)
		}
		if rates.Input == 0 && rates.Output == 0 && rates.CacheRead == 0 && !free {
			return nil, fmt.Errorf("tier %d has an unverified zero rate", index)
		}
		listRates, err := resolveCommandCodeDocumentListRates(tiers, index, make(map[int]bool))
		if err != nil {
			return nil, fmt.Errorf("tier %d list rates: %w", index, err)
		}

		minTokens, maxTokens, err := parseCommandCodeTierRange(tier.Context, len(tiers), index, previousMax)
		if err != nil {
			return nil, err
		}
		if maxTokens != nil {
			previousMax = *maxTokens
		}
		out = append(out, commandCodeCatalogTier{
			MinTokens: minTokens,
			MaxTokens: maxTokens,
			Rates:     rates,
			ListRates: listRates,
		})
	}
	return out, nil
}

func resolveCommandCodeDocumentListRates(
	tiers []commandCodeDocumentTier,
	index int,
	visiting map[int]bool,
) (*commandCodeCatalogRates, error) {
	if index < 0 || index >= len(tiers) {
		return nil, fmt.Errorf("reference target is out of range")
	}
	if visiting[index] {
		return nil, fmt.Errorf("cyclic list rates reference")
	}
	visiting[index] = true
	defer delete(visiting, index)

	rawMessage := tiers[index].ListRates
	if len(rawMessage) == 0 || string(rawMessage) == "null" || string(rawMessage) == "false" {
		return nil, nil
	}
	if rawMessage[0] == '{' {
		var raw commandCodeDocumentRates
		if err := json.Unmarshal(rawMessage, &raw); err != nil {
			return nil, err
		}
		value, valid := normalizeCommandCodeDocumentRates(raw)
		if !valid {
			return nil, fmt.Errorf("incomplete list rates")
		}
		return &value, nil
	}
	if rawMessage[0] != '"' {
		return nil, nil
	}
	var reference string
	if err := json.Unmarshal(rawMessage, &reference); err != nil {
		return nil, err
	}
	match := commandCodeListRatesReferencePattern.FindStringSubmatch(reference)
	if len(match) != 2 {
		return nil, nil
	}
	target, err := strconv.Atoi(match[1])
	if err != nil {
		return nil, fmt.Errorf("invalid list rates reference")
	}
	return resolveCommandCodeDocumentListRates(tiers, target, visiting)
}

func normalizeCommandCodeDocumentRates(raw commandCodeDocumentRates) (commandCodeCatalogRates, bool) {
	if raw.Input == nil || raw.Output == nil || raw.CacheRead == nil {
		return commandCodeCatalogRates{}, false
	}
	values := []float64{*raw.Input, *raw.Output, *raw.CacheRead}
	for _, value := range values {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return commandCodeCatalogRates{}, false
		}
	}
	rates := commandCodeCatalogRates{Input: *raw.Input, Output: *raw.Output, CacheRead: *raw.CacheRead}
	if raw.CacheWrite != nil {
		if *raw.CacheWrite < 0 || math.IsNaN(*raw.CacheWrite) || math.IsInf(*raw.CacheWrite, 0) {
			return commandCodeCatalogRates{}, false
		}
		rates.CacheWrite = *raw.CacheWrite
		rates.HasCacheWrite = true
	}
	return rates, true
}

func parseCommandCodeTierRange(contextLabel string, tierCount, index, previousMax int) (int, *int, error) {
	label := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(contextLabel), " ", ""))
	if label == "" {
		if tierCount != 1 {
			return 0, nil, fmt.Errorf("tier %d is missing a context boundary", index)
		}
		return 0, nil, nil
	}
	if strings.HasPrefix(label, "≤") || strings.HasPrefix(label, "<=") {
		thresholdText := strings.TrimPrefix(strings.TrimPrefix(label, "≤"), "<=")
		threshold, err := parseCommandCodeTokenCount(thresholdText)
		if err != nil || threshold <= previousMax {
			return 0, nil, fmt.Errorf("invalid context upper bound %q", contextLabel)
		}
		return previousMax, &threshold, nil
	}
	if strings.HasPrefix(label, ">") {
		threshold, err := parseCommandCodeTokenCount(strings.TrimPrefix(label, ">"))
		if err != nil || index != tierCount-1 || (previousMax > 0 && threshold != previousMax) {
			return 0, nil, fmt.Errorf("invalid context lower bound %q", contextLabel)
		}
		return threshold, nil, nil
	}
	return 0, nil, fmt.Errorf("unsupported context boundary %q", contextLabel)
}

func parseCommandCodeTokenCount(value string) (int, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "K"):
		multiplier = 1_000
		value = strings.TrimSuffix(value, "K")
	case strings.HasSuffix(value, "M"):
		multiplier = 1_000_000
		value = strings.TrimSuffix(value, "M")
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number <= 0 || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("invalid token count")
	}
	result := number * multiplier
	if result > math.MaxInt || math.Trunc(result) != result {
		return 0, fmt.Errorf("invalid token count")
	}
	return int(result), nil
}

func parseCommandCodeCreditsUSD(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "$$")
	value = strings.TrimPrefix(value, "$")
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("missing monthly credits")
	}
	credits, err := strconv.ParseFloat(value, 64)
	if err != nil || credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return 0, fmt.Errorf("invalid monthly credits %q", raw)
	}
	return credits, nil
}

func parseCommandCodeDocumentDeal(raw json.RawMessage) (*commandCodeCatalogDeal, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	var document commandCodeDocumentDeal
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if strings.TrimSpace(document.ID) == "" || strings.TrimSpace(document.Label) == "" ||
		document.DiscountPercent <= 0 || document.DiscountPercent > 100 {
		return nil, fmt.Errorf("invalid promotion metadata")
	}
	expiresAt, err := parseCommandCodeDealExpiry(document.Expires)
	if err != nil {
		return nil, err
	}
	term := strings.TrimSpace(document.Term)
	if term == "" {
		term = strings.TrimSpace(document.Note)
	}
	return &commandCodeCatalogDeal{
		Code:            strings.TrimSpace(document.ID),
		Label:           strings.TrimSpace(document.Label),
		DiscountPercent: document.DiscountPercent,
		Free:            document.Free,
		Term:            term,
		ExpiresAt:       expiresAt,
	}, nil
}

func parseCommandCodeDealExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid promotion expiry %q", value)
	}
	return parsed.Add(24*time.Hour - time.Nanosecond).UTC(), nil
}

func parseCommandCodeDocumentScheduledChange(raw json.RawMessage) (*commandCodeCatalogScheduledChange, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	var document commandCodeDocumentScheduledChange
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	effective, err := time.Parse(time.RFC3339, strings.TrimSpace(document.Effective))
	if err != nil {
		return nil, fmt.Errorf("invalid scheduled price effective time")
	}
	rates, ok := normalizeCommandCodeDocumentRates(document.Rates)
	if !ok {
		return nil, fmt.Errorf("invalid scheduled price rates")
	}
	return &commandCodeCatalogScheduledChange{Effective: effective.UTC(), Rates: rates}, nil
}

func parseCommandCodeDocumentTimeOfDay(raw json.RawMessage) (*commandCodeCatalogTimeOfDay, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, nil
	}
	var document commandCodeDocumentTimeOfDay
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	peak, peakOK := normalizeCommandCodeDocumentRates(document.Peak)
	offPeak, offPeakOK := normalizeCommandCodeDocumentRates(document.OffPeak)
	if !peakOK || !offPeakOK || !strings.Contains(strings.ToUpper(document.Windows), "UTC") {
		return nil, fmt.Errorf("invalid time-of-day pricing metadata")
	}
	effective, err := time.Parse(time.RFC3339, strings.TrimSpace(document.Effective))
	if err != nil {
		return nil, fmt.Errorf("invalid time-of-day effective time")
	}
	matches := commandCodeTimeWindowPattern.FindAllStringSubmatch(document.Windows, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("time-of-day pricing has no peak windows")
	}
	windows := make([]commandCodeCatalogTimeWindow, 0, len(matches))
	for _, match := range matches {
		start, startErr := strconv.Atoi(match[1])
		end, endErr := strconv.Atoi(match[2])
		if startErr != nil || endErr != nil || start < 0 || end > 24 || start >= end {
			return nil, fmt.Errorf("invalid peak window %q", match[0])
		}
		windows = append(windows, commandCodeCatalogTimeWindow{StartHourUTC: start, EndHourUTC: end})
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].StartHourUTC < windows[j].StartHourUTC })
	for index := 1; index < len(windows); index++ {
		if windows[index].StartHourUTC < windows[index-1].EndHourUTC {
			return nil, fmt.Errorf("overlapping peak windows")
		}
	}
	return &commandCodeCatalogTimeOfDay{
		Effective: effective.UTC(),
		Peak:      peak,
		OffPeak:   offPeak,
		Windows:   windows,
	}, nil
}

func cloneCommandCodeCatalogEntries(src map[string]commandCodeCatalogEntry) map[string]commandCodeCatalogEntry {
	out := make(map[string]commandCodeCatalogEntry, len(src))
	for key, entry := range src {
		out[key] = entry.clone()
	}
	return out
}

func findCommandCodeCatalogEntry(entries map[string]commandCodeCatalogEntry, model string) (commandCodeCatalogEntry, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return commandCodeCatalogEntry{}, false
	}
	if entry, ok := entries[model]; ok {
		return entry, true
	}
	if slash := strings.Index(model, "/"); slash >= 0 {
		if entry, ok := entries[model[slash+1:]]; ok {
			return entry, true
		}
	}
	var matched commandCodeCatalogEntry
	found := false
	for _, entry := range entries {
		id := strings.ToLower(entry.ID)
		if id == model || (strings.Contains(id, "/") && id[strings.LastIndex(id, "/")+1:] == model) {
			if found {
				return commandCodeCatalogEntry{}, false
			}
			matched = entry
			found = true
		}
	}
	return matched, found
}

func commandCodeModelIDsAt(entries map[string]commandCodeCatalogEntry, now time.Time) []string {
	models := make([]string, 0, len(entries))
	for _, entry := range entries {
		if commandCodeCatalogEntryAvailableAt(entry, now) {
			models = append(models, entry.ID)
		}
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i]) < strings.ToLower(models[j])
	})
	return models
}

func commandCodeCatalogEntryAvailableAt(entry commandCodeCatalogEntry, now time.Time) bool {
	if len(entry.Tiers) == 0 {
		return false
	}
	if entry.Deal == nil || entry.Deal.ExpiresAt.IsZero() || !now.After(entry.Deal.ExpiresAt) {
		return true
	}
	for _, tier := range entry.Tiers {
		if tier.ListRates == nil {
			return false
		}
	}
	return true
}

func isCommandCodeModelID(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	return !strings.ContainsAny(model, "*?#\\\t\r\n ")
}
