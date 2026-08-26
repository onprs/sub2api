package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/tidwall/gjson"
)

const (
	commandCodeUsageSourceOfficialAPI = "official_api"

	commandCodeWhoamiPath          = "/alpha/whoami"
	commandCodeCreditsPath         = "/alpha/billing/credits"
	commandCodeSubscriptionsPath   = "/alpha/billing/subscriptions"
	commandCodeChatCompletionsPath = "/provider/v1/chat/completions"
	commandCodeMessagesPath        = "/provider/v1/messages"

	commandCodeResponseBodyLimit = 2 << 20
)

// commandCodePlanMonthlyCredits 是官方 CLI 内置的计划月额度表（credits）。
// monthlyCredits 接口返回的是剩余额度，30d 窗口的总额度需要由此表推导。
var commandCodePlanMonthlyCredits = map[string]float64{
	"individual-go":       10,
	"individual-goat":     70,
	"individual-pro":      30,
	"individual-pro-v1":   80,
	"individual-provider": 15,
	"individual-max":      150,
	"individual-ultra":    300,
	"teams-pro":           40,
}

// CommandCodeUsageWindow 是单个滚动窗口的用量。
type CommandCodeUsageWindow struct {
	Used    float64
	Cap     float64
	ResetAt *time.Time
}

// CommandCodeUsageSnapshot 是一次官方用量抓取的快照。
type CommandCodeUsageSnapshot struct {
	FetchedAt          time.Time
	FiveHour           *CommandCodeUsageWindow
	Weekly             *CommandCodeUsageWindow
	MonthlyRemaining   float64
	PurchasedRemaining float64
	FreeRemaining      float64
	PlanID             string
	PlanMonthlyCap     float64
	PeriodEnd          *time.Time
}

// CommandCodeError 表示 Command Code 上游返回的错误响应。
type CommandCodeError struct {
	Status  int
	Code    string
	Type    string
	Message string
	Raw     []byte
}

func (e *CommandCodeError) Error() string {
	if e == nil {
		return "commandcode upstream error"
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("commandcode upstream returned HTTP %d", e.Status)
}

func (e *CommandCodeError) EffectiveStatus() int {
	if e == nil {
		return http.StatusBadGateway
	}
	if e.Status != 0 {
		return e.Status
	}
	return http.StatusBadGateway
}

// CommandCodeClient performs authenticated Command Code API requests.
type CommandCodeClient struct {
	httpUpstream        HTTPUpstream
	cfg                 *config.Config
	tlsFPProfileService *TLSFingerprintProfileService
}

func NewCommandCodeClient(httpUpstream HTTPUpstream, cfg *config.Config, tlsFPProfileService *TLSFingerprintProfileService) *CommandCodeClient {
	return &CommandCodeClient{httpUpstream: httpUpstream, cfg: cfg, tlsFPProfileService: tlsFPProfileService}
}

// FetchUsage 调用官方 alpha 计费端点获取 5h/7d 滚动窗口与月额度池。
// 端点与官方 CLI（command-code npm 包）使用的私有接口一致，使用 Bearer API key 鉴权。
func (c *CommandCodeClient) FetchUsage(ctx context.Context, account *Account) (*CommandCodeUsageSnapshot, error) {
	if c == nil || c.httpUpstream == nil {
		return nil, fmt.Errorf("commandcode HTTP client is not configured")
	}
	if account == nil || !account.IsCommandCodeAPIKey() {
		return nil, fmt.Errorf("commandcode account must use API key credentials")
	}
	apiKey := account.GetCommandCodeAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("commandcode account is missing api_key")
	}

	whoamiBody, err := c.getJSON(ctx, account, apiKey, commandCodeWhoamiPath)
	if err != nil {
		return nil, err
	}
	orgID := strings.TrimSpace(gjson.GetBytes(whoamiBody, "org.id").String())

	creditsBody, err := c.getJSON(ctx, account, apiKey, commandCodeCreditsPath, orgID)
	if err != nil {
		return nil, err
	}
	snapshot := &CommandCodeUsageSnapshot{FetchedAt: time.Now().UTC()}
	parseCommandCodeCredits(creditsBody, snapshot)

	if subscriptionBody, subErr := c.getJSON(ctx, account, apiKey, commandCodeSubscriptionsPath, orgID); subErr == nil {
		parseCommandCodeSubscription(subscriptionBody, snapshot)
	} else {
		// 订阅信息缺失不阻断 5h/7d 窗口；月额度池降级为仅剩余值。
		snapshot.PeriodEnd = nil
	}
	if snapshot.PlanMonthlyCap == 0 {
		snapshot.PlanMonthlyCap = commandCodePlanMonthlyCredits[snapshot.PlanID]
	}
	return snapshot, nil
}

func parseCommandCodeCredits(body []byte, snapshot *CommandCodeUsageSnapshot) {
	credits := gjson.GetBytes(body, "credits")
	if !credits.Exists() {
		return
	}
	snapshot.MonthlyRemaining = credits.Get("monthlyCredits").Float()
	snapshot.PurchasedRemaining = credits.Get("purchasedCredits").Float()
	snapshot.FreeRemaining = credits.Get("freeCredits").Float()
	snapshot.PlanID = strings.TrimSpace(credits.Get("planId").String())

	if window := credits.Get("windowLimits.fiveHour"); window.Exists() {
		snapshot.FiveHour = parseCommandCodeUsageWindow(window)
	}
	if window := credits.Get("windowLimits.weekly"); window.Exists() {
		snapshot.Weekly = parseCommandCodeUsageWindow(window)
	}
}

func parseCommandCodeSubscription(body []byte, snapshot *CommandCodeUsageSnapshot) {
	data := gjson.GetBytes(body, "data")
	if !data.Exists() {
		return
	}
	if planID := strings.TrimSpace(data.Get("planId").String()); planID != "" {
		snapshot.PlanID = planID
	}
	if end := parseCommandCodeTime(data.Get("currentPeriodEnd")); end != nil {
		snapshot.PeriodEnd = end
	}
}

func parseCommandCodeUsageWindow(window gjson.Result) *CommandCodeUsageWindow {
	used := window.Get("used").Float()
	cap := window.Get("cap").Float()
	if cap <= 0 && used <= 0 {
		return nil
	}
	result := &CommandCodeUsageWindow{Used: used, Cap: cap}
	if resetAt := parseCommandCodeTime(window.Get("resetAt")); resetAt != nil {
		result.ResetAt = resetAt
	}
	return result
}

// parseCommandCodeTime 兼容 epoch 毫秒/秒数值与 RFC3339 字符串两种时间表示。
func parseCommandCodeTime(value gjson.Result) *time.Time {
	if !value.Exists() {
		return nil
	}
	switch value.Type {
	case gjson.Number:
		epoch := value.Float()
		if epoch <= 0 {
			return nil
		}
		// 官方 CLI 使用毫秒；小于 1e12 视为秒。
		var parsed time.Time
		if epoch < 1e12 {
			parsed = time.Unix(int64(epoch), 0)
		} else {
			parsed = time.Unix(0, int64(epoch)*int64(time.Millisecond))
		}
		utc := parsed.UTC()
		return &utc
	case gjson.String:
		raw := strings.TrimSpace(value.String())
		if raw == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			utc := parsed.UTC()
			return &utc
		}
		return nil
	default:
		return nil
	}
}

func (c *CommandCodeClient) getJSON(ctx context.Context, account *Account, apiKey, endpointPath string, orgID ...string) ([]byte, error) {
	endpoint, err := c.endpointURL(account, endpointPath)
	if err != nil {
		return nil, err
	}
	if len(orgID) > 0 && strings.TrimSpace(orgID[0]) != "" {
		u, parseErr := url.Parse(endpoint)
		if parseErr != nil {
			return nil, parseErr
		}
		query := u.Query()
		query.Set("orgId", strings.TrimSpace(orgID[0]))
		u.RawQuery = query.Encode()
		endpoint = u.String()
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
	body, err := readCommandCodeBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, decodeCommandCodeError(resp.StatusCode, resp.Header, body)
	}
	return body, nil
}

func (c *CommandCodeClient) endpointURL(account *Account, endpoint string) (string, error) {
	if account == nil {
		return "", fmt.Errorf("commandcode account is required")
	}
	baseURL, err := validateCommandCodeBaseURL(c.cfg, account.GetCommandCodeBaseURL())
	if err != nil {
		return "", err
	}
	return appendCommandCodePath(baseURL, endpoint)
}

func (c *CommandCodeClient) do(account *Account, req *http.Request) (*http.Response, error) {
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

func validateCommandCodeBaseURL(cfg *config.Config, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultCommandCodeBaseURL
	}
	if cfg == nil {
		normalized, err := urlvalidator.ValidateURLFormat(raw, false)
		if err != nil {
			return "", fmt.Errorf("invalid Command Code base_url: %w", err)
		}
		return normalized, nil
	}
	if !cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(raw, cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", fmt.Errorf("invalid Command Code base_url: %w", err)
		}
		return normalized, nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", fmt.Errorf("invalid Command Code base_url: %w", err)
	}
	return normalized, nil
}

func appendCommandCodePath(baseURL, endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid Command Code base URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("commandcode base URL must not contain query or fragment")
	}
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), endpoint) {
		return u.String(), nil
	}
	u.Path = path.Join(u.Path, endpoint)
	return u.String(), nil
}

func readCommandCodeBody(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(reader, commandCodeResponseBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > commandCodeResponseBodyLimit {
		return nil, fmt.Errorf("commandcode response is too large")
	}
	return body, nil
}

// decodeCommandCodeError 解析 OpenAI/Anthropic 两种错误信封。
func decodeCommandCodeError(statusCode int, _ http.Header, body []byte) *CommandCodeError {
	errResp := &CommandCodeError{Status: statusCode, Raw: body}
	errValue := gjson.GetBytes(body, "error")
	if !errValue.Exists() {
		errValue = gjson.ParseBytes(body)
	}
	if errValue.IsObject() {
		errResp.Message = strings.TrimSpace(errValue.Get("message").String())
		errResp.Code = strings.TrimSpace(errValue.Get("code").String())
		errResp.Type = strings.TrimSpace(errValue.Get("type").String())
	} else if errValue.Type == gjson.String {
		errResp.Message = strings.TrimSpace(errValue.String())
	}
	if errResp.Message == "" && len(body) > 0 {
		// 非结构化错误体：截断后作为消息返回，便于排障。
		errResp.Message = truncateString(string(body), 512)
	}
	if errResp.Message == "" {
		errResp.Message = http.StatusText(statusCode)
	}
	return errResp
}
