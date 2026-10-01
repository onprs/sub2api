package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func commandCodeCreditHTML(t *testing.T, rows []map[string]any) []byte {
	t.Helper()
	model := commandCodeTestModel("moonshotai/Kimi-K3", "Go", 1_000_000, []map[string]any{
		commandCodeTestTier("", 3, 15, 0.3, nil),
	})
	model["name"] = "Kimi K3"
	payload, err := json.Marshal(map[string]any{
		"models": []any{model}, "planScope": map[string]any{"modelIds": []string{"moonshotai/Kimi-K3"}}, "rows": rows,
	})
	require.NoError(t, err)
	frame, err := json.Marshal([]any{1, "1:" + string(payload)})
	require.NoError(t, err)
	return []byte(`<script>self.__next_f.push(` + string(frame) + `)</script>`)
}

func commandCodeCreditRow() map[string]any {
	return map[string]any{"name": "Kimi K3", "credits": "$$60", "creditDeal": map[string]any{
		"was": "$$20", "starts": "2026-09-30", "expires": "2026-10-07", "term": "through Oct 7th", "liveAtBuild": false,
	}}
}

func TestCommandCodeMonthlyQuotaParsesOfficialCreditDealIndependentlyOfPrices(t *testing.T) {
	entries, err := parseCommandCodeGoatDocument(commandCodeCreditHTML(t, []map[string]any{commandCodeCreditRow()}))
	require.NoError(t, err)
	entry := entries["moonshotai/kimi-k3"]
	require.Nil(t, entry.Deal)
	require.Equal(t, 20.0, entry.MonthlyQuota.BaseCreditsUSD)
	require.Len(t, entry.MonthlyQuota.Periods, 1)
	period := entry.MonthlyQuota.Periods[0]
	require.Equal(t, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), *period.StartsAt)
	require.Equal(t, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), *period.ExpiresAt)
	catalog := &CommandCodeCatalog{entries: entries}
	for _, tt := range []struct {
		at      time.Time
		credits float64
	}{
		{period.StartsAt.Add(-time.Nanosecond), 20}, {*period.StartsAt, 60},
		{period.ExpiresAt.Add(-time.Nanosecond), 60}, {*period.ExpiresAt, 20},
	} {
		quota, ok := catalog.MonthlyQuotaAt("commandcode/kimi-k3", tt.at)
		require.True(t, ok)
		require.Equal(t, tt.credits, quota.CreditsUSD)
		pricing, ok := commandCodeCatalogPricingAt(entry, tt.at)
		require.True(t, ok)
		require.InDelta(t, 3e-6, pricing.InputPricePerToken, 1e-12)
		require.InDelta(t, 15e-6, pricing.OutputPricePerToken, 1e-12)
	}
}

func TestCommandCodeMonthlyQuotaRejectsMalformedAndConflictingCreditRows(t *testing.T) {
	for _, tt := range []struct {
		name string
		deal any
	}{
		{"缺少基础额度", map[string]any{"starts": "2026-09-30", "expires": "2026-10-07"}},
		{"错误截止日期", map[string]any{"was": "$$20", "expires": "待确认"}},
		{"倒置时间", map[string]any{"was": "$$20", "starts": "2026-10-10", "expires": "2026-10-07"}},
		{"没有有效期", map[string]any{"was": "$$20", "term": "限时"}},
		{"未知引用", "$unknown:creditDeal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			row := commandCodeCreditRow()
			row["creditDeal"] = tt.deal
			_, err := parseCommandCodeGoatDocument(commandCodeCreditHTML(t, []map[string]any{row}))
			require.Error(t, err)
		})
	}
	_, err := parseCommandCodeGoatDocument(commandCodeCreditHTML(t, []map[string]any{
		commandCodeCreditRow(), {"name": "Kimi K3 (latest)", "credits": "$$20"},
	}))
	require.ErrorContains(t, err, "conflicting")
	_, err = parseCommandCodeGoatDocument(commandCodeCreditHTML(t, []map[string]any{commandCodeCreditRow(), commandCodeCreditRow()}))
	require.NoError(t, err)
}

func TestCommandCodeMonthlyQuotaRFC3339PreservesExactInstant(t *testing.T) {
	row := commandCodeDocumentCreditRow{Name: "Kimi K3", Credits: "$$60", CreditDeal: json.RawMessage(
		`{"was":"$$20","starts":"2026-09-30T08:00:00+08:00","expires":"2026-10-08T08:00:00+08:00"}`,
	)}
	policy, err := parseCommandCodeMonthlyQuota(row)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), *policy.Periods[0].StartsAt)
	require.Equal(t, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), *policy.Periods[0].ExpiresAt)
}

func TestCommandCodeMonthlyQuotaSnapshotSurvivesOfflineExpiryAndRejectsLegacy(t *testing.T) {
	entries, err := parseCommandCodeGoatDocument(commandCodeCreditHTML(t, []map[string]any{commandCodeCreditRow()}))
	require.NoError(t, err)
	inside := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	catalog := NewCommandCodeCatalog(nil)
	catalog.entries = entries
	catalog.minimumModels = 1
	catalog.lastSuccess = inside
	file := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, catalog.SaveSnapshot(file))
	loaded := NewCommandCodeCatalog(nil)
	loaded.minimumModels = 1
	loaded.now = func() time.Time { return inside.AddDate(0, 0, 10) }
	require.NoError(t, loaded.LoadSnapshot(file))
	quota, ok := loaded.MonthlyQuotaAt("kimi-k3", inside)
	require.True(t, ok)
	require.Equal(t, 60.0, quota.CreditsUSD)
	quota, ok = loaded.MonthlyQuotaAt("kimi-k3", loaded.now())
	require.True(t, ok)
	require.Equal(t, 20.0, quota.CreditsUSD)
	// 对外返回的时间指针修改不能污染缓存。
	copy, ok := loaded.entry("kimi-k3")
	require.True(t, ok)
	*copy.MonthlyQuota.Periods[0].ExpiresAt = inside.AddDate(1, 0, 0)
	quota, ok = loaded.MonthlyQuotaAt("kimi-k3", loaded.now())
	require.True(t, ok)
	require.Equal(t, 20.0, quota.CreditsUSD)
	legacy := commandCodeCatalogDiskSnapshot{Version: 1, SavedAt: inside, Entries: entries}
	body, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, body, 0o600))
	require.ErrorContains(t, loaded.LoadSnapshot(file), "unsupported")
	quota, ok = loaded.MonthlyQuotaAt("kimi-k3", loaded.now())
	require.True(t, ok)
	require.Equal(t, 20.0, quota.CreditsUSD)
}

func TestCommandCodeMonthlyQuotaFailedRefreshKeepsLastValidatedSchedule(t *testing.T) {
	var document atomic.Value
	document.Store(commandCodeCreditHTML(t, []map[string]any{commandCodeCreditRow()}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"moonshotai/Kimi-K3","context_length":1000000}]}`))
			return
		}
		body, ok := document.Load().([]byte)
		if !ok {
			t.Error("官方文档测试副本类型错误")
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	catalog := NewCommandCodeCatalog(server.Client())
	catalog.providerEndpoint = server.URL + "/models"
	catalog.goatEndpoint = server.URL + "/goat"
	catalog.minimumModels = 1
	catalog.minimumRetainedPercent = 0
	_, err := catalog.ForceRefresh(context.Background())
	require.NoError(t, err)
	row := commandCodeCreditRow()
	row["creditDeal"] = map[string]any{"was": "$$20", "expires": "invalid"}
	document.Store(commandCodeCreditHTML(t, []map[string]any{row}))
	_, err = catalog.ForceRefresh(context.Background())
	require.Error(t, err)
	at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	quota, ok := catalog.MonthlyQuotaAt("kimi-k3", at)
	require.True(t, ok)
	require.Equal(t, 20.0, quota.CreditsUSD)
}
