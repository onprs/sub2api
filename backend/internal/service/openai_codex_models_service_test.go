package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newCodexModelsTestAccount() *Account {
	return &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "acc-123",
		},
	}
}

func TestFetchCodexModelsManifestPassthrough(t *testing.T) {
	manifestBody := `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5"}]}`

	var gotAuth, gotAccountID, gotOriginator, gotClientVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("Originator")
		gotClientVersion = r.URL.Query().Get("client_version")
		w.Header().Set("ETag", `W/"abc123"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestBody))
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	manifest, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", "")
	if err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}

	if string(manifest.Body) != manifestBody {
		t.Errorf("body not passed through verbatim: got %q", manifest.Body)
	}
	if manifest.ETag != `W/"abc123"` {
		t.Errorf("etag not passed through: got %q", manifest.ETag)
	}
	if gotAuth != "Bearer test-access-token" {
		t.Errorf("authorization header: got %q", gotAuth)
	}
	if gotAccountID != "acc-123" {
		t.Errorf("chatgpt-account-id header: got %q", gotAccountID)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Errorf("originator header: got %q", gotOriginator)
	}
	if gotClientVersion != "0.137.0" {
		t.Errorf("client_version query: got %q", gotClientVersion)
	}
}

func TestFetchCodexModelsManifestDefaultClientVersion(t *testing.T) {
	var gotClientVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientVersion = r.URL.Query().Get("client_version")
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "", ""); err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}
	if gotClientVersion != openAICodexProbeVersion {
		t.Errorf("default client_version: got %q, want %q", gotClientVersion, openAICodexProbeVersion)
	}
}

func TestFetchCodexModelsManifestNotModified(t *testing.T) {
	var gotIfNoneMatch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", `W/"abc123"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	manifest, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", `W/"abc123"`)
	if err != nil {
		t.Fatalf("FetchCodexModelsManifest returned error: %v", err)
	}
	if !manifest.NotModified {
		t.Error("expected NotModified to be true")
	}
	if gotIfNoneMatch != `W/"abc123"` {
		t.Errorf("if-none-match header: got %q", gotIfNoneMatch)
	}
}

func TestFetchCodexModelsManifestUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"boom"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	original := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	defer func() { chatgptCodexModelsURL = original }()

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), newCodexModelsTestAccount(), "0.137.0", ""); err == nil {
		t.Fatal("expected error for upstream 500, got nil")
	}
}

func TestFetchCodexModelsManifestMissingToken(t *testing.T) {
	account := newCodexModelsTestAccount()
	delete(account.Credentials, "access_token")

	s := &OpenAIGatewayService{}
	if _, err := s.FetchCodexModelsManifest(context.Background(), account, "0.137.0", ""); err == nil {
		t.Fatal("expected error for missing access token, got nil")
	}
}

func TestFetchCodexModelsManifestRejectsAPIKeyAccount(t *testing.T) {
	account := &Account{
		ID:          11,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key": "sk-test",
		},
	}

	s := &OpenAIGatewayService{}
	_, err := s.FetchCodexModelsManifest(context.Background(), account, "0.137.0", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an OpenAI OAuth account")
}

func TestSelectCodexModelsManifestAccountSkipsAPIKeyAndSelectsOAuth(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	groupID := int64(99)
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			LastUsedAt:  &old,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"api_key": "sk-priority"},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    5,
			LastUsedAt:  &now,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{
				"access_token":       "oauth-token",
				"chatgpt_account_id": "acct-oauth",
			},
		},
	}}}
	s := &OpenAIGatewayService{accountRepo: repo}

	selected, err := s.SelectCodexModelsManifestAccount(context.Background(), &groupID)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID)
}

func TestSelectCodexModelsManifestAccountSkipsShadowWithInvalidParent(t *testing.T) {
	parentID := int64(10)
	groupID := int64(99)
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{
		{
			ID:              1,
			Platform:        PlatformOpenAI,
			Type:            AccountTypeOAuth,
			Status:          StatusActive,
			Schedulable:     true,
			Concurrency:     1,
			Priority:        1,
			ParentAccountID: &parentID,
			GroupIDs:        []int64{groupID},
		},
		{
			ID:          parentID,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-parent"},
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    2,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"access_token": "oauth-token"},
		},
	}}}
	s := &OpenAIGatewayService{accountRepo: repo}

	selected, err := s.SelectCodexModelsManifestAccount(context.Background(), &groupID)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID)
}

func TestSelectCodexModelsManifestAccountNoOAuthAvailable(t *testing.T) {
	groupID := int64(99)
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"api_key": "sk-only"},
		},
	}}}
	s := &OpenAIGatewayService{accountRepo: repo}

	selected, err := s.SelectCodexModelsManifestAccount(context.Background(), &groupID)
	require.Error(t, err)
	require.Nil(t, selected)
	require.True(t, errors.Is(err, ErrNoAvailableAccounts), "got %v", err)
}

type codexModelsSnapshotHydrationCache struct {
	snapshot []*Account
	accounts map[int64]*Account
}

func (c *codexModelsSnapshotHydrationCache) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	return c.snapshot, true, nil
}

func (c *codexModelsSnapshotHydrationCache) SetSnapshot(ctx context.Context, bucket SchedulerBucket, accounts []Account) error {
	return nil
}

func (c *codexModelsSnapshotHydrationCache) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if c.accounts == nil {
		return nil, nil
	}
	return c.accounts[accountID], nil
}

func (c *codexModelsSnapshotHydrationCache) SetAccount(ctx context.Context, account *Account) error {
	return nil
}

func (c *codexModelsSnapshotHydrationCache) DeleteAccount(ctx context.Context, accountID int64) error {
	return nil
}

func (c *codexModelsSnapshotHydrationCache) UpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}

func (c *codexModelsSnapshotHydrationCache) TryLockBucket(ctx context.Context, bucket SchedulerBucket, ttl time.Duration) (bool, error) {
	return true, nil
}

func (c *codexModelsSnapshotHydrationCache) UnlockBucket(ctx context.Context, bucket SchedulerBucket) error {
	return nil
}

func (c *codexModelsSnapshotHydrationCache) ListBuckets(ctx context.Context) ([]SchedulerBucket, error) {
	return nil, nil
}

func (c *codexModelsSnapshotHydrationCache) GetOutboxWatermark(ctx context.Context) (int64, error) {
	return 0, nil
}

func (c *codexModelsSnapshotHydrationCache) SetOutboxWatermark(ctx context.Context, id int64) error {
	return nil
}

func TestSelectCodexModelsManifestAccountHydratesSnapshotBeforeTokenCheck(t *testing.T) {
	groupID := int64(99)
	cache := &codexModelsSnapshotHydrationCache{
		snapshot: []*Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{groupID},
			},
		},
		accounts: map[int64]*Account{
			1: {
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{groupID},
				Credentials: map[string]any{"access_token": "hydrated-token"},
			},
		},
	}
	s := &OpenAIGatewayService{
		schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil),
		cache:             &stubGatewayCache{},
	}

	selected, err := s.SelectCodexModelsManifestAccount(context.Background(), &groupID)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, "hydrated-token", selected.GetOpenAIAccessToken())
}
