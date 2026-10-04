package zcode

// 来源：src/proxy/endpoint-routing.ts；快照失败时保留旧映射，冷却后重查。
import (
	"context"
	"encoding/json"
	"golang.org/x/sync/singleflight"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Resolver struct {
	mu       sync.RWMutex
	mapping  map[string]string
	expires  time.Time
	retry    time.Time
	flight   singleflight.Group
	validate func(string) error
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
func (r *Resolver) Resolve(ctx context.Context, c *Client, original, key string) string {
	u, err := url.Parse(original)
	if err != nil {
		return original
	}
	clean := *u
	clean.RawQuery = ""
	lookup, err := routingKey(clean.String())
	if err != nil {
		return original
	}
	r.mu.RLock()
	fresh := time.Now().Before(r.expires) || time.Now().Before(r.retry)
	r.mu.RUnlock()
	if !fresh {
		pending := r.flight.DoChan("refresh", func() (any, error) { r.refresh(ctx, c, key); return nil, nil })
		select {
		case <-pending:
		case <-ctx.Done():
			return original
		}
	}
	r.mu.RLock()
	target := r.mapping[lookup]
	r.mu.RUnlock()
	if target == "" {
		return original
	}
	routed, err := url.Parse(target)
	if err != nil {
		return original
	}
	routed.RawQuery = u.RawQuery
	return routed.String()
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
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"mapping"`
		} `json:"proxyEndpoint"`
	}
	if err == nil {
		err = json.Unmarshal(data, &config)
	}
	next := map[string]string{}
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
			next[from] = entry.To
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.retry = time.Now().Add(30 * time.Second)
		return
	}
	r.mapping = next
	r.expires = time.Now().Add(5 * time.Minute)
	r.retry = time.Time{}
}
