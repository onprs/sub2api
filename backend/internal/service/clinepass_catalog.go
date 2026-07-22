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
	clinePassCatalogURL         = "https://api.cline.bot/api/v1/ai/cline/recommended-models"
	clinePassCatalogTTL         = time.Hour
	clinePassCatalogHTTPTimeout = 4 * time.Second
	clinePassCatalogBodyLimit   = 2 << 20
)

var clinePassFallbackModels = []string{
	"cline-pass/glm-5.2",
	"cline-pass/kimi-k3",
	"cline-pass/deepseek-v4-pro",
	"cline-pass/deepseek-v4-flash",
	"cline-pass/kimi-k2.7-code",
	"cline-pass/kimi-k2.6",
	"cline-pass/mimo-v2.5-pro",
	"cline-pass/mimo-v2.5",
	"cline-pass/minimax-m3",
	"cline-pass/qwen3.7-max",
	"cline-pass/qwen3.7-plus",
}

// ClinePassCatalog keeps a last-known-good copy of Cline's public model list.
type ClinePassCatalog struct {
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

var defaultClinePassCatalog = NewClinePassCatalog(nil)

func NewClinePassCatalog(client *http.Client) *ClinePassCatalog {
	if client == nil {
		client = &http.Client{Timeout: clinePassCatalogHTTPTimeout}
	}
	return &ClinePassCatalog{
		models:   append([]string(nil), clinePassFallbackModels...),
		ttl:      clinePassCatalogTTL,
		endpoint: clinePassCatalogURL,
		client:   client,
		now:      time.Now,
	}
}

func ClinePassDefaultModelIDs() []string {
	return defaultClinePassCatalog.ModelIDs(context.Background())
}

func ClinePassFallbackModelIDs() []string {
	return append([]string(nil), clinePassFallbackModels...)
}

func (c *ClinePassCatalog) ModelIDs(ctx context.Context) []string {
	if c == nil {
		return ClinePassFallbackModelIDs()
	}
	_ = c.refresh(ctx, false)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.models...)
}

func (c *ClinePassCatalog) ForceRefresh(ctx context.Context) ([]string, error) {
	if c == nil {
		return ClinePassFallbackModelIDs(), fmt.Errorf("clinepass catalog is not configured")
	}
	err := c.refresh(ctx, true)
	c.mu.RLock()
	models := append([]string(nil), c.models...)
	c.mu.RUnlock()
	return models, err
}

func (c *ClinePassCatalog) refresh(ctx context.Context, force bool) error {
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
			slog.Warn("clinepass_catalog_refresh_failed", "error", sanitizeUpstreamErrorMessage(fetchErr.Error()))
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

func (c *ClinePassCatalog) fetch(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, clinePassCatalogHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sub2api-clinepass-catalog/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("clinepass catalog returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, clinePassCatalogBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > clinePassCatalogBodyLimit {
		return nil, fmt.Errorf("clinepass catalog response is too large")
	}
	return parseClinePassCatalog(body)
}

func parseClinePassCatalog(body []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	var payload struct {
		ClinePass []string `json:"clinePass"`
	}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(payload.ClinePass))
	models := make([]string, 0, len(payload.ClinePass))
	for _, model := range payload.ClinePass {
		model = strings.TrimSpace(model)
		if !isClinePassModelID(model) {
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
		return nil, fmt.Errorf("clinepass catalog contained no valid models")
	}
	sort.Strings(models)
	return models, nil
}

func isClinePassModelID(model string) bool {
	model = strings.TrimSpace(model)
	if !strings.HasPrefix(model, "cline-pass/") || len(model) <= len("cline-pass/") {
		return false
	}
	return !strings.ContainsAny(model, "*?#\\\t\r\n ")
}
