package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesEveryConcretePlatform(t *testing.T) {
	require.ElementsMatch(t, []string{
		"anthropic",
		"openai",
		"opencode_go",
		"clinepass",
		"openrouter",
		"commandcode",
		"gemini",
		"antigravity",
		"grok",
		"kimi",
		"zhipu",
		"deepseek",
	}, AllPlatforms())
}
