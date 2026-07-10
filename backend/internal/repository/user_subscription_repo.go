package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userSubscriptionRepository struct {
	client *dbent.Client
}

func NewUserSubscriptionRepository(client *dbent.Client) service.UserSubscriptionRepository {
	return &userSubscriptionRepository{client: client}
}

func (r *userSubscriptionRepository) Create(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.Create().
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetNillablePlanID(sub.PlanID).
		SetExpiresAt(sub.ExpiresAt).
		SetNillableFiveHourLimitUsd(sub.FiveHourLimitUSD).
		SetNillableSevenDayLimitUsd(sub.SevenDayLimitUSD).
		SetNillableThirtyDayLimitUsd(sub.ThirtyDayLimitUSD).
		SetNillableDailyWindowStart(sub.DailyWindowStart).
		SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).
		SetNillableFiveHourWindowStart(sub.FiveHourWindowStart).
		SetNillableSevenDayWindowStart(sub.SevenDayWindowStart).
		SetNillableThirtyDayWindowStart(sub.ThirtyDayWindowStart).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetFiveHourUsageUsd(sub.FiveHourUsageUSD).
		SetSevenDayUsageUsd(sub.SevenDayUsageUSD).
		SetThirtyDayUsageUsd(sub.ThirtyDayUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy)

	if sub.StartsAt.IsZero() {
		builder.SetStartsAt(time.Now())
	} else {
		builder.SetStartsAt(sub.StartsAt)
	}
	if sub.Status != "" {
		builder.SetStatus(sub.Status)
	}
	if !sub.AssignedAt.IsZero() {
		builder.SetAssignedAt(sub.AssignedAt)
	}
	// Keep compatibility with historical behavior: always store notes as a string value.
	builder.SetNotes(sub.Notes)

	created, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, created)
	}
	return translatePersistenceError(err, nil, service.ErrSubscriptionAlreadyExists)
}

func (r *userSubscriptionRepository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithPlan().
		WithAssignedByUser().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		WithGroup().
		WithPlan().
		Order(usersubscription.ByExpiresAt(), usersubscription.ByID()).
		First(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func userSubscriptionPlanPredicate(planID *int64) predicate.UserSubscription {
	if planID == nil {
		return usersubscription.PlanIDIsNil()
	}
	return usersubscription.PlanIDEQ(*planID)
}

func (r *userSubscriptionRepository) GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			userSubscriptionPlanPredicate(planID),
		).
		WithGroup().
		WithPlan().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		WithPlan().
		Order(usersubscription.ByExpiresAt(), usersubscription.ByID()).
		First(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToService(m), nil
}

func (r *userSubscriptionRepository) Update(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(sub.ID).
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetStartsAt(sub.StartsAt).
		SetExpiresAt(sub.ExpiresAt).
		SetStatus(sub.Status).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetFiveHourUsageUsd(sub.FiveHourUsageUSD).
		SetSevenDayUsageUsd(sub.SevenDayUsageUSD).
		SetThirtyDayUsageUsd(sub.ThirtyDayUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy).
		SetAssignedAt(sub.AssignedAt).
		SetNotes(sub.Notes)
	if sub.PlanID == nil {
		builder.ClearPlanID()
	} else {
		builder.SetPlanID(*sub.PlanID)
	}
	setUserSubscriptionLimitSnapshotFields(builder, sub.FiveHourLimitUSD, sub.SevenDayLimitUSD, sub.ThirtyDayLimitUSD)
	setUserSubscriptionWindowFields(builder, sub.DailyWindowStart, sub.WeeklyWindowStart, sub.MonthlyWindowStart, sub.FiveHourWindowStart, sub.SevenDayWindowStart, sub.ThirtyDayWindowStart)

	updated, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, updated)
		return nil
	}
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func setUserSubscriptionLimitSnapshotFields(
	builder *dbent.UserSubscriptionUpdateOne,
	fiveHourLimitUSD, sevenDayLimitUSD, thirtyDayLimitUSD *float64,
) {
	if fiveHourLimitUSD == nil {
		builder.ClearFiveHourLimitUsd()
	} else {
		builder.SetFiveHourLimitUsd(*fiveHourLimitUSD)
	}
	if sevenDayLimitUSD == nil {
		builder.ClearSevenDayLimitUsd()
	} else {
		builder.SetSevenDayLimitUsd(*sevenDayLimitUSD)
	}
	if thirtyDayLimitUSD == nil {
		builder.ClearThirtyDayLimitUsd()
	} else {
		builder.SetThirtyDayLimitUsd(*thirtyDayLimitUSD)
	}
}

func setUserSubscriptionWindowFields(
	builder *dbent.UserSubscriptionUpdateOne,
	dailyWindowStart, weeklyWindowStart, monthlyWindowStart *time.Time,
	fiveHourWindowStart, sevenDayWindowStart, thirtyDayWindowStart *time.Time,
) {
	if dailyWindowStart == nil {
		builder.ClearDailyWindowStart()
	} else {
		builder.SetDailyWindowStart(*dailyWindowStart)
	}
	if weeklyWindowStart == nil {
		builder.ClearWeeklyWindowStart()
	} else {
		builder.SetWeeklyWindowStart(*weeklyWindowStart)
	}
	if monthlyWindowStart == nil {
		builder.ClearMonthlyWindowStart()
	} else {
		builder.SetMonthlyWindowStart(*monthlyWindowStart)
	}
	if fiveHourWindowStart == nil {
		builder.ClearFiveHourWindowStart()
	} else {
		builder.SetFiveHourWindowStart(*fiveHourWindowStart)
	}
	if sevenDayWindowStart == nil {
		builder.ClearSevenDayWindowStart()
	} else {
		builder.SetSevenDayWindowStart(*sevenDayWindowStart)
	}
	if thirtyDayWindowStart == nil {
		builder.ClearThirtyDayWindowStart()
	} else {
		builder.SetThirtyDayWindowStart(*thirtyDayWindowStart)
	}
}

func (r *userSubscriptionRepository) Delete(ctx context.Context, id int64) error {
	// Match GORM semantics: deleting a missing row is not an error.
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.Delete().Where(usersubscription.IDEQ(id)).Exec(ctx)
	return err
}

func (r *userSubscriptionRepository) ListByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID)).
		WithGroup().
		WithPlan().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListActiveByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		WithPlan().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		WithPlan().
		Order(usersubscription.ByExpiresAt(), usersubscription.ByID()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	subs, err := q.
		WithUser().
		WithGroup().
		WithPlan().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return userSubscriptionEntitiesToService(subs), paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query()
	if userID != nil {
		q = q.Where(usersubscription.UserIDEQ(*userID))
	}
	if groupID != nil {
		q = q.Where(usersubscription.GroupIDEQ(*groupID))
	}
	if platform != "" {
		q = q.Where(usersubscription.HasGroupWith(group.PlatformEQ(platform)))
	}

	// Status filtering with real-time expiration check
	now := time.Now()
	switch status {
	case service.SubscriptionStatusActive:
		// Active: status is active AND not yet expired
		q = q.Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(now),
		)
	case service.SubscriptionStatusExpired:
		// Expired: status is expired OR (status is active but already expired)
		q = q.Where(
			usersubscription.Or(
				usersubscription.StatusEQ(service.SubscriptionStatusExpired),
				usersubscription.And(
					usersubscription.StatusEQ(service.SubscriptionStatusActive),
					usersubscription.ExpiresAtLTE(now),
				),
			),
		)
	case "":
		// No filter
	default:
		// Other status (e.g., revoked)
		q = q.Where(usersubscription.StatusEQ(status))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Apply sorting
	q = q.WithUser().WithGroup().WithPlan().WithAssignedByUser()

	// Determine sort field
	var field string
	switch sortBy {
	case "expires_at":
		field = usersubscription.FieldExpiresAt
	case "status":
		field = usersubscription.FieldStatus
	default:
		field = usersubscription.FieldCreatedAt
	}

	// Determine sort order (default: desc)
	if sortOrder == "asc" && sortBy != "" {
		q = q.Order(dbent.Asc(field))
	} else {
		q = q.Order(dbent.Desc(field))
	}

	subs, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return userSubscriptionEntitiesToService(subs), paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		Exist(ctx)
}

func (r *userSubscriptionRepository) ExistsByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			userSubscriptionPlanPredicate(planID),
		).
		Exist(ctx)
}

func (r *userSubscriptionRepository) RenewTerm(ctx context.Context, input *service.RenewSubscriptionTermInput) error {
	if input == nil {
		return service.ErrSubscriptionNilInput
	}
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			starts_at = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.starts_at
				ELSE $2::timestamptz
			END,
			expires_at = LEAST(
				$4::timestamptz,
				GREATEST(us.expires_at, $2::timestamptz) + ($3::int * INTERVAL '1 day')
			),
			status = $5,
			five_hour_limit_usd = CASE
				WHEN $11::boolean THEN $7
				ELSE us.five_hour_limit_usd
			END,
			seven_day_limit_usd = CASE
				WHEN $11::boolean THEN $8
				ELSE us.seven_day_limit_usd
			END,
			thirty_day_limit_usd = CASE
				WHEN $11::boolean THEN $9
				ELSE us.thirty_day_limit_usd
			END,
			daily_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.daily_usage_usd
				ELSE 0
			END,
			weekly_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.weekly_usage_usd
				ELSE 0
			END,
			monthly_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.monthly_usage_usd
				ELSE 0
			END,
			five_hour_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.five_hour_usage_usd
				ELSE 0
			END,
			seven_day_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.seven_day_usage_usd
				ELSE 0
			END,
			thirty_day_usage_usd = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.thirty_day_usage_usd
				ELSE 0
			END,
			daily_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.daily_window_start
				ELSE $6::timestamptz
			END,
			weekly_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.weekly_window_start
				ELSE $6::timestamptz
			END,
			monthly_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.monthly_window_start
				ELSE $6::timestamptz
			END,
			five_hour_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.five_hour_window_start
				ELSE NULL
			END,
			seven_day_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.seven_day_window_start
				ELSE NULL
			END,
			thirty_day_window_start = CASE
				WHEN us.expires_at > $2::timestamptz THEN us.thirty_day_window_start
				ELSE NULL
			END,
			notes = CASE
				WHEN $10::text = '' THEN us.notes
				WHEN COALESCE(us.notes, '') = '' THEN $10::text
				ELSE us.notes || E'\n' || $10::text
			END,
			updated_at = NOW()
		WHERE us.id = $1
			AND us.deleted_at IS NULL
	`

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL,
		input.SubscriptionID,
		input.Now,
		input.ValidityDays,
		input.MaxExpiresAt,
		service.SubscriptionStatusActive,
		input.LegacyWindowStart,
		nullableFloatArg(input.FiveHourLimitUSD),
		nullableFloatArg(input.SevenDayLimitUSD),
		nullableFloatArg(input.ThirtyDayLimitUSD),
		input.Notes,
		input.HasRollingQuotaSnapshot,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func (r *userSubscriptionRepository) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetExpiresAt(newExpiresAt).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateStatus(ctx context.Context, subscriptionID int64, status string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(status).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetNotes(notes).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateRollingQuotaSnapshot(ctx context.Context, subscriptionID int64, fiveHourLimitUSD, sevenDayLimitUSD, thirtyDayLimitUSD *float64) error {
	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(subscriptionID)
	setUserSubscriptionLimitSnapshotFields(builder, fiveHourLimitUSD, sevenDayLimitUSD, thirtyDayLimitUSD)
	_, err := builder.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func nullableFloatArg(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r *userSubscriptionRepository) ActivateWindows(ctx context.Context, id int64, start time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetDailyWindowStart(start).
		SetWeeklyWindowStart(start).
		SetMonthlyWindowStart(start).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ActivateRollingWindows(ctx context.Context, id int64, start time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetFiveHourWindowStart(start).
		SetSevenDayWindowStart(start).
		SetThirtyDayWindowStart(start).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetDailyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetDailyUsageUsd(0).
		SetDailyWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetWeeklyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetWeeklyUsageUsd(0).
		SetWeeklyWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetMonthlyUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetMonthlyUsageUsd(0).
		SetMonthlyWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetFiveHourUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetFiveHourUsageUsd(0).
		SetFiveHourWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetSevenDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetSevenDayUsageUsd(0).
		SetSevenDayWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetThirtyDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetThirtyDayUsageUsd(0).
		SetThirtyDayWindowStart(newWindowStart).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

// IncrementUsage 原子性地累加订阅用量。
// 限额检查已在请求前由 BillingCacheService.CheckBillingEligibility 完成，
// 此处仅负责记录实际消费，确保消费数据的完整性。
func (r *userSubscriptionRepository) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			five_hour_usage_usd = CASE
				WHEN us.five_hour_window_start IS NULL
					OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
				THEN $1
				ELSE us.five_hour_usage_usd + $1
			END,
			seven_day_usage_usd = CASE
				WHEN us.seven_day_window_start IS NULL
					OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
				THEN $1
				ELSE us.seven_day_usage_usd + $1
			END,
			thirty_day_usage_usd = CASE
				WHEN us.thirty_day_window_start IS NULL
					OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
				THEN $1
				ELSE us.thirty_day_usage_usd + $1
			END,
			five_hour_window_start = CASE
				WHEN us.five_hour_window_start IS NULL
					OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
				THEN NOW()
				ELSE us.five_hour_window_start
			END,
			seven_day_window_start = CASE
				WHEN us.seven_day_window_start IS NULL
					OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
				THEN NOW()
				ELSE us.seven_day_window_start
			END,
			thirty_day_window_start = CASE
				WHEN us.thirty_day_window_start IS NULL
					OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
				THEN NOW()
				ELSE us.thirty_day_window_start
			END,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, costUSD, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected > 0 {
		return nil
	}

	// affected == 0：订阅不存在或已删除
	return service.ErrSubscriptionNotFound
}

func (r *userSubscriptionRepository) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Update().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtLTE(time.Now()),
		).
		SetStatus(service.SubscriptionStatusExpired).
		Save(ctx)
	return int64(n), err
}

// Extra repository helpers (currently used only by integration tests).

func (r *userSubscriptionRepository) ListExpired(ctx context.Context) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtLTE(time.Now()),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(subs), nil
}

func (r *userSubscriptionRepository) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID)).Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) CountActiveByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().
		Where(
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) DeleteByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Delete().Where(usersubscription.GroupIDEQ(groupID)).Exec(ctx)
	return int64(n), err
}

func userSubscriptionEntityToService(m *dbent.UserSubscription) *service.UserSubscription {
	if m == nil {
		return nil
	}
	out := &service.UserSubscription{
		ID:                   m.ID,
		UserID:               m.UserID,
		GroupID:              m.GroupID,
		PlanID:               m.PlanID,
		StartsAt:             m.StartsAt,
		ExpiresAt:            m.ExpiresAt,
		Status:               m.Status,
		FiveHourLimitUSD:     m.FiveHourLimitUsd,
		SevenDayLimitUSD:     m.SevenDayLimitUsd,
		ThirtyDayLimitUSD:    m.ThirtyDayLimitUsd,
		DailyWindowStart:     m.DailyWindowStart,
		WeeklyWindowStart:    m.WeeklyWindowStart,
		MonthlyWindowStart:   m.MonthlyWindowStart,
		FiveHourWindowStart:  m.FiveHourWindowStart,
		SevenDayWindowStart:  m.SevenDayWindowStart,
		ThirtyDayWindowStart: m.ThirtyDayWindowStart,
		DailyUsageUSD:        m.DailyUsageUsd,
		WeeklyUsageUSD:       m.WeeklyUsageUsd,
		MonthlyUsageUSD:      m.MonthlyUsageUsd,
		FiveHourUsageUSD:     m.FiveHourUsageUsd,
		SevenDayUsageUSD:     m.SevenDayUsageUsd,
		ThirtyDayUsageUSD:    m.ThirtyDayUsageUsd,
		AssignedBy:           m.AssignedBy,
		AssignedAt:           m.AssignedAt,
		Notes:                derefString(m.Notes),
		CreatedAt:            m.CreatedAt,
		UpdatedAt:            m.UpdatedAt,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	if m.Edges.Plan != nil {
		out.PlanName = m.Edges.Plan.Name
	}
	if m.Edges.AssignedByUser != nil {
		out.AssignedByUser = userEntityToService(m.Edges.AssignedByUser)
	}
	return out
}

func userSubscriptionEntitiesToService(models []*dbent.UserSubscription) []service.UserSubscription {
	out := make([]service.UserSubscription, 0, len(models))
	for i := range models {
		if s := userSubscriptionEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func applyUserSubscriptionEntityToService(dst *service.UserSubscription, src *dbent.UserSubscription) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.PlanID = src.PlanID
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
