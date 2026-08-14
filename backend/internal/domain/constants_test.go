package domain

import "testing"

func TestDefaultAntigravityModelMapping_ImageCompatibilityAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
		"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
		"gemini-3-pro-image":             "gemini-3.1-flash-image",
		"gemini-3-pro-image-preview":     "gemini-3.1-flash-image",
	}

	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}

func TestDefaultAntigravityModelMapping_ContainsNewClaudeModels(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-fable-5":  "claude-fable-5",
		"claude-opus-4-8": "claude-opus-4-8",
	}
	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}

func TestDefaultAntigravityModelMapping_Gemini31ProAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		AntigravityGemini31ProAgentModel: AntigravityGemini31ProAgentModel,
		"gemini-3.1-pro":                 AntigravityGemini31ProAgentModel,
		"gemini-3.1-pro-high":            AntigravityGemini31ProAgentModel,
		"gemini-3.1-pro-preview":         AntigravityGemini31ProAgentModel,
		"gemini-3.1-pro-low":             "gemini-3.1-pro-low",
	}

	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}

func TestAntigravityUserModelRoutes_SeparatesCatalogWireAndFingerprint(t *testing.T) {
	t.Parallel()

	routes := AntigravityUserModelRoutes()
	if len(routes) != 14 {
		t.Fatalf("unexpected agy route count: got %d want 14", len(routes))
	}

	byID := make(map[string]AntigravityModelRoute, len(routes))
	for _, route := range routes {
		byID[route.ModelID] = route
	}

	high := byID["gemini-3.7-flash-high"]
	if high.CatalogIDs[0] != "gemini-3.7-flash-tiered" || high.WireModel != "gemini-3.7-flash-high" || high.InternalModel != "MODEL_PLACEHOLDER_M298" {
		t.Fatalf("unexpected Gemini 3.7 High route: %+v", high)
	}
	if got := byID["gemini-3.5-flash-medium"].WireModel; got != "gemini-3.5-flash-low" {
		t.Fatalf("unexpected Gemini 3.5 Medium wire: %q", got)
	}
	if got := DefaultAntigravityModelMapping["gemini-3.5-flash-low"]; got != "gemini-3.5-flash-extra-low" {
		t.Fatalf("public Gemini 3.5 Low must win over the historical raw key: %q", got)
	}
	if _, ok := DefaultAntigravityModelMapping["MODEL_PLACEHOLDER_M298"]; ok {
		t.Fatal("internal model fingerprint must not become a request alias")
	}
}

func TestDefaultBedrockModelMapping_ContainsNewClaudeModels(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-fable-5":  "anthropic.claude-fable-5",
		"claude-opus-4-8": "us.anthropic.claude-opus-4-8-v1",
	}
	for from, want := range cases {
		got, ok := DefaultBedrockModelMapping[from]
		if !ok {
			t.Fatalf("expected Bedrock mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected Bedrock mapping for %q: got %q want %q", from, got, want)
		}
	}
}
