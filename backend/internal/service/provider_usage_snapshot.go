package service

import "time"

// buildProviderUsageSnapshot 将官方配额探测保存的窗口转换为管理只读接口的快照。
func buildProviderUsageSnapshot(account *Account, now time.Time) *UsageInfo {
	if account == nil || len(account.Extra) == 0 {
		return nil
	}
	usage := &UsageInfo{
		Source:    "stored",
		UpdatedAt: observerStoredUsageUpdatedAt(account.Extra, cnExtraKey(account.Platform, cnExtraSuffixUsageUpdated)),
	}
	window := func(usedSuffix, resetSuffix string) *UsageProgress {
		used, ok := account.Extra[cnExtraKey(account.Platform, usedSuffix)]
		if !ok || used == nil {
			return nil
		}
		progress := &UsageProgress{Utilization: parseExtraFloat64(used)}
		reset := observerStoredUsageUpdatedAt(account.Extra, cnExtraKey(account.Platform, resetSuffix))
		if reset != nil {
			if reset.After(now) {
				progress.ResetsAt = reset
				progress.RemainingSeconds = int(reset.Sub(now).Seconds())
			} else {
				progress.Utilization = 0
			}
		}
		return progress
	}
	usage.FiveHour = window(cnExtraSuffix5hUsed, cnExtraSuffix5hReset)
	usage.SevenDay = window(cnExtraSuffixWeeklyUsed, cnExtraSuffixWeeklyReset)
	usage.ThirtyDay = window(cnExtraSuffixMonthlyUsed, cnExtraSuffixMonthlyReset)
	if observerUsageEmpty(usage) {
		return nil
	}
	return usage
}
