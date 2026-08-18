package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepositoryGetByKeyForAuthPreservesGroupGPT56CacheWriteInference(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-gpt56-cache@test.com")

	group, err := client.Group.Create().
		SetName("g-auth-gpt56-cache").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetInferGpt56CacheWrite(true).
		SetInferGpt56CacheWriteMinTokens(2048).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-gpt56-cache",
		Name:    "GPT56 Cache Key",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.True(t, got.Group.ShouldInferGPT56CacheWrite())
	require.Equal(t, 2048, got.Group.GPT56CacheWriteInferenceMinTokens())
}
