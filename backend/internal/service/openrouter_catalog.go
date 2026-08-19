package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	openrouterCatalogURL         = "https://openrouter.ai/api/v1/models"
	openrouterCatalogTTL         = time.Hour
	openrouterCatalogHTTPTimeout = 5 * time.Second
	openrouterCatalogBodyLimit   = 4 << 20
)

var openrouterFallbackModels = []string{
	"dots-studio/dots-3-note-preview:free",
	"google/gemma-4-26b-a4b-it:free",
	"google/gemma-4-31b-it:free",
	"liquid/lfm-2.5-2.6b:free",
	"nvidia/nemotron-3.5-lightning:free",
	"nvidia/nemotron-3.5-content-safety:free",
	"nvidia/nemotron-3-ultra-550b-a55b:free",
	"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free",
	"nvidia/nemotron-3-super-120b-a12b:free",
	"nvidia/nemotron-3-nano-30b-a3b:free",
	"nvidia/nemotron-nano-12b-v2-vl:free",
	"nvidia/nemotron-nano-9b-v2:free",
	"openai/gpt-oss-20b:free",
	"openrouter/free",
	"poolside/laguna-s-2.1:free",
	"poolside/laguna-xs-2.1:free",
	"z-ai/glm-5.2:free",
}

// OpenRouterCatalog keeps a last-known-good copy of OpenRouter's public model list.
type OpenRouterCatalog struct {
	mu          sync.RWMutex
	models      []string
	lastSuccess time.Time
	lastAttempt time.Time
	ttl         time.Duration
	endpoint    string
	client      *http.Client
	now         func() time.Time
	flight      singleflight.Group
}

var defaultOpenRouterCatalog = NewOpenRouterCatalog(nil)

func NewOpenRouterCatalog(client *http.Client) *OpenRouterCatalog {
	if client == nil {
		client = &http.Client{Timeout: openrouterCatalogHTTPTimeout}
	}
	return &OpenRouterCatalog{
		models:   append([]string(nil), openrouterFallbackModels...),
		ttl:      openrouterCatalogTTL,
		endpoint: openrouterCatalogURL,
		client:   client,
		now:      time.Now,
	}
}

func OpenRouterDefaultModelIDs() []string {
	return defaultOpenRouterCatalog.ModelIDs(context.Background())
}

func OpenRouterFallbackModelIDs() []string {
	return append([]string(nil), openrouterFallbackModels...)
}

func (c *OpenRouterCatalog) ModelIDs(ctx context.Context) []string {
	if c == nil {
		return OpenRouterFallbackModelIDs()
	}
	_ = c.refresh(ctx, false)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.models...)
}

func (c *OpenRouterCatalog) ForceRefresh(ctx context.Context) ([]string, error) {
	if c == nil {
		return OpenRouterFallbackModelIDs(), fmt.Errorf("openrouter catalog is not configured")
	}
	err := c.refresh(ctx, true)
	c.mu.RLock()
	models := append([]string(nil), c.models...)
	c.mu.RUnlock()
	return models, err
}

func (c *OpenRouterCatalog) refresh(ctx context.Context, force bool) error {
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

		models, fetchErr := c.fetch(ctx)
		if fetchErr != nil {
			slog.Warn("openrouter_catalog_refresh_failed", "error", sanitizeUpstreamErrorMessage(fetchErr.Error()))
			return nil, fetchErr
		}
		c.mu.Lock()
		c.models = models
		c.lastSuccess = c.now()
		c.mu.Unlock()
		return nil, nil
	})
	return err
}

func (c *OpenRouterCatalog) fetch(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, openrouterCatalogHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sub2api-openrouter-catalog/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("openrouter catalog returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, openrouterCatalogBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > openrouterCatalogBodyLimit {
		return nil, fmt.Errorf("openrouter catalog response is too large")
	}
	return parseOpenRouterCatalog(body)
}

func parseOpenRouterCatalog(body []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]string, 0, len(payload.Data))
	for _, entry := range payload.Data {
		model := strings.TrimSpace(entry.ID)
		if !isOpenRouterModelID(model) {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("openrouter catalog contained no valid models")
	}
	sort.Strings(models)
	return models, nil
}

func isOpenRouterModelID(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	return !strings.ContainsAny(model, "*?#\\\t\r\n ")
}
