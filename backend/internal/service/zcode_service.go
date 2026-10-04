package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ZCodeService struct {
	repo         AccountRepository
	proxyRepo    ProxyRepository
	upstream     HTTPUpstream
	cipher       SecretEncryptor
	cfg          *config.Config
	store        zcode.Store
	rateLimit    *RateLimitService
	resolver     *zcode.Resolver
	signer       *zcode.Signer
	poolMu       sync.Mutex
	workerCursor int
	pools        map[string]*zcode.CaptchaPool
	solver       zcode.CaptchaSolver
	cancel       context.CancelFunc
	done         chan struct{}
	start        sync.Once
	stop         sync.Once
}

func NewZCodeService(repo AccountRepository, proxy ProxyRepository, httpClient HTTPUpstream, cipher SecretEncryptor, cfg *config.Config, store zcode.Store, rateLimit *RateLimitService) *ZCodeService {
	s := &ZCodeService{repo: repo, proxyRepo: proxy, upstream: httpClient, cipher: cipher, cfg: cfg, store: store, rateLimit: rateLimit, pools: map[string]*zcode.CaptchaPool{}, signer: zcode.NewSigner()}
	s.resolver = zcode.NewResolver(s.validateURL)
	browser := &zcode.BrowserSolver{}
	if cfg != nil {
		browser.Executable = cfg.Gateway.ZCode.ChromiumPath
		browser.NoSandbox = cfg.Gateway.ZCode.NoSandbox
	}
	s.solver = browser
	return s
}
func (s *ZCodeService) available() error {
	if s == nil || s.store == nil || s.cipher == nil || s.upstream == nil || s.cfg == nil || !s.cfg.Totp.EncryptionKeyConfigured {
		return infraerrors.New(http.StatusServiceUnavailable, "ZCODE_NOT_CONFIGURED", "ZCode OAuth 需要 Redis 和固定的凭据加密密钥")
	}
	return nil
}
func (s *ZCodeService) validateURL(raw string) error {
	allowed := []string{"zcode.z.ai", "api.z.ai", "open.bigmodel.cn", "bigmodel.cn"}
	if s.cfg != nil && s.cfg.Security.URLAllowlist.Enabled {
		allowed = s.cfg.Security.URLAllowlist.UpstreamHosts
	}
	_, err := urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{AllowedHosts: allowed, RequireAllowlist: true, AllowPrivate: false})
	if err != nil {
		return &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return nil
}
func (s *ZCodeService) proxyURL(ctx context.Context, id *int64) (string, error) {
	if id == nil || *id == 0 {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	p, err := s.proxyRepo.GetByID(ctx, *id)
	if err != nil || p == nil {
		return "", &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return p.URL(), nil
}
func (s *ZCodeService) client(a *Account, proxy string) *zcode.Client {
	version, origin := "", ""
	if s.cfg != nil {
		version = s.cfg.Gateway.ZCode.AppVersion
		origin = s.cfg.Gateway.ZCode.Origin
	}
	device := ""
	if a != nil {
		device = a.GetCredential("zcode_device_id")
		if override := a.GetCredential("zcode_app_version"); override != "" {
			version = override
		}
	}
	identity := zcode.DefaultIdentity(version, device)
	if s.cfg != nil {
		if value := s.cfg.Gateway.ZCode.IdentityPlatform; value != "" {
			identity.Platform = value
		}
		identity.OSVersion = s.cfg.Gateway.ZCode.IdentityOSVersion
	}
	id, concurrency := int64(0), 1
	if a != nil {
		id = a.ID
		concurrency = a.Concurrency
	}
	return &zcode.Client{Origin: origin, Identity: identity, Send: func(req *http.Request) (*http.Response, error) {
		if err := s.validateURL(req.URL.String()); err != nil {
			return nil, err
		}
		ctx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamPublicHostsOnly(req.Context()))
		req = req.Clone(ctx)
		return s.upstream.Do(req, proxy, id, concurrency)
	}}
}
func (s *ZCodeService) tokens(a *Account) (zcode.Tokens, error) {
	if !a.IsZCodeOAuth() {
		return zcode.Tokens{}, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	plain, err := s.cipher.Decrypt(a.GetCredential(zcodeTokensKey))
	if err != nil {
		return zcode.Tokens{}, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	var tokens zcode.Tokens
	if json.Unmarshal([]byte(plain), &tokens) != nil {
		return tokens, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return tokens, nil
}
func (s *ZCodeService) pool(proxy string) *zcode.CaptchaPool {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()
	pool := s.pools[proxy]
	if pool == nil {
		pool = zcode.NewCaptchaPool(s.solver)
		s.pools[proxy] = pool
	}
	return pool
}
func decodeZCodeExtra(value any, out any) error {
	if value == nil {
		return errors.New("状态尚未获取")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func zcodeAdminError(err error) error {
	if infraerrors.Reason(err) != "" {
		return err
	}
	kind := zcode.KindOf(err)
	status := http.StatusBadGateway
	if kind == zcode.ErrLogin {
		status = http.StatusConflict
	}
	if kind == zcode.ErrConfiguration {
		status = http.StatusServiceUnavailable
	}
	return infraerrors.New(status, "ZCODE_"+strings.ToUpper(string(kind)), "ZCode "+string(kind))
}
func (s *ZCodeService) stateError(ctx context.Context, a *Account, e *zcode.Error) {
	state := map[string]any{"last_error": string(e.Kind), "checked_at": time.Now().Unix(), "oauth_status": "authorized"}
	if e.Kind == zcode.ErrLogin {
		state["oauth_status"] = "login_required"
	}
	delay := time.Minute
	switch e.Kind {
	case zcode.ErrQuota:
		delay = 5 * time.Minute
	case zcode.ErrLogin, zcode.ErrConfiguration:
		delay = 10 * time.Minute
	case zcode.ErrCaptcha:
		delay = 2 * time.Minute
	}
	until := time.Now().Add(delay)
	if e.RetryAt > time.Now().Unix() {
		until = time.Unix(e.RetryAt, 0)
	}
	if until.After(time.Now().Add(24 * time.Hour)) {
		until = time.Now().Add(24 * time.Hour)
	}
	if _, err := s.updateState(ctx, a, map[string]any{zcodeStateKey: state}, &until, "zcode_"+string(e.Kind), false); err != nil {
		slog.Warn("zcode_cooldown_write_failed", "account_id", a.ID)
	}
	if s.rateLimit != nil {
		s.rateLimit.notifyAccountSchedulingBlocked(a, until, "zcode_"+string(e.Kind))
	}
}

// RoundTrip 由原网关在账号选中、权威复核及抢槽后调用。
func (s *ZCodeService) RoundTrip(req *http.Request, proxy string, a *Account) (*http.Response, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	tokens, err := s.tokens(a)
	if err != nil {
		return s.failureResponse(req, a, &zcode.Error{Kind: zcode.ErrConfiguration}), nil
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, int64(257<<20)))
	if err != nil {
		return nil, &zcode.Error{Kind: zcode.ErrNetwork}
	}
	_ = req.Body.Close()
	if len(body) > 256<<20 {
		return nil, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	client := s.client(a, proxy)
	resp, upstreamErr, err := client.Forward(req.Context(), req, body, zcode.ForwardOptions{Provider: a.ZCodeProvider(), Plan: a.GetAccountMode(), Tokens: tokens, Resolver: s.resolver, Captcha: s.pool(proxy), Signer: s.signer, Proxy: proxy})
	if err != nil {
		return nil, err
	}
	if upstreamErr != nil {
		return s.failureResponse(req, a, upstreamErr), nil
	}
	return resp, nil
}
func (s *ZCodeService) failureResponse(req *http.Request, a *Account, e *zcode.Error) *http.Response {
	s.stateError(req.Context(), a, e)
	status := http.StatusServiceUnavailable
	if e.Kind == zcode.ErrRateLimit || e.Kind == zcode.ErrQuota {
		status = http.StatusTooManyRequests
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "zcode_" + string(e.Kind), "message": "ZCode " + string(e.Kind)}})
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if e.RetryAt > time.Now().Unix() {
		headers.Set("Retry-After", strconv.FormatInt(e.RetryAt-time.Now().Unix(), 10))
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}
func (s *ZCodeService) Status(ctx context.Context, id int64) (map[string]any, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.IsZCodeOAuth() {
		return nil, infraerrors.BadRequest("ZCODE_ACCOUNT_INVALID", "账号不是 ZCode OAuth 智谱账号")
	}
	status := map[string]any{"auth_mode": "zcode_oauth", "provider": a.ZCodeProvider(), "plan": a.GetAccountMode(), "auto_claim": a.Credentials["zcode_auto_claim"], "state": a.Extra[zcodeStateKey], "quota": a.Extra[zcodeQuotaKey], "claim": a.Extra[zcodeClaimKey]}
	return status, nil
}
func (s *ZCodeService) clearOwnCooldown(ctx context.Context, a *Account) {
	if strings.HasPrefix(a.TempUnschedulableReason, "zcode_") {
		_ = s.repo.ClearTempUnschedulable(ctx, a.ID)
	}
}
func zcodeAccountKey(id int64) string { return fmt.Sprintf("account:%d", id) }
