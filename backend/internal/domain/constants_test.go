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
		"claude-fable-5-1": "claude-fable-5-1",
		"claude-fable-5":   "claude-fable-5",
		"claude-opus-4-8":  "claude-opus-4-8",
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

func TestDefaultAntigravityModelMapping_PreservesExplicitSonnet45AndMigratesLegacyAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-sonnet-4-5":          "claude-sonnet-4-5",
		"claude-sonnet-4-5-thinking": "claude-sonnet-4-6",
		"claude-sonnet-4-5-20250929": "claude-sonnet-4-6",
	}
	for model, want := range cases {
		if got := DefaultAntigravityModelMapping[model]; got != want {
			t.Fatalf("expected model %q to map to %q, got %q", model, want, got)
		}
	}
}

func TestDefaultAntigravityModelMapping_Gemini31ProAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		AntigravityGemini31ProAgentModel: AntigravityGemini31ProAgentModel,
		"gemini-3.1-pro":                 "gemini-3.1-pro",
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
	if len(routes) != 33 {
		t.Fatalf("unexpected agy route count: got %d want 33", len(routes))
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

func TestDefaultAntigravityModelMapping_Gemini36VerifiedModels(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"gemini-3.6-flash-high", "gemini-3.6-flash-low", "gemini-3.6-flash-medium", "gemini-3.6-flash-tiered"} {
		if got := DefaultAntigravityModelMapping[model]; got != model {
			t.Fatalf("expected %s to map to itself, got %q", model, got)
		}
	}
	if got := DefaultAntigravityModelMapping["gemini-3.6-flash"]; got != "gemini-3.6-flash" {
		t.Fatal("聚合模型必须保留公开身份，由请求思考程度选择档位")
	}
}

func TestDefaultAntigravityModelMapping_Gemini37FlashModels(t *testing.T) {
	for _, model := range []string{"gemini-3.7-flash", "gemini-3.7-flash-high", "gemini-3.7-flash-low", "gemini-3.7-flash-medium", "gemini-3.7-flash-tiered"} {
		if got := DefaultAntigravityModelMapping[model]; got != model {
			t.Fatalf("expected %s to map to itself, got %q", model, got)
		}
	}
}

func TestDefaultAntigravityModelMapping_Gemini38FlashModels(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash", "gemini-3.8-flash-high", "gemini-3.8-flash-low", "gemini-3.8-flash-medium", "gemini-3.8-flash-tiered"} {
		if got := DefaultAntigravityModelMapping[model]; got != model {
			t.Fatalf("expected %s to map to itself, got %q", model, got)
		}
	}
}

func TestDefaultAntigravityModelMapping_Gemini4AndClaude55Models(t *testing.T) {
	// 聚合档位保留公开身份，具体 wire 由请求思考程度或路由解析选择。
	for model, wantWire := range map[string]string{
		"gemini-4-argon":    "gemini-4-argon-high",
		"gemini-4-hydrogen": "gemini-4-hydrogen-high",
		"claude-opus-5-5":   "claude-opus-5-5-high",
		"claude-sonnet-5-5": "claude-sonnet-5-5-high",
	} {
		if route, ok := ResolveDefaultAntigravityModelRoute(model); !ok || route.WireModel != wantWire {
			t.Fatalf("expected %s to resolve to %q, got %+v ok=%t", model, wantWire, route, ok)
		}
	}
	cases := map[string]string{
		"gemini-4-argon":           "gemini-4-argon",
		"gemini-4-argon-high":      "gemini-4-argon-high",
		"gemini-4-argon-low":       "gemini-4-argon-low",
		"gemini-4-argon-medium":    "gemini-4-argon-medium",
		"gemini-4-hydrogen":        "gemini-4-hydrogen",
		"gemini-4-hydrogen-high":   "gemini-4-hydrogen-high",
		"gemini-4-hydrogen-xhigh":  "gemini-4-hydrogen-xhigh",
		"gemini-4-hydrogen-medium": "gemini-4-hydrogen-medium",
		"gemini-4-hydrogen-low":    "gemini-4-hydrogen-low",
		"claude-opus-5-5":          "claude-opus-5-5",
		"claude-opus-5-5-high":     "claude-opus-5-5-high",
		"claude-opus-5-5-max":      "claude-opus-5-5-max",
		"claude-opus-5-5-low":      "claude-opus-5-5-low",
		"claude-opus-5-5-medium":   "claude-opus-5-5-medium",
		"claude-sonnet-5-5":        "claude-sonnet-5-5",
		"claude-sonnet-5-5-high":   "claude-sonnet-5-5-high",
		"claude-sonnet-5-5-max":    "claude-sonnet-5-5-max",
		"claude-sonnet-5-5-low":    "claude-sonnet-5-5-low",
		"claude-sonnet-5-5-medium": "claude-sonnet-5-5-medium",
	}
	for from, want := range cases {
		if got := DefaultAntigravityModelMapping[from]; got != want {
			t.Fatalf("expected %s to map to %q, got %q", from, want, got)
		}
	}
}

func TestAntigravityPublicModelRoutes_MatchLocalAgyUserCatalog(t *testing.T) {
	routes := AntigravityPublicModelRoutes()
	got := make([]string, 0, len(routes))
	for _, route := range routes {
		got = append(got, route.ModelID)
	}
	want := []string{
		"gemini-3.8-flash", "gemini-4-argon", "gemini-4-hydrogen",
		"claude-opus-5-5", "claude-sonnet-5-5",
		"gpt-oss-120b-medium",
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected public route count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected public route order: got %v want %v", got, want)
		}
	}
}

func TestAntigravityGemini4TiersUseDistinctWireModels(t *testing.T) {
	for model, efforts := range map[string][]string{
		"gemini-4-argon":    {"high", "medium", "low"},
		"gemini-4-hydrogen": {"high", "xhigh", "medium", "low"},
	} {
		base, ok := ResolveDefaultAntigravityModelRoute(model)
		if !ok || base.WireModel != model+"-high" {
			t.Fatalf("expected %s to default to high, got %+v ok=%t", model, base, ok)
		}

		seen := make(map[string]string, len(efforts))
		for _, effort := range efforts {
			route, ok := ResolveAntigravityReasoningRoute(model, effort)
			if !ok {
				t.Fatalf("expected reasoning route for %s effort %q", model, effort)
			}
			want := model + "-" + effort
			if route.WireModel != want {
				t.Fatalf("effort %q wire model = %q, want %q", effort, route.WireModel, want)
			}
			if previous, exists := seen[route.WireModel]; exists {
				t.Fatalf("effort %q reused wire model %q already used by %q", effort, route.WireModel, previous)
			}
			seen[route.WireModel] = effort
		}
	}
}

func TestAntigravityClaude55TiersAndMaxRemainRoutable(t *testing.T) {
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		for _, effort := range []string{"high", "medium", "low", "max"} {
			route, ok := ResolveAntigravityReasoningRoute(model, effort)
			if !ok {
				t.Fatalf("expected %s effort %q to resolve", model, effort)
			}
			if route.WireModel != model+"-"+effort {
				t.Fatalf("%s effort %q wire = %q", model, effort, route.WireModel)
			}
		}
		route, ok := ResolveAntigravityReasoningRoute(model, "xhigh")
		if !ok || route.WireModel != model+"-max" {
			t.Fatalf("expected %s xhigh to map to max, got %+v ok=%t", model, route, ok)
		}
	}
}

func TestDefaultBedrockModelMapping_ContainsNewClaudeModels(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-fable-5-2":  "anthropic.claude-fable-5-2",
		"claude-mythos-5-2": "anthropic.claude-mythos-5-2",
		"claude-fable-5-1":  "anthropic.claude-fable-5-1",
		"claude-fable-5":    "anthropic.claude-fable-5",
		"claude-opus-4-8":   "us.anthropic.claude-opus-4-8-v1",
		"claude-sonnet-5-5": "global.anthropic.claude-sonnet-5-5",
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
