package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func nowForTest() time.Time {
	return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
}

func commandCodeTestModel(id, plan string, contextWindow int, tiers []map[string]any) map[string]any {
	return map[string]any{
		"id":            id,
		"name":          id,
		"contextWindow": contextWindow,
		"minPlanName":   plan,
		"tiers":         tiers,
		"deal":          false,
		"timeOfDay":     false,
	}
}

func commandCodeTestTier(contextLabel string, input, output, cacheRead float64, cacheWrite *float64) map[string]any {
	rates := map[string]any{"input": input, "output": output, "cacheRead": cacheRead}
	if cacheWrite != nil {
		rates["cacheWrite"] = *cacheWrite
	}
	return map[string]any{"context": contextLabel, "rates": rates, "listRates": false}
}

func commandCodeTestGoatHTML(t *testing.T, models []map[string]any) []byte {
	t.Helper()
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		plan, _ := model["minPlanName"].(string)
		if strings.EqualFold(plan, "Go") || strings.EqualFold(plan, "GOAT") {
			if id, ok := model["id"].(string); ok {
				modelIDs = append(modelIDs, id)
			}
		}
	}
	return commandCodeTestGoatHTMLWithScope(t, models, modelIDs)
}

func commandCodeTestGoatHTMLWithScope(t *testing.T, models []map[string]any, modelIDs []string) []byte {
	t.Helper()
	encodedModels, err := json.Marshal(models)
	require.NoError(t, err)
	encodedModelIDs, err := json.Marshal(modelIDs)
	require.NoError(t, err)
	rows := make([]map[string]any, 0, len(models))
	for _, model := range models {
		rows = append(rows, map[string]any{
			"name":    model["name"],
			"credits": "$$20",
		})
	}
	encodedRows, err := json.Marshal(rows)
	require.NoError(t, err)
	chunk := `1:{"models":` + string(encodedModels) + `,"planScope":{"modelIds":` + string(encodedModelIDs) + `},"rows":` + string(encodedRows) + `}`
	frame, err := json.Marshal([]any{1, chunk})
	require.NoError(t, err)
	return []byte(`<html><body><script>self.__next_f.push(` + string(frame) + `)</script></body></html>`)
}

func commandCodeTestProviderBody(t *testing.T, models []map[string]any) []byte {
	t.Helper()
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":             model["id"],
			"name":           model["name"],
			"context_length": model["contextWindow"],
		})
	}
	body, err := json.Marshal(map[string]any{"object": "list", "data": data})
	require.NoError(t, err)
	return body
}

func TestParseCommandCodeProviderCatalogCollectsValidatedModels(t *testing.T) {
	models, err := parseCommandCodeProviderCatalog([]byte(`{
		"object":"list",
		"data":[
			{"id":"gpt-5.6-luna","name":"GPT-5.6 Luna","context_length":1050000},
			{"id":"deepseek/deepseek-v4-flash","name":"DeepSeek V4 Flash","context_length":1000000},
			{"id":"bad model","context_length":1000},
			{"id":"missing-context"}
		]
	}`))
	require.NoError(t, err)
	require.Len(t, models, 2)
	require.Equal(t, 1_050_000, models["gpt-5.6-luna"].ContextWindow)

	_, err = parseCommandCodeProviderCatalog([]byte(`{"object":"list","data":[]}`))
	require.Error(t, err)
	_, err = parseCommandCodeProviderCatalog([]byte(`not-json`))
	require.Error(t, err)
}

func TestParseCommandCodeGoatDocumentFiltersPlansAndNormalizesRules(t *testing.T) {
	cacheWrite := 0.25
	models := []map[string]any{
		commandCodeTestModel("gpt-5.6-luna", "Go", 1_050_000, []map[string]any{
			commandCodeTestTier("≤272K", 0.2, 1.2, 0.02, &cacheWrite),
			commandCodeTestTier(">272K", 0.4, 1.8, 0.04, func() *float64 { value := 0.5; return &value }()),
		}),
		commandCodeTestModel("deepseek/deepseek-v4-flash", "GOAT", 1_000_000, []map[string]any{
			commandCodeTestTier("", 0.22, 0.66, 0.007, nil),
		}),
		commandCodeTestModel("claude-opus-premium", "Pro", 1_000_000, []map[string]any{
			commandCodeTestTier("", 5, 25, 0.5, nil),
		}),
	}
	models[0]["deal"] = map[string]any{
		"id": "luna-deal", "label": "50% off", "discountPercent": 50,
		"term": "limited time", "free": false, "expires": "2026-12-31T23:59:59Z",
	}
	if tiers, ok := models[0]["tiers"].([]map[string]any); ok && len(tiers) >= 2 {
		tiers[0]["listRates"] = map[string]any{
			"input": 0.4, "output": 2.4, "cacheRead": 0.04, "cacheWrite": 0.5,
		}
		tiers[1]["listRates"] = map[string]any{
			"input": 0.8, "output": 3.6, "cacheRead": 0.08, "cacheWrite": 1.0,
		}
	}
	models[1]["scheduledChange"] = map[string]any{
		"effective": "2026-08-16T16:00:00Z",
		"rates":     map[string]any{"input": 0.22, "output": 0.66, "cacheRead": 0.007},
	}
	models[1]["timeOfDay"] = map[string]any{
		"effective": "2026-08-16T16:00:00Z",
		"peak":      map[string]any{"input": 0.44, "output": 1.32, "cacheRead": 0.014},
		"offPeak":   map[string]any{"input": 0.22, "output": 0.66, "cacheRead": 0.007},
		"windows":   "01:00–04:00 and 06:00–10:00 UTC",
	}

	entries, err := parseCommandCodeGoatDocument(commandCodeTestGoatHTML(t, models))
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.NotContains(t, entries, "claude-opus-premium")

	luna := entries["gpt-5.6-luna"]
	require.Len(t, luna.Tiers, 2)
	require.Equal(t, 272_000, *luna.Tiers[0].MaxTokens)
	require.Equal(t, 272_000, luna.Tiers[1].MinTokens)
	require.InDelta(t, 0.4, luna.Tiers[1].Rates.Input, 1e-12)
	require.Equal(t, "luna-deal", luna.Deal.Code)
	require.Equal(t, time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC), luna.Deal.ExpiresAt)

	deepSeek := entries["deepseek/deepseek-v4-flash"]
	require.NotNil(t, deepSeek.ScheduledChange)
	require.Equal(t, time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC), deepSeek.ScheduledChange.Effective)
	require.Len(t, deepSeek.TimeOfDay.Windows, 2)
	require.Equal(t, commandCodeCatalogTimeWindow{StartHourUTC: 1, EndHourUTC: 4}, deepSeek.TimeOfDay.Windows[0])
	require.Equal(t, commandCodeCatalogTimeWindow{StartHourUTC: 6, EndHourUTC: 10}, deepSeek.TimeOfDay.Windows[1])
}

func TestParseCommandCodeGoatDocumentRejectsIncompletePlanScopeAndTierCoverage(t *testing.T) {
	models := []map[string]any{
		commandCodeTestModel("model/one", "Go", 1_000_000, []map[string]any{
			commandCodeTestTier("", 1, 2, 0.1, nil),
		}),
		commandCodeTestModel("model/two", "GOAT", 1_000_000, []map[string]any{
			commandCodeTestTier("", 1, 2, 0.1, nil),
		}),
	}
	_, err := parseCommandCodeGoatDocument(commandCodeTestGoatHTMLWithScope(t, models, []string{"model/one"}))
	require.ErrorContains(t, err, "plan scope")

	models = []map[string]any{
		commandCodeTestModel("model/incomplete", "Go", 1_000_000, []map[string]any{
			commandCodeTestTier("≤272K", 1, 2, 0.1, nil),
		}),
	}
	_, err = parseCommandCodeGoatDocument(commandCodeTestGoatHTML(t, models))
	require.ErrorContains(t, err, "do not cover")
}

func TestExtractCommandCodeNextFlightJoinsSplitFramesAndMultiplePushes(t *testing.T) {
	models := []map[string]any{
		commandCodeTestModel("gpt-5.6-luna", "Go", 1_050_000, []map[string]any{
			commandCodeTestTier("", 0.2, 1.2, 0.02, nil),
		}),
	}
	encodedModels, err := json.Marshal(models)
	require.NoError(t, err)
	fullChunk := `1:{"models":` + string(encodedModels) + `,"planScope":{"modelIds":["gpt-5.6-luna"]},"rows":[{"name":"gpt-5.6-luna","credits":"$$20"}]}`
	splitAt := strings.Index(fullChunk, "gpt-5.6-luna") + len("gpt-")
	require.Greater(t, splitAt, len("gpt-"))
	firstFrame, err := json.Marshal([]any{1, fullChunk[:splitAt]})
	require.NoError(t, err)
	secondFrame, err := json.Marshal([]any{1, fullChunk[splitAt:]})
	require.NoError(t, err)
	html := []byte(`<script>self.__next_f.push(` + string(firstFrame) + `);self.__next_f.push(` + string(secondFrame) + `)</script>`)

	entries, err := parseCommandCodeGoatDocument(html)
	require.NoError(t, err)
	require.Contains(t, entries, "gpt-5.6-luna")
}

func TestNormalizeCommandCodeDocumentTiersResolvesFlightListRateReference(t *testing.T) {
	tiers := []commandCodeDocumentTier{
		{
			Context:   "≤512K",
			Rates:     commandCodeDocumentRates{Input: testFloat64(0.3), Output: testFloat64(1.2), CacheRead: testFloat64(0.06)},
			ListRates: json.RawMessage(`{"input":0.6,"output":2.4,"cacheRead":0.12}`),
		},
		{
			Context:   ">512K",
			Rates:     commandCodeDocumentRates{Input: testFloat64(0.3), Output: testFloat64(1.2), CacheRead: testFloat64(0.06)},
			ListRates: json.RawMessage(`"$12:props:models:15:tiers:0:listRates"`),
		},
	}
	normalized, err := normalizeCommandCodeDocumentTiers(tiers, false)
	require.NoError(t, err)
	require.Len(t, normalized, 2)
	require.NotNil(t, normalized[1].ListRates)
	require.InDelta(t, 0.6, normalized[1].ListRates.Input, 1e-12)
	require.InDelta(t, 2.4, normalized[1].ListRates.Output, 1e-12)
}

func testFloat64(value float64) *float64 { return &value }

func TestCommandCodeCatalogRefreshCrossValidatesAndKeepsLastKnownGood(t *testing.T) {
	models := []map[string]any{
		commandCodeTestModel("gpt-5.6-luna", "Go", 1_050_000, []map[string]any{
			commandCodeTestTier("", 0.2, 1.2, 0.02, nil),
		}),
	}
	var providerContext atomic.Int64
	providerContext.Store(1_050_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/provider/v1/models":
			copyModels := []map[string]any{commandCodeTestModel("gpt-5.6-luna", "Go", int(providerContext.Load()), nil)}
			_, _ = w.Write(commandCodeTestProviderBody(t, copyModels))
		case "/docs/plans/goat":
			_, _ = w.Write(commandCodeTestGoatHTML(t, models))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	catalog := NewCommandCodeCatalog(server.Client())
	catalog.providerEndpoint = server.URL + "/provider/v1/models"
	catalog.goatEndpoint = server.URL + "/docs/plans/goat"
	catalog.minimumModels = 1
	catalog.minimumRetainedPercent = 0
	catalog.now = nowForTest

	ids, err := catalog.ForceRefresh(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.6-luna"}, ids)

	providerContext.Store(999_999)
	ids, err = catalog.ForceRefresh(context.Background())
	require.ErrorContains(t, err, "context mismatch")
	require.Equal(t, []string{"gpt-5.6-luna"}, ids)
	entry, ok := catalog.entry("gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, 1_050_000, entry.ContextWindow)
}

func TestCommandCodeCatalogRefreshRejectsUnexpectedLargeShrink(t *testing.T) {
	models := []map[string]any{
		commandCodeTestModel("gpt-5.6-luna", "Go", 1_050_000, []map[string]any{
			commandCodeTestTier("", 0.2, 1.2, 0.02, nil),
		}),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write(commandCodeTestProviderBody(t, models))
			return
		}
		_, _ = w.Write(commandCodeTestGoatHTML(t, models))
	}))
	defer server.Close()

	catalog := NewCommandCodeCatalog(server.Client())
	catalog.providerEndpoint = server.URL + "/models"
	catalog.goatEndpoint = server.URL + "/goat"
	catalog.minimumModels = 1
	catalog.now = nowForTest
	fallback := commandCodeFallbackCatalogEntries()
	catalog.mu.Lock()
	catalog.entries = make(map[string]commandCodeCatalogEntry)
	for key, entry := range fallback {
		catalog.entries[key] = entry
		if len(catalog.entries) == 10 {
			break
		}
	}
	catalog.lastSuccess = nowForTest().Add(-2 * time.Hour)
	catalog.mu.Unlock()

	ids, err := catalog.ForceRefresh(context.Background())
	require.ErrorContains(t, err, "shrank unexpectedly")
	require.Len(t, ids, 10)
}

func TestCommandCodeCatalogSnapshotRoundTripAndRejectsCorruption(t *testing.T) {
	catalog := NewCommandCodeCatalog(&http.Client{})
	catalog.minimumModels = 1
	catalog.now = nowForTest
	catalog.mu.Lock()
	catalog.entries = map[string]commandCodeCatalogEntry{
		"gpt-5.6-luna": commandCodeFallbackCatalogEntries()["gpt-5.6-luna"],
	}
	catalog.lastSuccess = nowForTest()
	catalog.mu.Unlock()

	path := filepath.Join(t.TempDir(), "commandcode.json")
	require.NoError(t, catalog.SaveSnapshot(path))

	restored := NewCommandCodeCatalog(&http.Client{})
	restored.minimumModels = 1
	restored.now = nowForTest
	require.NoError(t, restored.LoadSnapshot(path))
	entry, ok := restored.entry("gpt-5.6-luna")
	require.True(t, ok)
	require.Equal(t, 1_050_000, entry.ContextWindow)
	require.Len(t, entry.Tiers, 2)

	badSnapshot := commandCodeCatalogDiskSnapshot{
		Version: commandCodeCatalogSnapshotVersion,
		SavedAt: nowForTest(),
		Entries: map[string]commandCodeCatalogEntry{
			"wrong-key": commandCodeFallbackCatalogEntries()["gpt-5.6-luna"],
		},
	}
	body, err := json.Marshal(badSnapshot)
	require.NoError(t, err)
	badPath := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(badPath, body, 0o644))
	require.Error(t, restored.LoadSnapshot(badPath))
}

func TestCommandCodeCatalogConcurrentRefreshUsesSingleFlight(t *testing.T) {
	models := []map[string]any{
		commandCodeTestModel("gpt-5.6-luna", "Go", 1_050_000, []map[string]any{
			commandCodeTestTier("", 0.2, 1.2, 0.02, nil),
		}),
	}
	var requests atomic.Int64
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		<-release
		if r.URL.Path == "/models" {
			_, _ = w.Write(commandCodeTestProviderBody(t, models))
			return
		}
		_, _ = w.Write(commandCodeTestGoatHTML(t, models))
	}))
	defer server.Close()

	catalog := NewCommandCodeCatalog(server.Client())
	catalog.providerEndpoint = server.URL + "/models"
	catalog.goatEndpoint = server.URL + "/goat"
	catalog.minimumModels = 1
	catalog.minimumRetainedPercent = 0

	const workers = 12
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(workers)
	var done sync.WaitGroup
	done.Add(workers)
	for range workers {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			_, _ = catalog.ForceRefresh(context.Background())
		}()
	}
	ready.Wait()
	close(start)
	require.Eventually(t, func() bool { return requests.Load() == 2 }, time.Second, 10*time.Millisecond)
	close(release)
	done.Wait()
	require.Equal(t, int64(2), requests.Load())
}

func TestCommandCodeOfficialCatalogLive(t *testing.T) {
	if os.Getenv("COMMANDCODE_LIVE_TEST") != "1" {
		t.Skip("set COMMANDCODE_LIVE_TEST=1 to validate the official catalog")
	}
	catalog := NewCommandCodeCatalog(nil)
	ids, err := catalog.ForceRefresh(context.Background())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(ids), commandCodeCatalogMinModelCount)
	require.Contains(t, ids, "deepseek/deepseek-v4-flash")
	entry, ok := catalog.entry("gpt-5.6-luna")
	require.True(t, ok)
	require.Greater(t, entry.ContextWindow, 0)
	require.NotEmpty(t, entry.Tiers)
	deepSeek, ok := catalog.entry("deepseek/deepseek-v4-flash")
	require.True(t, ok)
	require.NotNil(t, deepSeek.ScheduledChange)
	require.NotNil(t, deepSeek.TimeOfDay)
	miniMax, ok := catalog.entry("MiniMaxAI/MiniMax-M3")
	require.True(t, ok)
	require.Len(t, miniMax.Tiers, 2)
	require.NotNil(t, miniMax.Tiers[0].ListRates)
	require.NotNil(t, miniMax.Tiers[1].ListRates)
}

func TestCommandCodeFallbackCatalogHasAllPricedModels(t *testing.T) {
	entries := commandCodeFallbackCatalogEntries()
	require.Len(t, entries, len(commandCodeFallbackModels))
	for _, model := range CommandCodeFallbackModelIDs() {
		entry, ok := entries[strings.ToLower(model)]
		require.True(t, ok, "fallback catalog model %q is missing", model)
		_, ok = commandCodeCatalogPricingAt(entry, nowForTest())
		require.True(t, ok, "fallback catalog model %q has no active pricing", model)
		require.Positive(t, entry.MonthlyCreditsUSD, "fallback catalog model %q has no monthly credits", model)
	}
}

func TestCommandCodeCatalogExposesFallbackWhenRefreshFails(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	catalog := NewCommandCodeCatalog(client)
	models := catalog.ModelIDs(context.Background())
	require.NotEmpty(t, models)
	require.Contains(t, models, "gpt-5.6-sol")
	require.Contains(t, models, "deepseek/deepseek-v4-flash")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
