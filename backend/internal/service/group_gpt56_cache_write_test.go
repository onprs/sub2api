package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupGPT56CacheWriteInferenceIsOpenAIScoped(t *testing.T) {
	openAIGroup := &Group{
		Platform:                      PlatformOpenAI,
		InferGPT56CacheWrite:          true,
		InferGPT56CacheWriteMinTokens: 2048,
	}
	require.True(t, openAIGroup.ShouldInferGPT56CacheWrite())
	require.Equal(t, 2048, openAIGroup.GPT56CacheWriteInferenceMinTokens())

	nonOpenAIGroup := &Group{Platform: PlatformOpenCodeGo, InferGPT56CacheWrite: true}
	require.False(t, nonOpenAIGroup.ShouldInferGPT56CacheWrite())
	require.Equal(t, 1024, nonOpenAIGroup.GPT56CacheWriteInferenceMinTokens())
}

func TestGPT56CacheWriteInferenceAuthSnapshotRoundTrip(t *testing.T) {
	apiKey := &APIKey{
		User: &User{ID: 1, Status: StatusActive, Role: RoleUser},
		Group: &Group{
			ID:                            2,
			Platform:                      PlatformOpenAI,
			InferGPT56CacheWrite:          true,
			InferGPT56CacheWriteMinTokens: 3072,
		},
	}
	svc := &APIKeyService{}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.Group)
	require.True(t, snapshot.Group.InferGPT56CacheWrite)
	require.Equal(t, 3072, snapshot.Group.InferGPT56CacheWriteMinTokens)

	restored := svc.snapshotToAPIKey("sk-test", snapshot)
	require.NotNil(t, restored)
	require.NotNil(t, restored.Group)
	require.True(t, restored.Group.ShouldInferGPT56CacheWrite())
	require.Equal(t, 3072, restored.Group.GPT56CacheWriteInferenceMinTokens())
}
