package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultOpenCodeGoConsoleBaseURL = "https://opencode.ai"

var (
	ErrOpenCodeGoConsoleAuthExpired = errors.New("opencode go console auth expired")
	ErrOpenCodeGoConsolePageInvalid = errors.New("opencode go console page invalid")
)

type OpenCodeGoConsoleClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewOpenCodeGoConsoleClient(baseURL string, httpClient *http.Client) *OpenCodeGoConsoleClient {
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
	return &OpenCodeGoConsoleClient{baseURL: baseURL, httpClient: httpClient}
}

func (c *OpenCodeGoConsoleClient) FetchSummary(ctx context.Context, workspaceID, consoleCookie string) (*OpenCodeGoConsoleSummary, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	consoleCookie = strings.TrimSpace(consoleCookie)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if consoleCookie == "" {
		return nil, fmt.Errorf("%w: missing console cookie", ErrOpenCodeGoConsoleAuthExpired)
	}

	endpoint := c.workspaceGoURL(workspaceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", consoleCookie)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	bodyText := string(body)
	summary, err := ParseOpenCodeGoConsolePage(bodyText, time.Now().UTC())
	if err == nil {
		if summary.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("%w: workspace mismatch", ErrOpenCodeGoConsolePageInvalid)
		}
		return summary, nil
	}
	if isOpenCodeGoConsoleAuthExpiredResponse(resp) {
		return nil, ErrOpenCodeGoConsoleAuthExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: upstream status %d", ErrOpenCodeGoConsolePageInvalid, resp.StatusCode)
	}
	return nil, fmt.Errorf("%w: %v", ErrOpenCodeGoConsolePageInvalid, err)
}

func (c *OpenCodeGoConsoleClient) workspaceGoURL(workspaceID string) string {
	return c.baseURL + "/workspace/" + url.PathEscape(workspaceID) + "/go"
}

func isOpenCodeGoConsoleAuthExpiredResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return true
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := strings.ToLower(resp.Header.Get("Location"))
		return strings.Contains(location, "/auth") || strings.Contains(location, "login")
	}
	if resp.Request != nil && resp.Request.URL != nil {
		path := strings.ToLower(resp.Request.URL.Path)
		return strings.Contains(path, "/auth") || strings.Contains(path, "login")
	}
	return false
}
