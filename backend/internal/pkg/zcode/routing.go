package zcode

// 来源：src/proxy/endpoint-routing.ts；快照失败时保留旧映射，冷却后重查。
// 3.16.2 起映射项可要求附带 X-ZCode-Authorization（仅限官方 ultra 路径）；
// 3.16.3 起同一份 agent/configs 下发 configs.captcha，用于判断领取平面是否需要验证码。
import (
	"context"
	"encoding/json"
	"golang.org/x/sync/singleflight"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const zcodeAuthorizationHeader = "X-ZCode-Authorization"
const zcodeAuthorizationMarker = "x-zcode-authorization"

var zcodeAuthorizationPaths = []string{"/api/v1/ultra", "/api/v1/ultra-zai"}

type routingTarget struct {
	withAuthorization bool
	url               string
}

// requestVerification 对应上游 3.16.3 的 configs.captcha 开关；nil 表示服务端尚未下发。
type requestVerification struct {
	enabled          *bool
	skipModelRequest *bool
}

// needsCaptcha 与上游 requiresClaimCaptcha 一致：字段缺省时保守要求验证码。
func (v *requestVerification) needsCaptcha() bool {
	if v == nil {
		return true
	}
	enabled, skip := true, false
	if v.enabled != nil {
		enabled = *v.enabled
	}
	if v.skipModelRequest != nil {
		skip = *v.skipModelRequest
	}
	return enabled && !skip
}

// Routed 描述重写后的目标地址及服务端要求的附加头。
type Routed struct {
	URL     string
	Headers http.Header
}

type Resolver struct {
	mu           sync.RWMutex
	mapping      map[string]routingTarget
	verification *requestVerification
	expires      time.Time
	retry        time.Time
	flight       singleflight.Group
	validate     func(string) error
}

func NewResolver(validate func(string) error) *Resolver { return &Resolver{validate: validate} }
func routingKey(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", &Error{Kind: ErrFormat}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	return "https://" + strings.ToLower(u.Hostname()) + ":" + port + path, nil
}

func printableHeaderValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return "", false
		}
	}
	return value, true
}

func (r *Resolver) allowedAuthorizationTarget(target, origin string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	o, err := url.Parse(origin)
	if err != nil || u.Scheme+"://"+u.Host != o.Scheme+"://"+o.Host {
		return false
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	for _, allowed := range zcodeAuthorizationPaths {
		if path == allowed || strings.HasPrefix(path, allowed+"/") {
			return true
		}
	}
	return false
}

// Resolve 解析目标地址；任何失败都保留原地址，且不阻断请求。
func (r *Resolver) Resolve(ctx context.Context, c *Client, original, key, zcodeJWT string) Routed {
	out := Routed{URL: original}
	u, err := url.Parse(original)
	if err != nil {
		return out
	}
	clean := *u
	clean.RawQuery = ""
	lookup, err := routingKey(clean.String())
	if err != nil {
		return out
	}
	if !r.fresh() {
		if !r.ensureFresh(ctx, c, key) {
			return out
		}
	}
	r.mu.RLock()
	target, ok := r.mapping[lookup]
	r.mu.RUnlock()
	if !ok {
		return out
	}
	routed, err := url.Parse(target.url)
	if err != nil {
		return out
	}
	routed.RawQuery = u.RawQuery
	out.URL = routed.String()
	if target.withAuthorization {
		if value, ok := printableHeaderValue(zcodeJWT); ok {
			if !strings.HasPrefix(strings.ToLower(value), "bearer ") {
				value = "Bearer " + value
			}
			headers := http.Header{}
			headers.Set(zcodeAuthorizationHeader, value)
			out.Headers = headers
		}
	}
	return out
}

// ClaimNeedsCaptcha 报告领取平面是否需要验证码；配置未下发或抓取失败时保守为需要。
func (r *Resolver) ClaimNeedsCaptcha(ctx context.Context, c *Client, key string) bool {
	if !r.fresh() {
		r.ensureFresh(ctx, c, key)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.verification.needsCaptcha()
}

func (r *Resolver) fresh() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return time.Now().Before(r.expires) || time.Now().Before(r.retry)
}

// ensureFresh 只发起一次刷新；返回 false 表示调用方被取消。
func (r *Resolver) ensureFresh(ctx context.Context, c *Client, key string) bool {
	pending := r.flight.DoChan("refresh", func() (any, error) { r.refresh(ctx, c, key); return nil, nil })
	select {
	case <-pending:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *Resolver) refresh(ctx context.Context, c *Client, key string) {
	h := c.Identity.Headers(false)
	h.Set("Accept", "application/json")
	if key != "" {
		h.Set("X-Api-Key", key)
	}
	data, err := c.request(ctx, "GET", c.OriginURL()+"/api/v1/agent/configs", h, nil, 3*time.Second)
	var config struct {
		Proxy struct {
			Mapping []struct {
				From                     string  `json:"from"`
				To                       string  `json:"to"`
				ZCodeAuthorizationHeader *string `json:"zcodeAuthorizationHeader"`
			} `json:"mapping"`
		} `json:"proxyEndpoint"`
		Configs struct {
			Captcha *struct {
				Enabled          *bool `json:"enabled"`
				SkipModelRequest *bool `json:"skip_model_request"`
			} `json:"captcha"`
		} `json:"configs"`
	}
	if err == nil {
		err = json.Unmarshal(data, &config)
	}
	origin := c.OriginURL()
	next := map[string]routingTarget{}
	if len(config.Proxy.Mapping) > 256 {
		err = &Error{Kind: ErrFormat}
	}
	if err == nil {
		for _, entry := range config.Proxy.Mapping {
			from, e := routingKey(entry.From)
			if e != nil {
				err = e
				break
			}
			if _, e = routingKey(entry.To); e != nil {
				err = e
				break
			}
			if r.validate != nil {
				if e = r.validate(entry.To); e != nil {
					err = e
					break
				}
			}
			if _, duplicate := next[from]; duplicate {
				err = &Error{Kind: ErrFormat}
				break
			}
			withAuthorization := false
			if entry.ZCodeAuthorizationHeader != nil {
				marker := strings.TrimSpace(*entry.ZCodeAuthorizationHeader)
				if marker != "" {
					// 标记值必须就是头名本身，且目标只能落在官方 ultra 路径。
					if !strings.EqualFold(marker, zcodeAuthorizationMarker) || !r.allowedAuthorizationTarget(entry.To, origin) {
						err = &Error{Kind: ErrFormat}
						break
					}
					withAuthorization = true
				}
			}
			next[from] = routingTarget{withAuthorization: withAuthorization, url: entry.To}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.retry = time.Now().Add(30 * time.Second)
		return
	}
	r.mapping = next
	r.verification = nil
	if config.Configs.Captcha != nil {
		r.verification = &requestVerification{enabled: config.Configs.Captcha.Enabled, skipModelRequest: config.Configs.Captcha.SkipModelRequest}
	}
	r.expires = time.Now().Add(5 * time.Minute)
	r.retry = time.Time{}
}
