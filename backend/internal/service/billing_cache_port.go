package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status    string
	ExpiresAt time.Time
	Version   int64

	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64

	FiveHourLimitUSD  *float64
	SevenDayLimitUSD  *float64
	ThirtyDayLimitUSD *float64

	FiveHourUsage  float64
	SevenDayUsage  float64
	ThirtyDayUsage float64

	FiveHourWindowStart  *time.Time
	SevenDayWindowStart  *time.Time
	ThirtyDayWindowStart *time.Time
}
