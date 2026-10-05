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

type pricingTestRemoteClient struct {
	pricingBodies map[string][]byte
	pricingErrs   map[string]error
	hashText      string
	fetchedURLs   []string
}

func (c *pricingTestRemoteClient) FetchPricingJSON(_ context.Context, url string) ([]byte, error) {
	c.fetchedURLs = append(c.fetchedURLs, url)
	if err, ok := c.pricingErrs[url]; ok {
		return nil, err
	}
	if err, ok := c.pricingErrs[strings.TrimRight(url, "/")]; ok {
		return nil, err
	}
	if body, ok := c.pricingBodies[url]; ok {
		return body, nil
	}
	if body, ok := c.pricingBodies[strings.TrimRight(url, "/")]; ok {
		return body, nil
	}
	return c.pricingBodies[strings.TrimRight(url, "/")+"/"], nil
}

func (c *pricingTestRemoteClient) FetchHashText(_ context.Context, _ string) (string, error) {
	return c.hashText, nil
}

type cliImportAPIKeyRepoStub struct {
	APIKeyRepository
	key *APIKey
}

func (r cliImportAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	return r.key, nil
}

type cliImportRoutingModelProvider struct {
	models map[int64][]string
}

func (p cliImportRoutingModelProvider) GetAvailableModels(_ context.Context, groupID *int64, _ string) []string {
	if groupID == nil {
		return nil
	}
	return append([]string(nil), p.models[*groupID]...)
}

func (p cliImportRoutingModelProvider) GetAvailableModelPricingCandidates(_ context.Context, _ *int64, _ string, models []string) map[string][]string {
	out := make(map[string][]string, len(models))
	for _, model := range models {
		out[model] = []string{model}
	}
	return out
}

type cliImportRoutingCapabilityProvider struct{}

func (cliImportRoutingCapabilityProvider) GetCLIImportModelCapability(_ context.Context, _ string, model string) (CLIImportModelCapability, bool) {
	return knownOpenCodeCapability(model), true
}

func TestAPIKeyServiceBuildCLIImportScript_MergesActiveRoutingCandidates(t *testing.T) {
	firstGroupID := int64(71)
	secondGroupID := int64(72)
	inactiveGroupID := int64(73)
	firstGroup := &Group{ID: firstGroupID, Name: "Primary", Platform: PlatformOpenAI, Status: StatusActive, DefaultMappedModel: "model-a"}
	secondGroup := &Group{ID: secondGroupID, Name: "Secondary", Platform: PlatformOpenAI, Status: StatusActive}
	inactiveGroup := &Group{ID: inactiveGroupID, Name: "Inactive", Platform: PlatformOpenAI, Status: StatusDisabled}
	key := &APIKey{
		ID: 42, UserID: 1001, Key: "sk-routing-test-key", Name: "routing key",
		GroupID: &firstGroupID, Group: firstGroup, Status: StatusAPIKeyActive,
		RoutingPlatform: PlatformOpenAI, RoutingStrategy: APIKeyRoutingStrategyBalanced,
		RoutingGroups: []APIKeyGroupBinding{
			{GroupID: firstGroupID, Priority: 0, Group: firstGroup},
			{GroupID: secondGroupID, Priority: 1, Group: secondGroup},
			{GroupID: inactiveGroupID, Priority: 2, Group: inactiveGroup},
		},
	}
	svc := &APIKeyService{apiKeyRepo: cliImportAPIKeyRepoStub{key: key}}

	result, err := svc.BuildCLIImportScript(
		context.Background(),
		CLIImportScriptInput{OS: CLIImportOSLinux, APIBaseURL: "https://api.example.com", APIKey: &APIKey{ID: key.ID}},
		key.UserID,
		cliImportRoutingModelProvider{models: map[int64][]string{
			firstGroupID:    {"model-a", "shared-model"},
			secondGroupID:   {"model-b", "shared-model"},
			inactiveGroupID: {"inactive-model"},
		}},
		cliImportRoutingCapabilityProvider{},
	)
	require.NoError(t, err)
	body := string(result.Body)
	require.Contains(t, body, `"id":"model-a"`)
	require.Contains(t, body, `"id":"model-b"`)
	require.Contains(t, body, `"id":"shared-model"`)
	require.NotContains(t, body, "inactive-model")
	require.Equal(t, 1, strings.Count(body, `"id":"shared-model"`))
}

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
		ModelAllowlist: GroupModelAllowlist{
			Enabled: true,
			Models:  []string{" gpt-5.1-codex ", "gpt-5.1-codex", "claude-sonnet-4-20250514"},
		},
	}
	models := resolveCLIImportModelList(nil, group)
	require.Equal(t, []string{"gpt-5.1-codex", "claude-sonnet-4-20250514"}, models)

	group.ModelAllowlist = GroupModelAllowlist{}
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
