package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeRedactsProbeKey(t *testing.T) {
	t.Setenv("SUB2API_PROBE_API_KEY", "probe-secret-value")
	result := sanitize("upstream rejected probe-secret-value\nwith details")
	require.Equal(t, "upstream rejected [redacted] with details", result)
}

func TestSanitizeWithoutKeyDoesNotCorruptText(t *testing.T) {
	require.NoError(t, os.Unsetenv("SUB2API_PROBE_API_KEY"))
	require.Equal(t, "plain error", sanitize("plain error"))
}

func TestRunRequiresEnvironmentKeyBeforeNetwork(t *testing.T) {
	t.Setenv("SUB2API_PROBE_API_KEY", "")
	result := run("openai_chat_completions", "openai_responses", "http://invalid.example", "model", "hello", false)
	require.Equal(t, 0, result.HTTPStatus)
	require.Equal(t, "SUB2API_PROBE_API_KEY is not set", result.Error)
}
