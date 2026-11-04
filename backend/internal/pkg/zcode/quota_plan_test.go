package zcode

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 真实 Start Plan 响应在生效前只返回 plans、balances 为空；不能据此判为无套餐或额度耗尽。
func TestQuotaScheduledPlanWithoutBalances(t *testing.T) {
	now := time.Now().Unix()
	body := fmt.Sprintf(`{"server_time":%d,"plans":[{"plan_id":"synthetic-global-build","name":"ZCODE GLOBAL BUILD","status":"active","starts_at":%d,"ends_at":%d,"entitlements":[{"show_name":"GLM-5.3-Flash","unit_type":"token","grant_units":300000000,"effective_at":%d,"period":"one_time"}]}],"balances":[]}`, now, now-600, now+36000, now+1800)
	quota, err := ParseQuota([]byte(body), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "scheduled", quota.Status)
	require.Equal(t, now+1800, quota.NextAvailableAt)
	require.Len(t, quota.Plans, 1)
	require.Equal(t, "ZCODE GLOBAL BUILD", quota.Plans[0].Name)
	require.Len(t, quota.Plans[0].Entitlements, 1)
	require.Equal(t, "GLM-5.3-Flash", quota.Plans[0].Entitlements[0].Name)
	require.Equal(t, float64(300000000), *quota.Plans[0].Entitlements[0].Units)
	require.Equal(t, now+1800, quota.Plans[0].Entitlements[0].EffectiveAt)
	require.False(t, quota.Exhausted(time.Now()))
}

func TestQuotaActiveAndExhaustedPlan(t *testing.T) {
	now := time.Now().Unix()
	plan := fmt.Sprintf(`{"plan_id":"synthetic-plan","name":"合成套餐","status":"active","starts_at":%d,"ends_at":%d,"entitlements":[{"show_name":"GLM-5.3-Flash","unit_type":"token","grant_units":100,"effective_at":%d}]}`, now-60, now+3600, now-60)
	activeBody := fmt.Sprintf(`{"server_time":%d,"plans":[%s],"balances":[{"show_name":"GLM-5.3-Flash","remaining_units":75,"total_units":100,"unit_type":"token"}]}`, now, plan)
	active, err := ParseQuota([]byte(activeBody), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "active", active.Status)
	require.False(t, active.Exhausted(time.Now()))

	spentBody := fmt.Sprintf(`{"server_time":%d,"plans":[%s],"balances":[{"show_name":"GLM-5.3-Flash","remaining_units":0,"total_units":100,"unit_type":"token"}]}`, now, plan)
	spent, err := ParseQuota([]byte(spentBody), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "exhausted", spent.Status)
	require.True(t, spent.Exhausted(time.Now()))
}

func TestQuotaPlanWithoutBalancesIsActiveNotExhausted(t *testing.T) {
	now := time.Now().Unix()
	body := fmt.Sprintf(`{"plans":[{"plan_id":"p","name":"合成套餐","status":"active","starts_at":%d,"ends_at":%d,"entitlements":[{"show_name":"GLM-5.3-Flash","effective_at":%d}]}],"balances":[]}`, now-60, now+3600, now-60)
	quota, err := ParseQuota([]byte(body), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "active", quota.Status)
	require.False(t, quota.Exhausted(time.Now()))
}

func TestQuotaExpiredPlan(t *testing.T) {
	now := time.Now().Unix()
	body := fmt.Sprintf(`{"plans":[{"plan_id":"p","name":"合成套餐","status":"active","starts_at":%d,"ends_at":%d}],"balances":[{"show_name":"GLM-5.3-Flash","remaining_units":10}]}`, now-7200, now-60)
	quota, err := ParseQuota([]byte(body), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "expired", quota.Status)
	require.True(t, quota.Exhausted(time.Now()))
}

func TestQuotaMissingBalancesWithPlans(t *testing.T) {
	now := time.Now().Unix()
	body := fmt.Sprintf(`{"plans":[{"plan_id":"p","name":"合成套餐","status":"active","starts_at":%d,"ends_at":%d,"entitlements":[{"effective_at":%d}]}]}`, now-60, now+3600, now+600)
	quota, err := ParseQuota([]byte(body), PlanStart)
	require.NoError(t, err)
	require.Equal(t, "scheduled", quota.Status)
	require.Equal(t, now+600, quota.NextAvailableAt)
}

func TestQuotaPlansDoNotHideMalformedBalances(t *testing.T) {
	_, err := ParseQuota([]byte(`{"plans":[],"balances":"broken"}`), PlanStart)
	require.Error(t, err)
	_, err = ParseQuota([]byte(`{"plans":[]}`), PlanStart)
	require.NoError(t, err)
}
