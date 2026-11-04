package zcode

// 来源：src/server/routes-quota.ts。缺失 total 保留 nil；Coding 的 number 不作为积分总额。
import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Balance struct {
	Window      string   `json:"window,omitempty"`
	Name        string   `json:"name"`
	Remaining   *float64 `json:"remaining,omitempty"`
	Total       *float64 `json:"total,omitempty"`
	Used        *float64 `json:"used,omitempty"`
	Percent     *float64 `json:"used_percent,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
	ResetAt     int64    `json:"reset_at,omitempty"`
	EffectiveAt int64    `json:"effective_at,omitempty"`
}
type Quota struct {
	Status          string      `json:"status,omitempty"`
	NextAvailableAt int64       `json:"next_available_at,omitempty"`
	Plan            string      `json:"plan"`
	Plans           []QuotaPlan `json:"plans,omitempty"`
	Balances        []Balance   `json:"balances"`
	UpdatedAt       int64       `json:"updated_at"`
}

// QuotaPlan 是 Start Plan 的已领取计划；生效时间在 entitlement 上，
// 余额列表在生效前可能为空，不能据此把账号判为无套餐或额度耗尽。
type QuotaPlan struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Status       string             `json:"status,omitempty"`
	StartsAt     int64              `json:"starts_at,omitempty"`
	EndsAt       int64              `json:"ends_at,omitempty"`
	Entitlements []QuotaEntitlement `json:"entitlements,omitempty"`
}
type QuotaEntitlement struct {
	Name        string   `json:"name,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Units       *float64 `json:"units,omitempty"`
	EffectiveAt int64    `json:"effective_at,omitempty"`
	Period      string   `json:"period,omitempty"`
}

func number(v any) *float64 {
	var n float64
	switch v := v.(type) {
	case float64:
		n = v
	case json.Number:
		var e error
		n, e = v.Float64()
		if e != nil {
			return nil
		}
	case string:
		var e error
		n, e = strconv.ParseFloat(v, 64)
		if e != nil {
			return nil
		}
	default:
		return nil
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func value(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}
func epoch(v any) int64 {
	n := number(v)
	if n == nil {
		return 0
	}
	seconds := int64(*n)
	if seconds > 1e12 {
		seconds /= 1000
	}
	return seconds
}
func hasQuotaPlans(d map[string]any) bool {
	raw, exists := d["plans"]
	if !exists || raw == nil {
		return false
	}
	_, ok := raw.([]any)
	return ok
}
func parseQuotaPlans(d map[string]any) []QuotaPlan {
	rows, ok := d["plans"].([]any)
	if !ok {
		return nil
	}
	plans := make([]QuotaPlan, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		id := firstString(m, "plan_id", "planId", "id")
		name := firstString(m, "name", "show_name", "showName")
		if id == "" && name == "" {
			continue
		}
		if name == "" {
			name = id
		}
		plan := QuotaPlan{
			ID:       id,
			Name:     name,
			Status:   strings.ToLower(strings.TrimSpace(firstString(m, "status"))),
			StartsAt: epoch(value(m, "starts_at", "startsAt")),
			EndsAt:   epoch(value(m, "ends_at", "endsAt")),
		}
		if entitlements, ok := m["entitlements"].([]any); ok {
			for _, item := range entitlements {
				entitlement, ok := item.(map[string]any)
				if !ok {
					continue
				}
				plan.Entitlements = append(plan.Entitlements, QuotaEntitlement{
					Name:        firstString(entitlement, "show_name", "showName", "name"),
					Unit:        firstString(entitlement, "unit_type", "unitType", "unit"),
					Units:       number(value(entitlement, "grant_units", "grantUnits")),
					EffectiveAt: epoch(value(entitlement, "effective_at", "effectiveAt")),
					Period:      firstString(entitlement, "period"),
				})
			}
		}
		plans = append(plans, plan)
	}
	return plans
}
func (q *Quota) deriveStartStatus(now int64, rowCount int) {
	if len(q.Plans) > 0 {
		q.deriveStartStatusFromPlans(now)
		return
	}
	q.Status = "unknown"
	if rowCount == 0 {
		q.Status = "no_plan"
		return
	}
	if len(q.Balances) == 0 {
		return
	}
	active := false
	future := false
	remaining := false
	known := false
	for _, b := range q.Balances {
		if b.ExpiresAt > 0 && b.ExpiresAt <= now {
			continue
		}
		if b.EffectiveAt > now {
			future = true
			if q.NextAvailableAt == 0 || b.EffectiveAt < q.NextAvailableAt {
				q.NextAvailableAt = b.EffectiveAt
			}
			continue
		}
		active = true
		if b.Remaining != nil {
			known = true
			remaining = remaining || *b.Remaining > 0
		}
	}
	switch {
	case remaining:
		q.Status = "active"
	case active && known:
		q.Status = "exhausted"
	case !active && future:
		q.Status = "scheduled"
	case !active:
		q.Status = "expired"
	}
}
func (q *Quota) deriveStartStatusFromPlans(now int64) {
	live := false
	effective := false
	known := false
	remaining := false
	var next int64
	for _, plan := range q.Plans {
		if plan.Status == "expired" || (plan.EndsAt > 0 && plan.EndsAt <= now) {
			continue
		}
		live = true
		planEffective := false
		var planNext int64
		if len(plan.Entitlements) == 0 {
			if plan.StartsAt == 0 || plan.StartsAt <= now {
				planEffective = true
			} else {
				planNext = plan.StartsAt
			}
		} else {
			for _, entitlement := range plan.Entitlements {
				if entitlement.EffectiveAt == 0 || entitlement.EffectiveAt <= now {
					planEffective = true
				} else if planNext == 0 || entitlement.EffectiveAt < planNext {
					planNext = entitlement.EffectiveAt
				}
			}
			if plan.StartsAt > now && planNext == 0 {
				planNext = plan.StartsAt
			}
		}
		if planEffective {
			effective = true
		}
		if planNext > 0 && (next == 0 || planNext < next) {
			next = planNext
		}
	}
	for _, b := range q.Balances {
		if b.ExpiresAt > 0 && b.ExpiresAt <= now {
			continue
		}
		if b.EffectiveAt > now {
			continue
		}
		if b.Remaining != nil {
			known = true
			remaining = remaining || *b.Remaining > 0
		}
		if b.Percent != nil {
			known = true
			remaining = remaining || *b.Percent < 100
		}
	}
	switch {
	case effective && remaining:
		q.Status = "active"
	case effective && known:
		q.Status = "exhausted"
		if next > 0 {
			q.NextAvailableAt = next
		}
	case effective:
		q.Status = "active"
	case live:
		q.Status = "scheduled"
		q.NextAvailableAt = next
	case len(q.Plans) > 0:
		q.Status = "expired"
	default:
		q.Status = "no_plan"
	}
}
func ParseQuota(data []byte, plan string) (Quota, error) {
	var d map[string]any
	if json.Unmarshal(data, &d) != nil {
		return Quota{}, &Error{Kind: ErrFormat}
	}
	q := Quota{Plan: plan, Balances: []Balance{}, UpdatedAt: time.Now().Unix()}
	key := "balances"
	if plan == PlanCoding {
		key = "limits"
	}
	rawRows, exists := d[key]
	var rows []any
	if exists && rawRows != nil {
		parsed, ok := rawRows.([]any)
		if !ok {
			return Quota{}, &Error{Kind: ErrFormat}
		}
		rows = parsed
	} else if plan == PlanCoding || !hasQuotaPlans(d) {
		// Coding Plan 必须读取 limits；Start Plan 只在同时没有 plans 时视为格式错误。
		return Quota{}, &Error{Kind: ErrFormat}
	}
	for _, row := range rows {
		b, ok := row.(map[string]any)
		if !ok {
			return Quota{}, &Error{Kind: ErrFormat}
		}
		out := Balance{}
		if plan == PlanStart {
			out.Name = firstString(b, "show_name", "showName")
			out.Remaining = number(value(b, "remaining_units", "remainingUnits"))
			out.Total = number(value(b, "total_units", "totalUnits"))
			out.Used = number(value(b, "used_units", "usedUnits"))
			out.Unit = firstString(b, "unit_type", "unitType")
			out.ExpiresAt = epoch(value(b, "expires_at", "expiresAt"))
			out.EffectiveAt = epoch(value(b, "effective_at", "effectiveAt"))
			if out.Total != nil && *out.Total > 0 && out.Remaining != nil {
				used := *out.Total - *out.Remaining
				if used < 0 {
					used = 0
				}
				percent := math.Min(100, 100*used / *out.Total)
				out.Percent = &percent
			}
		} else {
			out.Name = firstString(b, "type")
			out.Remaining = number(value(b, "remaining"))
			out.Used = number(value(b, "usage"))
			out.Percent = number(value(b, "percentage"))
			out.Unit = ""
			switch epoch(value(b, "unit")) {
			case 3:
				out.Window = "5h"
			case 6:
				out.Window = "weekly"
			}
			out.ResetAt = epoch(value(b, "next_reset_time", "nextResetTime"))
			// 上游 number 与 remaining 并非可靠总额关系，不能根据 number 推算百分比。
		}
		if out.Remaining == nil && out.Percent == nil && out.Total == nil {
			continue
		}
		q.Balances = append(q.Balances, out)
	}
	if plan == PlanStart {
		q.Plans = parseQuotaPlans(d)
		q.deriveStartStatus(time.Now().Unix(), len(rows))
	}
	if plan == PlanCoding {
		tokens := []Balance{}
		fallback := []Balance{}
		for _, b := range q.Balances {
			switch b.Name {
			case "TOKENS_LIMIT":
				tokens = append(tokens, b)
			case "CREDIT_LIMIT":
				fallback = append(fallback, b)
			}
		}
		if len(tokens) > 0 {
			q.Balances = tokens
		} else {
			q.Balances = fallback
		}
		used := map[string]bool{}
		unclassified := []int{}
		for i, b := range q.Balances {
			if b.Window != "" && !used[b.Window] {
				used[b.Window] = true
			} else {
				q.Balances[i].Window = ""
				unclassified = append(unclassified, i)
			}
		}
		sort.SliceStable(unclassified, func(i, j int) bool { return q.Balances[unclassified[i]].ResetAt < q.Balances[unclassified[j]].ResetAt })
		for _, index := range unclassified {
			for _, window := range []string{"5h", "weekly"} {
				if !used[window] {
					q.Balances[index].Window = window
					used[window] = true
					break
				}
			}
		}
	}
	return q, nil
}
func (q Quota) Exhausted(now time.Time) bool {
	if q.Plan == PlanStart {
		switch q.Status {
		case "scheduled":
			return false
		case "no_plan", "expired":
			return true
		case "exhausted":
			if len(q.Balances) == 0 {
				return true
			}
		}
	}
	if len(q.Balances) == 0 {
		return false
	}
	if q.Plan == PlanCoding {
		for _, b := range q.Balances {
			if b.ResetAt > 0 && b.ResetAt <= now.Unix() {
				continue
			}
			if b.Percent != nil && *b.Percent >= 100 {
				return true
			}
			if b.Remaining != nil && *b.Remaining <= 0 {
				return true
			}
		}
		return false
	}
	known := false
	for _, b := range q.Balances {
		if b.EffectiveAt > now.Unix() {
			known = true
			continue
		}
		if b.ExpiresAt > 0 && b.ExpiresAt <= now.Unix() {
			known = true
			continue
		}
		if b.ResetAt > 0 && b.ResetAt <= now.Unix() {
			return false
		}
		if b.Remaining != nil {
			known = true
			if *b.Remaining > 0 {
				return false
			}
		} else if b.Percent != nil {
			known = true
			if *b.Percent < 100 {
				return false
			}
		} else {
			return false
		}
	}
	return known
}
func (c *Client) QueryQuota(ctx context.Context, p Provider, plan string, t Tokens) (Quota, error) {
	h := http.Header{}
	endpoint := ""
	if plan == PlanStart {
		h = c.Identity.Headers(false)
		h.Set("Authorization", "Bearer "+t.JWT)
		h.Set("Accept", "application/json")
		endpoint = c.OriginURL() + "/api/v1/zcode-plan/billing/balance?app_version=" + url.QueryEscape(c.Identity.Version()) + "&platform=" + url.QueryEscape(c.Identity.Platform)
	} else {
		h.Set("Authorization", t.CodingKey)
		h.Set("Accept", "application/json")
		endpoint = p.MonitorOrigin() + "/api/monitor/usage/quota/limit"
	}
	data, err := c.request(ctx, "GET", endpoint, h, nil, 15*time.Second)
	if err != nil {
		return Quota{}, err
	}
	return ParseQuota(data, plan)
}
