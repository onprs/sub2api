package zcode

// 来源：src/claim/client.ts、scheduler.ts。保留 starts_at 与 server retry，业务轮询在服务层。
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Campaign struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	StartsAt       int64  `json:"starts_at"`
	EndsAt         int64  `json:"ends_at"`
	ClaimUntil     int64  `json:"claim_until,omitempty"`
	MinimumVersion string `json:"min_app_version"`
	Plans          []Plan `json:"plans"`
}
type Plan struct {
	ID       string   `json:"id"`
	Name     string   `json:"show_name"`
	StartsAt int64    `json:"starts_at"`
	EndsAt   int64    `json:"ends_at"`
	Units    *float64 `json:"total_units,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

// 桌面协议使用 plan_id/name，保留内部 DTO 的 id/show_name 兼容。
func (p *Plan) UnmarshalJSON(data []byte) error {
	type alias Plan
	var raw struct {
		alias
		PlanID string `json:"plan_id"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Plan(raw.alias)
	if raw.PlanID != "" {
		p.ID = raw.PlanID
	}
	if raw.Name != "" {
		p.Name = raw.Name
	}
	return nil
}

type Preview struct {
	Campaign  *Campaign `json:"campaign"`
	Claimable bool      `json:"claimable"`
	Blocked   string    `json:"blocked,omitempty"`
	Plan      *Plan     `json:"claimed_plan,omitempty"`
	RetryAt   int64     `json:"retry_at,omitempty"`
}
type ClaimResult struct {
	OK      bool   `json:"ok"`
	Result  string `json:"result"`
	Code    int    `json:"code,omitempty"`
	Plan    *Plan  `json:"plan,omitempty"`
	RetryAt int64  `json:"retry_at,omitempty"`
}

func VersionAtLeast(version, minimum string) bool {
	parse := func(v string) []int {
		parts := strings.Split(strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")[0], ".")
		values := make([]int, 3)
		for i := 0; i < len(parts) && i < 3; i++ {
			n, e := strconv.Atoi(parts[i])
			if e != nil || n < 0 {
				return nil
			}
			values[i] = n
		}
		return values
	}
	a, b := parse(version), parse(minimum)
	if a == nil || b == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}
func ParsePreview(data []byte) (Preview, error) {
	var desktop struct {
		Plans    json.RawMessage `json:"plans"`
		Campaign json.RawMessage `json:"campaign"`
	}
	if json.Unmarshal(data, &desktop) != nil {
		return Preview{}, &Error{Kind: ErrFormat}
	}
	if len(desktop.Plans) > 0 {
		var plans []Plan
		if json.Unmarshal(desktop.Plans, &plans) != nil {
			return Preview{}, &Error{Kind: ErrFormat}
		}
		valid := plans[:0]
		for _, plan := range plans {
			if plan.ID != "" {
				valid = append(valid, plan)
			}
		}
		if len(valid) == 0 {
			return Preview{Blocked: "none"}, nil
		}
		sort.SliceStable(valid, func(i, j int) bool { return valid[i].Priority > valid[j].Priority })
		// 顶层 plans 与 campaign 元信息可能同时出现：保留活动窗口与最低版本。
		campaign := &Campaign{Plans: valid}
		if len(desktop.Campaign) > 0 {
			var base Campaign
			if json.Unmarshal(desktop.Campaign, &base) == nil {
				if len(base.Plans) == 0 {
					base.Plans = valid
				}
				campaign = &base
			}
		}
		return Preview{Campaign: campaign, Claimable: true}, nil
	}
	var raw struct {
		Campaign  *Campaign `json:"campaign"`
		Claimable *bool     `json:"claimable"`
		CanClaim  *bool     `json:"can_claim"`
		Reason    string    `json:"reason"`
		Blocked   string    `json:"blocked"`
		Plan      *Plan     `json:"plan"`
		RetryAt   int64     `json:"next_available_at"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return Preview{}, &Error{Kind: ErrFormat}
	}
	reason := raw.Reason
	if reason == "" {
		reason = raw.Blocked
	}
	result := Preview{Campaign: raw.Campaign, Plan: raw.Plan, RetryAt: raw.RetryAt}
	switch {
	case raw.Claimable != nil:
		result.Claimable = *raw.Claimable
	case raw.CanClaim != nil:
		result.Claimable = *raw.CanClaim
	case raw.Campaign != nil && reason == "":
		// 新活动格式省略 claimable；窗口内且未给出阻塞原因时按可领取处理。
		result.Claimable = true
	}
	// 只存固定枚举，避免 unknown reason 携带敏感值进入状态与日志。
	switch reason {
	case "none", "not_in_window", "version_too_low", "already_claimed", "quota_exhausted", "not_eligible":
		result.Blocked = reason
	case "":
	default:
		result.Blocked = "unavailable"
	}
	return result, nil
}
func claimHeaders(id Identity, jwt string) http.Header {
	h := bearer(jwt)
	h.Set("X-ZCode-App-Version", id.Version())
	h.Set("X-Platform", id.Platform)
	if id.DeviceMid != "" {
		h.Set("X-Device-Mid", id.DeviceMid)
	}
	return h
}
func (c *Client) Preview(ctx context.Context, jwt string) (Preview, error) {
	endpoint := c.OriginURL() + "/api/v1/zcode-plan/billing/preview?app_version=" + url.QueryEscape(c.Identity.Version()) + "&platform=" + url.QueryEscape(c.Identity.Platform)
	headers := bearer(jwt)
	if c.Identity.DeviceMid != "" {
		headers.Set("X-Device-Mid", c.Identity.DeviceMid)
	}
	data, err := c.requestOptionalData(ctx, "GET", endpoint, headers, nil, 15*time.Second)
	if err != nil {
		return Preview{}, err
	}
	if len(data) == 0 {
		// 无活动时上游省略 data：这是正常的"没有可领取活动"。
		return Preview{Blocked: "none"}, nil
	}
	return ParsePreview(data)
}
func (c *Client) Claim(ctx context.Context, jwt, planID string, captcha *CaptchaToken) (ClaimResult, error) {
	h := claimHeaders(c.Identity, jwt)
	h.Set("Content-Type", "application/json")
	if captcha != nil {
		h.Set(CaptchaHeader, captcha.Param)
		if captcha.Region != "" {
			h.Set(CaptchaRegionHeader, captcha.Region)
		}
	}
	data, err := c.request(ctx, "POST", c.OriginURL()+"/api/v1/zcode-plan/billing/claim", h, map[string]string{"plan_id": planID}, 15*time.Second)
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			return ClaimResult{}, err
		}
		if e.Code != 0 {
			result := ClaimResult{Code: e.Code, Result: claimReason(e.Code), RetryAt: e.RetryAt}
			return result, nil
		}
		return ClaimResult{}, err
	}
	var raw struct {
		Plan *Plan `json:"plan"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Plan == nil || raw.Plan.ID == "" {
		return ClaimResult{}, &Error{Kind: ErrFormat}
	}
	return ClaimResult{OK: true, Result: "claimed", Plan: raw.Plan}, nil
}
func claimReason(code int) string {
	switch code {
	case 1001:
		return "none"
	case 1002:
		return "not_in_window"
	case 1003:
		return "already_claimed"
	case 1004:
		return "not_eligible"
	case 1005:
		return "quota_exhausted"
	case 3001:
		return "version_too_low"
	case 3007:
		return "captcha"
	case 401, 3012:
		return "login_required"
	default:
		return "unavailable"
	}
}
