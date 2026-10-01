package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// MonthlyQuotaAt 是 Command Code 的统一额度入口；在线目录、离线目录和磁盘
// 快照均保留相同规则，活动到期时无需等待目录刷新即可恢复基础额度。
func (c *CommandCodeCatalog) MonthlyQuotaAt(model string, at time.Time) (ModelMonthlyQuota, bool) {
	entry, ok := c.entry(model)
	if !ok || !commandCodeCatalogEntryAvailableAt(entry, at) {
		return ModelMonthlyQuota{}, false
	}
	return entry.MonthlyQuota.ResolveAt(at, commandCodeSharedMonthlyQuotaUSD)
}

func parseCommandCodeMonthlyQuota(row commandCodeDocumentCreditRow) (MonthlyQuotaPolicy, error) {
	credits, err := parseCommandCodeCreditsUSD(row.Credits)
	if err != nil {
		return MonthlyQuotaPolicy{}, err
	}
	policy := MonthlyQuotaPolicy{BaseCreditsUSD: credits}
	raw := strings.TrimSpace(string(row.CreditDeal))
	switch raw {
	case "", "null", "false", `"$undefined"`:
		return policy, nil
	}
	if !strings.HasPrefix(raw, "{") {
		return MonthlyQuotaPolicy{}, fmt.Errorf("unsupported monthly credit deal")
	}
	var deal commandCodeDocumentCreditDeal
	if err := json.Unmarshal(row.CreditDeal, &deal); err != nil {
		return MonthlyQuotaPolicy{}, err
	}
	base, err := parseCommandCodeCreditsUSD(deal.Was)
	if err != nil {
		return MonthlyQuotaPolicy{}, fmt.Errorf("invalid base credits: %w", err)
	}
	startsAt, err := parseCommandCodeCreditBoundary(deal.Starts, false)
	if err != nil {
		return MonthlyQuotaPolicy{}, err
	}
	expiresAt, err := parseCommandCodeCreditBoundary(deal.Expires, true)
	if err != nil {
		return MonthlyQuotaPolicy{}, err
	}
	policy.BaseCreditsUSD = base
	policy.Periods = []MonthlyQuotaPeriod{{
		CreditsUSD: credits,
		StartsAt:   startsAt,
		ExpiresAt:  expiresAt,
		Term:       strings.TrimSpace(deal.Term),
	}}
	if err := policy.Validate(); err != nil {
		return MonthlyQuotaPolicy{}, err
	}
	return policy, nil
}

// 官方仅给日期的 expires 表示包含当天，转换为下一天 UTC 00:00 的排他终点。
// RFC3339 时刻直接作为边界；不从 term 等自然语言猜测日期。
func parseCommandCodeCreditBoundary(raw string, endOfDate bool) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, fmt.Errorf("invalid monthly credit time %q", value)
	}
	if endOfDate {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return &parsed, nil
}

func commandCodeMonthlyQuotaPoliciesEqual(left, right MonthlyQuotaPolicy) bool {
	return reflect.DeepEqual(left, right)
}
