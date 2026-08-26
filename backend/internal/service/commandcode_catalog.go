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
	commandCodeCatalogURL         = "https://api.commandcode.ai/provider/v1/models"
	commandCodeCatalogTTL         = time.Hour
	commandCodeCatalogHTTPTimeout = 4 * time.Second
	commandCodeCatalogBodyLimit   = 2 << 20
)

// commandCodeFallbackModels 是 Command Code Provider API 目录的离线回退，
// 覆盖官方发布的主要模型。线上以 /provider/v1/models 实时目录为准。
var commandCodeFallbackModels = []string{
	"claude-sonnet-5",
	"claude-sonnet-4-6",
	"claude-sonnet-4-5-20250929",
	"claude-fable-5",
	"claude-opus-5",
	"claude-opus-4-8",
	"claude-opus-4-7",
	"claude-opus-4-6",
	"claude-haiku-4-5-20251001",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"deepseek/deepseek-v4-pro",
	"deepseek/deepseek-v4-flash",
	"deepseek/deepseek-v4-flash-vision-exp",
	"moonshotai/Kimi-K3",
	"moonshotai/Kimi-K2.7-Code",
	"moonshotai/Kimi-K2.7-Code-Highspeed",
	"moonshotai/Kimi-K2.6",
	"moonshotai/Kimi-K2.5",
	"zai-org/GLM-5.3",
	"zai-org/GLM-5.2",
	"zai-org/GLM-5.2-Fast",
	"zai-org/GLM-5.1",
	"zai-org/GLM-5",
	"MiniMaxAI/MiniMax-M3",
	"MiniMaxAI/MiniMax-M2.7",
	"minimax/minimax-m3-free",
	"minimax/minimax-m2.7-free",
	"MiniMaxAI/MiniMax-M2.5",
	"xiaomi/mimo-v2.5-pro",
	"xiaomi/mimo-v2.5",
	"Qwen/Qwen3.8-Max",
	"Qwen/Qwen3.8-27B",
	"Qwen/Qwen3.7-Max",
	"Qwen/Qwen3.7-Plus",
	"Qwen/Qwen3.7-Flash",
	"Qwen/Qwen3.6-Max-Preview",
	"Qwen/Qwen3.6-Plus",
	"stepfun/Step-3.7-Flash",
	"stepfun/Step-3.5-Flash",
	"tencent/hy3-paid",
	"google/gemini-3.7-flash",
	"google/gemini-3.6-flash",
	"google/gemini-3.5-flash",
	"google/gemini-3.5-flash-lite",
	"google/gemini-3.1-flash-lite",
	"sakana/fugu-ultra",
	"nvidia/nemotron-3-ultra-550b-a55b",
	"thinkingmachines/inkling",
	"thinkingmachines/inkling-small",
	"stealth/ox-alpha",
	"poolside/laguna-s-2.1-free",
	"meta/muse-spark-1.1",
	"meta/muse-spark-1.2",
	"meta/muse-spark-1.2-contributor",
	"xai/grok-4.5",
	"xai/grok-4.6",
}

// CommandCodeCatalog maintains a last-known-good copy of the Command Code
// public Provider API model list.
type CommandCodeCatalog struct {
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

var defaultCommandCodeCatalog = NewCommandCodeCatalog(nil)

func NewCommandCodeCatalog(client *http.Client) *CommandCodeCatalog {
	if client == nil {
		client = &http.Client{Timeout: commandCodeCatalogHTTPTimeout}
	}
	return &CommandCodeCatalog{
		models:   append([]string(nil), commandCodeFallbackModels...),
		ttl:      commandCodeCatalogTTL,
		endpoint: commandCodeCatalogURL,
		client:   client,
		now:      time.Now,
	}
}

func CommandCodeDefaultModelIDs() []string {
	return defaultCommandCodeCatalog.ModelIDs(context.Background())
}

func CommandCodeFallbackModelIDs() []string {
	return append([]string(nil), commandCodeFallbackModels...)
}

func (c *CommandCodeCatalog) ModelIDs(ctx context.Context) []string {
	if c == nil {
		return CommandCodeFallbackModelIDs()
	}
	_ = c.refresh(ctx, false)
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.models...)
}

func (c *CommandCodeCatalog) ForceRefresh(ctx context.Context) ([]string, error) {
	if c == nil {
		return CommandCodeFallbackModelIDs(), fmt.Errorf("commandcode catalog is not configured")
	}
	err := c.refresh(ctx, true)
	c.mu.RLock()
	models := append([]string(nil), c.models...)
	c.mu.RUnlock()
	return models, err
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

		models, fetchErr := c.fetch(ctx)
		if fetchErr != nil {
			slog.Warn("commandcode_catalog_refresh_failed", "error", sanitizeUpstreamErrorMessage(fetchErr.Error()))
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

func (c *CommandCodeCatalog) fetch(ctx context.Context) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandCodeCatalogHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sub2api-commandcode-catalog/1.0")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("commandcode catalog returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, commandCodeCatalogBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > commandCodeCatalogBodyLimit {
		return nil, fmt.Errorf("commandcode catalog response is too large")
	}
	return parseCommandCodeCatalog(body)
}

func parseCommandCodeCatalog(body []byte) ([]string, error) {
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
		if !isCommandCodeModelID(model) {
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
		return nil, fmt.Errorf("commandcode catalog contained no valid models")
	}
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i]) < strings.ToLower(models[j])
	})
	return models, nil
}

func isCommandCodeModelID(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	return !strings.ContainsAny(model, "*?#\\\t\r\n ")
}
