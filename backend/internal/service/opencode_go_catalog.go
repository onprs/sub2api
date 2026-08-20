package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	openCodeGoOfficialDocsURL    = "https://opencode.ai/docs/go/"
	openCodeGoOfficialModelsURL  = "https://opencode.ai/zen/go/v1/models"
	openCodeGoCatalogTTL         = 6 * time.Hour
	openCodeGoCatalogHTTPTimeout = 3 * time.Second
)

// OpenCodeGoCatalogEntry is the read-only model metadata we need for model
// discovery and protocol routing. Protocol can be empty when the official
// /models endpoint lists the model but docs do not publish endpoint metadata.
type OpenCodeGoCatalogEntry struct {
	ID       string
	Protocol string
}

type OpenCodeGoCatalog struct {
	mu          sync.RWMutex
	entries     map[string]OpenCodeGoCatalogEntry
	lastAttempt time.Time
	ttl         time.Duration
	client      *http.Client
}

var defaultOpenCodeGoCatalog = newOpenCodeGoCatalog(openCodeGoSeedCatalog())

func newOpenCodeGoCatalog(seed map[string]OpenCodeGoCatalogEntry) *OpenCodeGoCatalog {
	return &OpenCodeGoCatalog{
		entries: seed,
		ttl:     openCodeGoCatalogTTL,
		client:  &http.Client{Timeout: openCodeGoCatalogHTTPTimeout},
	}
}

func openCodeGoSeedCatalog() map[string]OpenCodeGoCatalogEntry {
	protocols := map[string]string{
		"gpt-5.6-luna":               OpenCodeGoProtocolResponses,
		"grok-4.5":                   OpenCodeGoProtocolResponses,
		"muse-spark-1.2":             OpenCodeGoProtocolResponses,
		"muse-spark-1.2-contributor": OpenCodeGoProtocolResponses,
		"deepseek-v4-flash":          OpenCodeGoProtocolChatCompletions,
		"deepseek-v4-pro":            OpenCodeGoProtocolChatCompletions,
		"glm-5":                      OpenCodeGoProtocolChatCompletions,
		"glm-5.1":                    OpenCodeGoProtocolChatCompletions,
		"glm-5.2":                    OpenCodeGoProtocolChatCompletions,
		"glm-5.3":                    OpenCodeGoProtocolChatCompletions,
		"kimi-k2.6":                  OpenCodeGoProtocolChatCompletions,
		"kimi-k2.5":                  OpenCodeGoProtocolChatCompletions,
		"kimi-k2.7-code":             OpenCodeGoProtocolChatCompletions,
		"kimi-k3":                    OpenCodeGoProtocolChatCompletions,
		"hy3":                        OpenCodeGoProtocolChatCompletions,
		"mimo-v2.5":                  OpenCodeGoProtocolChatCompletions,
		"mimo-v2.5-pro":              OpenCodeGoProtocolChatCompletions,
		"mimo-v2-pro":                OpenCodeGoProtocolChatCompletions,
		"mimo-v2-omni":               OpenCodeGoProtocolChatCompletions,
		"minimax-m2.5":               OpenCodeGoProtocolMessages,
		"minimax-m2.7":               OpenCodeGoProtocolMessages,
		"minimax-m3":                 OpenCodeGoProtocolMessages,
		"qwen3.5-plus":               OpenCodeGoProtocolMessages,
		"qwen3.6-plus":               OpenCodeGoProtocolMessages,
		"qwen3.7-max":                OpenCodeGoProtocolMessages,
		"qwen3.7-plus":               OpenCodeGoProtocolMessages,
		"qwen3.8-max":                OpenCodeGoProtocolMessages,
	}
	models := []string{
		"gpt-5.6-luna",
		"grok-4.5",
		"muse-spark-1.2",
		"muse-spark-1.2-contributor",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
		"glm-5",
		"glm-5.1",
		"glm-5.2",
		"glm-5.3",
		"kimi-k2.6",
		"kimi-k2.5",
		"kimi-k2.7-code",
		"kimi-k3",
		"mimo-v2.5",
		"mimo-v2.5-pro",
		"mimo-v2-pro",
		"mimo-v2-omni",
		"minimax-m2.5",
		"minimax-m2.7",
		"minimax-m3",
		"qwen3.5-plus",
		"qwen3.6-plus",
		"qwen3.7-max",
		"qwen3.7-plus",
		"qwen3.8-max",
		"hy3",
		"hy3-preview",
	}
	out := make(map[string]OpenCodeGoCatalogEntry, len(models))
	for _, id := range models {
		out[id] = OpenCodeGoCatalogEntry{ID: id, Protocol: protocols[id]}
	}
	return out
}

func OpenCodeGoDefaultModelIDs() []string {
	return defaultOpenCodeGoCatalog.ModelIDs(context.Background())
}

func OpenCodeGoFallbackModelIDs() []string {
	seed := openCodeGoSeedCatalog()
	ids := make([]string, 0, len(seed))
	for _, entry := range seed {
		if strings.TrimSpace(entry.ID) != "" {
			ids = append(ids, entry.ID)
		}
	}
	sortOpenCodeGoModelIDs(ids)
	return ids
}

func OpenCodeGoCatalogModelProtocol(model string) (string, bool) {
	return defaultOpenCodeGoCatalog.Protocol(context.Background(), model)
}

func (c *OpenCodeGoCatalog) ModelIDs(ctx context.Context) []string {
	c.refreshIfStale(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]string, 0, len(c.entries))
	for _, entry := range c.entries {
		if strings.TrimSpace(entry.ID) != "" {
			ids = append(ids, entry.ID)
		}
	}
	sortOpenCodeGoModelIDs(ids)
	return ids
}

func (c *OpenCodeGoCatalog) Protocol(ctx context.Context, model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	c.refreshIfStale(ctx)
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[model]
	if !ok {
		for _, candidate := range c.entries {
			if strings.EqualFold(candidate.ID, model) {
				entry = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return "", false
	}
	protocol := normalizeOpenCodeGoModelProtocol(entry.Protocol)
	return protocol, protocol != ""
}

func (c *OpenCodeGoCatalog) refreshIfStale(ctx context.Context) {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.RLock()
	stale := c.lastAttempt.IsZero() || now.Sub(c.lastAttempt) > c.ttl
	c.mu.RUnlock()
	if !stale {
		return
	}

	c.mu.Lock()
	if !c.lastAttempt.IsZero() && now.Sub(c.lastAttempt) <= c.ttl {
		c.mu.Unlock()
		return
	}
	c.lastAttempt = now
	current := cloneOpenCodeGoCatalogEntries(c.entries)
	c.mu.Unlock()

	refreshed, err := c.fetch(ctx, current)
	if err != nil || len(refreshed) == 0 {
		return
	}

	c.mu.Lock()
	c.entries = refreshed
	c.mu.Unlock()
}

func (c *OpenCodeGoCatalog) fetch(ctx context.Context, base map[string]OpenCodeGoCatalogEntry) (map[string]OpenCodeGoCatalogEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, openCodeGoCatalogHTTPTimeout)
	defer cancel()

	docsCatalog := map[string]OpenCodeGoCatalogEntry{}
	if docs, err := c.fetchBytes(ctx, openCodeGoOfficialDocsURL); err == nil {
		docsCatalog = parseOpenCodeGoCatalogDocument(docs)
	}
	if models, err := c.fetchBytes(ctx, openCodeGoOfficialModelsURL); err == nil {
		if ids, parseErr := parseOpenCodeGoModelsEndpoint(models); parseErr == nil {
			out := make(map[string]OpenCodeGoCatalogEntry, len(ids))
			for _, id := range ids {
				protocol := ""
				if entry, ok := docsCatalog[id]; ok {
					protocol = entry.Protocol
				} else if entry, ok := findOpenCodeGoCatalogEntry(base, id); ok {
					protocol = entry.Protocol
				}
				mergeOpenCodeGoCatalogEntry(out, id, protocol)
			}
			for id, entry := range docsCatalog {
				if _, ok := findOpenCodeGoCatalogEntry(out, id); ok {
					mergeOpenCodeGoCatalogEntry(out, id, entry.Protocol)
				}
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("opencode go official models refresh failed")
}

func (c *OpenCodeGoCatalog) fetchBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json,text/html,*/*")
	req.Header.Set("User-Agent", "sub2api-opencode-go-catalog/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode go catalog fetch %s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func cloneOpenCodeGoCatalogEntries(src map[string]OpenCodeGoCatalogEntry) map[string]OpenCodeGoCatalogEntry {
	out := make(map[string]OpenCodeGoCatalogEntry, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func mergeOpenCodeGoCatalogEntry(entries map[string]OpenCodeGoCatalogEntry, id, protocol string) {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "*") || !isOpenCodeGoModelID(id) {
		return
	}
	existing, ok := entries[id]
	if !ok {
		for key, entry := range entries {
			if strings.EqualFold(key, id) || strings.EqualFold(entry.ID, id) {
				existing = entry
				delete(entries, key)
				break
			}
		}
	}
	if existing.ID == "" {
		existing.ID = id
	}
	if normalized := normalizeOpenCodeGoModelProtocol(protocol); normalized != "" {
		existing.Protocol = normalized
	} else if inferred := inferOpenCodeGoModelFamilyProtocol(id); inferred != "" {
		existing.Protocol = inferred
	}
	entries[id] = existing
}

func findOpenCodeGoCatalogEntry(entries map[string]OpenCodeGoCatalogEntry, id string) (OpenCodeGoCatalogEntry, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return OpenCodeGoCatalogEntry{}, false
	}
	if entry, ok := entries[id]; ok {
		return entry, true
	}
	for key, entry := range entries {
		if strings.EqualFold(key, id) || strings.EqualFold(entry.ID, id) {
			return entry, true
		}
	}
	return OpenCodeGoCatalogEntry{}, false
}

func parseOpenCodeGoCatalogDocument(body []byte) map[string]OpenCodeGoCatalogEntry {
	rows := extractHTMLTableRows(body)
	out := make(map[string]OpenCodeGoCatalogEntry)
	for _, cells := range rows {
		if len(cells) < 3 || !isOpenCodeGoModelID(cells[1]) {
			continue
		}
		endpoint := strings.ToLower(cells[2])
		protocol := ""
		switch {
		case strings.Contains(endpoint, "/responses"):
			protocol = OpenCodeGoProtocolResponses
		case strings.Contains(endpoint, "/chat/completions"):
			protocol = OpenCodeGoProtocolChatCompletions
		case strings.Contains(endpoint, "/messages"):
			protocol = OpenCodeGoProtocolMessages
		default:
			continue
		}
		id := strings.TrimSpace(cells[1])
		out[id] = OpenCodeGoCatalogEntry{ID: id, Protocol: protocol}
	}
	return out
}

func parseOpenCodeGoModelsEndpoint(body []byte) ([]string, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		var ids []string
		if arrErr := json.Unmarshal(body, &ids); arrErr != nil {
			return nil, err
		}
		return normalizeOpenCodeGoModelIDs(ids), nil
	}
	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		ids = append(ids, item.ID)
	}
	return normalizeOpenCodeGoModelIDs(ids), nil
}

func normalizeOpenCodeGoModelIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "*") || !isOpenCodeGoModelID(id) {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	sortOpenCodeGoModelIDs(out)
	return out
}

func sortOpenCodeGoModelIDs(ids []string) {
	sort.SliceStable(ids, func(i, j int) bool {
		return strings.ToLower(ids[i]) < strings.ToLower(ids[j])
	})
}
