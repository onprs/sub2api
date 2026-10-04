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
	Status          string    `json:"status,omitempty"`
	NextAvailableAt int64     `json:"next_available_at,omitempty"`
	Plan            string    `json:"plan"`
	Balances        []Balance `json:"balances"`
	UpdatedAt       int64     `json:"updated_at"`
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
	rows, ok := d[key].([]any)
	if !ok {
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
		q.Status = "unknown"
		if len(rows) == 0 {
			q.Status = "no_plan"
		}
		if len(q.Balances) > 0 {
			active := false
			future := false
			remaining := false
			known := false
			for _, b := range q.Balances {
				if b.ExpiresAt > 0 && b.ExpiresAt <= time.Now().Unix() {
					continue
				}
				if b.EffectiveAt > time.Now().Unix() {
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
			if remaining {
				q.Status = "active"
			} else if active && known {
				q.Status = "exhausted"
			} else if !active && future {
				q.Status = "scheduled"
			} else if !active {
				q.Status = "expired"
			}
		}
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
	if len(q.Balances) == 0 {
		return q.Plan == PlanStart && q.Status == "no_plan"
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
