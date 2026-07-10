//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var exactChallengePromptRegex = regexp.MustCompile(`(?m)^Reply with ONLY this exact check code:\s*(\d+)\s*$`)

type antigravityCaptureHandler struct {
	lastBody    map[string]any
	lastHeaders http.Header
	lastPath    string
}

func (h *antigravityCaptureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	h.lastPath = r.URL.Path
	defer func() { _ = r.Body.Close() }()

	var parsed map[string]any
	_ = json.NewDecoder(r.Body).Decode(&parsed)
	h.lastBody = parsed

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": answerFromMonitorPrompt(promptFromAnthropicBody(parsed))},
			},
		})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{
					"parts": []map[string]any{{"text": answerFromMonitorPrompt(promptFromGeminiBody(parsed))}},
				}},
			},
		})
	}
}

func setupFakeAntigravity(t *testing.T, handler *antigravityCaptureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func promptFromAnthropicBody(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return ""
	}
	first, _ := messages[0].(map[string]any)
	prompt, _ := first["content"].(string)
	return prompt
}

func promptFromGeminiBody(body map[string]any) string {
	contents, _ := body["contents"].([]any)
	if len(contents) == 0 {
		return ""
	}
	firstContent, _ := contents[0].(map[string]any)
	parts, _ := firstContent["parts"].([]any)
	if len(parts) == 0 {
		return ""
	}
	firstPart, _ := parts[0].(map[string]any)
	prompt, _ := firstPart["text"].(string)
	return prompt
}

func answerFromExactChallengePrompt(prompt string) string {
	m := exactChallengePromptRegex.FindStringSubmatch(prompt)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func answerFromMonitorPrompt(prompt string) string {
	if answer := answerFromExactChallengePrompt(prompt); answer != "" {
		return answer
	}
	return answerFromChallengePrompt(prompt)
}

func TestRunCheckForModel_AntigravityClaude_DefaultRequest(t *testing.T) {
	h := &antigravityCaptureHandler{}
	endpoint := setupFakeAntigravity(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderAntigravityClaude, endpoint, "sk-test", "claude-sonnet-4-6", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("antigravity claude request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != "/antigravity/v1/messages" {
		t.Fatalf("expected antigravity claude path, got %q", h.lastPath)
	}
	if h.lastHeaders.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("expected bearer auth header, got %q", h.lastHeaders.Get("Authorization"))
	}
	if h.lastHeaders.Get("anthropic-version") != monitorAnthropicAPIVersion {
		t.Errorf("expected anthropic-version header, got %q", h.lastHeaders.Get("anthropic-version"))
	}
	if h.lastHeaders.Get("x-api-key") != "" {
		t.Errorf("antigravity claude must not use x-api-key header, got %q", h.lastHeaders.Get("x-api-key"))
	}
	if h.lastBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("claude body should contain requested model, got %v", h.lastBody["model"])
	}
	if messages, _ := h.lastBody["messages"].([]any); len(messages) == 0 {
		t.Error("claude body should contain non-empty messages")
	}
}

func TestRunCheckForModel_AntigravityGemini_DefaultRequest(t *testing.T) {
	h := &antigravityCaptureHandler{}
	endpoint := setupFakeAntigravity(t, h)

	res := runCheckForModel(context.Background(), MonitorProviderAntigravityGemini, endpoint, "sk-test", "gemini-3.1-pro-high", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("antigravity gemini request should pass challenge, got status=%s message=%q", res.Status, res.Message)
	}
	if h.lastPath != "/antigravity/v1beta/models/gemini-3.1-pro-high:generateContent" {
		t.Fatalf("expected antigravity gemini path, got %q", h.lastPath)
	}
	if h.lastHeaders.Get("x-goog-api-key") != "sk-test" {
		t.Errorf("expected x-goog-api-key header, got %q", h.lastHeaders.Get("x-goog-api-key"))
	}
	if h.lastHeaders.Get("Authorization") != "" {
		t.Errorf("antigravity gemini must not use Authorization header, got %q", h.lastHeaders.Get("Authorization"))
	}
	if contents, _ := h.lastBody["contents"].([]any); len(contents) == 0 {
		t.Error("gemini body should contain non-empty contents")
	} else if first, _ := contents[0].(map[string]any); first["role"] != "user" {
		t.Errorf("gemini body should mark challenge content as user role, got %v", first["role"])
	}
	config, _ := h.lastBody["generationConfig"].(map[string]any)
	if config["maxOutputTokens"] == nil {
		t.Errorf("gemini body should contain generationConfig.maxOutputTokens, got %v", h.lastBody["generationConfig"])
	} else if config["maxOutputTokens"] != float64(1024) {
		t.Errorf("antigravity gemini should reserve enough visible output budget for thinking models, got %v", config["maxOutputTokens"])
	}
	thinkingConfig, _ := config["thinkingConfig"].(map[string]any)
	if thinkingConfig["includeThoughts"] != false {
		t.Errorf("antigravity gemini monitor should not request thought text, got %v", thinkingConfig["includeThoughts"])
	}
	if thinkingConfig["thinkingLevel"] != "low" {
		t.Errorf("antigravity gemini 3 monitor should constrain thinking level, got %v", thinkingConfig["thinkingLevel"])
	}
}

func TestRunCheckForModel_AntigravityGemini_ExtractsAnswerFromLaterTextPart(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		var parsed map[string]any
		_ = json.NewDecoder(r.Body).Decode(&parsed)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"functionCall": map[string]any{"name": "noop", "args": map[string]any{}}},
							{"text": answerFromMonitorPrompt(promptFromGeminiBody(parsed))},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderAntigravityGemini, srv.URL, "sk-test", "gemini-3.5-flash-high", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("antigravity gemini request should read later text parts, got status=%s message=%q", res.Status, res.Message)
	}
}

func TestRunCheckForModel_AntigravityGemini_SkipsThoughtTextParts(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		var parsed map[string]any
		_ = json.NewDecoder(r.Body).Decode(&parsed)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "9", "thought": true},
							{"text": answerFromMonitorPrompt(promptFromGeminiBody(parsed))},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderAntigravityGemini, srv.URL, "sk-test", "gemini-3.1-pro-high", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("antigravity gemini monitor should ignore thought text parts, got status=%s message=%q", res.Status, res.Message)
	}
}

func TestRunCheckForModel_AntigravityGemini_UsesEchoChallengeForAgentModels(t *testing.T) {
	swapMonitorHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()

		var parsed map[string]any
		_ = json.NewDecoder(r.Body).Decode(&parsed)
		prompt := promptFromGeminiBody(parsed)

		answer := answerFromExactChallengePrompt(prompt)
		if answer == "" {
			answer = "not the requested arithmetic answer"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{
					"parts": []map[string]any{{"text": answer}},
				}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	res := runCheckForModel(context.Background(), MonitorProviderAntigravityGemini, srv.URL, "sk-test", "gemini-3.5-flash-high", nil)

	if res.Status != MonitorStatusOperational {
		t.Fatalf("antigravity gemini agent monitor should use exact echo challenge, got status=%s message=%q", res.Status, res.Message)
	}
}

func TestValidateProvider_AcceptsAntigravityProviders(t *testing.T) {
	for _, provider := range []string{MonitorProviderAntigravityClaude, MonitorProviderAntigravityGemini} {
		if err := validateProvider(provider); err != nil {
			t.Fatalf("validateProvider(%q) returned error: %v", provider, err)
		}
	}
}

func TestChannelMonitorTemplate_AcceptsAntigravityProviders(t *testing.T) {
	for _, provider := range []string{MonitorProviderAntigravityClaude, MonitorProviderAntigravityGemini} {
		err := validateTemplateCreateParams(ChannelMonitorRequestTemplateCreateParams{
			Name:             "Antigravity default",
			Provider:         provider,
			APIMode:          MonitorAPIModeChatCompletions,
			BodyOverrideMode: MonitorBodyOverrideModeOff,
		})
		if err != nil {
			t.Fatalf("template create params for %q returned error: %v", provider, err)
		}
	}
}

func TestRunCheckForModel_AntigravityMergeModeProtectsProtocolFields(t *testing.T) {
	claudeHandler := &antigravityCaptureHandler{}
	claudeEndpoint := setupFakeAntigravity(t, claudeHandler)
	claudeRes := runCheckForModel(context.Background(), MonitorProviderAntigravityClaude, claudeEndpoint, "sk-test", "claude-sonnet-4-6", &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]any{
			"model":      "hacked-claude",
			"messages":   []any{},
			"max_tokens": float64(20),
		},
	})
	if claudeRes.Status != MonitorStatusOperational {
		t.Fatalf("antigravity claude merge request should pass, got status=%s message=%q", claudeRes.Status, claudeRes.Message)
	}
	if claudeHandler.lastBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("claude merge should protect model, got %v", claudeHandler.lastBody["model"])
	}
	if messages, _ := claudeHandler.lastBody["messages"].([]any); len(messages) == 0 {
		t.Error("claude merge should protect non-empty messages")
	}
	if claudeHandler.lastBody["max_tokens"] != float64(20) {
		t.Errorf("claude merge should allow max_tokens override, got %v", claudeHandler.lastBody["max_tokens"])
	}

	geminiHandler := &antigravityCaptureHandler{}
	geminiEndpoint := setupFakeAntigravity(t, geminiHandler)
	geminiRes := runCheckForModel(context.Background(), MonitorProviderAntigravityGemini, geminiEndpoint, "sk-test", "gemini-3-flash", &CheckOptions{
		BodyOverrideMode: MonitorBodyOverrideModeMerge,
		BodyOverride: map[string]any{
			"contents":         []any{},
			"generationConfig": map[string]any{"temperature": float64(0)},
		},
	})
	if geminiRes.Status != MonitorStatusOperational {
		t.Fatalf("antigravity gemini merge request should pass, got status=%s message=%q", geminiRes.Status, geminiRes.Message)
	}
	if contents, _ := geminiHandler.lastBody["contents"].([]any); len(contents) == 0 {
		t.Error("gemini merge should protect non-empty contents")
	}
	config, _ := geminiHandler.lastBody["generationConfig"].(map[string]any)
	if config["temperature"] != float64(0) {
		t.Errorf("gemini merge should allow generationConfig override, got %v", config)
	}
}
