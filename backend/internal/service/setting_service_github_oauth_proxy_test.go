package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type githubOAuthProxySettingRepo struct {
	values  map[string]string
	updates map[string]string
}

func (r *githubOAuthProxySettingRepo) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (r *githubOAuthProxySettingRepo) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (r *githubOAuthProxySettingRepo) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (r *githubOAuthProxySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *githubOAuthProxySettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	r.updates = make(map[string]string, len(settings))
	for key, value := range settings {
		r.updates[key] = value
	}
	return nil
}

func (r *githubOAuthProxySettingRepo) GetAll(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *githubOAuthProxySettingRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

type githubOAuthProxyRepo struct {
	byID map[int64]*Proxy
}

func (r *githubOAuthProxyRepo) Create(context.Context, *Proxy) error { panic("unexpected Create call") }
func (r *githubOAuthProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	if proxy, ok := r.byID[id]; ok {
		return proxy, nil
	}
	return nil, ErrProxyNotFound
}
func (r *githubOAuthProxyRepo) ListByIDs(context.Context, []int64) ([]Proxy, error) {
	panic("unexpected ListByIDs call")
}
func (r *githubOAuthProxyRepo) Update(context.Context, *Proxy) error { panic("unexpected Update call") }
func (r *githubOAuthProxyRepo) Delete(context.Context, int64) error  { panic("unexpected Delete call") }
func (r *githubOAuthProxyRepo) List(context.Context, pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (r *githubOAuthProxyRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (r *githubOAuthProxyRepo) ListWithFiltersAndAccountCount(context.Context, pagination.PaginationParams, string, string, string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFiltersAndAccountCount call")
}
func (r *githubOAuthProxyRepo) ListActive(context.Context) ([]Proxy, error) {
	panic("unexpected ListActive call")
}
func (r *githubOAuthProxyRepo) ListActiveWithAccountCount(context.Context) ([]ProxyWithAccountCount, error) {
	panic("unexpected ListActiveWithAccountCount call")
}
func (r *githubOAuthProxyRepo) ExistsByHostPortAuth(context.Context, string, int, string, string) (bool, error) {
	panic("unexpected ExistsByHostPortAuth call")
}
func (r *githubOAuthProxyRepo) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	panic("unexpected CountAccountsByProxyID call")
}
func (r *githubOAuthProxyRepo) ListAccountSummariesByProxyID(context.Context, int64) ([]ProxyAccountSummary, error) {
	panic("unexpected ListAccountSummariesByProxyID call")
}
func (r *githubOAuthProxyRepo) SweepExpiredProxies(context.Context, time.Time) (int64, error) {
	panic("unexpected SweepExpiredProxies call")
}
func (r *githubOAuthProxyRepo) ListAllForFallback(context.Context) ([]Proxy, error) {
	panic("unexpected ListAllForFallback call")
}
func (r *githubOAuthProxyRepo) CountExpired(context.Context) (int64, error) {
	panic("unexpected CountExpired call")
}
func (r *githubOAuthProxyRepo) CountExpiringSoon(context.Context, time.Time) (int64, error) {
	panic("unexpected CountExpiringSoon call")
}

func TestMergeEmailOAuthBaseConfigPreservesProxyFallback(t *testing.T) {
	proxyID := int64(7)
	merged := mergeEmailOAuthBaseConfig(config.EmailOAuthProviderConfig{}, config.EmailOAuthProviderConfig{
		ProxyID:  &proxyID,
		ProxyURL: " http://127.0.0.1:7890 ",
	})

	require.NotNil(t, merged.ProxyID)
	require.Equal(t, int64(7), *merged.ProxyID)
	require.Equal(t, "http://127.0.0.1:7890", merged.ProxyURL)

	proxyID = 8
	require.Equal(t, int64(7), *merged.ProxyID, "合并结果不能引用调用方可变指针")
}

func TestSettingServiceGitHubOAuthProxyIDFallsBackToConfigWhenSettingAbsent(t *testing.T) {
	ctx := context.Background()
	proxyID := int64(9)
	repo := &githubOAuthProxySettingRepo{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{GitHubOAuth: config.EmailOAuthProviderConfig{
		Enabled:      true,
		ClientID:     "github-client",
		ClientSecret: "github-secret",
		RedirectURL:  "https://example.com/api/v1/auth/oauth/github/callback",
		ProxyID:      &proxyID,
	}})
	svc.SetProxyRepository(&githubOAuthProxyRepo{byID: map[int64]*Proxy{
		proxyID: {
			ID:       proxyID,
			Protocol: "http",
			Host:     "proxy.example.com",
			Port:     8080,
			Status:   StatusActive,
		},
	}})

	cfg, err := svc.GetEmailOAuthProviderConfig(ctx, "github")
	require.NoError(t, err)
	require.NotNil(t, cfg.ProxyID)
	require.Equal(t, proxyID, *cfg.ProxyID)
	require.Equal(t, "http://proxy.example.com:8080", cfg.ProxyURL)
}

func TestSettingServiceGitHubOAuthProxyIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	proxyID := int64(7)
	repo := &githubOAuthProxySettingRepo{values: map[string]string{
		SettingKeyGitHubOAuthEnabled:             "true",
		SettingKeyGitHubOAuthClientID:            "github-client",
		SettingKeyGitHubOAuthClientSecret:        "github-secret",
		SettingKeyGitHubOAuthRedirectURL:         "https://cdn.api.onprs.top/api/v1/auth/oauth/github/callback",
		SettingKeyGitHubOAuthFrontendRedirectURL: "/auth/oauth/callback",
		SettingKeyGitHubOAuthProxyID:             "7",
	}}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetProxyRepository(&githubOAuthProxyRepo{byID: map[int64]*Proxy{
		proxyID: {
			ID:        proxyID,
			Name:      "github-proxy",
			Protocol:  "http",
			Host:      "proxy.example.com",
			Port:      8080,
			Username:  "user",
			Password:  "pass",
			Status:    StatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}})

	settings, err := svc.GetAllSettings(ctx)
	require.NoError(t, err)
	require.NotNil(t, settings.GitHubOAuthProxyID)
	require.Equal(t, proxyID, *settings.GitHubOAuthProxyID)

	cfg, err := svc.GetEmailOAuthProviderConfig(ctx, "github")
	require.NoError(t, err)
	require.NotNil(t, cfg.ProxyID)
	require.Equal(t, proxyID, *cfg.ProxyID)
	require.Equal(t, "http://user:pass@proxy.example.com:8080", cfg.ProxyURL)

	err = svc.UpdateSettings(ctx, &SystemSettings{
		GitHubOAuthProxyID: &proxyID,
	})
	require.NoError(t, err)
	require.Equal(t, "7", repo.updates[SettingKeyGitHubOAuthProxyID])

	err = svc.UpdateSettings(ctx, &SystemSettings{})
	require.NoError(t, err)
	require.Equal(t, "", repo.updates[SettingKeyGitHubOAuthProxyID])
}
