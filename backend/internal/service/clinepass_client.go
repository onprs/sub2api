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
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	clinePassUsageSourceOfficialAPI = "official_api"
	clinePassUsagePath              = "/users/me/plan/usage-limits"
	clinePassChatCompletionsPath    = "/chat/completions"
	clinePassResponseBodyLimit      = 2 << 20
)

type ClinePassUsageWindow struct {
	Type        string
	PercentUsed float64
	ResetsAt    *time.Time
}

type ClinePassUsageSnapshot struct {
	FetchedAt time.Time
	Windows   map[string]ClinePassUsageWindow
}

// ClinePassClient performs authenticated, account-scoped Cline API requests.
type ClinePassClient struct {
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
}

func NewClinePassClient(httpUpstream HTTPUpstream, cfg *config.Config, tlsFPProfileService *TLSFingerprintProfileService) *ClinePassClient {
	return &ClinePassClient{httpUpstream: httpUpstream, cfg: cfg, tlsFPProfileService: tlsFPProfileService}
}

func (c *ClinePassClient) FetchUsage(ctx context.Context, account *Account) (*ClinePassUsageSnapshot, error) {
	if c == nil || c.httpUpstream == nil {
		return nil, fmt.Errorf("clinepass HTTP client is not configured")
	}
	if account == nil || !account.IsClinePassAPIKey() {
		return nil, fmt.Errorf("clinepass account must use API key credentials")
	}
	apiKey := account.GetClinePassAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("clinepass account is missing api_key")
	}
	endpoint, err := c.endpointURL(account, clinePassUsagePath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.do(account, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readClinePassBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, decodeClinePassError(resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Limits []struct {
				Type        string   `json:"type"`
				PercentUsed *float64 `json:"percentUsed"`
				ResetsAt    string   `json:"resetsAt"`
			} `json:"limits"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode clinepass usage response: %w", err)
	}
	if !payload.Success {
		return nil, decodeClinePassError(resp.StatusCode, resp.Header, body)
	}

	snapshot := &ClinePassUsageSnapshot{FetchedAt: time.Now().UTC(), Windows: make(map[string]ClinePassUsageWindow, 3)}
	for _, limit := range payload.Data.Limits {
		prefix, ok := clinePassUsageWindowPrefix(limit.Type)
		if !ok {
			slog.Warn("clinepass_usage_unknown_window", "type", strings.TrimSpace(limit.Type))
			continue
		}
		if limit.PercentUsed == nil || math.IsNaN(*limit.PercentUsed) || math.IsInf(*limit.PercentUsed, 0) {
			continue
		}
		percent := *limit.PercentUsed
		if percent < 0 || percent > 100 {
			slog.Warn("clinepass_usage_percent_out_of_range", "window", prefix, "value", percent)
			if percent < 0 {
				percent = 0
			} else {
				percent = 100
			}
		}
		window := ClinePassUsageWindow{Type: prefix, PercentUsed: percent}
		if raw := strings.TrimSpace(limit.ResetsAt); raw != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
				utc := parsed.UTC()
				window.ResetsAt = &utc
			} else {
				slog.Warn("clinepass_usage_invalid_reset", "window", prefix)
			}
		}
		snapshot.Windows[prefix] = window
	}
	return snapshot, nil
}

func (c *ClinePassClient) endpointURL(account *Account, endpoint string) (string, error) {
	if account == nil {
		return "", fmt.Errorf("clinepass account is required")
	}
	baseURL, err := validateClinePassBaseURL(c.cfg, account.GetClinePassBaseURL())
	if err != nil {
		return "", err
	}
	return appendClinePassPath(baseURL, endpoint)
}

func (c *ClinePassClient) do(account *Account, req *http.Request) (*http.Response, error) {
	proxyURL := ""
	if account != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var profile *tlsfingerprint.Profile
	if c.tlsFPProfileService != nil && account != nil {
		profile = c.tlsFPProfileService.ResolveTLSProfile(account)
	}
	return c.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, profile)
}

func validateClinePassBaseURL(cfg *config.Config, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultClinePassBaseURL
	}
	if cfg == nil {
		normalized, err := urlvalidator.ValidateURLFormat(raw, false)
		if err != nil {
			return "", fmt.Errorf("invalid ClinePass base_url: %w", err)
		}
		return normalized, nil
	}
	if !cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid ClinePass base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid ClinePass base_url: %w", err)
	}
	return normalized, nil
}

func appendClinePassPath(baseURL, endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid ClinePass base URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("ClinePass base URL must not contain query or fragment")
	}
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), endpoint) {
		return u.String(), nil
	}
	u.Path = path.Join(u.Path, endpoint)
	return u.String(), nil
}

func readClinePassBody(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(reader, clinePassResponseBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > clinePassResponseBodyLimit {
		return nil, fmt.Errorf("clinepass response is too large")
	}
	return body, nil
}

func clinePassUsageWindowPrefix(windowType string) (string, bool) {
	switch strings.TrimSpace(windowType) {
	case "five_hour":
		return "5h", true
	case "weekly":
		return "7d", true
	case "monthly":
		return "30d", true
	default:
		return "", false
	}
}

func normalizeClinePassChatRequest(body []byte, upstreamModel string) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	messages, ok := root["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("messages must be an array")
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("message must be an object")
		}
		if role, _ := message["role"].(string); role == "developer" {
			message["role"] = "system"
		}
	}
	root["model"] = upstreamModel
	return json.Marshal(root)
}
