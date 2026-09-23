package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestValidateAnthropicAPIKeyBaseURLCustomHostPolicy(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled:                         true,
			UpstreamHosts:                   []string{"api.anthropic.com"},
			AllowAnthropicAPIKeyCustomHosts: true,
		}},
	}

	normalized, err := validateAnthropicAPIKeyBaseURL("https://relay.example.com/v1/", cfg)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.com/v1", normalized)

	for _, raw := range []string{
		"http://relay.example.com/v1",
		"https://localhost/v1",
		"https://127.0.0.1/v1",
		"https://10.0.0.8/v1",
	} {
		_, err := validateAnthropicAPIKeyBaseURL(raw, cfg)
		require.Error(t, err, "expected unsafe Anthropic API Key base URL %q to be rejected", raw)
	}

	cfg.Security.URLAllowlist.AllowAnthropicAPIKeyCustomHosts = false
	_, err = validateAnthropicAPIKeyBaseURL("https://relay.example.com/v1", cfg)
	require.Error(t, err, "strict allowlist mode must reject unknown Anthropic hosts")

	normalized, err = validateAnthropicAPIKeyBaseURL("https://api.anthropic.com", cfg)
	require.NoError(t, err)
	require.Equal(t, "https://api.anthropic.com", normalized)
}
