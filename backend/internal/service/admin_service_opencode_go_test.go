package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsListCandidateIDs_OpenCodeGoUsesOpenCodeCatalog(t *testing.T) {
	models := defaultModelsListCandidateIDs(PlatformOpenCodeGo)

	require.Contains(t, models, "glm-5.2")
	require.Contains(t, models, "qwen3.5-plus")
	require.NotContains(t, models, "claude-sonnet-4-6")
}
