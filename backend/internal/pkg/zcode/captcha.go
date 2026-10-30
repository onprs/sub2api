package zcode

// 来源：src/proxy/captcha.ts、captcha-pool.ts、captcha-token.ts。
// 验证参数单次使用：缓存可供下一次 Take 消费，不能 singleflight 共享同一个 token。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"golang.org/x/sync/singleflight"
	"net/url"
	"strings"
	"sync"
	"time"
)

type CaptchaConfig struct {
	Enabled          bool   `json:"enabled"`
	SkipModelRequest bool   `json:"skip_model_request"`
	Prefix           string `json:"prefix"`
	SceneID          string `json:"sceneId"`
	Region           string `json:"region"`
}
type CaptchaToken struct {
	Param    string
	Region   string
	IssuedAt time.Time
	Key      string
}
type CaptchaSolver interface {
	Solve(context.Context, CaptchaConfig, string) (string, error)
}
type CaptchaPool struct {
	solver  CaptchaSolver
	mu      sync.Mutex
	config  CaptchaConfig
	known   bool
	expires time.Time
	tokens  []CaptchaToken
	seen    map[string]time.Time
	slots   chan struct{}
	flight  singleflight.Group
}

func NewCaptchaPool(solver CaptchaSolver) *CaptchaPool {
	return &CaptchaPool{solver: solver, slots: make(chan struct{}, 2), seen: map[string]time.Time{}}
}
func configKey(c CaptchaConfig) string { return c.Prefix + "\n" + c.SceneID + "\n" + c.Region }
func ValidateCaptcha(param string) (string, error) {
	if len(param) < 200 || len(param) > 32<<10 {
		return "", &Error{Kind: ErrCaptcha}
	}
	decoded, err := base64.StdEncoding.DecodeString(param)
	if err != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	var v struct {
		CertifyID string `json:"certifyId"`
		Security  string `json:"securityToken"`
		Upper     string `json:"SecurityToken"`
	}
	if json.Unmarshal(decoded, &v) != nil || v.CertifyID == "" || (len(v.Security) < 50 && len(v.Upper) < 50) {
		return "", &Error{Kind: ErrCaptcha}
	}
	return v.CertifyID, nil
}
func (p *CaptchaPool) fetchConfiguration(ctx context.Context, c *Client) (CaptchaConfig, error) {
	data, err := c.request(ctx, "GET", c.OriginURL()+"/api/v1/client/configs?app_version="+url.QueryEscape(c.Identity.Version())+"&platform=win32-x64", nil, nil, 5*time.Second)
	if err != nil {
		return CaptchaConfig{}, err
	}
	var env struct {
		Configs struct {
			Captcha *CaptchaConfig `json:"captcha"`
		} `json:"configs"`
	}
	if json.Unmarshal(data, &env) != nil || env.Configs.Captcha == nil {
		return CaptchaConfig{}, &Error{Kind: ErrCaptcha}
	}
	return *env.Configs.Captcha, nil
}

// configuration 返回验证码配置快照。enabled=false 是服务端下发的合法状态，不当作错误。
// 上游 3.16.3 使用 1h 成功快照与 in-flight 复用，失败不写负缓存；这里在首次成功前
// 直接返回错误，已有快照时短暂沿用 last-good 后重试。
func (p *CaptchaPool) configuration(ctx context.Context, c *Client) (CaptchaConfig, error) {
	p.mu.Lock()
	if time.Now().Before(p.expires) {
		cfg := p.config
		p.mu.Unlock()
		return cfg, nil
	}
	p.mu.Unlock()
	result := p.flight.DoChan("config", func() (any, error) {
		cfg, err := p.fetchConfiguration(ctx, c)
		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			if p.known {
				p.expires = time.Now().Add(30 * time.Second)
				return nil, nil
			}
			return nil, err
		}
		if configKey(p.config) != configKey(cfg) {
			p.tokens = nil
		}
		p.config = cfg
		p.known = true
		p.expires = time.Now().Add(time.Hour)
		return nil, nil
	})
	select {
	case <-ctx.Done():
		return CaptchaConfig{}, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return CaptchaConfig{}, result.Err
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.known {
			return CaptchaConfig{}, &Error{Kind: ErrCaptcha}
		}
		return p.config, nil
	}
}
func (p *CaptchaPool) consume(token CaptchaToken) (CaptchaToken, error) {
	certify, err := ValidateCaptcha(token.Param)
	if err != nil {
		return CaptchaToken{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for id, at := range p.seen {
		if now.Sub(at) > 110*time.Second {
			delete(p.seen, id)
		}
	}
	if _, used := p.seen[certify]; used {
		return CaptchaToken{}, &Error{Kind: ErrCaptcha}
	}
	p.seen[certify] = now
	return token, nil
}

// Token 取一个可用验证参数。返回 nil 表示服务端已关闭验证码，调用方不应发送验证头。
func (p *CaptchaPool) Token(ctx context.Context, c *Client, proxy string) (*CaptchaToken, error) {
	cfg, err := p.configuration(ctx, c)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || cfg.Prefix == "" || cfg.SceneID == "" {
		return nil, nil
	}
	token, err := p.take(ctx, cfg, proxy)
	if err != nil {
		return nil, err
	}
	return &token, nil
}
func (p *CaptchaPool) Take(ctx context.Context, c *Client, proxy string) (CaptchaToken, error) {
	token, err := p.Token(ctx, c, proxy)
	if err != nil {
		return CaptchaToken{}, err
	}
	if token == nil {
		return CaptchaToken{}, &Error{Kind: ErrCaptcha}
	}
	return *token, nil
}
func (p *CaptchaPool) take(ctx context.Context, cfg CaptchaConfig, proxy string) (CaptchaToken, error) {
	p.mu.Lock()
	for len(p.tokens) > 0 {
		token := p.tokens[0]
		p.tokens = p.tokens[1:]
		if token.Key == configKey(cfg) && time.Since(token.IssuedAt) < 95*time.Second {
			p.mu.Unlock()
			return p.consume(token)
		}
	}
	p.mu.Unlock()
	token, err := p.solve(ctx, cfg, proxy)
	if err != nil {
		return CaptchaToken{}, err
	}
	return p.consume(token)
}
func (p *CaptchaPool) solve(ctx context.Context, cfg CaptchaConfig, proxy string) (CaptchaToken, error) {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-ctx.Done():
		return CaptchaToken{}, ctx.Err()
	}
	if p.solver == nil {
		return CaptchaToken{}, &Error{Kind: ErrConfiguration}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	param, err := p.solver.Solve(ctx, cfg, proxy)
	if err != nil {
		if ctx.Err() != nil {
			return CaptchaToken{}, ctx.Err()
		}
		return CaptchaToken{}, &Error{Kind: ErrCaptcha}
	}
	if _, err = ValidateCaptcha(param); err != nil {
		return CaptchaToken{}, err
	}
	return CaptchaToken{Param: param, Region: cfg.Region, IssuedAt: time.Now(), Key: configKey(cfg)}, nil
}
func (p *CaptchaPool) Warm(ctx context.Context, c *Client, proxy string) error {
	result := p.flight.DoChan("warm", func() (any, error) {
		cfg, err := p.configuration(ctx, c)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled || cfg.Prefix == "" || cfg.SceneID == "" {
			// 服务端已关闭验证码：无需预热。
			return nil, nil
		}
		p.mu.Lock()
		valid := false
		fresh := p.tokens[:0]
		for _, token := range p.tokens {
			if token.Key == configKey(cfg) && time.Since(token.IssuedAt) < 80*time.Second {
				fresh = append(fresh, token)
				valid = true
			}
		}
		p.tokens = fresh
		p.mu.Unlock()
		if valid {
			return nil, nil
		}
		token, err := p.solve(ctx, cfg, proxy)
		if err != nil {
			return nil, err
		}
		return nil, p.Cache(token)
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-result:
		return result.Err
	}
}
func (p *CaptchaPool) Cache(token CaptchaToken) error {
	certify, err := ValidateCaptcha(token.Param)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) >= 2 {
		return errors.New("ZCode CAPTCHA 缓存已满")
	}
	if _, used := p.seen[certify]; used {
		return &Error{Kind: ErrCaptcha}
	}
	for _, queued := range p.tokens {
		id, _ := ValidateCaptcha(queued.Param)
		if id == certify {
			return &Error{Kind: ErrCaptcha}
		}
	}
	p.tokens = append(p.tokens, token)
	return nil
}
func IsCaptchaChallenge(status int, h map[string][]string, body []byte) bool {
	if status >= 200 && status < 300 {
		return false
	}
	for k, v := range h {
		if strings.EqualFold(k, CaptchaHeader) && len(v) > 0 && strings.TrimSpace(v[0]) != "" {
			return true
		}
	}
	var env struct {
		Code int `json:"code"`
	}
	return json.Unmarshal(body, &env) == nil && env.Code == 3007
}
