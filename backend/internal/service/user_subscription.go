package service

import "time"

const subscriptionDayDuration = 24 * time.Hour

type UserSubscription struct {
	ID      int64
	UserID  int64
	GroupID int64
	PlanID  *int64

	PlanName string

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

	FiveHourLimitUSD  *float64
	SevenDayLimitUSD  *float64
	ThirtyDayLimitUSD *float64

	DailyWindowStart     *time.Time
	WeeklyWindowStart    *time.Time
	MonthlyWindowStart   *time.Time
	FiveHourWindowStart  *time.Time
	SevenDayWindowStart  *time.Time
	ThirtyDayWindowStart *time.Time

	DailyUsageUSD     float64
	WeeklyUsageUSD    float64
	MonthlyUsageUSD   float64
	FiveHourUsageUSD  float64
	SevenDayUsageUSD  float64
	ThirtyDayUsageUSD float64

	AssignedBy *int64
	AssignedAt time.Time
	Notes      string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	User           *User
	Group          *Group
	AssignedByUser *User
}

const (
	SubscriptionWindowFiveHour  = 5 * time.Hour
	SubscriptionWindowSevenDay  = 7 * 24 * time.Hour
	SubscriptionWindowThirtyDay = 30 * 24 * time.Hour

	subscriptionQuotaPreflightReserveUSD = 0.005
)

func (s *UserSubscription) IsActive() bool {
	return s.Status == SubscriptionStatusActive && time.Now().Before(s.ExpiresAt)
}

func (s *UserSubscription) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *UserSubscription) DaysRemaining() int {
	return s.daysRemainingAt(time.Now())
}

func (s *UserSubscription) daysRemainingAt(now time.Time) int {
	remaining := s.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}

	days := int(remaining / subscriptionDayDuration)
	if remaining%subscriptionDayDuration != 0 {
		days++
	}
	return days
}

func (s *UserSubscription) IsWindowActivated() bool {
	return s.DailyWindowStart != nil || s.WeeklyWindowStart != nil || s.MonthlyWindowStart != nil
}

func (s *UserSubscription) IsRollingWindowActivated() bool {
	return s.FiveHourWindowStart != nil || s.SevenDayWindowStart != nil || s.ThirtyDayWindowStart != nil
}

func (s *UserSubscription) HasOneTimeDailyQuota() bool {
	if s == nil || s.StartsAt.IsZero() || s.ExpiresAt.IsZero() {
		return false
	}
	return !s.ExpiresAt.After(s.StartsAt.AddDate(0, 0, 1))
}

func (s *UserSubscription) NeedsDailyReset() bool {
	return s.NeedsDailyResetAt(time.Now())
}

func (s *UserSubscription) NeedsDailyResetAt(now time.Time) bool {
	if s.DailyWindowStart == nil {
		return false
	}
	if s.HasOneTimeDailyQuota() {
		return false
	}
	return !now.Before(s.DailyWindowStart.Add(24 * time.Hour))
}

func (s *UserSubscription) NeedsWeeklyReset() bool {
	return s.NeedsWeeklyResetAt(time.Now())
}

func (s *UserSubscription) NeedsWeeklyResetAt(now time.Time) bool {
	if s.WeeklyWindowStart == nil {
		return false
	}
	return !now.Before(s.WeeklyWindowStart.Add(7 * 24 * time.Hour))
}

func (s *UserSubscription) NeedsMonthlyReset() bool {
	return s.NeedsMonthlyResetAt(time.Now())
}

func (s *UserSubscription) NeedsMonthlyResetAt(now time.Time) bool {
	if s.MonthlyWindowStart == nil {
		return false
	}
	return !now.Before(s.MonthlyWindowStart.Add(30 * 24 * time.Hour))
}

func (s *UserSubscription) canAutomaticallyResetDailyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.DailyWindowStart, 24*time.Hour, now)
	return !s.HasOneTimeDailyQuota() && ok
}

func (s *UserSubscription) canAutomaticallyResetWeeklyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.WeeklyWindowStart, 7*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) canAutomaticallyResetMonthlyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.MonthlyWindowStart, 30*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) automaticWindowStartAt(previous *time.Time, period time.Duration, now time.Time) (time.Time, bool) {
	if previous == nil {
		return time.Time{}, false
	}

	anchor := *previous
	// Older subscriptions initialized their first windows at midnight on their
	// start date. Only that initial value is unambiguous; later midnight anchors
	// may be manual resets and must remain authoritative.
	legacyAnchor := startOfDay(s.StartsAt)
	if legacyAnchor.Before(s.StartsAt) && anchor.Equal(legacyAnchor) {
		anchor = s.StartsAt
	}
	next := anchor.Add(period)
	if now.Before(next) || !next.Before(s.ExpiresAt) {
		return time.Time{}, false
	}

	periods := now.Sub(anchor) / period
	lastPeriodBeforeExpiry := (s.ExpiresAt.Sub(anchor) - 1) / period
	if periods > lastPeriodBeforeExpiry {
		periods = lastPeriodBeforeExpiry
	}
	return anchor.Add(periods * period), true
}

func (s *UserSubscription) NeedsFiveHourResetAt(now time.Time) bool {
	return s.FiveHourWindowStart != nil && !now.Before(s.FiveHourWindowStart.Add(SubscriptionWindowFiveHour))
}

func (s *UserSubscription) NeedsSevenDayResetAt(now time.Time) bool {
	return s.SevenDayWindowStart != nil && !now.Before(s.SevenDayWindowStart.Add(SubscriptionWindowSevenDay))
}

func (s *UserSubscription) NeedsThirtyDayResetAt(now time.Time) bool {
	return s.ThirtyDayWindowStart != nil && !now.Before(s.ThirtyDayWindowStart.Add(SubscriptionWindowThirtyDay))
}

func (s *UserSubscription) NeedsFiveHourReset() bool {
	return s.NeedsFiveHourResetAt(time.Now())
}

func (s *UserSubscription) NeedsSevenDayReset() bool {
	return s.NeedsSevenDayResetAt(time.Now())
}

func (s *UserSubscription) NeedsThirtyDayReset() bool {
	return s.NeedsThirtyDayResetAt(time.Now())
}

// NormalizeSubscriptionWindowsForDisplay clears expired rolling quota windows on a copy or DTO path.
// It does not persist anything; write-side maintenance still owns DB resets.
func NormalizeSubscriptionWindowsForDisplay(sub *UserSubscription) {
	if sub == nil {
		return
	}
	now := time.Now()
	if sub.NeedsFiveHourResetAt(now) {
		sub.FiveHourWindowStart = nil
		sub.FiveHourUsageUSD = 0
	}
	if sub.NeedsSevenDayResetAt(now) {
		sub.SevenDayWindowStart = nil
		sub.SevenDayUsageUSD = 0
	}
	if sub.NeedsThirtyDayResetAt(now) {
		sub.ThirtyDayWindowStart = nil
		sub.ThirtyDayUsageUSD = 0
	}
}

func (s *UserSubscription) DailyResetTime() *time.Time {
	if s.DailyWindowStart == nil {
		return nil
	}
	if s.HasOneTimeDailyQuota() {
		t := s.ExpiresAt
		return &t
	}
	t := s.DailyWindowStart.Add(24 * time.Hour)
	return &t
}

func (s *UserSubscription) WeeklyResetTime() *time.Time {
	if s.WeeklyWindowStart == nil {
		return nil
	}
	t := s.WeeklyWindowStart.Add(7 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) MonthlyResetTime() *time.Time {
	if s.MonthlyWindowStart == nil {
		return nil
	}
	t := s.MonthlyWindowStart.Add(30 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) FiveHourResetTime() *time.Time {
	if s.FiveHourWindowStart == nil {
		return nil
	}
	t := s.FiveHourWindowStart.Add(SubscriptionWindowFiveHour)
	return &t
}

func (s *UserSubscription) SevenDayResetTime() *time.Time {
	if s.SevenDayWindowStart == nil {
		return nil
	}
	t := s.SevenDayWindowStart.Add(SubscriptionWindowSevenDay)
	return &t
}

func (s *UserSubscription) ThirtyDayResetTime() *time.Time {
	if s.ThirtyDayWindowStart == nil {
		return nil
	}
	t := s.ThirtyDayWindowStart.Add(SubscriptionWindowThirtyDay)
	return &t
}

func (s *UserSubscription) CheckDailyLimit(group *Group, additionalCost float64) bool {
	if !group.HasDailyLimit() {
		return true
	}
	return s.DailyUsageUSD+additionalCost <= *group.DailyLimitUSD
}

func (s *UserSubscription) CheckWeeklyLimit(group *Group, additionalCost float64) bool {
	if !group.HasWeeklyLimit() {
		return true
	}
	return s.WeeklyUsageUSD+additionalCost <= *group.WeeklyLimitUSD
}

func (s *UserSubscription) CheckMonthlyLimit(group *Group, additionalCost float64) bool {
	if !group.HasMonthlyLimit() {
		return true
	}
	return s.MonthlyUsageUSD+additionalCost <= *group.MonthlyLimitUSD
}

func (s *UserSubscription) CheckFiveHourLimit(additionalCost float64) bool {
	return checkRollingSubscriptionLimit(s.FiveHourLimitUSD, s.FiveHourUsageUSD, additionalCost)
}

func (s *UserSubscription) CheckSevenDayLimit(additionalCost float64) bool {
	return checkRollingSubscriptionLimit(s.SevenDayLimitUSD, s.SevenDayUsageUSD, additionalCost)
}

func (s *UserSubscription) CheckThirtyDayLimit(additionalCost float64) bool {
	return checkRollingSubscriptionLimit(s.ThirtyDayLimitUSD, s.ThirtyDayUsageUSD, additionalCost)
}

func checkRollingSubscriptionLimit(limit *float64, usage, additionalCost float64) bool {
	if limit == nil {
		return true
	}
	if *limit <= 0 {
		return false
	}
	if additionalCost <= 0 {
		additionalCost = subscriptionQuotaPreflightReserveUSD
	}
	return usage+additionalCost <= *limit
}

func (s *UserSubscription) CheckAllLimits(group *Group, additionalCost float64) (daily, weekly, monthly bool) {
	daily = s.CheckDailyLimit(group, additionalCost)
	weekly = s.CheckWeeklyLimit(group, additionalCost)
	monthly = s.CheckMonthlyLimit(group, additionalCost)
	return
}
