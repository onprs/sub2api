package service

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonthlyQuotaPolicyResolvesBoundariesAndFutureChanges(t *testing.T) {
	start := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	next := end.AddDate(0, 0, 2)
	policy := MonthlyQuotaPolicy{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{
		{CreditsUSD: 60, StartsAt: &start, ExpiresAt: &end, Term: "限时额度"},
		{CreditsUSD: 40, StartsAt: &next},
	}}
	for _, tt := range []struct {
		name    string
		at      time.Time
		credits float64
		next    *time.Time
		active  bool
	}{
		{"开始前", start.Add(-time.Nanosecond), 20, &start, false},
		{"开始时", start, 60, &end, true},
		{"结束前", end.Add(-time.Nanosecond), 60, &end, true},
		{"结束时", end, 20, &next, false},
		{"未来变更", next, 40, nil, true},
		{"不同时区同一时刻", start.In(time.FixedZone("UTC+8", 8*3600)), 60, &end, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			quota, ok := policy.ResolveAt(tt.at, 70)
			require.True(t, ok)
			require.Equal(t, tt.credits, quota.CreditsUSD)
			require.InDelta(t, 70/tt.credits, quota.CostMultiplier, 1e-12)
			require.Equal(t, tt.next, quota.NextChangeAt)
			require.Equal(t, tt.active, quota.StartsAt != nil)
		})
	}
}

func TestMonthlyQuotaPolicyRejectsUnknownAmountsAndAmbiguousPeriods(t *testing.T) {
	at := nowForTest()
	later := at.Add(time.Hour)
	for _, policy := range []MonthlyQuotaPolicy{
		{BaseCreditsUSD: -1},
		{BaseCreditsUSD: math.NaN()},
		{BaseCreditsUSD: math.Inf(1)},
		{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{{CreditsUSD: 60}}},
		{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{{CreditsUSD: 0, StartsAt: &at}}},
		{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{{CreditsUSD: 60, StartsAt: &later, ExpiresAt: &at}}},
		{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{
			{CreditsUSD: 60, StartsAt: &at, ExpiresAt: &later}, {CreditsUSD: 40, StartsAt: &at},
		}},
	} {
		require.Error(t, policy.Validate())
		_, ok := policy.ResolveAt(at, 70)
		require.False(t, ok)
	}
	for _, shared := range []float64{0, -1, math.Inf(1), math.NaN()} {
		_, ok := (MonthlyQuotaPolicy{BaseCreditsUSD: 20}).ResolveAt(at, shared)
		require.False(t, ok)
	}
	_, ok := (MonthlyQuotaPolicy{BaseCreditsUSD: 20}).ResolveAt(time.Time{}, 70)
	require.False(t, ok)
	// 没有已确认的基础额度时，活动结束后不能猜测下一段额度。
	unknown := MonthlyQuotaPolicy{Periods: []MonthlyQuotaPeriod{{CreditsUSD: 60, ExpiresAt: &later}}}
	_, ok = unknown.ResolveAt(at, 70)
	require.True(t, ok)
	_, ok = unknown.ResolveAt(later, 70)
	require.False(t, ok)
}

func TestMonthlyQuotaPolicyCopiesTimesAndSupportsAdjacentPeriods(t *testing.T) {
	start := nowForTest()
	boundary := start.Add(time.Hour)
	policy := MonthlyQuotaPolicy{BaseCreditsUSD: 20, Periods: []MonthlyQuotaPeriod{
		{CreditsUSD: 60, StartsAt: &start, ExpiresAt: &boundary}, {CreditsUSD: 40, StartsAt: &boundary},
	}}
	require.NoError(t, policy.Validate())
	quota, ok := policy.ResolveAt(boundary, 70)
	require.True(t, ok)
	require.Equal(t, 40.0, quota.CreditsUSD)
	*quota.StartsAt = boundary.Add(time.Hour)
	require.Equal(t, boundary, *policy.Periods[1].StartsAt)
	cloned := policy.Clone()
	cloned.Periods[0].CreditsUSD = 99
	*cloned.Periods[0].StartsAt = start.Add(time.Hour)
	require.Equal(t, 60.0, policy.Periods[0].CreditsUSD)
	require.Equal(t, start, *policy.Periods[0].StartsAt)
}
