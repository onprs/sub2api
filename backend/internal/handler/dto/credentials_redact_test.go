package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactCredentials_NilInput(t *testing.T) {
	out, status := RedactCredentials(nil)
	require.Nil(t, out)
	require.Nil(t, status)
}

func TestRedactCredentials_StripsSensitiveKeysAndReportsStatus(t *testing.T) {
	in := map[string]any{
		"refresh_token":            "rt-secret",
		"access_token":             "at-secret",
		"api_key":                  "sk-secret",
		"console_cookie":           "auth=secret",
		"console_workspace_id":     "wrk_secret",
		"console_auth_source":      "windows_cdp_helper",
		"console_auth_imported_at": "2026-06-22T10:00:00Z",
		"console_auth_expires_at":  "2026-06-29T10:00:00Z",
		"aws_secret_access_key":    "aws-secret",
		"service_account_json":     map[string]any{"private_key": "..."},
		"private_key":              "raw-key",
		"agent_private_key":        "agent-key-secret",
		// 非敏感
		"base_url":      "https://api.example.com",
		"model_mapping": map[string]any{"foo": "bar"},
		"project_id":    "proj-1",
		"expires_at":    int64(123456),
	}

	out, status := RedactCredentials(in)

	require.NotContains(t, out, "refresh_token")
	require.NotContains(t, out, "access_token")
	require.NotContains(t, out, "api_key")
	require.NotContains(t, out, "console_cookie")
	require.NotContains(t, out, "console_workspace_id")
	require.NotContains(t, out, "console_auth_source")
	require.NotContains(t, out, "console_auth_imported_at")
	require.NotContains(t, out, "console_auth_expires_at")
	require.NotContains(t, out, "aws_secret_access_key")
	require.NotContains(t, out, "service_account_json")
	require.NotContains(t, out, "private_key")
	require.NotContains(t, out, "agent_private_key")

	require.Equal(t, "https://api.example.com", out["base_url"])
	require.Equal(t, map[string]any{"foo": "bar"}, out["model_mapping"])
	require.Equal(t, "proj-1", out["project_id"])
	require.Equal(t, int64(123456), out["expires_at"])

	require.True(t, status["has_refresh_token"])
	require.True(t, status["has_access_token"])
	require.True(t, status["has_api_key"])
	require.True(t, status["has_console_cookie"])
	require.True(t, status["has_console_workspace_id"])
	require.True(t, status["has_console_auth_source"])
	require.True(t, status["has_console_auth_imported_at"])
	require.True(t, status["has_console_auth_expires_at"])
	require.True(t, status["has_aws_secret_access_key"])
	require.True(t, status["has_service_account_json"])
	require.True(t, status["has_private_key"])
	require.True(t, status["has_agent_private_key"])

	// 状态 map 不应携带非敏感键的 has_*
	require.NotContains(t, status, "has_base_url")
	require.NotContains(t, status, "has_project_id")
}

func TestRedactCredentials_EmptyValuesNotMarkedPresent(t *testing.T) {
	in := map[string]any{
		"refresh_token": "",
		"access_token":  nil,
		"api_key":       false,
		"id_token":      "actual-id",
	}
	out, status := RedactCredentials(in)
	require.Empty(t, out, "敏感键即使为空也不应出现在 redacted output")
	require.False(t, status["has_refresh_token"])
	require.False(t, status["has_access_token"])
	require.False(t, status["has_api_key"])
	require.True(t, status["has_id_token"])
}

func TestRedactCredentials_DoesNotMutateInput(t *testing.T) {
	in := map[string]any{
		"refresh_token": "secret",
		"base_url":      "x",
	}
	_, _ = RedactCredentials(in)
	require.Equal(t, "secret", in["refresh_token"], "原始 map 不应被修改")
	require.Equal(t, "x", in["base_url"])
}

func TestRedactCredentials_AllKnownSensitiveKeys(t *testing.T) {
	keys := []string{
		"access_token", "refresh_token", "id_token",
		"api_key", "session_key", "cookie", "console_cookie",
		"console_workspace_id", "console_auth_source", "console_auth_imported_at", "console_auth_expires_at",
		"aws_secret_access_key", "aws_session_token",
		"service_account_json", "service_account", "private_key",
		"agent_private_key",
	}
	in := make(map[string]any, len(keys))
	for _, k := range keys {
		in[k] = "filled"
	}
	out, status := RedactCredentials(in)
	require.Empty(t, out)
	for _, k := range keys {
		require.True(t, status["has_"+k], "key %s 应在 status 中标记为已配置", k)
	}
}
