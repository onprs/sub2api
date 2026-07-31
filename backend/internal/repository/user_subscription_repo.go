package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/ent/user"
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

func (r *userSubscriptionRepository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Only(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	return userSubscriptionEntityToServicePreserveStatus(m), nil
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

func (r *userSubscriptionRepository) Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(restoredStatus).
		ClearDeletedAt().
		SetUpdatedAt(time.Now()).
		Save(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionRestoreConflict)
	}
	return r.GetByID(ctx, subscriptionID)
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
	queryCtx, q := r.buildListQuery(ctx, userID, groupID, status, platform)

	total, err := q.Clone().Count(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	includeSoftDeleted := status == "" || status == service.SubscriptionStatusRevoked
	if !includeSoftDeleted {
		q = q.WithUser().WithGroup().WithPlan().WithAssignedByUser()
	}

	q = orderUserSubscriptionQuery(q, sortBy, sortOrder)

	subs, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	result := userSubscriptionEntitiesToService(subs)
	if includeSoftDeleted {
		if err := r.attachUserSubscriptionRelations(ctx, result); err != nil {
			return nil, nil, err
		}
	}

	return result, paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) ListIDs(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]int64, *pagination.PaginationResult, error) {
	queryCtx, q := r.buildListQuery(ctx, userID, groupID, status, platform)
	total, err := q.Clone().Count(queryCtx)
	if err != nil {
		return nil, nil, err
	}
	ids, err := orderUserSubscriptionQuery(q, sortBy, sortOrder).
		Offset(params.Offset()).
		Limit(params.Limit()).
		IDs(queryCtx)
	if err != nil {
		return nil, nil, err
	}
	return ids, paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) buildListQuery(ctx context.Context, userID, groupID *int64, status, platform string) (context.Context, *dbent.UserSubscriptionQuery) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query()
	includeSoftDeleted := status == "" || status == service.SubscriptionStatusRevoked
	if userID != nil {
		q = q.Where(usersubscription.UserIDEQ(*userID))
	}
	if groupID != nil {
		q = q.Where(usersubscription.GroupIDEQ(*groupID))
	}
	if platform != "" {
		groupPredicates := []predicate.Group{group.PlatformEQ(platform)}
		if includeSoftDeleted {
			groupPredicates = append(groupPredicates, group.DeletedAtIsNil())
		}
		q = q.Where(usersubscription.HasGroupWith(groupPredicates...))
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
	case service.SubscriptionStatusRevoked:
		// Revoked is a DTO/API display state backed by user_subscriptions.deleted_at.
		q = q.Where(usersubscription.DeletedAtNotNil())
	case "":
		// No filter. Use SkipSoftDelete below so admin "all status" includes revoked history.
	default:
		// Other persisted status.
		q = q.Where(usersubscription.StatusEQ(status))
	}

	queryCtx := ctx
	if includeSoftDeleted {
		queryCtx = mixins.SkipSoftDelete(ctx)
	}
	return queryCtx, q
}

func orderUserSubscriptionQuery(q *dbent.UserSubscriptionQuery, sortBy, sortOrder string) *dbent.UserSubscriptionQuery {
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
		return q.Order(dbent.Asc(field))
	}
	return q.Order(dbent.Desc(field))
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

func (r *userSubscriptionRepository) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.ExistsByUserIDAndGroupID(ctx, userID, groupID)
}

func (r *userSubscriptionRepository) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetExpiresAt(newExpiresAt).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) RenewTerm(ctx context.Context, input *service.RenewSubscriptionTermInput) (*service.UserSubscription, error) {
	if input == nil {
		return nil, service.ErrSubscriptionNilInput
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.renewTermLocked(ctx, input)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	txCtx := dbent.NewTxContext(ctx, tx)
	renewed, err := r.renewTermLocked(txCtx, input)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return renewed, nil
}

func (r *userSubscriptionRepository) renewTermLocked(ctx context.Context, input *service.RenewSubscriptionTermInput) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	existingEntity, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(input.SubscriptionID)).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	existing := userSubscriptionEntityToServicePreserveStatus(existingEntity)

	activeTerm := existing.ExpiresAt.After(input.Now)
	startsAt := existing.StartsAt
	expiresAt := existing.ExpiresAt
	if activeTerm {
		expiresAt = expiresAt.AddDate(0, 0, input.ValidityDays)
	} else {
		startsAt = input.Now
		expiresAt = input.Now.AddDate(0, 0, input.ValidityDays)
	}
	if expiresAt.After(input.MaxExpiresAt) {
		expiresAt = input.MaxExpiresAt
	}

	builder := client.UserSubscription.UpdateOneID(input.SubscriptionID).
		SetStartsAt(startsAt).
		SetExpiresAt(expiresAt).
		SetStatus(service.SubscriptionStatusActive).
		SetNotes(appendSubscriptionNotes(existing.Notes, input.Notes))
	if !activeTerm {
		builder.
			SetDailyUsageUsd(0).
			SetWeeklyUsageUsd(0).
			SetMonthlyUsageUsd(0).
			SetFiveHourUsageUsd(0).
			SetSevenDayUsageUsd(0).
			SetThirtyDayUsageUsd(0).
			SetDailyWindowStart(input.LegacyWindowStart).
			SetWeeklyWindowStart(input.LegacyWindowStart).
			SetMonthlyWindowStart(input.LegacyWindowStart).
			ClearFiveHourWindowStart().
			ClearSevenDayWindowStart().
			ClearThirtyDayWindowStart()
	}
	if input.HasRollingQuotaSnapshot {
		setUserSubscriptionLimitSnapshotFields(builder, input.FiveHourLimitUSD, input.SevenDayLimitUSD, input.ThirtyDayLimitUSD)
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
	}
	return userSubscriptionEntityToService(updated), nil
}

func appendSubscriptionNotes(existingNotes, newNotes string) string {
	if newNotes == "" {
		return existingNotes
	}
	if existingNotes == "" {
		return newNotes
	}
	return existingNotes + "\n" + newNotes
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

func (r *userSubscriptionRepository) ActivateWindows(ctx context.Context, id int64, start time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(id).
		SetDailyWindowStart(start).
		SetWeeklyWindowStart(start).
		SetMonthlyWindowStart(start).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, newWindowStart time.Time) error {
	if !resetDaily && !resetWeekly && !resetMonthly {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	update := client.UserSubscription.UpdateOneID(id)
	if resetDaily {
		update.SetDailyUsageUsd(0).SetDailyWindowStart(newWindowStart)
	}
	if resetWeekly {
		update.SetWeeklyUsageUsd(0).SetWeeklyWindowStart(newWindowStart)
	}
	if resetMonthly {
		update.SetMonthlyUsageUsd(0).SetMonthlyWindowStart(newWindowStart)
	}
	_, err := update.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetFiveHourUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	const updateSQL = `
		UPDATE user_subscriptions
		SET
			seven_day_usage_usd = GREATEST(seven_day_usage_usd - five_hour_usage_usd, 0),
			thirty_day_usage_usd = GREATEST(thirty_day_usage_usd - five_hour_usage_usd, 0),
			five_hour_usage_usd = 0,
			five_hour_window_start = $2,
			updated_at = $2
		WHERE id = $1
			AND deleted_at IS NULL
	`
	return r.execResetUsageSQL(ctx, updateSQL, id, newWindowStart)
}

func (r *userSubscriptionRepository) ResetSevenDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	const updateSQL = `
		UPDATE user_subscriptions
		SET
			thirty_day_usage_usd = GREATEST(thirty_day_usage_usd - seven_day_usage_usd, 0),
			five_hour_usage_usd = 0,
			seven_day_usage_usd = 0,
			five_hour_window_start = $2,
			seven_day_window_start = $2,
			updated_at = $2
		WHERE id = $1
			AND deleted_at IS NULL
	`
	return r.execResetUsageSQL(ctx, updateSQL, id, newWindowStart)
}

func (r *userSubscriptionRepository) ResetThirtyDayUsage(ctx context.Context, id int64, newWindowStart time.Time) error {
	const updateSQL = `
		UPDATE user_subscriptions
		SET
			five_hour_usage_usd = 0,
			seven_day_usage_usd = 0,
			thirty_day_usage_usd = 0,
			five_hour_window_start = $2,
			seven_day_window_start = $2,
			thirty_day_window_start = $2,
			updated_at = $2
		WHERE id = $1
			AND deleted_at IS NULL
	`
	return r.execResetUsageSQL(ctx, updateSQL, id, newWindowStart)
}

func (r *userSubscriptionRepository) execResetUsageSQL(ctx context.Context, query string, id int64, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, query, id, newWindowStart)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrSubscriptionNotFound
	}
	return nil
}

func (r *userSubscriptionRepository) ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.DailyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.DailyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetDailyUsageUsd(0).
		SetDailyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.WeeklyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.WeeklyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetWeeklyUsageUsd(0).
		SetWeeklyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.MonthlyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.MonthlyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetMonthlyUsageUsd(0).
		SetMonthlyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) translateConditionalWindowReset(ctx context.Context, client *dbent.Client, id int64, affected int, err error) error {
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if affected > 0 {
		return nil
	}

	// A stale reset is an expected no-op: another request already advanced the
	// window. Preserve not-found semantics for callers that target a missing row.
	exists, err := client.UserSubscription.Query().Where(usersubscription.IDEQ(id)).Exist(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if !exists {
		return service.ErrSubscriptionNotFound
	}
	return nil
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

func (r *userSubscriptionRepository) attachUserSubscriptionRelations(ctx context.Context, subs []service.UserSubscription) error {
	if len(subs) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(subs))
	groupIDs := make([]int64, 0, len(subs))
	assignedByIDs := make([]int64, 0, len(subs))
	planIDs := make([]int64, 0, len(subs))
	for i := range subs {
		userIDs = append(userIDs, subs[i].UserID)
		groupIDs = append(groupIDs, subs[i].GroupID)
		if subs[i].AssignedBy != nil {
			assignedByIDs = append(assignedByIDs, *subs[i].AssignedBy)
		}
		if subs[i].PlanID != nil {
			planIDs = append(planIDs, *subs[i].PlanID)
		}
	}

	client := clientFromContext(ctx, r.client)
	users, err := client.User.Query().Where(user.IDIn(uniqueInt64s(userIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	userByID := make(map[int64]*service.User, len(users))
	for _, u := range users {
		userByID[u.ID] = userEntityToService(u)
	}

	groups, err := client.Group.Query().Where(group.IDIn(uniqueInt64s(groupIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	groupByID := make(map[int64]*service.Group, len(groups))
	for _, g := range groups {
		groupByID[g.ID] = groupEntityToService(g)
	}

	assignedByID := map[int64]*service.User{}
	if len(assignedByIDs) > 0 {
		assignedUsers, err := client.User.Query().Where(user.IDIn(uniqueInt64s(assignedByIDs)...)).All(ctx)
		if err != nil {
			return err
		}
		assignedByID = make(map[int64]*service.User, len(assignedUsers))
		for _, u := range assignedUsers {
			assignedByID[u.ID] = userEntityToService(u)
		}
	}

	planNameByID := map[int64]string{}
	if len(planIDs) > 0 {
		plans, err := client.SubscriptionPlan.Query().Where(subscriptionplan.IDIn(uniqueInt64s(planIDs)...)).All(ctx)
		if err != nil {
			return err
		}
		planNameByID = make(map[int64]string, len(plans))
		for _, p := range plans {
			planNameByID[p.ID] = p.Name
		}
	}

	for i := range subs {
		subs[i].User = userByID[subs[i].UserID]
		subs[i].Group = groupByID[subs[i].GroupID]
		if subs[i].AssignedBy != nil {
			subs[i].AssignedByUser = assignedByID[*subs[i].AssignedBy]
		}
		if subs[i].PlanID != nil {
			subs[i].PlanName = planNameByID[*subs[i].PlanID]
		}
	}
	return nil
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func userSubscriptionEntityToService(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, true)
}

func userSubscriptionEntityToServicePreserveStatus(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, false)
}

func userSubscriptionEntityToServiceWithStatusMapping(m *dbent.UserSubscription, mapDeletedToRevoked bool) *service.UserSubscription {
	if m == nil {
		return nil
	}
	status := m.Status
	if mapDeletedToRevoked && m.DeletedAt != nil {
		status = service.SubscriptionStatusRevoked
	}
	out := &service.UserSubscription{
		ID:                   m.ID,
		UserID:               m.UserID,
		GroupID:              m.GroupID,
		PlanID:               m.PlanID,
		StartsAt:             m.StartsAt,
		ExpiresAt:            m.ExpiresAt,
		Status:               status,
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
		DeletedAt:            m.DeletedAt,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	if m.Edges.Plan != nil {
		out.PlanID = &m.Edges.Plan.ID
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
