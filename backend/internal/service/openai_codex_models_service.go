package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
)

// chatgptCodexModelsURL is the ChatGPT Codex models manifest endpoint.
// Package-level variable so tests can point it at a stub server.
var chatgptCodexModelsURL = "https://chatgpt.com/backend-api/codex/models"

const codexModelsManifestBodyLimit int64 = 8 << 20

// CodexModelsManifest carries the raw upstream manifest payload plus caching
// metadata so handlers can pass both through to the client untouched.
type CodexModelsManifest struct {
	Body        []byte
	ETag        string
	NotModified bool
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
	credAccount, _, err := s.resolveCodexModelsManifestCredentialAccount(ctx, account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_OAUTH_REQUIRED", "Codex models manifest requires an OpenAI OAuth account: %v", err)
	}
	accessToken, err := s.getCodexModelsManifestAccessToken(ctx, credAccount)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_TOKEN_MISSING", "account has no Codex backend access token: %v", err)
	}

	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = openAICodexProbeVersion
	}
	requestURL := chatgptCodexModelsURL + "?client_version=" + url.QueryEscape(clientVersion)

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_REQUEST_FAILED", "create codex models request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Originator", "codex_cli_rs")
	req.Header.Set("Version", clientVersion)
	req.Header.Set("User-Agent", codexCLIUserAgent)
	if ifNoneMatch = strings.TrimSpace(ifNoneMatch); ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	setOpenAIChatGPTAccountHeaders(req.Header, credAccount)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               15 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_CODEX_MODELS_PROXY_INVALID", "invalid proxy configuration: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest request failed: %v", err)
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
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "codex models manifest upstream error %d: %s", resp.StatusCode, message)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, codexModelsManifestBodyLimit))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_CODEX_MODELS_UPSTREAM_FAILED", "read codex models manifest response: %v", err)
	}
	return &CodexModelsManifest{Body: body, ETag: resp.Header.Get("ETag")}, nil
}
