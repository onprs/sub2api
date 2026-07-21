package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type ticketTransitionUpdate struct {
	PublicActivity bool
	NotifyUser     bool
	SetAssignee    bool
	AssigneeID     *int64
	SetPriority    bool
	Priority       domain.TicketPriority
}

func (r *ticketRepository) AppendUserReply(ctx context.Context, params service.UserTicketReplyParams) (*service.UserTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, &params.UserID)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	if err := enforceTicketReplyRateLimit(ctx, tx, params.UserID, ticketMutationTime(params.Now)); err != nil {
		return nil, err
	}
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionUserReply, Now: params.Now})
	if err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	authorName, err := loadTicketActorName(ctx, tx, params.UserID)
	if err != nil {
		return nil, err
	}
	message, err := insertTicketMessage(ctx, tx, ticket.ID, &params.UserID, domain.TicketActorUser, domain.TicketVisibilityPublic, authorName, params.Body, now)
	if err != nil {
		return nil, err
	}
	if transition.From != transition.To {
		if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.UserID, domain.TicketActorUser, domain.TicketEventStatusChanged, &transition.From, &transition.To, map[string]any{}, domain.TicketVisibilityPublic, now); err != nil {
			return nil, err
		}
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true}); err != nil {
		return nil, err
	}
	if err := claimTicketAttachments(ctx, tx, ticket.ID, message.ID, params.UserID, domain.TicketActorUser, params.AttachmentTokens, now, r.maxTicketAttachmentBytes()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetUserTicket(ctx, params.UserID, params.TicketNo)
}

func (r *ticketRepository) AppendAdminReply(ctx context.Context, params service.AdminTicketReplyParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: params.NextAction, Now: now, ReopenWindow: r.reopenWindow()})
	if err != nil {
		return nil, err
	}
	authorName, err := loadTicketActorName(ctx, tx, params.AdminID)
	if err != nil {
		return nil, err
	}
	message, err := insertTicketMessage(ctx, tx, ticket.ID, &params.AdminID, domain.TicketActorAdmin, domain.TicketVisibilityPublic, authorName, params.Body, now)
	if err != nil {
		return nil, err
	}
	if transition.From != transition.To {
		eventType := domain.TicketEventStatusChanged
		if transition.To == domain.TicketStatusResolved {
			eventType = domain.TicketEventResolved
		}
		if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.AdminID, domain.TicketActorAdmin, eventType, &transition.From, &transition.To, map[string]any{}, domain.TicketVisibilityPublic, now); err != nil {
			return nil, err
		}
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true, NotifyUser: true}); err != nil {
		return nil, err
	}
	if err := claimTicketAttachments(ctx, tx, ticket.ID, message.ID, params.AdminID, domain.TicketActorAdmin, params.AttachmentTokens, now, r.maxTicketAttachmentBytes()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) AppendInternalNote(ctx context.Context, params service.InternalTicketNoteParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionInternalNote, Now: now})
	if err != nil {
		return nil, err
	}
	authorName, err := loadTicketActorName(ctx, tx, params.AdminID)
	if err != nil {
		return nil, err
	}
	message, err := insertTicketMessage(ctx, tx, ticket.ID, &params.AdminID, domain.TicketActorAdmin, domain.TicketVisibilityInternal, authorName, params.Body, now)
	if err != nil {
		return nil, err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{}); err != nil {
		return nil, err
	}
	if err := claimTicketAttachments(ctx, tx, ticket.ID, message.ID, params.AdminID, domain.TicketActorAdmin, params.AttachmentTokens, now, r.maxTicketAttachmentBytes()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) ClaimTicket(ctx context.Context, params service.TicketVersionActionParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	if ticket.AssigneeID != nil && *ticket.AssigneeID != params.ActorID {
		return nil, domain.ErrTicketVersionConflict
	}
	if err := requireAdminAssignee(ctx, tx, params.ActorID); err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionClaim, Now: now})
	if err != nil {
		return nil, err
	}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.ActorID, domain.TicketActorAdmin, domain.TicketEventClaimed, nil, nil, map[string]any{"assignee_id": params.ActorID}, domain.TicketVisibilityInternal, now); err != nil {
		return nil, err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{SetAssignee: true, AssigneeID: &params.ActorID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) AssignTicket(ctx context.Context, params service.AssignTicketParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	if params.AssigneeID != nil {
		if err := requireAdminAssignee(ctx, tx, *params.AssigneeID); err != nil {
			return nil, err
		}
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionAssign, Now: now})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"from_assignee_id": ticket.AssigneeID, "to_assignee_id": params.AssigneeID}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.AdminID, domain.TicketActorAdmin, domain.TicketEventAssigned, nil, nil, payload, domain.TicketVisibilityInternal, now); err != nil {
		return nil, err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{SetAssignee: true, AssigneeID: params.AssigneeID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) ChangePriority(ctx context.Context, params service.ChangeTicketPriorityParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionChangePriority, Now: now})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"from_priority": ticket.Priority, "to_priority": params.Priority}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.AdminID, domain.TicketActorAdmin, domain.TicketEventPriorityChanged, nil, nil, payload, domain.TicketVisibilityInternal, now); err != nil {
		return nil, err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{SetPriority: true, Priority: params.Priority}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) ResolveTicket(ctx context.Context, params service.TicketVersionActionParams) (*service.AdminTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, nil)
	if err != nil {
		return nil, err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return nil, err
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionAdminResolve, Now: now, ReopenWindow: r.reopenWindow()})
	if err != nil {
		return nil, err
	}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.ActorID, domain.TicketActorAdmin, domain.TicketEventResolved, &transition.From, &transition.To, map[string]any{}, domain.TicketVisibilityPublic, now); err != nil {
		return nil, err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true, NotifyUser: true}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetAdminTicket(ctx, params.TicketNo)
}

func (r *ticketRepository) CloseTicket(ctx context.Context, params service.CloseTicketParams) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID *int64
	if params.ActorRole == domain.TicketActorUser {
		ownerID = &params.ActorID
	}
	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, ownerID)
	if err != nil {
		return err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return err
	}
	action := service.TicketActionAdminClose
	notifyUser := true
	if params.ActorRole == domain.TicketActorUser {
		action = service.TicketActionUserClose
		notifyUser = false
	}
	now := ticketMutationTime(params.Now)
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: action, Now: now})
	if err != nil {
		return err
	}
	payload := map[string]any{}
	if params.Reason != "" {
		payload["reason"] = params.Reason
	} else {
		payload["reason_code"] = "closed_by_user"
	}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.ActorID, params.ActorRole, domain.TicketEventClosed, &transition.From, &transition.To, payload, domain.TicketVisibilityPublic, now); err != nil {
		return err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true, NotifyUser: notifyUser}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ticketRepository) ReopenTicket(ctx context.Context, params service.ReopenTicketParams) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID *int64
	if params.ActorRole == domain.TicketActorUser {
		ownerID = &params.ActorID
	}
	ticket, err := lockTicketForUpdate(ctx, tx, params.TicketNo, ownerID)
	if err != nil {
		return err
	}
	if err := requireTicketVersion(ticket, params.ExpectedVersion); err != nil {
		return err
	}
	action := service.TicketActionAdminReopen
	notifyUser := true
	if params.ActorRole == domain.TicketActorUser {
		action = service.TicketActionUserReopen
		notifyUser = false
	}
	now := ticketMutationTime(params.Now)
	if params.ActorRole == domain.TicketActorUser {
		if err := enforceTicketReplyRateLimit(ctx, tx, params.ActorID, now); err != nil {
			return err
		}
	}
	transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: action, Now: now, ReopenDeadline: ticket.ReopenDeadline})
	if err != nil {
		return err
	}
	if params.ActorRole == domain.TicketActorUser {
		authorName, err := loadTicketActorName(ctx, tx, params.ActorID)
		if err != nil {
			return err
		}
		if _, err := insertTicketMessage(ctx, tx, ticket.ID, &params.ActorID, domain.TicketActorUser, domain.TicketVisibilityPublic, authorName, params.Body, now); err != nil {
			return err
		}
	}
	if _, err := insertTicketEvent(ctx, tx, ticket.ID, &params.ActorID, params.ActorRole, domain.TicketEventReopened, &transition.From, &transition.To, map[string]any{}, domain.TicketVisibilityPublic, now); err != nil {
		return err
	}
	if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true, NotifyUser: notifyUser}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ticketRepository) MarkUserRead(ctx context.Context, params service.MarkTicketReadParams) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE tickets
		SET user_last_read_seq = GREATEST(user_last_read_seq, LEAST(user_notification_seq, $3))
		WHERE ticket_no = $1 AND user_id = $2`, params.TicketNo, params.UserID, params.ObservedNotificationSeq)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrTicketNotFound
	}
	return nil
}

func lockTicketForUpdate(ctx context.Context, tx *sql.Tx, ticketNo string, ownerID *int64) (*service.Ticket, error) {
	query := `SELECT ` + ticketSelectColumns + ` FROM tickets WHERE ticket_no = $1`
	args := []any{ticketNo}
	if ownerID != nil {
		query += ` AND user_id = $2`
		args = append(args, *ownerID)
	}
	query += ` FOR UPDATE`
	ticket, err := scanTicket(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketNotFound
	}
	return ticket, err
}

func requireTicketVersion(ticket *service.Ticket, expected int64) error {
	if ticket == nil {
		return domain.ErrTicketNotFound
	}
	if ticket.Version != expected {
		return domain.ErrTicketVersionConflict.WithMetadata(map[string]string{
			"current_version": fmt.Sprintf("%d", ticket.Version),
		})
	}
	return nil
}

func loadTicketActorName(ctx context.Context, tx *sql.Tx, actorID int64) (string, error) {
	var name string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(username, ''), email)
		FROM users
		WHERE id = $1 AND deleted_at IS NULL`, actorID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrTicketNotFound
	}
	return name, err
}

func requireAdminAssignee(ctx context.Context, tx *sql.Tx, assigneeID int64) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM users
			WHERE id = $1 AND role = 'admin' AND status = 'active' AND deleted_at IS NULL
		)`, assigneeID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return service.ErrTicketAssigneeInvalid
	}
	return nil
}

func enforceTicketReplyRateLimit(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) error {
	var lockedUserID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrTicketNotFound
		}
		return err
	}
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM ticket_messages m
		WHERE m.author_id = $1
		  AND m.author_role = 'user'
		  AND m.created_at >= $2
		  AND EXISTS (
			SELECT 1 FROM ticket_messages earlier
			WHERE earlier.ticket_id = m.ticket_id AND earlier.id < m.id
		  )`, userID, now.Add(-time.Hour)).Scan(&count)
	if err != nil {
		return err
	}
	if count >= domain.TicketReplyHourlyLimit {
		return domain.ErrTicketRateLimited
	}
	return nil
}

func updateTicketForTransition(
	ctx context.Context,
	tx *sql.Tx,
	current *service.Ticket,
	transition service.TicketTransitionResult,
	now time.Time,
	update ticketTransitionUpdate,
) (*service.Ticket, error) {
	var reopenDeadline any
	if transition.ReopenDeadline != nil {
		reopenDeadline = *transition.ReopenDeadline
	}
	row := tx.QueryRowContext(ctx, `
		UPDATE tickets
		SET status = $3,
			version = version + 1,
			updated_at = $4,
			last_activity_at = $4,
			last_public_message_at = CASE WHEN $5 THEN $4 ELSE last_public_message_at END,
			user_notification_seq = user_notification_seq + CASE WHEN $6 THEN 1 ELSE 0 END,
			action_required_since = CASE $7
				WHEN 'set' THEN $4
				WHEN 'clear' THEN NULL
				ELSE action_required_since
			END,
			resolved_at = CASE $8
				WHEN 'set' THEN $4
				WHEN 'clear' THEN NULL
				ELSE resolved_at
			END,
			reopen_deadline = CASE $9
				WHEN 'set' THEN $10::timestamptz
				WHEN 'clear' THEN NULL
				ELSE reopen_deadline
			END,
			closed_at = CASE $11
				WHEN 'set' THEN $4
				WHEN 'clear' THEN NULL
				ELSE closed_at
			END,
			assignee_id = CASE WHEN $12 THEN $13::bigint ELSE assignee_id END,
			priority = CASE WHEN $14 THEN $15::varchar ELSE priority END
		WHERE id = $1 AND version = $2
		RETURNING `+ticketSelectColumns,
		current.ID,
		current.Version,
		string(transition.To),
		now,
		update.PublicActivity,
		update.NotifyUser,
		string(transition.ActionRequiredSinceUpdate),
		string(transition.ResolvedAtUpdate),
		string(transition.ReopenDeadlineUpdate),
		reopenDeadline,
		string(transition.ClosedAtUpdate),
		update.SetAssignee,
		update.AssigneeID,
		update.SetPriority,
		string(update.Priority),
	)
	updated, err := scanTicket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketVersionConflict
	}
	return updated, err
}
