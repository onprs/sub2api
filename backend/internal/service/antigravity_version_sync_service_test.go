package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

type stubAntigravityManifestFetcher struct {
	version string
	err     error
	calls   int
}

func (f *stubAntigravityManifestFetcher) FetchLatestVersion(_ context.Context) (string, error) {
	f.calls++
	return f.version, f.err
}

type antigravitySyncSettingRepoStub struct {
	values    map[string]string
	updatedAt map[string]time.Time
	setCalls  int
}

func newAntigravitySyncSettingRepoStub(initial map[string]string) *antigravitySyncSettingRepoStub {
	vals := make(map[string]string, len(initial))
	for k, v := range initial {
		vals[k] = v
	}
	return &antigravitySyncSettingRepoStub{
		values:    vals,
		updatedAt: make(map[string]time.Time),
	}
}

func (r *antigravitySyncSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	v, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v, UpdatedAt: r.updatedAt[key]}, nil
}

func (r *antigravitySyncSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	v, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (r *antigravitySyncSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.setCalls++
	r.values[key] = value
	r.updatedAt[key] = time.Now()
	return nil
}

func (r *antigravitySyncSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := r.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (r *antigravitySyncSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.values[k] = v
	}
	return nil
}

func (r *antigravitySyncSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(r.values))
	for k, v := range r.values {
		out[k] = v
	}
	return out, nil
}

func (r *antigravitySyncSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func TestAntigravityVersionSyncService_RunOnce_PersistsNewerVersionAndInvalidatesCache(t *testing.T) {
	repo := newAntigravitySyncSettingRepoStub(nil)
	settingSvc := NewSettingService(repo, nil)
	// 先预热缓存为内置默认值
	require.Equal(t, antigravity.GetDefaultUserAgentVersion(), settingSvc.GetAntigravityUserAgentVersion(context.Background()))

	fetcher := &stubAntigravityManifestFetcher{version: "1.9.0"}
	syncSvc := NewAntigravityVersionSyncService(repo, settingSvc, fetcher, time.Hour)
	syncSvc.runOnce()

	require.Equal(t, 1, fetcher.calls)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, "1.9.0", repo.values[SettingKeyAntigravityUserAgentVersionSynced])
	// 缓存应已失效并立即生效
	require.Equal(t, "1.9.0", settingSvc.GetAntigravityUserAgentVersion(context.Background()))
}

func TestAntigravityVersionSyncService_RunOnce_RespectsDisabledSwitch(t *testing.T) {
	repo := newAntigravitySyncSettingRepoStub(map[string]string{
		SettingKeyAntigravityVersionAutoSyncEnabled: "false",
	})
	fetcher := &stubAntigravityManifestFetcher{version: "1.9.0"}
	syncSvc := NewAntigravityVersionSyncService(repo, NewSettingService(repo, nil), fetcher, time.Hour)
	syncSvc.runOnce()

	require.Equal(t, 0, fetcher.calls)
	require.Equal(t, 0, repo.setCalls)
}

func TestAntigravityVersionSyncService_RunOnce_NeverDowngrades(t *testing.T) {
	repo := newAntigravitySyncSettingRepoStub(map[string]string{
		SettingKeyAntigravityUserAgentVersionSynced: "1.9.0",
	})
	fetcher := &stubAntigravityManifestFetcher{version: "1.8.0"}
	syncSvc := NewAntigravityVersionSyncService(repo, NewSettingService(repo, nil), fetcher, time.Hour)
	syncSvc.runOnce()

	require.Equal(t, 1, fetcher.calls)
	require.Equal(t, 0, repo.setCalls)
	require.Equal(t, "1.9.0", repo.values[SettingKeyAntigravityUserAgentVersionSynced])
}

func TestAntigravityVersionSyncService_RunInitial_SkipsWhenFresh(t *testing.T) {
	repo := newAntigravitySyncSettingRepoStub(map[string]string{
		SettingKeyAntigravityUserAgentVersionSynced: "1.9.0",
	})
	repo.updatedAt[SettingKeyAntigravityUserAgentVersionSynced] = time.Now().Add(-5 * time.Minute)
	fetcher := &stubAntigravityManifestFetcher{version: "2.0.0"}
	syncSvc := NewAntigravityVersionSyncService(repo, NewSettingService(repo, nil), fetcher, time.Hour)
	syncSvc.runInitial()

	require.Equal(t, 0, fetcher.calls)
}

func TestHTTPAntigravityManifestFetcher_ParsesOfficialManifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1.6.2","url":"https://storage.googleapis.com/example.tar.gz","sha512":"abc"}`))
	}))
	defer srv.Close()

	fetcher := &httpAntigravityManifestFetcher{
		httpClient:  srv.Client(),
		manifestURL: func() string { return srv.URL },
	}
	version, err := fetcher.FetchLatestVersion(context.Background())
	require.NoError(t, err)
	require.Equal(t, "1.6.2", version)
}
