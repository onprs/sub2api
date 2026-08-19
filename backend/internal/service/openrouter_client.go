package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	openrouterUsageSourceOfficialAPI = "official_api"
	openrouterUsagePath              = "/key"
	openrouterChatCompletionsPath    = "/chat/completions"
	openrouterResponseBodyLimit      = 4 << 20
)

type OpenRouterUsageSnapshot struct {
	FetchedAt       time.Time
	Label           string
	UsageUSD        float64
	LimitUSD        *float64
	LimitRemaining  *float64
	IsFreeTier      bool
	RateLimitReqs   *int
	RateLimitPeriod string
}

// OpenRouterClient performs authenticated, account-scoped OpenRouter API requests.
type OpenRouterClient struct {
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
}

func NewOpenRouterClient(httpUpstream HTTPUpstream, cfg *config.Config, tlsFPProfileService *TLSFingerprintProfileService) *OpenRouterClient {
	return &OpenRouterClient{httpUpstream: httpUpstream, cfg: cfg, tlsFPProfileService: tlsFPProfileService}
}

func (c *OpenRouterClient) FetchUsage(ctx context.Context, account *Account) (*OpenRouterUsageSnapshot, error) {
	if c == nil || c.httpUpstream == nil {
		return nil, fmt.Errorf("openrouter HTTP client is not configured")
	}
	if account == nil || !account.IsOpenRouterAPIKey() {
		return nil, fmt.Errorf("openrouter account must use API key credentials")
	}
	apiKey := account.GetOpenRouterAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter account is missing api_key")
	}
	endpoint, err := c.endpointURL(account, openrouterUsagePath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://sub2api.local")
	req.Header.Set("X-Title", "sub2api")

	resp, err := c.do(account, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readOpenRouterBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, decodeOpenRouterError(resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data struct {
			Label          string   `json:"label"`
			Usage          *float64 `json:"usage"`
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
			IsFreeTier     bool     `json:"is_free_tier"`
			RateLimit      struct {
				Requests *int   `json:"requests"`
				Interval string `json:"interval"`
			} `json:"rate_limit"`
		} `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode openrouter usage response: %w", err)
	}
	if len(payload.Error) > 0 && string(payload.Error) != "null" {
		return nil, decodeOpenRouterError(resp.StatusCode, resp.Header, body)
	}

	snapshot := &OpenRouterUsageSnapshot{
		FetchedAt:       time.Now().UTC(),
		Label:           strings.TrimSpace(payload.Data.Label),
		IsFreeTier:      payload.Data.IsFreeTier,
		RateLimitReqs:   payload.Data.RateLimit.Requests,
		RateLimitPeriod: strings.TrimSpace(payload.Data.RateLimit.Interval),
	}
	if payload.Data.Usage != nil && !math.IsNaN(*payload.Data.Usage) && !math.IsInf(*payload.Data.Usage, 0) {
		snapshot.UsageUSD = *payload.Data.Usage
	}
	if payload.Data.Limit != nil && !math.IsNaN(*payload.Data.Limit) && !math.IsInf(*payload.Data.Limit, 0) {
		limit := *payload.Data.Limit
		snapshot.LimitUSD = &limit
	}
	if payload.Data.LimitRemaining != nil && !math.IsNaN(*payload.Data.LimitRemaining) && !math.IsInf(*payload.Data.LimitRemaining, 0) {
		rem := *payload.Data.LimitRemaining
		snapshot.LimitRemaining = &rem
	}
	return snapshot, nil
}

func (c *OpenRouterClient) endpointURL(account *Account, subPath string) (string, error) {
	baseURL := DefaultOpenRouterBaseURL
	if account != nil {
		baseURL = account.GetOpenRouterBaseURL()
	}
	normalizedBase, err := validateOpenRouterBaseURL(c.cfg, baseURL)
	if err != nil {
		return "", err
	}
	return appendOpenRouterPath(normalizedBase, subPath)
}

func (c *OpenRouterClient) do(account *Account, req *http.Request) (*http.Response, error) {
	var proxyURL string
	if account != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var profile *tlsfingerprint.Profile
	if c.tlsFPProfileService != nil && account != nil {
		profile = c.tlsFPProfileService.ResolveTLSProfile(account)
	}
	return c.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, profile)
}

func validateOpenRouterBaseURL(cfg *config.Config, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultOpenRouterBaseURL
	}
	if cfg == nil {
		normalized, err := urlvalidator.ValidateURLFormat(raw, false)
		if err != nil {
			return "", fmt.Errorf("invalid OpenRouter base_url: %w", err)
		}
		return normalized, nil
	}
	if !cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid OpenRouter base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid OpenRouter base_url: %w", err)
	}
	return normalized, nil
}

func appendOpenRouterPath(baseURL, endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid OpenRouter base URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("OpenRouter base URL must not contain query or fragment")
	}
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), endpoint) {
		return u.String(), nil
	}
	u.Path = path.Join(u.Path, endpoint)
	return u.String(), nil
}

func readOpenRouterBody(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(reader, openrouterResponseBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > openrouterResponseBodyLimit {
		return nil, fmt.Errorf("openrouter response is too large")
	}
	return body, nil
}

func normalizeOpenRouterChatRequest(body []byte, upstreamModel string) ([]byte, error) {
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
