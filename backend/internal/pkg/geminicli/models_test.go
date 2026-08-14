package geminicli

import (
	"slices"
	"testing"
)

func TestDefaultModels_ContainsImageModels(t *testing.T) {
	t.Parallel()

	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		byID[model.ID] = model
	}

	required := []string{
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
	}

	for _, id := range required {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected curated Gemini model %q to exist", id)
		}
	}
}

func TestAIStudioFreeModels_MatchesCurrentRequestIDs(t *testing.T) {
	t.Parallel()

	want := []string{
		"gemini-3-flash-preview",
		"gemini-2.5-flash",
		"gemini-2.5-flash-lite",
		"gemini-3.1-flash-lite",
		"gemini-3.5-flash",
		"gemini-3.5-flash-lite",
		"gemini-3.6-flash",
		"gemini-3.7-flash",
		"gemma-4-26b-a4b-it",
		"gemma-4-31b-it",
	}
	got := make([]string, 0, len(AIStudioFreeModels))
	for _, model := range AIStudioFreeModels {
		got = append(got, model.ID)
		if model.Type != "model" {
			t.Fatalf("model %q type = %q, want model", model.ID, model.Type)
		}
		if model.DisplayName == "" {
			t.Fatalf("model %q has an empty display name", model.ID)
		}
	}

	if !slices.Equal(got, want) {
		t.Fatalf("AI Studio Free Tier IDs = %v, want %v", got, want)
	}
	if slices.Contains(got, "gemma-4-26b-it") {
		t.Fatal("Gemma 4 26B must use the gemma-4-26b-a4b-it request ID")
	}
}

func TestModelsForAIStudioTier_SelectsFreeCatalog(t *testing.T) {
	t.Parallel()

	for _, tierID := range []string{"aistudio_free", " AISTUDIO_FREE ", "free", ""} {
		models := ModelsForAIStudioTier(tierID)
		if len(models) != len(AIStudioFreeModels) {
			t.Fatalf("tier %q returned %d models, want %d", tierID, len(models), len(AIStudioFreeModels))
		}
		for i := range models {
			if models[i].ID != AIStudioFreeModels[i].ID {
				t.Fatalf("tier %q model[%d] = %q, want %q", tierID, i, models[i].ID, AIStudioFreeModels[i].ID)
			}
		}
	}

	paid := ModelsForAIStudioTier("aistudio_paid")
	if len(paid) != len(DefaultModels) {
		t.Fatalf("paid catalog returned %d models, want %d", len(paid), len(DefaultModels))
	}
	for i := range paid {
		if paid[i].ID != DefaultModels[i].ID {
			t.Fatalf("paid model[%d] = %q, want %q", i, paid[i].ID, DefaultModels[i].ID)
		}
	}

	if got := DefaultModelForAIStudioTier("aistudio_free"); got != "gemini-3-flash-preview" {
		t.Fatalf("free default test model = %q, want gemini-3-flash-preview", got)
	}
	if got := DefaultModelForAIStudioTier("aistudio_paid"); got != DefaultTestModel {
		t.Fatalf("paid default test model = %q, want %q", got, DefaultTestModel)
	}
}
