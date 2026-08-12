package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildCLIImportScript_LinuxEmbedsAllClientConfigs(t *testing.T) {
	groupID := int64(7)
	input := CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex", "claude-sonnet-4-20250514"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex":            knownOpenCodeCapability("GPT-5.1 Codex"),
			"claude-sonnet-4-20250514": knownOpenCodeCapability("Claude Sonnet 4"),
		},
	}

	result, err := BuildCLIImportScript(input)
	require.NoError(t, err)
	require.Equal(t, "sub2api-cli-import.sh", result.Filename)
	require.Equal(t, "application/octet-stream", result.ContentType)

	body := string(result.Body)
	require.Contains(t, body, "#!/usr/bin/env bash")
	require.Contains(t, body, "SUB2API_KEY_42")
	require.Contains(t, body, "https://api.example.com/v1")
	require.Contains(t, body, `"provider_name":"OnprsCodexApi"`)
	require.Contains(t, body, "sk-user-test-key")
	require.Contains(t, body, "wire_api = \"responses\"")
	require.Contains(t, body, "~/.codex/config.toml")
	require.Contains(t, body, "~/.config/opencode/opencode.jsonc")
	require.Contains(t, body, "@ai-sdk/openai-compatible")
	require.Contains(t, body, "OpenCode Desktop caches provider/auth data")
	require.Contains(t, body, "refresh_opencode_desktop_if_needed")
	require.Contains(t, body, "\"reasoning\":true")
	require.Contains(t, body, "\"attachment\":true")
	require.Contains(t, body, "\"tool_call\":true")
	require.Contains(t, body, "\"modalities\"")
	require.Contains(t, body, "\"input\":[\"text\",\"image\",\"pdf\"]")
	require.Contains(t, body, "\"limit\":{\"context\":1000000,\"output\":32768}")
	require.Contains(t, body, "\"cost\":{\"input\":1.25,\"output\":10,\"cache_read\":0.125,\"cache_write\":1.25}")
	require.Contains(t, body, "~/.claude/settings.json")
	require.Contains(t, body, "ANTHROPIC_BASE_URL")
	require.Contains(t, body, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY")
}

func TestBuildCLIImportScript_LinuxDoesNotConsumePromptStdinWithPythonHereDoc(t *testing.T) {
	groupID := int64(7)
	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex": knownOpenCodeCapability("GPT-5.1 Codex"),
		},
	})
	require.NoError(t, err)

	body := string(result.Body)
	require.NotContains(t, body, "python3 - <<'PY'")
	require.Contains(t, body, "cat > \"$tmp_py\" <<'PY'")
	require.Contains(t, body, "python3 \"$tmp_py\"")
}

func TestBuildCLIImportShellHelperWritesConfigsInTempHome(t *testing.T) {
	pythonPath := findPythonForCLIImportTest(t)
	groupID := int64(7)
	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex": knownOpenCodeCapability("GPT-5.1 Codex"),
		},
	})
	require.NoError(t, err)

	helper := extractShellPythonHelper(t, string(result.Body))
	home := t.TempDir()
	runPythonHelper := func(stdin string) string {
		t.Helper()
		cmd := exec.Command(pythonPath, "-c", helper)
		cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "SHELL=/bin/bash", "SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH=1")
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}

	runPythonHelper("4\ny\n")

	codexConfig := readTestFile(t, filepath.Join(home, ".codex", "config.toml"))
	require.Contains(t, codexConfig, `model_provider = "sub2api_openai_42"`)
	require.Contains(t, codexConfig, `model = "gpt-5.1-codex"`)
	require.Equal(t, 1, strings.Count(codexConfig, "[model_providers.sub2api_openai_42]"))
	require.Contains(t, codexConfig, `name = "OnprsCodexApi"`)
	require.Contains(t, codexConfig, `wire_api = "responses"`)

	var opencode map[string]any
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"))), &opencode))
	require.NoFileExists(t, filepath.Join(home, ".config", "opencode", "opencode.json"))
	providers, ok := opencode["provider"].(map[string]any)
	require.True(t, ok)
	provider, ok := providers["sub2api_openai_42"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "OnprsCodexApi", provider["name"])
	require.Equal(t, "@ai-sdk/openai-compatible", provider["npm"])
	options, ok := provider["options"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.com/v1", options["baseURL"])
	require.Equal(t, "{file:~/.config/opencode/sub2api_openai_42.key}", options["apiKey"])
	require.NotContains(t, readTestFile(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc")), "sk-user-test-key")
	require.Equal(t, "sk-user-test-key", readTestFile(t, filepath.Join(home, ".config", "opencode", "sub2api_openai_42.key")))
	models, ok := provider["models"].(map[string]any)
	require.True(t, ok)
	model, ok := models["gpt-5.1-codex"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "GPT-5.1 Codex", model["name"])
	require.Equal(t, true, model["reasoning"])
	require.Equal(t, true, model["attachment"])
	require.Equal(t, true, model["tool_call"])
	require.Equal(t, map[string]any{"input": []any{"text", "image", "pdf"}, "output": []any{"text"}}, model["modalities"])
	require.Equal(t, map[string]any{"context": float64(1000000), "output": float64(32768)}, model["limit"])
	require.Equal(t, map[string]any{"cache_read": 0.125, "cache_write": 1.25, "input": 1.25, "output": float64(10)}, model["cost"])
	require.NotContains(t, model, "supports_tool_choice")
	require.NotContains(t, model, "mode")
	require.Equal(t, "sub2api_openai_42/gpt-5.1-codex", opencode["model"])
	assertOpenCodeAuthCredential(t, home, "sub2api_openai_42", "sk-user-test-key")

	var claudeSettings map[string]any
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, filepath.Join(home, ".claude", "settings.json"))), &claudeSettings))
	claudeEnv, ok := claudeSettings["env"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.com/v1", claudeEnv["ANTHROPIC_BASE_URL"])
	require.Equal(t, "sk-user-test-key", claudeEnv["ANTHROPIC_AUTH_TOKEN"])
	require.Equal(t, "1", claudeEnv["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"])
	require.Equal(t, "gpt-5.1-codex", claudeEnv["ANTHROPIC_MODEL"])
	require.Equal(t, "gpt-5.1-codex", claudeEnv["ANTHROPIC_CUSTOM_MODEL_OPTION"])

	runPythonHelper("4\nn\n")
	codexConfig = readTestFile(t, filepath.Join(home, ".codex", "config.toml"))
	require.Equal(t, 1, strings.Count(codexConfig, "[model_providers.sub2api_openai_42]"))
	backups, err := filepath.Glob(filepath.Join(home, ".codex", "config.toml.bak.*"))
	require.NoError(t, err)
	require.NotEmpty(t, backups)
}

func TestBuildCLIImportShellScriptRunsWithPipedChoices(t *testing.T) {
	groupID := int64(7)
	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex": knownOpenCodeCapability("GPT-5.1 Codex"),
		},
	})
	require.NoError(t, err)

	scriptPath := filepath.Join(t.TempDir(), "sub2api-cli-import.sh")
	require.NoError(t, os.WriteFile(scriptPath, result.Body, 0700))

	if runtime.GOOS == "windows" {
		t.Skip("full shell wrapper execution is covered on Linux/CI")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	_ = findPythonForCLIImportTest(t)

	home := t.TempDir()
	cmd := exec.Command(bashPath, scriptPath)
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "SHELL=/bin/bash", "SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH=1")
	cmd.Stdin = strings.NewReader("2\nn\n")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.Contains(t, string(out), "OpenCode config written")
	require.Equal(t, "sk-user-test-key", readTestFile(t, filepath.Join(home, ".config", "opencode", "sub2api_openai_42.key")))
}

func TestBuildCLIImportPowerShellHelperWritesConfigsInTempHome(t *testing.T) {
	powershellPath := findPowerShellForCLIImportTest(t)
	groupID := int64(7)
	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSWindows,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex": knownOpenCodeCapability("GPT-5.1 Codex"),
		},
	})
	require.NoError(t, err)

	helper := extractPowerShellHelper(t, string(result.Body))
	require.Contains(t, helper, "Restart-OpenCodeDesktopIfNeeded")
	require.Contains(t, helper, "OpenCode Desktop caches provider/auth data")
	home := t.TempDir()
	scriptPath := filepath.Join(t.TempDir(), "sub2api-cli-import.ps1")
	require.NoError(t, os.WriteFile(scriptPath, []byte(helper), 0600))
	runPowerShellHelper := func(stdin string) string {
		t.Helper()
		cmd := exec.Command(powershellPath, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
		cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH=1")
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}

	opencodeDir := filepath.Join(home, ".config", "opencode")
	require.NoError(t, os.MkdirAll(opencodeDir, 0700))
	existingJSONCPath := filepath.Join(opencodeDir, "opencode.jsonc")
	require.NoError(t, os.WriteFile(existingJSONCPath, []byte(`{
  // keep unrelated provider
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "existing": {
      "name": "Existing",
      "models": {},
    },
  },
}
`), 0600))

	runPowerShellHelper("4\ny\n")

	codexConfig := readTestFile(t, filepath.Join(home, ".codex", "config.toml"))
	require.Contains(t, codexConfig, `model_provider = "sub2api_openai_42"`)
	require.Contains(t, codexConfig, `model = "gpt-5.1-codex"`)
	require.Equal(t, 1, strings.Count(codexConfig, "[model_providers.sub2api_openai_42]"))
	require.Contains(t, codexConfig, `name = "OnprsCodexApi"`)
	require.Contains(t, codexConfig, `wire_api = "responses"`)

	var opencode map[string]any
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, existingJSONCPath)), &opencode))
	require.NoFileExists(t, filepath.Join(opencodeDir, "opencode.json"))
	providers, ok := opencode["provider"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, providers, "existing")
	provider, ok := providers["sub2api_openai_42"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "OnprsCodexApi", provider["name"])
	require.Equal(t, "@ai-sdk/openai-compatible", provider["npm"])
	options, ok := provider["options"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.com/v1", options["baseURL"])
	require.Equal(t, "{file:~/.config/opencode/sub2api_openai_42.key}", options["apiKey"])
	require.NotContains(t, readTestFile(t, existingJSONCPath), "sk-user-test-key")
	require.Equal(t, "sk-user-test-key", readTestFile(t, filepath.Join(opencodeDir, "sub2api_openai_42.key")))
	models, ok := provider["models"].(map[string]any)
	require.True(t, ok)
	model, ok := models["gpt-5.1-codex"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "GPT-5.1 Codex", model["name"])
	require.Equal(t, true, model["reasoning"])
	require.Equal(t, true, model["attachment"])
	require.Equal(t, true, model["tool_call"])
	require.Equal(t, map[string]any{"input": []any{"text", "image", "pdf"}, "output": []any{"text"}}, model["modalities"])
	require.Equal(t, map[string]any{"context": float64(1000000), "output": float64(32768)}, model["limit"])
	require.Equal(t, map[string]any{"cache_read": 0.125, "cache_write": 1.25, "input": 1.25, "output": float64(10)}, model["cost"])
	require.Equal(t, "sub2api_openai_42/gpt-5.1-codex", opencode["model"])
	assertOpenCodeAuthCredential(t, home, "sub2api_openai_42", "sk-user-test-key")

	var claudeSettings map[string]any
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, filepath.Join(home, ".claude", "settings.json"))), &claudeSettings))
	claudeEnv, ok := claudeSettings["env"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://api.example.com/v1", claudeEnv["ANTHROPIC_BASE_URL"])
	require.Equal(t, "sk-user-test-key", claudeEnv["ANTHROPIC_AUTH_TOKEN"])
	require.Equal(t, "1", claudeEnv["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY"])
	require.Equal(t, "gpt-5.1-codex", claudeEnv["ANTHROPIC_MODEL"])
	require.Equal(t, "gpt-5.1-codex", claudeEnv["ANTHROPIC_CUSTOM_MODEL_OPTION"])

	runPowerShellHelper("4\nn\n")
	codexConfig = readTestFile(t, filepath.Join(home, ".codex", "config.toml"))
	require.Equal(t, 1, strings.Count(codexConfig, "[model_providers.sub2api_openai_42]"))
	backups, err := filepath.Glob(filepath.Join(home, ".codex", "config.toml.bak.*"))
	require.NoError(t, err)
	require.NotEmpty(t, backups)
}

func TestBuildCLIImportWindowsBatWrapperExecutesHelper(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows .bat wrapper test requires Windows")
	}
	powershellPath := findPowerShellForCLIImportTest(t)
	groupID := int64(7)
	keyID := int64(424242)
	envName := "SUB2API_KEY_424242"
	t.Cleanup(func() {
		cmd := exec.Command(powershellPath, "-NoProfile", "-Command", `[Environment]::SetEnvironmentVariable("SUB2API_KEY_424242", $null, "User")`)
		_ = cmd.Run()
	})

	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSWindows,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      keyID,
			UserID:  1001,
			Key:     "sk-user-test-key",
			Name:    "daily key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "Pro Coding",
				Platform:           PlatformOpenAI,
				Status:             StatusActive,
				DefaultMappedModel: "gpt-5.1-codex",
			},
		},
		Models: []string{"gpt-5.1-codex"},
		Capabilities: map[string]CLIImportModelCapability{
			"gpt-5.1-codex": knownOpenCodeCapability("GPT-5.1 Codex"),
		},
	})
	require.NoError(t, err)

	home := t.TempDir()
	batPath := filepath.Join(t.TempDir(), "sub2api-cli-import.bat")
	require.NoError(t, os.WriteFile(batPath, result.Body, 0600))
	cmd := exec.Command("cmd.exe", "/c", batPath)
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	cmd.Stdin = strings.NewReader("1\nn\n")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.Contains(t, string(out), "Sub2API CLI import")
	require.Contains(t, string(out), "Codex CLI config written")
	require.Contains(t, readTestFile(t, filepath.Join(home, ".codex", "config.toml")), envName)
}

func TestBuildCLIImportScript_WindowsSupportsCodexForOpenCodeGo(t *testing.T) {
	groupID := int64(8)
	input := CLIImportScriptInput{
		OS:         CLIImportOSWindows,
		APIBaseURL: "https://api.example.com/",
		APIKey: &APIKey{
			ID:      99,
			UserID:  1001,
			Key:     "sk-user-opencode-go",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "OpenCode Go",
				Platform:           PlatformOpenCodeGo,
				Status:             StatusActive,
				DefaultMappedModel: "qwen3.7-plus",
			},
		},
		Models: []string{"qwen3.7-plus"},
		Capabilities: map[string]CLIImportModelCapability{
			"qwen3.7-plus": knownOpenCodeCapability("Qwen3.7 Plus"),
		},
	}

	result, err := BuildCLIImportScript(input)
	require.NoError(t, err)
	require.Equal(t, "sub2api-cli-import.bat", result.Filename)

	body := string(result.Body)
	require.Contains(t, body, "@echo off")
	require.Contains(t, body, "SUB2API_KEY_99")
	require.Contains(t, body, "https://api.example.com/v1")
	require.NotContains(t, body, "codex_supported")
	require.NotContains(t, body, "Codex CLI import is skipped")
	require.Contains(t, body, `wire_api = "responses"`)
	require.NotContains(t, body, `wire_api = "chat"`)
}

func TestBuildCLIImportShellHelperWritesOpenCodeGoModelsInTempHome(t *testing.T) {
	pythonPath := findPythonForCLIImportTest(t)
	groupID := int64(7)
	result, err := BuildCLIImportScript(CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-opencode-go",
			Name:    "opencode go key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:                 groupID,
				Name:               "OpenCode Go",
				Platform:           PlatformOpenCodeGo,
				Status:             StatusActive,
				DefaultMappedModel: "kimi-k2.6",
			},
		},
		Models: []string{"kimi-k2.6", "kimi-k2.7-code", "minimax-m3"},
		Capabilities: map[string]CLIImportModelCapability{
			"kimi-k2.6":      knownOpenCodeCapabilityWithNameFamilyAndRelease("Kimi K2.6", "kimi-k2", "2026-04-21"),
			"kimi-k2.7-code": knownOpenCodeCapabilityWithNameFamilyAndRelease("Kimi K2.7 Code", "kimi-k2", "2026-06-12"),
			"minimax-m3": knownOpenCodeCapabilityWithNameAndFamily(
				"MiniMax M3 (3x usage)",
				"minimax-m3",
			),
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(result.Body), `"provider_name":"OnprsCodexApi"`)
	require.Contains(t, string(result.Body), `"id":"kimi-k2.6"`)
	require.Contains(t, string(result.Body), `"name":"MiniMax M3"`)
	require.NotContains(t, string(result.Body), "MiniMax M3 (3x usage)")

	helper := extractShellPythonHelper(t, string(result.Body))
	home := t.TempDir()
	cmd := exec.Command(pythonPath, "-c", helper)
	cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "SHELL=/bin/bash", "SUB2API_SKIP_OPENCODE_DESKTOP_REFRESH=1")
	cmd.Stdin = strings.NewReader("2\nn\n")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	var opencode map[string]any
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, filepath.Join(home, ".config", "opencode", "opencode.jsonc"))), &opencode))
	providers, ok := opencode["provider"].(map[string]any)
	require.True(t, ok)
	provider, ok := providers["sub2api_opencode_go_42"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "OnprsCodexApi", provider["name"])
	models, ok := provider["models"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, models, "kimi-k2.6")
	require.Contains(t, models, "kimi-k2.7-code")
	require.Contains(t, models, "minimax-m3")
	kimi26, ok := models["kimi-k2.6"].(map[string]any)
	require.True(t, ok)
	kimi27, ok := models["kimi-k2.7-code"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Kimi K2.6", kimi26["name"])
	require.Equal(t, "Kimi K2.7 Code", kimi27["name"])
	require.Equal(t, "kimi-k2", kimi26["family"])
	require.Equal(t, "kimi-k2", kimi27["family"])
	require.NotContains(t, kimi26, "release_date")
	require.NotContains(t, kimi27, "release_date")
	minimaxM3, ok := models["minimax-m3"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "MiniMax M3", minimaxM3["name"])
}

func TestBuildCLIImportScriptRequiresKnownOpenCodeCapabilitiesForEveryModel(t *testing.T) {
	groupID := int64(7)
	input := CLIImportScriptInput{
		OS:         CLIImportOSLinux,
		APIBaseURL: "https://api.example.com",
		APIKey: &APIKey{
			ID:      42,
			UserID:  1001,
			Key:     "sk-user-test-key",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
			Group: &Group{
				ID:       groupID,
				Name:     "Pro Coding",
				Platform: PlatformOpenAI,
				Status:   StatusActive,
			},
		},
		Models: []string{"known-model", "unknown-model"},
		Capabilities: map[string]CLIImportModelCapability{
			"known-model": knownOpenCodeCapability("Known Model"),
		},
	}

	_, err := BuildCLIImportScript(input)
	require.ErrorIs(t, err, ErrCLIImportModelCapabilityUnknown)
	require.Contains(t, err.Error(), "unknown-model")
}

func TestValidateCLIImportAPIKeyRejectsUnsafeStates(t *testing.T) {
	groupID := int64(7)
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	valid := func() *APIKey {
		return &APIKey{
			ID:        42,
			UserID:    1001,
			Key:       "sk-valid-test-key",
			GroupID:   &groupID,
			Status:    StatusAPIKeyActive,
			ExpiresAt: &future,
			Group:     &Group{ID: groupID, Status: StatusActive, Platform: PlatformOpenAI},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*APIKey)
		userID  int64
		wantErr error
	}{
		{
			name:    "wrong owner",
			userID:  2002,
			wantErr: ErrCLIImportAPIKeyForbidden,
		},
		{
			name:    "disabled",
			userID:  1001,
			mutate:  func(k *APIKey) { k.Status = StatusAPIKeyDisabled },
			wantErr: ErrCLIImportAPIKeyInactive,
		},
		{
			name:    "expired status",
			userID:  1001,
			mutate:  func(k *APIKey) { k.Status = StatusAPIKeyExpired },
			wantErr: ErrCLIImportAPIKeyExpired,
		},
		{
			name:    "expired time",
			userID:  1001,
			mutate:  func(k *APIKey) { k.ExpiresAt = &past },
			wantErr: ErrCLIImportAPIKeyExpired,
		},
		{
			name:    "quota exhausted status",
			userID:  1001,
			mutate:  func(k *APIKey) { k.Status = StatusAPIKeyQuotaExhausted },
			wantErr: ErrCLIImportAPIKeyQuotaExhausted,
		},
		{
			name:    "quota exhausted amount",
			userID:  1001,
			mutate:  func(k *APIKey) { k.Quota = 1; k.QuotaUsed = 1 },
			wantErr: ErrCLIImportAPIKeyQuotaExhausted,
		},
		{
			name:    "missing group id",
			userID:  1001,
			mutate:  func(k *APIKey) { k.GroupID = nil },
			wantErr: ErrCLIImportAPIKeyNoGroup,
		},
		{
			name:    "missing group edge",
			userID:  1001,
			mutate:  func(k *APIKey) { k.Group = nil },
			wantErr: ErrCLIImportAPIKeyNoGroup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := valid()
			if tt.mutate != nil {
				tt.mutate(key)
			}
			err := validateCLIImportAPIKey(key, tt.userID)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}

	err := validateCLIImportAPIKey(valid(), 1001)
	require.NoError(t, err)
}

func TestResolveCLIImportModelListUsesCustomThenProviderThenDefault(t *testing.T) {
	groupID := int64(7)
	group := &Group{
		ID:       groupID,
		Platform: PlatformOpenAI,
		ModelsListConfig: GroupModelsListConfig{
			Enabled: true,
			Models:  []string{" gpt-5.1-codex ", "gpt-5.1-codex", "claude-sonnet-4-20250514"},
		},
	}
	models := resolveCLIImportModelList(nil, group)
	require.Equal(t, []string{"gpt-5.1-codex", "claude-sonnet-4-20250514"}, models)

	group.ModelsListConfig = GroupModelsListConfig{}
	models = resolveCLIImportModelList([]string{"z-model", "a-model", "a-model"}, group)
	require.Equal(t, []string{"z-model", "a-model"}, models)

	models = resolveCLIImportModelList(nil, &Group{Platform: PlatformOpenCodeGo})
	require.True(t, len(models) > 0)
	require.True(t, strings.Contains(strings.Join(models, ","), "qwen3.7-plus"))
}

func TestPricingServiceGetCLIImportModelCapabilityMapsLiteLLMFields(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-test": {
				InputCostPerToken:                0.000001,
				OutputCostPerToken:               0.000002,
				CacheReadInputTokenCost:          0.0000001,
				CacheCreationInputTokenCost:      0.0000005,
				OutputCostPerImage:               0.04,
				OutputCostPerImageToken:          0.00004,
				SupportsReasoning:                true,
				SupportsVision:                   true,
				SupportsPDFInput:                 true,
				SupportsFunctionCalling:          true,
				SupportsToolChoice:               true,
				MaxInputTokens:                   1000000,
				MaxOutputTokens:                  32768,
				Mode:                             "responses",
				SupportsReasoningKnown:           true,
				SupportsVisionKnown:              true,
				SupportsPDFInputKnown:            true,
				SupportsFunctionCallingKnown:     true,
				SupportsToolChoiceKnown:          true,
				InputCostPerTokenKnown:           true,
				OutputCostPerTokenKnown:          true,
				CacheReadInputTokenCostKnown:     true,
				CacheCreationInputTokenCostKnown: true,
				MaxInputTokensKnown:              true,
				MaxOutputTokensKnown:             true,
			},
		},
	}

	capability, ok := svc.GetCLIImportModelCapability(context.Background(), PlatformOpenAI, "gpt-test")
	require.True(t, ok)
	require.True(t, capability.SupportsReasoning)
	require.True(t, capability.ReasoningKnown)
	require.True(t, capability.SupportsVision)
	require.True(t, capability.SupportsPDFInput)
	require.True(t, capability.SupportsFunctionCalling)
	require.True(t, capability.SupportsToolChoice)
	require.Equal(t, 1000000, capability.MaxInputTokens)
	require.Equal(t, 32768, capability.MaxOutputTokens)
	require.Equal(t, "responses", capability.Mode)
	require.Equal(t, 1.0, *capability.InputCostPerToken)
	require.Equal(t, 2.0, *capability.OutputCostPerToken)
	require.InDelta(t, 0.1, *capability.CacheReadCostPerToken, 1e-12)
	require.Equal(t, 0.5, *capability.CacheWriteCostPerToken)
	require.Equal(t, 0.04, *capability.OutputCostPerImage)
	require.Equal(t, 0.00004, *capability.OutputCostPerImageToken)
}

func TestPricingServiceGetCLIImportModelCapabilityUsesModelsDevCatalog(t *testing.T) {
	const catalog = `{
		"openai": {
			"models": {
				"gpt-test": {
					"id": "gpt-test",
					"name": "GPT Test",
					"family": "gpt",
					"attachment": true,
					"reasoning": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image", "pdf"], "output": ["text"]},
					"limit": {"context": 1000000, "output": 32768},
					"cost": {"input": 1.25, "output": 10, "cache_read": 0.125, "cache_write": 1.25}
				}
			}
		}
	}`
	svc := &PricingService{
		remoteClient: &pricingTestRemoteClient{
			pricingBodies: map[string][]byte{
				cliImportModelsDevAPIURL: []byte(catalog),
			},
		},
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-test": {
				InputCostPerToken:  0.000001,
				OutputCostPerToken: 0.000002,
			},
		},
	}

	capability, ok := svc.GetCLIImportModelCapability(context.Background(), PlatformOpenAI, "gpt-test")
	require.True(t, ok)
	require.Equal(t, "GPT Test", capability.Name)
	require.Equal(t, "gpt", capability.Family)
	require.True(t, capability.ReasoningKnown)
	require.True(t, capability.AttachmentKnown)
	require.True(t, capability.ToolCallKnown)
	require.True(t, capability.ModalitiesKnown)
	require.True(t, capability.LimitKnown)
	require.True(t, capability.CostKnown)
	require.True(t, capability.SupportsReasoning)
	require.True(t, capability.Attachment)
	require.True(t, capability.SupportsFunctionCalling)
	require.Equal(t, []string{"text", "image", "pdf"}, capability.InputModalities)
	require.Equal(t, []string{"text"}, capability.OutputModalities)
	require.Equal(t, 1000000, capability.MaxInputTokens)
	require.Equal(t, 32768, capability.MaxOutputTokens)
	require.Equal(t, 1.25, *capability.InputCostPerToken)
	require.Equal(t, 10.0, *capability.OutputCostPerToken)
	require.Equal(t, 0.125, *capability.CacheReadCostPerToken)
	require.Equal(t, 1.25, *capability.CacheWriteCostPerToken)
}

func TestPricingServiceGetCLIImportModelCapabilityUsesOpenCodeGoModelsDevProvider(t *testing.T) {
	const catalog = `{
		"opencode": {
			"models": {
				"qwen3.6-plus": {
					"id": "qwen3.6-plus",
					"name": "Wrong provider sentinel",
					"attachment": true,
					"reasoning": true,
					"tool_call": true,
					"modalities": {"input": ["text"], "output": ["text"]},
					"limit": {"context": 1000, "output": 1000},
					"cost": {"input": 99, "output": 99}
				}
			}
		},
		"opencode-go": {
			"models": {
				"kimi-k2.7-code": {
					"id": "kimi-k2.7-code",
					"name": "Kimi K2.7 Code",
					"family": "kimi-k2",
					"release_date": "2026-06-12",
					"attachment": true,
					"reasoning": true,
					"temperature": false,
					"tool_call": true,
					"modalities": {"input": ["text", "image", "video"], "output": ["text"]},
					"limit": {"context": 262144, "output": 262144},
					"cost": {"input": 0.95, "output": 4, "cache_read": 0.19}
				},
				"mimo-v2.5": {
					"id": "mimo-v2.5",
					"name": "MiMo V2.5",
					"family": "mimo-v2.5",
					"release_date": "2026-04-22",
					"attachment": true,
					"reasoning": true,
					"temperature": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image", "audio", "video"], "output": ["text"]},
					"limit": {"context": 1000000, "output": 128000},
					"cost": {"input": 0.14, "output": 0.28, "cache_read": 0.0028}
				},
				"mimo-v2.5-pro": {
					"id": "mimo-v2.5-pro",
					"name": "MiMo V2.5 Pro",
					"family": "mimo-v2.5-pro",
					"release_date": "2026-04-22",
					"attachment": true,
					"reasoning": true,
					"temperature": true,
					"tool_call": true,
					"modalities": {"input": ["text"], "output": ["text"]},
					"limit": {"context": 1048576, "output": 128000},
					"cost": {"input": 1.74, "output": 3.48, "cache_read": 0.0145}
				},
				"minimax-m3": {
					"id": "minimax-m3",
					"name": "MiniMax M3 (3x usage)",
					"family": "minimax-m3",
					"release_date": "2026-05-31",
					"attachment": false,
					"reasoning": true,
					"temperature": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image", "video"], "output": ["text"]},
					"limit": {"context": 512000, "output": 131072},
					"cost": {"input": 0.1, "output": 0.4, "cache_read": 0.02}
				},
				"qwen3.7-max": {
					"id": "qwen3.7-max",
					"name": "Qwen3.7 Max",
					"family": "qwen3.7-max",
					"release_date": "2026-05-21",
					"attachment": false,
					"reasoning": true,
					"temperature": true,
					"tool_call": true,
					"modalities": {"input": ["text"], "output": ["text"]},
					"limit": {"context": 1000000, "output": 65536},
					"cost": {"input": 2.5, "output": 7.5, "cache_read": 0.5, "cache_write": 3.125}
				},
				"qwen3.7-plus": {
					"id": "qwen3.7-plus",
					"name": "Qwen3.7 Plus",
					"family": "qwen3.7-plus",
					"release_date": "2026-06-02",
					"attachment": true,
					"reasoning": true,
					"temperature": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image", "video"], "output": ["text"]},
					"limit": {"context": 1000000, "output": 65536},
					"cost": {"input": 0.4, "output": 1.6, "cache_read": 0.04, "cache_write": 0.5}
				}
			}
		}
	}`
	svc := &PricingService{
		remoteClient: &pricingTestRemoteClient{
			pricingBodies: map[string][]byte{
				cliImportModelsDevAPIURL: []byte(catalog),
			},
		},
	}

	for _, model := range []string{
		"kimi-k2.7-code",
		"mimo-v2.5",
		"mimo-v2.5-pro",
		"minimax-m3",
		"qwen3.7-max",
		"qwen3.7-plus",
	} {
		t.Run(model, func(t *testing.T) {
			capability, ok := svc.GetCLIImportModelCapability(context.Background(), PlatformOpenCodeGo, model)
			require.True(t, ok)
			require.True(t, capability.openCodeComplete())
			require.NotEqual(t, "Wrong provider sentinel", capability.Name)
		})
	}

	qwen, ok := svc.GetCLIImportModelCapability(context.Background(), PlatformOpenCodeGo, "qwen3.7-plus")
	require.True(t, ok)
	require.Equal(t, "Qwen3.7 Plus", qwen.Name)
	require.Equal(t, 1000000, qwen.MaxInputTokens)
	require.Equal(t, 65536, qwen.MaxOutputTokens)
	require.Equal(t, 0.4, *qwen.InputCostPerToken)
	require.Equal(t, 1.6, *qwen.OutputCostPerToken)
	require.Equal(t, 0.04, *qwen.CacheReadCostPerToken)
	require.Equal(t, 0.5, *qwen.CacheWriteCostPerToken)
}

func TestPricingServiceGetCLIImportModelCapabilityHasBuiltinFallbackForEveryOpenCodeGoDefault(t *testing.T) {
	svc := &PricingService{
		remoteClient: &pricingTestRemoteClient{pricingBodies: map[string][]byte{}},
	}

	for _, model := range OpenCodeGoDefaultModelIDs() {
		t.Run(model, func(t *testing.T) {
			capability, ok := svc.GetCLIImportModelCapability(context.Background(), PlatformOpenCodeGo, model)
			require.True(t, ok)
			require.True(t, capability.openCodeComplete())
			require.NotEmpty(t, capability.Name)
			require.NotEmpty(t, capability.Family)
		})
	}
}

func TestParsePricingDataTracksCapabilityFieldPresence(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"known-false": {
			"input_cost_per_token": 0,
			"output_cost_per_token": 0,
			"supports_reasoning": false,
			"supports_vision": false,
			"supports_pdf_input": false,
			"supports_function_calling": false,
			"supports_tool_choice": false,
			"max_input_tokens": 1000,
			"max_output_tokens": 200
		},
		"missing-bools": {
			"input_cost_per_token": 0,
			"output_cost_per_token": 0
		}
	}`))
	require.NoError(t, err)

	knownFalse := pricingData["known-false"]
	require.False(t, knownFalse.SupportsReasoning)
	require.True(t, knownFalse.SupportsReasoningKnown)
	require.True(t, knownFalse.SupportsVisionKnown)
	require.True(t, knownFalse.SupportsPDFInputKnown)
	require.True(t, knownFalse.SupportsFunctionCallingKnown)
	require.True(t, knownFalse.SupportsToolChoiceKnown)
	require.True(t, knownFalse.MaxInputTokensKnown)
	require.True(t, knownFalse.MaxOutputTokensKnown)

	missing := pricingData["missing-bools"]
	require.False(t, missing.SupportsReasoning)
	require.False(t, missing.SupportsReasoningKnown)
	require.False(t, missing.SupportsVisionKnown)
	require.False(t, missing.SupportsPDFInputKnown)
	require.False(t, missing.SupportsFunctionCallingKnown)
	require.False(t, missing.SupportsToolChoiceKnown)
}

func findPythonForCLIImportTest(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "--version").Run(); err == nil {
			return path
		}
	}
	t.Skip("python3/python not found")
	return ""
}

func findPowerShellForCLIImportTest(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"pwsh", "powershell"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := exec.Command(path, "-NoProfile", "-Command", "$PSVersionTable.PSVersion.Major").Run(); err == nil {
			return path
		}
	}
	t.Skip("PowerShell not found")
	return ""
}

func extractShellPythonHelper(t *testing.T, script string) string {
	t.Helper()
	startMarker := "cat > \"$tmp_py\" <<'PY'\n"
	start := strings.Index(script, startMarker)
	require.NotEqual(t, -1, start)
	start += len(startMarker)
	end := strings.LastIndex(script, "\nPY\npython3 \"$tmp_py\"")
	require.NotEqual(t, -1, end)
	return script[start:end]
}

func extractPowerShellHelper(t *testing.T, script string) string {
	t.Helper()
	startMarker := "### SUB2API_CLI_IMPORT_POWERSHELL ###\r\n"
	start := strings.Index(script, startMarker)
	require.NotEqual(t, -1, start)
	return script[start+len(startMarker):]
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func assertOpenCodeAuthCredential(t *testing.T, home string, providerID string, apiKey string) {
	t.Helper()
	var auth map[string]map[string]any
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	require.NoError(t, json.Unmarshal([]byte(readTestFile(t, authPath)), &auth))
	credential, ok := auth[providerID]
	require.True(t, ok, "missing OpenCode auth credential for %s", providerID)
	require.Equal(t, "api", credential["type"])
	require.Equal(t, apiKey, credential["key"])
}

func knownOpenCodeCapability(name string) CLIImportModelCapability {
	return knownOpenCodeCapabilityWithNameAndFamily(name, "")
}

func knownOpenCodeCapabilityWithNameAndFamily(name string, family string) CLIImportModelCapability {
	return CLIImportModelCapability{
		Name:                         name,
		Family:                       family,
		Attachment:                   true,
		SupportsReasoning:            true,
		SupportsVision:               true,
		SupportsPDFInput:             true,
		SupportsFunctionCalling:      true,
		SupportsToolChoice:           true,
		MaxInputTokens:               1000000,
		MaxOutputTokens:              32768,
		InputModalities:              []string{"text", "image", "pdf"},
		OutputModalities:             []string{"text"},
		InputCostPerToken:            ptrFloat64(1.25),
		OutputCostPerToken:           ptrFloat64(10),
		CacheReadCostPerToken:        ptrFloat64(0.125),
		CacheWriteCostPerToken:       ptrFloat64(1.25),
		ReasoningKnown:               true,
		AttachmentKnown:              true,
		ToolCallKnown:                true,
		ModalitiesKnown:              true,
		LimitKnown:                   true,
		CostKnown:                    true,
		SupportsVisionKnown:          true,
		SupportsPDFInputKnown:        true,
		SupportsFunctionCallingKnown: true,
		SupportsToolChoiceKnown:      true,
	}
}

func knownOpenCodeCapabilityWithNameFamilyAndRelease(name string, family string, releaseDate string) CLIImportModelCapability {
	capability := knownOpenCodeCapabilityWithNameAndFamily(name, family)
	capability.ReleaseDate = releaseDate
	return capability
}
