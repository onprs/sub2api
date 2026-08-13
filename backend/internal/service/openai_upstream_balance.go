package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

const (
	openAIUpstreamBalanceStatusAvailable   = "available"
	openAIUpstreamBalanceStatusUnsupported = "unsupported"
	openAIUpstreamBalanceStatusError       = "error"

	openAIUpstreamBalanceKindWallet       = "wallet"
	openAIUpstreamBalanceKindAPIKeyQuota  = "api_key_quota"
	openAIUpstreamBalanceKindSubscription = "subscription"
	openAIUpstreamBalanceKindRateLimits   = "rate_limits"

	openAIUpstreamBalanceRequestTimeout = 10 * time.Second
	openAIUpstreamBalanceMaxBodyBytes   = 64 * 1024
)

// UpstreamBalanceInfo 表示 OpenAI API Key 账户从上游 sub2api `/v1/usage`
// 实时读取到的可用金额。该数据只随当前响应返回，不写入账户表。
type UpstreamBalanceInfo struct {
	Status       string     `json:"status"`
	Source       string     `json:"source,omitempty"`
	Kind         string     `json:"kind,omitempty"`
	Amount       *float64   `json:"amount,omitempty"`
	Limit        *float64   `json:"limit,omitempty"`
	Used         *float64   `json:"used,omitempty"`
	Unit         string     `json:"unit,omitempty"`
	Mode         string     `json:"mode,omitempty"`
	PlanName     string     `json:"plan_name,omitempty"`
	IsValid      *bool      `json:"is_valid,omitempty"`
	RemoteStatus string     `json:"remote_status,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
}

type openAIUpstreamUsageQuota struct {
	Limit     *float64 `json:"limit"`
	Used      *float64 `json:"used"`
	Remaining *float64 `json:"remaining"`
	Unit      string   `json:"unit"`
}

type openAIUpstreamUsageResponse struct {
	Object        string                    `json:"object"`
	SchemaVersion int                       `json:"schema_version"`
	Mode          string                    `json:"mode"`
	IsValid       *bool                     `json:"isValid"`
	Status        string                    `json:"status"`
	PlanName      string                    `json:"planName"`
	Remaining     *float64                  `json:"remaining"`
	Unit          string                    `json:"unit"`
	Balance       *float64                  `json:"balance"`
	Quota         *openAIUpstreamUsageQuota `json:"quota"`
	RateLimits    json.RawMessage           `json:"rate_limits"`
	Subscription  json.RawMessage           `json:"subscription"`
}

// getOpenAIAPIKeyUpstreamBalance 对 OpenAI API Key 自定义上游做一次实时探测。
// 非 sub2api 上游通常返回 404/405 或不同结构，此时按 unsupported 降级，
// 不影响账户状态、调度或凭据。
func (s *AccountUsageService) getOpenAIAPIKeyUpstreamBalance(ctx context.Context, account *Account) (*UsageInfo, error) {
	now := time.Now().UTC()
	usage := &UsageInfo{UpdatedAt: &now}
	usage.UpstreamBalance = &UpstreamBalanceInfo{
		Status:    openAIUpstreamBalanceStatusUnsupported,
		UpdatedAt: &now,
	}

	if account == nil || !account.IsOpenAIApiKey() {
		return usage, nil
	}

	baseURL := strings.TrimSpace(account.GetOpenAIBaseURL())
	if baseURL == "" || isOfficialOpenAIUpstreamBalanceTarget(baseURL) {
		usage.UpstreamBalance.ErrorCode = "unsupported"
		return usage, nil
	}
	if s == nil || s.httpUpstream == nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "transport_unavailable"
		return usage, nil
	}

	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "missing_api_key"
		return usage, nil
	}

	normalizedBaseURL, err := validateOpenAIAPIKeyBaseURL(baseURL, s.cfg)
	if err != nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "invalid_base_url"
		return usage, nil
	}

	proxyURL := ""
	if account.ProxyID != nil {
		if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
			usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
			usage.UpstreamBalance.ErrorCode = "proxy_unavailable"
			return usage, nil
		}
		proxyURL = account.Proxy.URL()
	}

	requestCtx, cancel := context.WithTimeout(ctx, openAIUpstreamBalanceRequestTimeout)
	defer cancel()
	requestCtx = WithHTTPUpstreamProfile(requestCtx, HTTPUpstreamProfileOpenAI)
	requestCtx = WithHTTPUpstreamRedirectsDisabled(requestCtx)

	usageURL := buildOpenAIUpstreamUsageURL(normalizedBaseURL, now)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, usageURL, bytes.NewReader(nil))
	if err != nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "request_build_failed"
		return usage, nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	account.ApplyHeaderOverrides(req.Header)

	var tlsProfile = s.resolveAccountUsageTLSProfile(account)
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, tlsProfile)
	if err != nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "request_failed"
		return usage, nil
	}
	if resp == nil || resp.Body == nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "empty_response"
		return usage, nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, openAIUpstreamBalanceMaxBodyBytes+1))
	if readErr != nil {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "response_read_failed"
		return usage, nil
	}
	if len(body) > openAIUpstreamBalanceMaxBodyBytes {
		usage.UpstreamBalance.ErrorCode = "response_too_large"
		return usage, nil
	}

	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		usage.UpstreamBalance.ErrorCode = "unsupported"
		return usage, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "unauthorized"
		return usage, nil
	case http.StatusTooManyRequests:
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "rate_limited"
		return usage, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		usage.UpstreamBalance.Status = openAIUpstreamBalanceStatusError
		usage.UpstreamBalance.ErrorCode = "http_error"
		return usage, nil
	}

	balance, ok := parseOpenAIUpstreamUsageBalance(body, now)
	if !ok {
		// 2xx 但结构不符合 sub2api `/v1/usage` 契约，视为普通兼容上游。
		usage.UpstreamBalance.ErrorCode = "invalid_response"
		return usage, nil
	}
	usage.UpstreamBalance = balance
	return usage, nil
}

func buildOpenAIUpstreamUsageURL(baseURL string, now time.Time) string {
	usageURL := buildOpenAIEndpointURL(baseURL, "/v1/usage")
	parsed, err := url.Parse(usageURL)
	if err != nil {
		return usageURL
	}

	// sub2api 的 /v1/usage 默认附带 30 天明细；余额探测只请求当天数据，
	// 降低列表实时刷新时的数据库与响应体开销。旧版 sub2api 会忽略未知参数。
	date := now.Format(time.DateOnly)
	query := parsed.Query()
	query.Set("days", "1")
	query.Set("start_date", date)
	query.Set("end_date", date)
	query.Set("summary_only", "true")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *AccountUsageService) resolveAccountUsageTLSProfile(account *Account) *tlsfingerprint.Profile {
	if s == nil || s.tlsFPProfileService == nil {
		return nil
	}
	return s.tlsFPProfileService.ResolveTLSProfile(account)
}

func parseOpenAIUpstreamUsageBalance(body []byte, now time.Time) (*UpstreamBalanceInfo, bool) {
	var payload openAIUpstreamUsageResponse
	if err := json.Unmarshal(body, &payload); err != nil || payload.IsValid == nil {
		return nil, false
	}

	payload.Mode = strings.TrimSpace(payload.Mode)
	unit := strings.ToUpper(strings.TrimSpace(payload.Unit))
	if payload.Object != "" && payload.Object != "sub2api.usage" {
		return nil, false
	}
	if payload.SchemaVersion != 0 && payload.SchemaVersion != 1 {
		return nil, false
	}
	if payload.Mode != "unrestricted" && payload.Mode != "quota_limited" {
		return nil, false
	}
	if unit == "" {
		unit = "USD"
	}
	if unit != "USD" {
		return nil, false
	}

	result := &UpstreamBalanceInfo{
		Status:       openAIUpstreamBalanceStatusAvailable,
		Source:       "sub2api",
		Unit:         unit,
		Mode:         payload.Mode,
		PlanName:     strings.TrimSpace(payload.PlanName),
		IsValid:      payload.IsValid,
		RemoteStatus: strings.TrimSpace(payload.Status),
		UpdatedAt:    &now,
	}

	if payload.Mode == "quota_limited" {
		if payload.Quota != nil && validFiniteAmount(payload.Quota.Limit) && validFiniteAmount(payload.Quota.Used) && validFiniteAmount(payload.Quota.Remaining) {
			quotaUnit := strings.ToUpper(strings.TrimSpace(payload.Quota.Unit))
			if quotaUnit != "" && quotaUnit != "USD" {
				return nil, false
			}
			result.Kind = openAIUpstreamBalanceKindAPIKeyQuota
			result.Amount = cloneFloat64Pointer(payload.Quota.Remaining)
			result.Limit = cloneFloat64Pointer(payload.Quota.Limit)
			result.Used = cloneFloat64Pointer(payload.Quota.Used)
			return result, true
		}
		if len(payload.RateLimits) > 0 && string(payload.RateLimits) != "null" {
			result.Kind = openAIUpstreamBalanceKindRateLimits
			return result, true
		}
		return nil, false
	}

	if validFiniteAmount(payload.Balance) {
		result.Kind = openAIUpstreamBalanceKindWallet
		result.Amount = cloneFloat64Pointer(payload.Balance)
		return result, true
	}
	if validFiniteAmount(payload.Remaining) && len(payload.Subscription) > 0 && string(payload.Subscription) != "null" {
		result.Kind = openAIUpstreamBalanceKindSubscription
		if *payload.Remaining >= 0 {
			result.Amount = cloneFloat64Pointer(payload.Remaining)
		}
		return result, true
	}
	return nil, false
}

func validFiniteAmount(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func isOfficialOpenAIUpstreamBalanceTarget(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	return host == "openai.com" || strings.HasSuffix(host, ".openai.com") ||
		host == "openai.azure.com" || strings.HasSuffix(host, ".openai.azure.com")
}
