package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"golang.org/x/net/http2"
	"golang.org/x/sync/singleflight"
)

// chatgptCodexModelsURL is the ChatGPT Codex models manifest endpoint.
// Package-level variable so tests can point it at a stub server.
var chatgptCodexModelsURL = "https://chatgpt.com/backend-api/codex/models"

const (
	codexModelsManifestBodyLimit       int64 = 8 << 20
	codexModelsManifestCacheBodyLimit        = 1 << 20
	codexModelsManifestCacheMaxEntries       = 64
	codexModelsManifestCacheTTL              = 30 * time.Second
	codexModelsManifestCacheStaleTTL         = 5 * time.Minute
	codexModelsManifestRequestTimeout        = 15 * time.Second
)

// CodexModelsManifest carries the client representation plus caching metadata.
type CodexModelsManifest struct {
	Body                         []byte
	ETag                         string
	upstreamETag                 string
	upstreamSourceBody           []byte
	convertedFromOpenAIModelList bool
	NotModified                  bool
}

// SelectCodexModelsManifestAccount selects a schedulable OpenAI account that can
// authenticate to ChatGPT's Codex models manifest endpoint. Unlike normal
// request routing, /v1/models for Codex cannot be served with OpenAI API-key
// accounts: the upstream manifest endpoint requires ChatGPT OAuth/PAT
// credentials. Keep the existing priority/LRU scheduler by repeatedly asking the
// normal selector for the best candidate and excluding ineligible API-key or
// tokenless accounts.
func (s *OpenAIGatewayService) SelectCodexModelsManifestAccount(ctx context.Context, groupID *int64) (*Account, error) {
	if s == nil {
		return nil, errors.New("openai gateway service is nil")
	}

	excludedIDs := make(map[int64]struct{})
	skipped := 0
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	for {
		account, err := s.selectAccountForModelWithExclusions(ctx, groupID, PlatformOpenAI, "", "", excludedIDs, false, 0, "")
		if err != nil {
			if skipped > 0 {
				return nil, fmt.Errorf("no available OpenAI OAuth accounts for Codex models manifest after skipping %d ineligible accounts: %w", skipped, ErrNoAvailableAccounts)
			}
			return nil, err
		}
		if account == nil || account.ID <= 0 {
			return nil, errors.New("selected OpenAI account is invalid")
		}

		credAccount, skipReason, err := s.resolveCodexModelsManifestCredentialAccount(ctx, account)
		if err == nil {
			if accessToken, tokenErr := s.getCodexModelsManifestAccessToken(ctx, credAccount); tokenErr == nil {
				if account.IsShadow() || strings.TrimSpace(account.GetOpenAIAccessToken()) == "" {
					account = cloneAccountForCodexModelsManifest(account, credAccount, accessToken)
				}
				return account, nil
			} else {
				skipReason = "access_token_unavailable"
				err = tokenErr
			}
		}

		slog.Warn("codex_models_account_skipped",
			"account_id", account.ID,
			"account_type", account.Type,
			"platform", account.Platform,
			"group_id", derefGroupID(groupID),
			"skip_reason", skipReason,
			"error", err,
		)
		excludedIDs[account.ID] = struct{}{}
		skipped++
	}
}

type codexModelsManifestUpstreamError struct {
	err       error
	retryable bool
}

func (e *codexModelsManifestUpstreamError) Error() string { return e.err.Error() }

func (e *codexModelsManifestUpstreamError) Unwrap() error { return e.err }

type codexModelsManifestRequest struct {
	url                 string
	headers             http.Header
	proxyURL            string
	accountID           int64
	credentialAccountID int64
	accountConcurrency  int
	useAPIKeyUpstream   bool
}

type codexModelsManifestCacheEntry struct {
	manifest   *CodexModelsManifest
	order      uint64
	expiresAt  time.Time
	staleUntil time.Time
}

type codexModelsManifestCacheState uint8

const (
	codexModelsManifestCacheMiss codexModelsManifestCacheState = iota
	codexModelsManifestCacheFresh
	codexModelsManifestCacheStale
)

type codexModelsManifestCache struct {
	mu        sync.Mutex
	entries   map[string]codexModelsManifestCacheEntry
	nextOrder uint64
	refresh   singleflight.Group
}

func (c *codexModelsManifestCache) get(key string, now time.Time) (*CodexModelsManifest, codexModelsManifestCacheState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, codexModelsManifestCacheMiss
	}
	if !now.Before(entry.staleUntil) {
		delete(c.entries, key)
		return nil, codexModelsManifestCacheMiss
	}
	if now.Before(entry.expiresAt) {
		return entry.manifest, codexModelsManifestCacheFresh
	}
	return entry.manifest, codexModelsManifestCacheStale
}

func (c *codexModelsManifestCache) set(key string, manifest *CodexModelsManifest, now time.Time) {
	if manifest == nil || len(manifest.Body) > codexModelsManifestCacheBodyLimit {
		return
	}
	remainingBodyBudget := codexModelsManifestCacheBodyLimit - len(manifest.Body)
	if len(manifest.upstreamSourceBody) > remainingBodyBudget {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]codexModelsManifestCacheEntry)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= codexModelsManifestCacheMaxEntries {
		oldestKey := ""
		var oldestOrder uint64
		for candidateKey, entry := range c.entries {
			if !now.Before(entry.staleUntil) {
				delete(c.entries, candidateKey)
				continue
			}
			if oldestKey == "" || entry.order < oldestOrder {
				oldestKey = candidateKey
				oldestOrder = entry.order
			}
		}
		if len(c.entries) >= codexModelsManifestCacheMaxEntries && oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.nextOrder++
	c.entries[key] = codexModelsManifestCacheEntry{
		manifest:   manifest,
		order:      c.nextOrder,
		expiresAt:  now.Add(codexModelsManifestCacheTTL),
		staleUntil: now.Add(codexModelsManifestCacheStaleTTL),
	}
}

// IsRetryableCodexModelsManifestError reports whether another selected account
// may succeed without changing the request. Configuration and upstream 4xx
// responses, except 429, are intentionally not retried.
func IsRetryableCodexModelsManifestError(err error) bool {
	var upstreamErr *codexModelsManifestUpstreamError
	return errors.As(err, &upstreamErr) && upstreamErr.retryable
}

func cloneAccountForCodexModelsManifest(account *Account, credAccount *Account, accessToken string) *Account {
	if account == nil || credAccount == nil {
		return account
	}
	cloned := *account
	if account.Credentials != nil {
		cloned.Credentials = make(map[string]any, len(account.Credentials)+len(credAccount.Credentials)+1)
		for key, value := range account.Credentials {
			cloned.Credentials[key] = value
		}
	} else {
		cloned.Credentials = make(map[string]any, len(credAccount.Credentials)+1)
	}
	for key, value := range credAccount.Credentials {
		if _, exists := cloned.Credentials[key]; !exists {
			cloned.Credentials[key] = value
		}
	}
	cloned.Credentials["access_token"] = accessToken
	return &cloned
}

func (s *OpenAIGatewayService) resolveCodexModelsManifestCredentialAccount(ctx context.Context, account *Account) (*Account, string, error) {
	if account == nil {
		return nil, "account_required", errors.New("account is required")
	}
	if account.Platform != PlatformOpenAI {
		return nil, "not_openai_platform", fmt.Errorf("account %d is platform %s", account.ID, account.Platform)
	}
	if account.Type == AccountTypeAPIKey {
		return nil, "api_key_account", fmt.Errorf("account %d is an OpenAI API-key account", account.ID)
	}
	if account.Type != AccountTypeOAuth {
		return nil, "non_oauth_account", fmt.Errorf("account %d type %s is not OpenAI OAuth", account.ID, account.Type)
	}
	if account.IsShadow() && s.accountRepo == nil {
		return nil, "shadow_parent_repo_unavailable", fmt.Errorf("account %d is a shadow account but account repository is unavailable", account.ID)
	}

	credAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, "credential_resolution_failed", err
	}
	if credAccount == nil || !credAccount.IsOpenAIOAuth() {
		return nil, "not_openai_oauth", fmt.Errorf("account %d credential owner is not OpenAI OAuth", account.ID)
	}
	return credAccount, "", nil
}

func (s *OpenAIGatewayService) getCodexModelsManifestAccessToken(ctx context.Context, credAccount *Account) (string, error) {
	if credAccount == nil || !credAccount.IsOpenAIOAuth() {
		return "", errors.New("credential account is not OpenAI OAuth")
	}
	if s != nil && s.openAITokenProvider != nil {
		accessToken, err := s.openAITokenProvider.GetAccessToken(ctx, credAccount)
		if err != nil {
			return "", err
		}
		if accessToken = strings.TrimSpace(accessToken); accessToken != "" {
			return accessToken, nil
		}
	}
	accessToken := strings.TrimSpace(credAccount.GetOpenAIAccessToken())
	if accessToken == "" {
		return "", errors.New("access_token not found in credentials")
	}
	return accessToken, nil
}

// FetchCodexModelsManifest fetches the live Codex models manifest from the
// ChatGPT backend using the account's OAuth credentials.
//
// The response body is passed through verbatim: the manifest schema evolves
// with Codex client releases, and interpreting it here would force the gateway
// to chase upstream changes. Passing it through keeps the gateway
// schema-agnostic and always reflects the account's real entitlements.
func (s *OpenAIGatewayService) FetchCodexModelsManifest(ctx context.Context, account *Account, clientVersion, ifNoneMatch string) (*CodexModelsManifest, error) {
	if account == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_ACCOUNT_REQUIRED", "account is required")
	}
	credAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_CREDENTIALS_FAILED", "resolve credential account: %v", err)
	}

	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = openAICodexProbeVersion
	}

	requestEndpoint := chatgptCodexModelsURL
	authToken := ""
	useAPIKeyUpstream := false
	appendModelsPath := false
	switch {
	case credAccount.IsOpenAIOAuth():
		authToken = strings.TrimSpace(credAccount.GetOpenAIAccessToken())
		if authToken == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_TOKEN_MISSING", "account has no Codex backend access token")
		}
	case credAccount.IsOpenAIApiKey():
		baseURL := strings.TrimSpace(credAccount.GetCredential("base_url"))
		if baseURL == "" || isOfficialOpenAIModelsBaseURL(baseURL) {
			return nil, infraerrors.New(
				http.StatusBadGateway,
				"OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_UNSUPPORTED",
				"Codex models manifest requires a custom API key upstream base URL",
			)
		}
		authToken = strings.TrimSpace(credAccount.GetOpenAIApiKey())
		if authToken == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_MISSING", "account has no API key for the Codex models upstream")
		}
		normalizedBaseURL, validateErr := s.validateUpstreamBaseURL(baseURL)
		if validateErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_INVALID", "invalid Codex models upstream base URL: %v", validateErr)
		}
		requestEndpoint = normalizedBaseURL
		useAPIKeyUpstream = true
		appendModelsPath = true
	default:
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_ACCOUNT_TYPE_UNSUPPORTED", "account type %q cannot fetch the Codex models manifest", credAccount.Type)
	}

	requestURL, err := buildCodexModelsManifestURL(requestEndpoint, appendModelsPath, clientVersion)
	if err != nil {
		if useAPIKeyUpstream {
			return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_API_KEY_UPSTREAM_INVALID", "invalid Codex models upstream base URL: %v", err)
		}
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "parse codex models request URL: %v", err)
	}

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+authToken)
	headers.Set("Accept", "application/json")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("Version", clientVersion)
	headers.Set("User-Agent", codexCLIUserAgent)
	if useAPIKeyUpstream {
		credAccount.ApplyHeaderOverrides(headers)
	} else {
		setOpenAIChatGPTAccountHeaders(headers, credAccount)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	request := codexModelsManifestRequest{
		url:                 requestURL.String(),
		headers:             headers,
		proxyURL:            proxyURL,
		accountID:           account.ID,
		credentialAccountID: credAccount.ID,
		accountConcurrency:  account.Concurrency,
		useAPIKeyUpstream:   useAPIKeyUpstream,
	}
	if useAPIKeyUpstream {
		return s.fetchCachedAPIKeyCodexModelsManifest(ctx, request, ifNoneMatch)
	}
	return s.fetchCodexModelsManifestUpstream(ctx, request, ifNoneMatch)
}

func (s *OpenAIGatewayService) fetchCachedAPIKeyCodexModelsManifest(ctx context.Context, request codexModelsManifestRequest, ifNoneMatch string) (*CodexModelsManifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cacheKey := buildCodexModelsManifestCacheKey(request)
	manifest, state := s.codexModelsManifestCache.get(cacheKey, time.Now())
	if state == codexModelsManifestCacheFresh {
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
	resultCh := s.refreshCachedAPIKeyCodexModelsManifest(cacheKey, request)
	if state == codexModelsManifestCacheStale {
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			return nil, result.Err
		}
		manifest, ok := result.Val.(*CodexModelsManifest)
		if !ok || manifest == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "invalid shared Codex models manifest result")
		}
		return codexModelsManifestForClient(manifest, ifNoneMatch), nil
	}
}

func (s *OpenAIGatewayService) refreshCachedAPIKeyCodexModelsManifest(cacheKey string, request codexModelsManifestRequest) <-chan singleflight.Result {
	return s.codexModelsManifestCache.refresh.DoChan(cacheKey, func() (any, error) {
		cached, _ := s.codexModelsManifestCache.get(cacheKey, time.Now())
		ifNoneMatch := ""
		if cached != nil {
			ifNoneMatch = cached.ETag
		}
		manifest, err := s.fetchCodexModelsManifestUpstream(context.Background(), request, ifNoneMatch)
		if err != nil {
			return nil, err
		}
		if manifest.NotModified && cached != nil {
			s.codexModelsManifestCache.set(cacheKey, cached, time.Now())
			return cached, nil
		}
		if !manifest.NotModified {
			s.codexModelsManifestCache.set(cacheKey, manifest, time.Now())
		}
		return manifest, nil
	})
}

func (s *OpenAIGatewayService) fetchCodexModelsManifestUpstream(ctx context.Context, request codexModelsManifestRequest, ifNoneMatch string) (*CodexModelsManifest, error) {
	reqCtx, cancel := context.WithTimeout(ctx, codexModelsManifestRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, request.url, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request: %v", err)
	}
	req.Header = request.headers.Clone()
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	var resp *http.Response
	if request.useAPIKeyUpstream {
		if s.httpUpstream == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_UPSTREAM_NOT_CONFIGURED", "Codex models upstream HTTP client is not configured")
		}
		req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
		resp, err = s.httpUpstream.Do(req, request.proxyURL, request.accountID, request.accountConcurrency)
	} else {
		client, clientErr := httpclient.GetClient(httpclient.Options{
			ProxyURL:              request.proxyURL,
			Timeout:               codexModelsManifestRequestTimeout,
			ResponseHeaderTimeout: 10 * time.Second,
		})
		if clientErr != nil {
			return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_PROXY_INVALID", "invalid proxy configuration: %v", clientErr)
		}
		resp, err = client.Do(req)
	}
	if err != nil {
		return nil, &codexModelsManifestUpstreamError{
			err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed: %v", err),
			retryable: isRetryableCodexModelsManifestTransportError(err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return &CodexModelsManifest{ETag: resp.Header.Get("ETag"), NotModified: true}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, &codexModelsManifestUpstreamError{
			err: infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest upstream error %d: %s", resp.StatusCode, message),
			retryable: resp.StatusCode == http.StatusTooManyRequests ||
				(resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexModelsManifestBodyLimit))
	if err != nil {
		return nil, &codexModelsManifestUpstreamError{
			err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "read codex models manifest response: %v", err),
			retryable: isRetryableCodexModelsManifestTransportError(err),
		}
	}
	return &CodexModelsManifest{Body: body, ETag: resp.Header.Get("ETag")}, nil
}

func isRetryableCodexModelsManifestTransportError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var goAwayErr http2.GoAwayError
	if errors.As(err, &goAwayErr) {
		return true
	}
	var streamErr http2.StreamError
	if errors.As(err, &streamErr) {
		return true
	}
	var connectionErr http2.ConnectionError
	if errors.As(err, &connectionErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// net/http uses unexported HTTP/2 error types, so typed matching is not
	// possible for errors produced by the standard library transport.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "http2:") &&
		(strings.Contains(message, "goaway") ||
			strings.Contains(message, "refused_stream") ||
			strings.Contains(message, "frame too large")) {
		return true
	}
	if strings.Contains(message, "stream error: stream id ") {
		return true
	}
	for _, code := range []http2.ErrCode{
		http2.ErrCodeNo,
		http2.ErrCodeProtocol,
		http2.ErrCodeInternal,
		http2.ErrCodeFlowControl,
		http2.ErrCodeSettingsTimeout,
		http2.ErrCodeStreamClosed,
		http2.ErrCodeFrameSize,
		http2.ErrCodeRefusedStream,
		http2.ErrCodeCancel,
		http2.ErrCodeCompression,
		http2.ErrCodeConnect,
		http2.ErrCodeEnhanceYourCalm,
		http2.ErrCodeInadequateSecurity,
		http2.ErrCodeHTTP11Required,
	} {
		if strings.Contains(message, "connection error: "+strings.ToLower(code.String())) {
			return true
		}
	}
	return false
}

func buildCodexModelsManifestCacheKey(request codexModelsManifestRequest) string {
	hasher := sha256.New()
	_, _ = fmt.Fprintf(hasher, "%d\n%d\n%s\n%s\n", request.accountID, request.credentialAccountID, request.proxyURL, request.url)
	headerNames := make([]string, 0, len(request.headers))
	for name := range request.headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		_, _ = fmt.Fprintf(hasher, "%s\n", strings.ToLower(name))
		for _, value := range request.headers[name] {
			_, _ = fmt.Fprintf(hasher, "%s\n", value)
		}
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func codexModelsManifestForClient(manifest *CodexModelsManifest, ifNoneMatch string) *CodexModelsManifest {
	if manifest == nil {
		return nil
	}
	if codexModelsManifestETagMatches(ifNoneMatch, manifest.ETag) {
		return &CodexModelsManifest{ETag: manifest.ETag, NotModified: true}
	}
	return manifest
}

func codexModelsManifestETagMatches(ifNoneMatch, etag string) bool {
	etag = strings.TrimSpace(etag)
	if etag == "" {
		return false
	}
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		if len(value) >= 2 && strings.EqualFold(value[:2], "W/") {
			value = strings.TrimSpace(value[2:])
		}
		return value
	}
	want := normalize(etag)
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || normalize(candidate) == want {
			return true
		}
	}
	return false
}

func isOfficialOpenAIModelsBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	hostname := strings.TrimSuffix(parsed.Hostname(), ".")
	return strings.EqualFold(hostname, "api.openai.com")
}

func buildCodexModelsManifestURL(endpoint string, appendModelsPath bool, clientVersion string) (*url.URL, error) {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if requestURL.Fragment != "" {
		return nil, fmt.Errorf("URL fragments are not supported")
	}

	query := requestURL.Query()
	requestURL.RawQuery = ""
	requestURL.ForceQuery = false
	if appendModelsPath {
		requestURL, err = url.Parse(buildOpenAIModelsURL(requestURL.String()))
		if err != nil {
			return nil, err
		}
	}
	query.Set("client_version", clientVersion)
	requestURL.RawQuery = query.Encode()
	return requestURL, nil
}
