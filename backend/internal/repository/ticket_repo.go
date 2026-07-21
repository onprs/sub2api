package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const (
	ticketAttachmentMaxTicketBytes = int64(30 * 1024 * 1024)
	ticketNumberAllocationAttempts = 8
)

type ticketRepository struct {
	db  *sql.DB
	cfg *config.Config
}

func NewTicketRepository(db *sql.DB, cfg *config.Config) service.TicketRepository {
	return &ticketRepository{db: db, cfg: cfg}
}

type ticketRowScanner interface {
	Scan(dest ...any) error
}

const ticketSelectColumns = `
	id, ticket_no, user_id, requester_email, requester_username,
	subject, category, impact, priority, status, assignee_id,
	request_id, usage_log_id, api_key_id, api_key_name,
	payment_order_id, payment_order_no, user_subscription_id, subscription_name,
	last_public_message_at, last_activity_at, action_required_since,
	user_notification_seq, user_last_read_seq, resolved_at, reopen_deadline,
	closed_at, version, created_at, updated_at`

func scanTicket(row ticketRowScanner) (*service.Ticket, error) {
	var (
		ticket              service.Ticket
		userID              sql.NullInt64
		assigneeID          sql.NullInt64
		usageLogID          sql.NullInt64
		apiKeyID            sql.NullInt64
		paymentOrderID      sql.NullInt64
		subscriptionID      sql.NullInt64
		actionRequiredSince sql.NullTime
		resolvedAt          sql.NullTime
		reopenDeadline      sql.NullTime
		closedAt            sql.NullTime
		category            string
		impact              string
		priority            string
		status              string
	)
	err := row.Scan(
		&ticket.ID,
		&ticket.TicketNo,
		&userID,
		&ticket.RequesterEmail,
		&ticket.RequesterUsername,
		&ticket.Subject,
		&category,
		&impact,
		&priority,
		&status,
		&assigneeID,
		&ticket.RequestID,
		&usageLogID,
		&apiKeyID,
		&ticket.APIKeyName,
		&paymentOrderID,
		&ticket.PaymentOrderNo,
		&subscriptionID,
		&ticket.SubscriptionName,
		&ticket.LastPublicMessageAt,
		&ticket.LastActivityAt,
		&actionRequiredSince,
		&ticket.UserNotificationSeq,
		&ticket.UserLastReadSeq,
		&resolvedAt,
		&reopenDeadline,
		&closedAt,
		&ticket.Version,
		&ticket.CreatedAt,
		&ticket.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	ticket.UserID = nullInt64Pointer(userID)
	ticket.AssigneeID = nullInt64Pointer(assigneeID)
	ticket.UsageLogID = nullInt64Pointer(usageLogID)
	ticket.APIKeyID = nullInt64Pointer(apiKeyID)
	ticket.PaymentOrderID = nullInt64Pointer(paymentOrderID)
	ticket.UserSubscriptionID = nullInt64Pointer(subscriptionID)
	ticket.ActionRequiredSince = nullTimePointer(actionRequiredSince)
	ticket.ResolvedAt = nullTimePointer(resolvedAt)
	ticket.ReopenDeadline = nullTimePointer(reopenDeadline)
	ticket.ClosedAt = nullTimePointer(closedAt)
	ticket.Category = domain.TicketCategory(category)
	ticket.Impact = domain.TicketImpact(impact)
	ticket.Priority = domain.TicketPriority(priority)
	ticket.Status = domain.TicketStatus(status)
	ticket.ActionRequired = domain.TicketActionRequiredFor(ticket.Status)
	return &ticket, nil
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}

func (r *ticketRepository) CreateTicketWithInitialMessage(ctx context.Context, params service.CreateTicketParams) (*service.UserTicketDetail, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := ticketMutationTime(params.Now)
	requesterEmail, requesterName, err := lockTicketRequester(ctx, tx, params.UserID)
	if err != nil {
		return nil, err
	}
	if err := enforceTicketCreateLimits(ctx, tx, params.UserID, now); err != nil {
		return nil, err
	}
	snapshots, err := loadTicketReferenceSnapshots(ctx, tx, params.UserID, params.References)
	if err != nil {
		return nil, err
	}
	transition, err := service.TransitionTicket(service.TicketTransitionInput{
		Action: service.TicketActionCreate,
		Now:    now,
	})
	if err != nil {
		return nil, err
	}

	ticket, err := insertTicketWithAllocatedNumber(ctx, tx, params, snapshots, requesterEmail, requesterName, transition.To, now)
	if err != nil {
		return nil, err
	}
	message, err := insertTicketMessage(ctx, tx, ticket.ID, &params.UserID, domain.TicketActorUser, domain.TicketVisibilityPublic, requesterName, params.Body, now)
	if err != nil {
		return nil, err
	}
	event, err := insertTicketEvent(ctx, tx, ticket.ID, &params.UserID, domain.TicketActorUser, domain.TicketEventCreated, nil, ticketStatusPointer(transition.To), map[string]any{}, domain.TicketVisibilityPublic, now)
	if err != nil {
		return nil, err
	}
	if err := claimTicketAttachments(ctx, tx, ticket.ID, message.ID, params.UserID, domain.TicketActorUser, params.AttachmentTokens, now, r.maxTicketAttachmentBytes()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.UserTicketDetail{Ticket: *ticket, Messages: []service.TicketMessage{*message}, Events: []service.TicketEvent{*event}}, nil
}

type ticketReferenceSnapshots struct {
	RequestID        string
	UsageLogID       *int64
	APIKeyID         *int64
	APIKeyName       string
	PaymentOrderID   *int64
	PaymentOrderNo   string
	SubscriptionID   *int64
	SubscriptionName string
}

func lockTicketRequester(ctx context.Context, tx *sql.Tx, userID int64) (string, string, error) {
	var email, username string
	err := tx.QueryRowContext(ctx, `
		SELECT email, username
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, userID).Scan(&email, &username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", service.ErrTicketNotFound
	}
	if err != nil {
		return "", "", err
	}
	if username == "" {
		username = email
	}
	return email, username, nil
}

func enforceTicketCreateLimits(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) error {
	var active, hourly, daily int
	err := tx.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status NOT IN ('resolved', 'closed')),
			COUNT(*) FILTER (WHERE created_at >= $2),
			COUNT(*) FILTER (WHERE created_at >= $3)
		FROM tickets
		WHERE user_id = $1`, userID, now.Add(-time.Hour), now.Add(-24*time.Hour)).Scan(&active, &hourly, &daily)
	if err != nil {
		return err
	}
	if active >= domain.TicketMaxOpenPerUser {
		return domain.ErrTicketTooManyOpen
	}
	if hourly >= domain.TicketCreateHourlyLimit || daily >= domain.TicketCreateDailyLimit {
		return domain.ErrTicketRateLimited
	}
	return nil
}

func loadTicketReferenceSnapshots(ctx context.Context, tx *sql.Tx, userID int64, refs service.TicketReferenceInput) (ticketReferenceSnapshots, error) {
	result := ticketReferenceSnapshots{}
	if refs.UsageLogID != nil {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id, request_id FROM usage_logs WHERE id = $1 AND user_id = $2`, *refs.UsageLogID, userID).Scan(&id, &result.RequestID); err != nil {
			return ticketReferenceSnapshots{}, ticketReferenceError(err)
		}
		result.UsageLogID = &id
	}
	if refs.APIKeyID != nil {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id, name FROM api_keys WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`, *refs.APIKeyID, userID).Scan(&id, &result.APIKeyName); err != nil {
			return ticketReferenceSnapshots{}, ticketReferenceError(err)
		}
		result.APIKeyID = &id
	}
	if refs.PaymentOrderID != nil {
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id, out_trade_no FROM payment_orders WHERE id = $1 AND user_id = $2`, *refs.PaymentOrderID, userID).Scan(&id, &result.PaymentOrderNo); err != nil {
			return ticketReferenceSnapshots{}, ticketReferenceError(err)
		}
		result.PaymentOrderID = &id
	}
	if refs.UserSubscriptionID != nil {
		var id int64
		if err := tx.QueryRowContext(ctx, `
			SELECT us.id, COALESCE(sp.name, g.name, '')
			FROM user_subscriptions us
			LEFT JOIN subscription_plans sp ON sp.id = us.plan_id
			LEFT JOIN groups g ON g.id = us.group_id
			WHERE us.id = $1 AND us.user_id = $2 AND us.deleted_at IS NULL`, *refs.UserSubscriptionID, userID).Scan(&id, &result.SubscriptionName); err != nil {
			return ticketReferenceSnapshots{}, ticketReferenceError(err)
		}
		result.SubscriptionID = &id
	}
	return result, nil
}

func ticketReferenceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrTicketReferenceNotFound
	}
	return err
}

func insertTicketWithAllocatedNumber(
	ctx context.Context,
	tx *sql.Tx,
	params service.CreateTicketParams,
	snapshots ticketReferenceSnapshots,
	requesterEmail, requesterName string,
	status domain.TicketStatus,
	now time.Time,
) (*service.Ticket, error) {
	for attempt := 0; attempt < ticketNumberAllocationAttempts; attempt++ {
		ticketNo, err := service.NewTicketNumber(now)
		if err != nil {
			return nil, err
		}
		row := tx.QueryRowContext(ctx, `
			INSERT INTO tickets (
				ticket_no, user_id, requester_email, requester_username,
				subject, category, impact, priority, status, request_id,
				usage_log_id, api_key_id, api_key_name, payment_order_id, payment_order_no,
				user_subscription_id, subscription_name, last_public_message_at,
				last_activity_at, action_required_since, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, 'normal', $8, $9,
				$10, $11, $12, $13, $14, $15, $16, $17, $17, $17, $17, $17
			)
			ON CONFLICT (ticket_no) DO NOTHING
			RETURNING `+ticketSelectColumns,
			ticketNo,
			params.UserID,
			requesterEmail,
			requesterName,
			params.Subject,
			string(params.Category),
			string(params.Impact),
			string(status),
			snapshots.RequestID,
			snapshots.UsageLogID,
			snapshots.APIKeyID,
			snapshots.APIKeyName,
			snapshots.PaymentOrderID,
			snapshots.PaymentOrderNo,
			snapshots.SubscriptionID,
			snapshots.SubscriptionName,
			now,
		)
		ticket, err := scanTicket(row)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return ticket, err
	}
	return nil, fmt.Errorf("allocate ticket number after %d attempts", ticketNumberAllocationAttempts)
}

func insertTicketMessage(
	ctx context.Context,
	tx *sql.Tx,
	ticketID int64,
	authorID *int64,
	authorRole domain.TicketActorRole,
	visibility domain.TicketVisibility,
	authorName, body string,
	now time.Time,
) (*service.TicketMessage, error) {
	message := &service.TicketMessage{
		TicketID:   ticketID,
		AuthorID:   authorID,
		AuthorRole: authorRole,
		Visibility: visibility,
		AuthorName: authorName,
		Body:       body,
		CreatedAt:  now,
	}
	err := tx.QueryRowContext(ctx, `
		INSERT INTO ticket_messages (ticket_id, author_id, author_role, visibility, author_name, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`, ticketID, authorID, string(authorRole), string(visibility), authorName, body, now).Scan(&message.ID)
	if err != nil {
		return nil, err
	}
	return message, nil
}

func insertTicketEvent(
	ctx context.Context,
	tx *sql.Tx,
	ticketID int64,
	actorID *int64,
	actorRole domain.TicketActorRole,
	eventType domain.TicketEventType,
	fromStatus, toStatus *domain.TicketStatus,
	payload map[string]any,
	visibility domain.TicketVisibility,
	now time.Time,
) (*service.TicketEvent, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	event := &service.TicketEvent{
		TicketID:   ticketID,
		ActorID:    actorID,
		ActorRole:  actorRole,
		EventType:  eventType,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Payload:    payload,
		Visibility: visibility,
		CreatedAt:  now,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ticket_events (
			ticket_id, actor_id, actor_role, event_type, from_status, to_status, payload, visibility, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
		RETURNING id`, ticketID, actorID, string(actorRole), string(eventType), ticketStatusValue(fromStatus), ticketStatusValue(toStatus), payloadJSON, string(visibility), now).Scan(&event.ID)
	if err != nil {
		return nil, err
	}
	return event, nil
}

func ticketStatusPointer(status domain.TicketStatus) *domain.TicketStatus {
	value := status
	return &value
}

func ticketStatusValue(status *domain.TicketStatus) any {
	if status == nil {
		return nil
	}
	return string(*status)
}

func claimTicketAttachments(
	ctx context.Context,
	tx *sql.Tx,
	ticketID, messageID, actorID int64,
	actorRole domain.TicketActorRole,
	tokens []string,
	now time.Time,
	maxTicketBytes int64,
) error {
	if len(tokens) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, byte_size
		FROM ticket_attachments
		WHERE upload_token = ANY($1)
		  AND uploaded_by = $2
		  AND uploader_role = $3
		  AND state = 'pending'
		  AND expires_at > $4
		FOR UPDATE`, pq.Array(tokens), actorID, string(actorRole), now)
	if err != nil {
		return err
	}
	var ids []int64
	var pendingBytes int64
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
		pendingBytes += size
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) != len(tokens) {
		return domain.ErrTicketAttachmentNotFound
	}

	var attachedBytes int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(byte_size), 0)
		FROM ticket_attachments
		WHERE ticket_id = $1 AND state = 'attached'`, ticketID).Scan(&attachedBytes); err != nil {
		return err
	}
	if attachedBytes+pendingBytes > maxTicketBytes {
		return domain.ErrTicketAttachmentLimitExceeded
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE ticket_attachments
		SET ticket_id = $1, message_id = $2, state = 'attached', expires_at = NULL
		WHERE id = ANY($3) AND state = 'pending'`, ticketID, messageID, pq.Array(ids))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != int64(len(ids)) {
		return domain.ErrTicketAttachmentAlreadyClaimed
	}
	return nil
}

func (r *ticketRepository) maxTicketAttachmentBytes() int64 {
	if r.cfg != nil && r.cfg.Ticketing.Attachments.MaxTicketBytes > 0 {
		return r.cfg.Ticketing.Attachments.MaxTicketBytes
	}
	return ticketAttachmentMaxTicketBytes
}

func (r *ticketRepository) reopenWindow() time.Duration {
	if r.cfg != nil && r.cfg.Ticketing.ResolvedAutoCloseDays > 0 {
		return time.Duration(r.cfg.Ticketing.ResolvedAutoCloseDays) * 24 * time.Hour
	}
	return service.DefaultTicketReopenWindow
}

func ticketMutationTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}
