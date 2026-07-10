package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error)
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	Update(ctx context.Context, sub *UserSubscription) error
	Delete(ctx context.Context, id int64) error

	ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]UserSubscription, error)
	ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error)

	ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExistsByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (bool, error)
	RenewTerm(ctx context.Context, input *RenewSubscriptionTermInput) error
	ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) error
	UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error
	UpdateRollingQuotaSnapshot(ctx context.Context, subscriptionID int64, fiveHourLimitUSD, sevenDayLimitUSD, thirtyDayLimitUSD *float64) error

	ActivateWindows(ctx context.Context, id int64, start time.Time) error
	ActivateRollingWindows(ctx context.Context, id int64, start time.Time) error
	ResetDailyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetWeeklyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetMonthlyUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetFiveHourUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetSevenDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	ResetThirtyDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error
	IncrementUsage(ctx context.Context, id int64, costUSD float64) error

	BatchUpdateExpiredStatus(ctx context.Context) (int64, error)
}

type RenewSubscriptionTermInput struct {
	SubscriptionID          int64
	ValidityDays            int
	Now                     time.Time
	MaxExpiresAt            time.Time
	LegacyWindowStart       time.Time
	FiveHourLimitUSD        *float64
	SevenDayLimitUSD        *float64
	ThirtyDayLimitUSD       *float64
	HasRollingQuotaSnapshot bool
	Notes                   string
}
