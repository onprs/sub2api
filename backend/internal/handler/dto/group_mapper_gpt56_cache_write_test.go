package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupFromServiceAdminIncludesGPT56CacheWriteInference(t *testing.T) {
	group := &service.Group{
		ID:                            42,
		Platform:                      service.PlatformOpenAI,
		InferGPT56CacheWrite:          true,
		InferGPT56CacheWriteMinTokens: 2048,
	}

	got := GroupFromServiceAdmin(group)
	require.NotNil(t, got)
	require.True(t, got.InferGPT56CacheWrite)
	require.Equal(t, 2048, got.InferGPT56CacheWriteMinTokens)
}

func TestGroupFromServiceAdminUsesDefaultGPT56CacheWriteThreshold(t *testing.T) {
	got := GroupFromServiceAdmin(&service.Group{Platform: service.PlatformOpenAI})
	require.NotNil(t, got)
	require.Equal(t, 1024, got.InferGPT56CacheWriteMinTokens)
}
