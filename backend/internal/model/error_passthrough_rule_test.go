package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllPlatformsIncludesOpenCodeGo(t *testing.T) {
	require.Contains(t, AllPlatforms(), PlatformOpenCodeGo)
}
