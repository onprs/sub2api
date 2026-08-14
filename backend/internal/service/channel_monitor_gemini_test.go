//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type gemma4MonitorCaptureHandler struct {
	lastBody    map[string]any
	lastHeaders http.Header
	lastPath    string
}

func (h *gemma4MonitorCaptureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastHeaders = r.Header.Clone()
	h.lastPath = r.URL.Path
	defer func() { _ = r.Body.Close() }()

	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	h.lastBody = body

	config, _ := body["generationConfig"].(map[string]any)
	thinking, _ := config["thinkingConfig"].(map[string]any)
	budget, _ := config["maxOutputTokens"].(float64)
	answer := answerFromChallengePrompt(promptFromGeminiBody(body))
	responseText := "* Input: A few-shot prompt with basic arithmetic problems.\n" +
		"* Task: Calculate the requested result.\n" +
		"* Constraint: Respond with ONLY the number, nothing else."
	finishReason := "MAX_TOKENS"
	if budget >= float64(monitorGemma4ChallengeMaxTokens) && thinking["thinkingLevel"] == monitorGemma4ThinkingLevel {
		responseText += "\n" + answer
		finishReason = "STOP"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"parts": []map[string]any{{"text": responseText}},
			},
			"finishReason": finishReason,
		}},
	})
}

func setupFakeGemma4Monitor(t *testing.T, handler *gemma4MonitorCaptureHandler) string {
	t.Helper()
	swapMonitorHTTPClient(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

func TestRunCheckForModel_GeminiGemma4ReservesFinalAnswerBudget(t *testing.T) {
	for _, model := range []string{"gemma-4-26b-a4b-it", "gemma-4-31b-it"} {
		t.Run(model, func(t *testing.T) {
			handler := &gemma4MonitorCaptureHandler{}
			endpoint := setupFakeGemma4Monitor(t, handler)

			result := runCheckForModel(context.Background(), MonitorProviderGemini, endpoint, "gemini-key", model, nil)

			if result.Status != MonitorStatusOperational {
				t.Fatalf("Gemma 4 探活应在可见分析后取得最终答案，status=%s message=%q", result.Status, result.Message)
			}
			if handler.lastPath != "/v1beta/models/"+model+":generateContent" {
				t.Fatalf("Gemma 4 探活路径错误：%q", handler.lastPath)
			}
			if handler.lastHeaders.Get("x-goog-api-key") != "gemini-key" {
				t.Fatalf("Gemma 4 探活缺少 x-goog-api-key：%q", handler.lastHeaders.Get("x-goog-api-key"))
			}
			config, _ := handler.lastBody["generationConfig"].(map[string]any)
			if config["maxOutputTokens"] != float64(monitorGemma4ChallengeMaxTokens) {
				t.Errorf("Gemma 4 输出预算错误：%v", config["maxOutputTokens"])
			}
			if config["temperature"] != float64(0) {
				t.Errorf("Gemma 4 temperature 应为 0：%v", config["temperature"])
			}
			thinking, _ := config["thinkingConfig"].(map[string]any)
			if thinking["thinkingLevel"] != monitorGemma4ThinkingLevel {
				t.Errorf("Gemma 4 thinkingLevel 错误：%v", thinking["thinkingLevel"])
			}
		})
	}
}

func TestGeminiMonitorGenerationConfig_NonGemmaKeepsDefaultBudget(t *testing.T) {
	config := geminiMonitorGenerationConfig("gemini-3-flash-preview")
	if config["maxOutputTokens"] != monitorChallengeMaxTokens {
		t.Errorf("普通 Gemini 输出预算不应变化：%v", config["maxOutputTokens"])
	}
	if _, ok := config["thinkingConfig"]; ok {
		t.Errorf("普通 Gemini 不应注入 Gemma thinkingConfig：%v", config["thinkingConfig"])
	}
	if _, ok := config["temperature"]; ok {
		t.Errorf("普通 Gemini temperature 不应变化：%v", config["temperature"])
	}
}

func TestIsGemma4MonitorModel_NormalizesModelPrefix(t *testing.T) {
	for _, model := range []string{"gemma-4-31b-it", " models/GEMMA-4-26B-A4B-IT "} {
		if !isGemma4MonitorModel(model) {
			t.Errorf("应识别 Gemma 4 模型：%q", model)
		}
	}
	if isGemma4MonitorModel("gemini-3-flash-preview") {
		t.Error("不应把 Gemini 模型识别为 Gemma 4")
	}
}
