package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrOpenCodeGoReferralRewardAlreadyApplied = errors.New("opencode go referral reward already applied")

var errOpenCodeGoReferralActionHashStale = errors.New("opencode go referral action hash stale")

type openCodeGoReferralActionCacheEntry struct {
	actions  OpenCodeGoServerActions
	cachedAt time.Time
}

var openCodeGoReferralActionCache = struct {
	sync.Mutex
	entries map[string]openCodeGoReferralActionCacheEntry
}{
	entries: map[string]openCodeGoReferralActionCacheEntry{},
}

type openCodeGoReferralActionStaleHashError struct {
	reason string
}

func (e openCodeGoReferralActionStaleHashError) Error() string {
	if e.reason == "" {
		return errOpenCodeGoReferralActionHashStale.Error()
	}
	return errOpenCodeGoReferralActionHashStale.Error() + ": " + e.reason
}

func (e openCodeGoReferralActionStaleHashError) Is(target error) bool {
	return target == errOpenCodeGoReferralActionHashStale
}

type OpenCodeGoReferralUsagePreview struct {
	RollingUsage OpenCodeGoReferralUsagePreviewWindow `json:"rollingUsage"`
	WeeklyUsage  OpenCodeGoReferralUsagePreviewWindow `json:"weeklyUsage"`
	MonthlyUsage OpenCodeGoReferralUsagePreviewWindow `json:"monthlyUsage"`
}

type OpenCodeGoReferralUsagePreviewWindow struct {
	BeforePercent float64 `json:"beforePercent"`
	AfterPercent  float64 `json:"afterPercent"`
	ResetInSec    int     `json:"resetInSec"`
}

type OpenCodeGoReferralActionClient struct {
	baseURL    string
	httpClient *http.Client
	cacheTTL   time.Duration
}

func NewOpenCodeGoReferralActionClient(baseURL string, httpClient *http.Client) *OpenCodeGoReferralActionClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenCodeGoConsoleBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &OpenCodeGoReferralActionClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		cacheTTL:   10 * time.Minute,
	}
}

func EncodeOpenCodeGoSolidStringArgs(args ...string) ([]byte, error) {
	type serovalStringNode struct {
		T int    `json:"t"`
		S string `json:"s"`
	}
	type serovalArrayNode struct {
		T int                 `json:"t"`
		I int                 `json:"i"`
		L int                 `json:"l"`
		A []serovalStringNode `json:"a"`
		O int                 `json:"o"`
	}
	type serovalPayload struct {
		T serovalArrayNode `json:"t"`
		F int              `json:"f"`
		M []any            `json:"m"`
	}
	nodes := make([]serovalStringNode, 0, len(args))
	for _, arg := range args {
		nodes = append(nodes, serovalStringNode{T: 1, S: arg})
	}
	return json.Marshal(serovalPayload{
		T: serovalArrayNode{T: 9, I: 0, L: len(args), A: nodes, O: 0},
		F: 31,
		M: []any{},
	})
}

func (c *OpenCodeGoReferralActionClient) Preview(ctx context.Context, workspaceID, referralID, consoleCookie string) (*OpenCodeGoReferralUsagePreview, error) {
	body, err := c.postResolvedServerAction(ctx, workspaceID, referralID, consoleCookie, func(actions OpenCodeGoServerActions) string {
		return actions.ReferralUsagePreview
	})
	if err != nil {
		return nil, err
	}
	return parseOpenCodeGoReferralUsagePreviewResponse(body)
}

func (c *OpenCodeGoReferralActionClient) Apply(ctx context.Context, workspaceID, referralID, consoleCookie string) error {
	_, err := c.postResolvedServerAction(ctx, workspaceID, referralID, consoleCookie, func(actions OpenCodeGoServerActions) string {
		return actions.ReferralRewardApply
	})
	return err
}

func (c *OpenCodeGoReferralActionClient) ResolveActions(ctx context.Context, workspaceID, consoleCookie string) (OpenCodeGoServerActions, error) {
	return c.resolveActions(ctx, workspaceID, consoleCookie, false)
}

func (c *OpenCodeGoReferralActionClient) resolveActions(ctx context.Context, workspaceID, consoleCookie string, force bool) (OpenCodeGoServerActions, error) {
	if !force {
		openCodeGoReferralActionCache.Lock()
		if entry, ok := openCodeGoReferralActionCache.entries[c.actionCacheKey()]; ok &&
			time.Since(entry.cachedAt) < c.cacheTTL &&
			entry.actions.ReferralRewardApply != "" &&
			entry.actions.ReferralUsagePreview != "" {
			actions := entry.actions
			openCodeGoReferralActionCache.Unlock()
			return actions, nil
		}
		openCodeGoReferralActionCache.Unlock()
	}

	html, err := c.fetchText(ctx, c.workspaceGoURL(workspaceID), consoleCookie)
	if err != nil {
		return OpenCodeGoServerActions{}, err
	}
	var actions OpenCodeGoServerActions
	for _, href := range prioritizeOpenCodeGoConsoleJSAssetHrefs(extractOpenCodeGoConsoleJSAssetHrefs(html)) {
		assetURL := c.absoluteURL(href)
		js, err := c.fetchText(ctx, assetURL, consoleCookie)
		if err != nil {
			continue
		}
		applyOpenCodeGoActionMappings(&actions, parseOpenCodeGoServerActionMappings(js))
		if actions.ReferralRewardApply != "" && actions.ReferralUsagePreview != "" {
			openCodeGoReferralActionCache.Lock()
			openCodeGoReferralActionCache.entries[c.actionCacheKey()] = openCodeGoReferralActionCacheEntry{
				actions:  actions,
				cachedAt: time.Now(),
			}
			openCodeGoReferralActionCache.Unlock()
			return actions, nil
		}
	}
	if actions.ReferralUsagePreview == "" {
		return actions, fmt.Errorf("opencode go action hash not found: go.referral.usagePreview")
	}
	if actions.ReferralRewardApply == "" {
		return actions, fmt.Errorf("opencode go action hash not found: go.referral.reward.apply")
	}
	openCodeGoReferralActionCache.Lock()
	openCodeGoReferralActionCache.entries[c.actionCacheKey()] = openCodeGoReferralActionCacheEntry{
		actions:  actions,
		cachedAt: time.Now(),
	}
	openCodeGoReferralActionCache.Unlock()
	return actions, nil
}

func (c *OpenCodeGoReferralActionClient) postResolvedServerAction(
	ctx context.Context,
	workspaceID string,
	referralID string,
	consoleCookie string,
	selectServerID func(OpenCodeGoServerActions) string,
) ([]byte, error) {
	actions, err := c.ResolveActions(ctx, workspaceID, consoleCookie)
	if err != nil {
		return nil, err
	}
	body, err := c.postServerAction(ctx, selectServerID(actions), workspaceID, referralID, consoleCookie)
	if !errors.Is(err, errOpenCodeGoReferralActionHashStale) {
		return body, err
	}
	c.invalidateCachedActions()
	actions, resolveErr := c.resolveActions(ctx, workspaceID, consoleCookie, true)
	if resolveErr != nil {
		return nil, resolveErr
	}
	return c.postServerAction(ctx, selectServerID(actions), workspaceID, referralID, consoleCookie)
}

func (c *OpenCodeGoReferralActionClient) invalidateCachedActions() {
	openCodeGoReferralActionCache.Lock()
	delete(openCodeGoReferralActionCache.entries, c.actionCacheKey())
	openCodeGoReferralActionCache.Unlock()
}

func (c *OpenCodeGoReferralActionClient) actionCacheKey() string {
	return c.baseURL
}

func (c *OpenCodeGoReferralActionClient) postServerAction(ctx context.Context, serverID, workspaceID, referralID, consoleCookie string) ([]byte, error) {
	body, err := EncodeOpenCodeGoSolidStringArgs(workspaceID, referralID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_server", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", consoleCookie)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Server-Id", serverID)
	req.Header.Set("X-Server-Instance", "server-fn:"+randomOpenCodeGoServerInstanceID())
	req.Header.Set("Origin", c.origin())
	req.Header.Set("Referer", c.workspaceGoURL(workspaceID))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if readErr != nil {
		return nil, readErr
	}
	if isOpenCodeGoConsoleAuthExpiredResponse(resp) {
		return nil, ErrOpenCodeGoConsoleAuthExpired
	}
	if resp.Header.Get("X-Error") != "" {
		if strings.Contains(strings.ToLower(string(raw)), "already applied") {
			return nil, ErrOpenCodeGoReferralRewardAlreadyApplied
		}
		return nil, openCodeGoReferralActionStaleHashError{reason: "x-error " + strings.TrimSpace(string(raw))}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusConflict && strings.Contains(strings.ToLower(string(raw)), "already applied") {
			return nil, ErrOpenCodeGoReferralRewardAlreadyApplied
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, openCodeGoReferralActionStaleHashError{reason: "status 404"}
		}
		return nil, fmt.Errorf("opencode go referral action status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func (c *OpenCodeGoReferralActionClient) fetchText(ctx context.Context, endpoint, consoleCookie string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	if consoleCookie != "" {
		req.Header.Set("Cookie", consoleCookie)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if isOpenCodeGoConsoleAuthExpiredResponse(resp) {
		return "", ErrOpenCodeGoConsoleAuthExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch opencode go console asset status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *OpenCodeGoReferralActionClient) workspaceGoURL(workspaceID string) string {
	return c.baseURL + "/workspace/" + url.PathEscape(workspaceID) + "/go"
}

func (c *OpenCodeGoReferralActionClient) absoluteURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "/") {
		return c.baseURL + href
	}
	return c.baseURL + "/" + href
}

func (c *OpenCodeGoReferralActionClient) origin() string {
	parsed, err := url.Parse(c.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return defaultOpenCodeGoConsoleBaseURL
	}
	return parsed.Scheme + "://" + parsed.Host
}

var openCodeGoJSAssetHrefPattern = regexp.MustCompile(`href="([^"]*/_build/assets/[^"]+\.js)"`)

func extractOpenCodeGoConsoleJSAssetHrefs(html string) []string {
	seen := map[string]bool{}
	var hrefs []string
	for _, match := range openCodeGoJSAssetHrefPattern.FindAllStringSubmatch(html, -1) {
		if len(match) != 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		hrefs = append(hrefs, match[1])
	}
	return hrefs
}

func prioritizeOpenCodeGoConsoleJSAssetHrefs(hrefs []string) []string {
	out := append([]string(nil), hrefs...)
	sort.SliceStable(out, func(i, j int) bool {
		return openCodeGoConsoleJSAssetPriority(out[i]) < openCodeGoConsoleJSAssetPriority(out[j])
	})
	return out
}

func openCodeGoConsoleJSAssetPriority(href string) int {
	name := href
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	switch {
	case strings.HasPrefix(name, "index-"):
		return 0
	case strings.HasPrefix(name, "workspace-"), strings.HasPrefix(name, "_id_-"):
		return 1
	case strings.Contains(name, "query"), strings.Contains(name, "action"):
		return 2
	default:
		return 3
	}
}

func parseOpenCodeGoReferralUsagePreviewResponse(raw []byte) (*OpenCodeGoReferralUsagePreview, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty opencode go referral preview response")
	}
	var envelope struct {
		Data *OpenCodeGoReferralUsagePreview `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Data != nil {
		return envelope.Data, nil
	}
	var direct OpenCodeGoReferralUsagePreview
	if err := json.Unmarshal(raw, &direct); err == nil && (direct.RollingUsage.ResetInSec != 0 || direct.RollingUsage.BeforePercent != 0 || direct.RollingUsage.AfterPercent != 0) {
		return &direct, nil
	}
	return parseOpenCodeGoReferralUsagePreviewFromSerovalStream(raw)
}

func parseOpenCodeGoReferralUsagePreviewFromSerovalStream(raw []byte) (*OpenCodeGoReferralUsagePreview, error) {
	text := string(raw)
	if strings.Contains(text, "rollingUsage") && strings.Contains(text, "beforePercent") {
		jsonStart := strings.Index(text, "{")
		jsonEnd := strings.LastIndex(text, "}")
		if jsonStart >= 0 && jsonEnd > jsonStart {
			var preview OpenCodeGoReferralUsagePreview
			if err := json.Unmarshal([]byte(text[jsonStart:jsonEnd+1]), &preview); err == nil {
				return &preview, nil
			}
		}
	}
	if preview, ok := parseOpenCodeGoReferralUsagePreviewFromJSObjectStream(text); ok {
		return preview, nil
	}
	return nil, fmt.Errorf("unsupported opencode go referral preview response")
}

func parseOpenCodeGoReferralUsagePreviewFromJSObjectStream(text string) (*OpenCodeGoReferralUsagePreview, bool) {
	rolling, okRolling := parseOpenCodeGoReferralPreviewWindowFromJSObject(text, "rollingUsage")
	weekly, okWeekly := parseOpenCodeGoReferralPreviewWindowFromJSObject(text, "weeklyUsage")
	monthly, okMonthly := parseOpenCodeGoReferralPreviewWindowFromJSObject(text, "monthlyUsage")
	if !okRolling || !okWeekly || !okMonthly {
		return nil, false
	}
	return &OpenCodeGoReferralUsagePreview{
		RollingUsage: rolling,
		WeeklyUsage:  weekly,
		MonthlyUsage: monthly,
	}, true
}

func parseOpenCodeGoReferralPreviewWindowFromJSObject(text, field string) (OpenCodeGoReferralUsagePreviewWindow, bool) {
	pattern := regexp.MustCompile(regexp.QuoteMeta(field) + `:\s*(?:\$R\[\d+\]=)?\{beforePercent:(-?\d+(?:\.\d+)?),afterPercent:(-?\d+(?:\.\d+)?),resetInSec:(\d+)\}`)
	match := pattern.FindStringSubmatch(text)
	if len(match) != 4 {
		return OpenCodeGoReferralUsagePreviewWindow{}, false
	}
	before, errBefore := strconv.ParseFloat(match[1], 64)
	after, errAfter := strconv.ParseFloat(match[2], 64)
	resetInSec, errReset := strconv.Atoi(match[3])
	if errBefore != nil || errAfter != nil || errReset != nil {
		return OpenCodeGoReferralUsagePreviewWindow{}, false
	}
	return OpenCodeGoReferralUsagePreviewWindow{
		BeforePercent: before,
		AfterPercent:  after,
		ResetInSec:    resetInSec,
	}, true
}

func randomOpenCodeGoServerInstanceID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
