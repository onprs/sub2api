package service

// MonthlyQuotaCost 描述订阅制平台模型当前有效的月可用额度及额度成本乘数。
type MonthlyQuotaCost struct {
	IncludedMonthlyUsageUSD float64
	Multiplier              float64
}
