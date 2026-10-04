package zcode

// 来源：src/auth/oauth.ts、resolver.ts、jwt-age.ts；固定 commit 见 UpstreamCommit。
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LoginFlow struct {
	Provider  Provider `json:"provider"`
	FlowID    string   `json:"flow_id"`
	PollToken string   `json:"poll_token"`
	ExpiresAt int64    `json:"expires_at"`
	Interval  int      `json:"interval"`
}

func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (c *Client) StartLogin(ctx context.Context, p Provider) (LoginFlow, string, error) {
	if !p.Valid() {
		return LoginFlow{}, "", &Error{Kind: ErrConfiguration}
	}
	token, err := RandomToken()
	if err != nil {
		return LoginFlow{}, "", err
	}
	h := bearer(token)
	h.Set("Content-Type", "application/json")
	data, err := c.request(ctx, "POST", c.OriginURL()+"/api/v1/oauth/cli/init", h, map[string]string{"provider": string(p)}, 15*time.Second)
	if err != nil {
		return LoginFlow{}, "", err
	}
	var init struct {
		FlowID    string `json:"flow_id"`
		Authorize string `json:"authorize_url"`
		Expires   int64  `json:"expires_at"`
		Interval  int    `json:"poll_interval_sec"`
	}
	if json.Unmarshal(data, &init) != nil || init.FlowID == "" || init.Expires <= time.Now().Unix() || init.Interval <= 0 {
		return LoginFlow{}, "", &Error{Kind: ErrFormat}
	}
	u, err := url.Parse(init.Authorize)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return LoginFlow{}, "", &Error{Kind: ErrFormat}
	}
	// 授权页面只能属于已核实的官方域名；服务端返回 URL 不得变成任意外部链接。
	switch strings.ToLower(u.Hostname()) {
	case "chat.z.ai", "z.ai", "bigmodel.cn", "zcode.z.ai":
	default:
		return LoginFlow{}, "", &Error{Kind: ErrFormat}
	}
	redirect, _ := url.Parse(c.OriginURL() + "/app/oauth/login")
	q := redirect.Query()
	q.Set("redirect", "zcode://oauth/callback")
	q.Set("app_version", c.Identity.Version())
	redirect.RawQuery = q.Encode()
	q = u.Query()
	param := "redirect_uri"
	if p == BigModel {
		param = "redirect"
	}
	q.Set(param, redirect.String())
	u.RawQuery = q.Encode()
	expiry := init.Expires
	if max := time.Now().Add(5 * time.Minute).Unix(); expiry > max {
		expiry = max
	}
	interval := init.Interval
	if interval < 1 {
		interval = 1
	}
	if interval > 30 {
		interval = 30
	}
	return LoginFlow{Provider: p, FlowID: init.FlowID, PollToken: token, ExpiresAt: expiry, Interval: interval}, u.String(), nil
}

// PollLogin 执行一次上游 poll。暂时网络/5xx/429/畸形 200 返回 pending，致命错误终止会话。
func (c *Client) PollLogin(ctx context.Context, f LoginFlow) (*Tokens, error) {
	if time.Now().Unix() >= f.ExpiresAt {
		return nil, &Error{Kind: ErrLogin}
	}
	data, err := c.request(ctx, "GET", c.OriginURL()+"/api/v1/oauth/cli/poll/"+url.PathEscape(f.FlowID), bearer(f.PollToken), nil, 15*time.Second)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Kind == ErrNetwork || e.Kind == ErrUnavailable || e.Kind == ErrRateLimit || (e.Kind == ErrFormat && e.Code == 0 && e.Status == 200)) {
			return nil, nil
		}
		return nil, err
	}
	var result struct {
		Status string `json:"status"`
		JWT    string `json:"token"`
		User   struct {
			ID string `json:"user_id"`
		} `json:"user"`
		ZAI struct {
			Access string `json:"access_token"`
		} `json:"zai"`
		BigModel struct {
			Access string `json:"access_token"`
		} `json:"bigmodel"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, nil
	}
	if result.Status == "pending" {
		return nil, nil
	}
	if result.Status != "ready" {
		return nil, &Error{Kind: ErrLogin}
	}
	access := result.ZAI.Access
	if f.Provider == BigModel {
		access = result.BigModel.Access
	}
	if strings.TrimSpace(access) == "" {
		return nil, &Error{Kind: ErrFormat}
	}
	t := &Tokens{AccessToken: access, JWT: strings.TrimSpace(result.JWT), UserID: result.User.ID}
	if t.JWT != "" {
		var e error
		t.IssuedAt, t.ExpiresAt, e = InspectJWT(t.JWT)
		if e != nil {
			return nil, e
		}
		if t.Expired(time.Now()) {
			return nil, &Error{Kind: ErrLogin}
		}
	}
	return t, nil
}

func (c *Client) ResolveCodingKey(ctx context.Context, p Provider, access string) (string, error) {
	auth := access
	host := p.BusinessOrigin()
	if p == ZAI {
		// z/login 响应不使用常规 code/data envelope。
		raw, err := c.rawZaiLogin(ctx, access)
		if err != nil {
			return "", err
		}
		auth = "Bearer " + raw
	}
	h := http.Header{"Authorization": []string{auth}, "Content-Type": []string{"application/json"}}
	data, err := c.request(ctx, "GET", host+"/api/biz/customer/getCustomerInfo", h, nil, 15*time.Second)
	if err != nil {
		return "", err
	}
	var customer struct {
		Organizations []map[string]any `json:"organizations"`
		Orgs          []map[string]any `json:"orgs"`
	}
	if json.Unmarshal(data, &customer) != nil {
		return "", &Error{Kind: ErrFormat}
	}
	orgs := customer.Organizations
	if len(orgs) == 0 {
		orgs = customer.Orgs
	}
	if len(orgs) == 0 {
		return "", &Error{Kind: ErrConfiguration}
	}
	org := orgs[0]
	for _, o := range orgs {
		if strings.Contains(firstString(o, "organizationName", "name"), "默认机构") {
			org = o
			break
		}
	}
	orgID := firstString(org, "organizationId", "id", "orgId")
	projects, _ := org["projects"].([]any)
	if orgID == "" || len(projects) == 0 {
		return "", &Error{Kind: ErrFormat}
	}
	project, _ := projects[0].(map[string]any)
	for _, value := range projects {
		pr, _ := value.(map[string]any)
		if strings.Contains(firstString(pr, "projectName", "name"), "默认项目") {
			project = pr
			break
		}
	}
	projectID := firstString(project, "projectId", "id")
	if projectID == "" {
		return "", &Error{Kind: ErrFormat}
	}
	endpoint := host + "/api/biz/v1/organization/" + url.PathEscape(orgID) + "/projects/" + url.PathEscape(projectID) + "/api_keys"
	data, err = c.request(ctx, "GET", endpoint, h, nil, 15*time.Second)
	if err != nil {
		return "", err
	}
	var keys []map[string]any
	if json.Unmarshal(data, &keys) != nil {
		return "", &Error{Kind: ErrFormat}
	}
	key := ""
	for _, k := range keys {
		if k["name"] == "zcode-api-key" {
			key = firstString(k, "apiKey")
			if key != "" {
				break
			}
		}
	}
	if key == "" {
		data, err = c.request(ctx, "POST", endpoint, h, map[string]string{"name": "zcode-api-key"}, 15*time.Second)
		if err != nil {
			return "", err
		}
		var created map[string]any
		if json.Unmarshal(data, &created) != nil {
			return "", &Error{Kind: ErrFormat}
		}
		key = firstString(created, "apiKey")
	}
	if key == "" {
		return "", &Error{Kind: ErrFormat}
	}
	data, err = c.request(ctx, "GET", endpoint+"/copy/"+url.PathEscape(key), h, nil, 15*time.Second)
	if err != nil {
		if p == BigModel {
			return key, nil
		}
		return "", err
	}
	var copied map[string]any
	if json.Unmarshal(data, &copied) != nil {
		return "", &Error{Kind: ErrFormat}
	}
	secret := firstString(copied, "secretKey", "secret_key")
	if secret == "" {
		if p == BigModel {
			return key, nil
		}
		return "", &Error{Kind: ErrFormat}
	}
	return key + "." + secret, nil
}
func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
