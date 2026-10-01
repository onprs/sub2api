package service

import (
	"fmt"
	"math"
	"time"
)

// MonthlyQuotaPeriod 用绝对时间覆盖基础月额度，与 token 价格优惠及账号账期独立。
// StartsAt 包含起点，ExpiresAt 不包含终点；nil 表示该方向没有时间限制。
type MonthlyQuotaPeriod struct {
	CreditsUSD float64    `json:"credits_usd"`
	StartsAt   *time.Time `json:"starts_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Term       string     `json:"term,omitempty"`
}

// MonthlyQuotaPolicy 是平台无关的月额度规则，支持多段活动和预定额度变更。
// 时间段必须按起点排序且互不重叠；不命中时间段时恢复已确认的基础额度。
type MonthlyQuotaPolicy struct {
	BaseCreditsUSD float64              `json:"base_credits_usd"`
	Periods        []MonthlyQuotaPeriod `json:"periods,omitempty"`
}

// ModelMonthlyQuota 是指定时刻的额度快照，计费和公开模型接口共用同一结果。
// NextChangeAt 让调用方知道缓存何时失效，时刻均采用 UTC。
type ModelMonthlyQuota struct {
	BaseCreditsUSD float64    `json:"base_credits_usd"`
	CreditsUSD     float64    `json:"credits_usd"`
	CostMultiplier float64    `json:"cost_multiplier"`
	StartsAt       *time.Time `json:"starts_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	NextChangeAt   *time.Time `json:"next_change_at,omitempty"`
	Term           string     `json:"term,omitempty"`
}

func (p MonthlyQuotaPolicy) Validate() error {
	if !monthlyQuotaAmountValid(p.BaseCreditsUSD) {
		return fmt.Errorf("invalid base monthly credits")
	}
	for index, period := range p.Periods {
		if !monthlyQuotaAmountValid(period.CreditsUSD) || period.CreditsUSD == 0 {
			return fmt.Errorf("invalid monthly credits in period %d", index)
		}
		if (period.StartsAt == nil && period.ExpiresAt == nil) ||
			(period.StartsAt != nil && period.StartsAt.IsZero()) ||
			(period.ExpiresAt != nil && period.ExpiresAt.IsZero()) ||
			(period.StartsAt != nil && period.ExpiresAt != nil && !period.StartsAt.Before(*period.ExpiresAt)) {
			return fmt.Errorf("invalid monthly credit period %d", index)
		}
		if index > 0 {
			previous := p.Periods[index-1]
			if previous.ExpiresAt == nil || period.StartsAt == nil || period.StartsAt.Before(*previous.ExpiresAt) {
				return fmt.Errorf("overlapping or unordered monthly credit periods")
			}
		}
	}
	return nil
}

// ResolveAt 只解析本地规则，不发起网络请求。未确认的基础额度（0）不生成倍率。
func (p MonthlyQuotaPolicy) ResolveAt(at time.Time, sharedCreditsUSD float64) (ModelMonthlyQuota, bool) {
	if at.IsZero() || p.Validate() != nil || !monthlyQuotaAmountValid(sharedCreditsUSD) || sharedCreditsUSD == 0 {
		return ModelMonthlyQuota{}, false
	}
	quota := ModelMonthlyQuota{BaseCreditsUSD: p.BaseCreditsUSD, CreditsUSD: p.BaseCreditsUSD}
	for _, period := range p.Periods {
		if period.StartsAt != nil && at.Before(*period.StartsAt) {
			quota.NextChangeAt = monthlyQuotaTimeCopy(period.StartsAt)
			break
		}
		if period.ExpiresAt != nil && !at.Before(*period.ExpiresAt) {
			continue
		}
		quota.CreditsUSD = period.CreditsUSD
		quota.StartsAt = monthlyQuotaTimeCopy(period.StartsAt)
		quota.ExpiresAt = monthlyQuotaTimeCopy(period.ExpiresAt)
		quota.NextChangeAt = monthlyQuotaTimeCopy(period.ExpiresAt)
		quota.Term = period.Term
		break
	}
	if quota.CreditsUSD <= 0 {
		return ModelMonthlyQuota{}, false
	}
	quota.CostMultiplier = sharedCreditsUSD / quota.CreditsUSD
	if !monthlyQuotaAmountValid(quota.CostMultiplier) || quota.CostMultiplier == 0 {
		return ModelMonthlyQuota{}, false
	}
	return quota, true
}

func (p MonthlyQuotaPolicy) Clone() MonthlyQuotaPolicy {
	cloned := p
	if p.Periods != nil {
		cloned.Periods = make([]MonthlyQuotaPeriod, len(p.Periods))
		for index, period := range p.Periods {
			cloned.Periods[index] = period
			cloned.Periods[index].StartsAt = monthlyQuotaTimeCopy(period.StartsAt)
			cloned.Periods[index].ExpiresAt = monthlyQuotaTimeCopy(period.ExpiresAt)
		}
	}
	return cloned
}

func monthlyQuotaTimeCopy(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func monthlyQuotaAmountValid(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
