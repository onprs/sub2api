package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *ticketRepository) GetUserTicket(ctx context.Context, userID int64, ticketNo string) (*service.UserTicketDetail, error) {
	ticket, err := scanTicket(r.db.QueryRowContext(ctx, `SELECT `+ticketSelectColumns+` FROM tickets WHERE ticket_no = $1 AND user_id = $2`, ticketNo, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	messages, err := queryTicketMessages(ctx, r.db, ticket.ID, true)
	if err != nil {
		return nil, err
	}
	events, err := queryTicketEvents(ctx, r.db, ticket.ID, true)
	if err != nil {
		return nil, err
	}
	return &service.UserTicketDetail{Ticket: *ticket, Messages: messages, Events: events}, nil
}

func (r *ticketRepository) GetAdminTicket(ctx context.Context, ticketNo string) (*service.AdminTicketDetail, error) {
	ticket, err := scanTicket(r.db.QueryRowContext(ctx, `SELECT `+ticketSelectColumns+` FROM tickets WHERE ticket_no = $1`, ticketNo))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	messages, err := queryTicketMessages(ctx, r.db, ticket.ID, false)
	if err != nil {
		return nil, err
	}
	events, err := queryTicketEvents(ctx, r.db, ticket.ID, false)
	if err != nil {
		return nil, err
	}
	return &service.AdminTicketDetail{Ticket: *ticket, Messages: messages, Events: events}, nil
}

type ticketQueryExecutor interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryTicketMessages(ctx context.Context, executor ticketQueryExecutor, ticketID int64, publicOnly bool) ([]service.TicketMessage, error) {
	query := `
		SELECT id, ticket_id, author_id, author_role, visibility, author_name, body, created_at
		FROM ticket_messages
		WHERE ticket_id = $1`
	if publicOnly {
		query += ` AND visibility = 'public'`
	}
	query += ` ORDER BY id ASC`
	rows, err := executor.QueryContext(ctx, query, ticketID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	messages := make([]service.TicketMessage, 0)
	for rows.Next() {
		var (
			message    service.TicketMessage
			authorID   sql.NullInt64
			authorRole string
			visibility string
		)
		if err := rows.Scan(&message.ID, &message.TicketID, &authorID, &authorRole, &visibility, &message.AuthorName, &message.Body, &message.CreatedAt); err != nil {
			return nil, err
		}
		message.AuthorID = nullInt64Pointer(authorID)
		message.AuthorRole = domain.TicketActorRole(authorRole)
		message.Visibility = domain.TicketVisibility(visibility)
		message.Attachments = make([]service.TicketMessageAttachment, 0)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	attachments, err := queryTicketMessageAttachments(ctx, executor, ticketID, publicOnly)
	if err != nil {
		return nil, err
	}
	byMessage := make(map[int64][]service.TicketMessageAttachment)
	for _, attachment := range attachments {
		byMessage[attachment.messageID] = append(byMessage[attachment.messageID], attachment.attachment)
	}
	for i := range messages {
		if items, ok := byMessage[messages[i].ID]; ok {
			messages[i].Attachments = items
		}
	}
	return messages, nil
}

type ticketMessageAttachmentRow struct {
	messageID  int64
	attachment service.TicketMessageAttachment
}

func queryTicketMessageAttachments(ctx context.Context, executor ticketQueryExecutor, ticketID int64, publicOnly bool) ([]ticketMessageAttachmentRow, error) {
	query := `
		SELECT a.message_id, a.id, a.original_name, a.content_type, a.byte_size, a.created_at
		FROM ticket_attachments a
		JOIN ticket_messages m ON m.id = a.message_id AND m.ticket_id = a.ticket_id
		WHERE a.ticket_id = $1 AND a.state = 'attached'`
	if publicOnly {
		query += ` AND m.visibility = 'public'`
	}
	query += ` ORDER BY a.message_id ASC, a.id ASC`
	rows, err := executor.QueryContext(ctx, query, ticketID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]ticketMessageAttachmentRow, 0)
	for rows.Next() {
		var item ticketMessageAttachmentRow
		if err := rows.Scan(
			&item.messageID, &item.attachment.ID, &item.attachment.OriginalName,
			&item.attachment.ContentType, &item.attachment.ByteSize, &item.attachment.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryTicketEvents(ctx context.Context, executor ticketQueryExecutor, ticketID int64, publicOnly bool) ([]service.TicketEvent, error) {
	query := `
		SELECT id, ticket_id, actor_id, actor_role, event_type, from_status, to_status, payload, visibility, created_at
		FROM ticket_events
		WHERE ticket_id = $1`
	if publicOnly {
		query += ` AND visibility = 'public'`
	}
	query += ` ORDER BY id ASC`
	rows, err := executor.QueryContext(ctx, query, ticketID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	events := make([]service.TicketEvent, 0)
	for rows.Next() {
		var (
			event      service.TicketEvent
			actorID    sql.NullInt64
			actorRole  string
			eventType  string
			fromStatus sql.NullString
			toStatus   sql.NullString
			payload    []byte
			visibility string
		)
		if err := rows.Scan(&event.ID, &event.TicketID, &actorID, &actorRole, &eventType, &fromStatus, &toStatus, &payload, &visibility, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.ActorID = nullInt64Pointer(actorID)
		event.ActorRole = domain.TicketActorRole(actorRole)
		event.EventType = domain.TicketEventType(eventType)
		event.FromStatus = nullableTicketStatus(fromStatus)
		event.ToStatus = nullableTicketStatus(toStatus)
		event.Visibility = domain.TicketVisibility(visibility)
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func nullableTicketStatus(value sql.NullString) *domain.TicketStatus {
	if !value.Valid {
		return nil
	}
	status := domain.TicketStatus(value.String)
	return &status
}

func (r *ticketRepository) ListUserTickets(ctx context.Context, userID int64, page pagination.PaginationParams, filters service.UserTicketListFilters) ([]service.Ticket, *pagination.PaginationResult, error) {
	builder := newTicketListBuilder("user_id =", userID)
	applyUserTicketFilters(builder, filters)
	return r.queryTicketList(ctx, page, builder, userTicketOrder(page))
}

func (r *ticketRepository) ListAdminTickets(ctx context.Context, page pagination.PaginationParams, filters service.AdminTicketListFilters) ([]service.Ticket, *pagination.PaginationResult, error) {
	builder := newTicketListBuilder("", nil)
	applyAdminTicketFilters(builder, filters)
	return r.queryTicketList(ctx, page, builder, adminTicketOrder(page))
}

type ticketListBuilder struct {
	conditions []string
	args       []any
}

func newTicketListBuilder(initial string, value any) *ticketListBuilder {
	builder := &ticketListBuilder{}
	if initial != "" {
		builder.add(initial, value)
	}
	return builder
}

func (b *ticketListBuilder) placeholder(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *ticketListBuilder) add(prefix string, value any) {
	b.conditions = append(b.conditions, prefix+" "+b.placeholder(value))
}

func (b *ticketListBuilder) whereSQL() string {
	if len(b.conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conditions, " AND ")
}

func applyUserTicketFilters(builder *ticketListBuilder, filters service.UserTicketListFilters) {
	switch filters.Bucket {
	case "active":
		builder.conditions = append(builder.conditions, "status IN ('open', 'in_progress')")
	case "waiting_user":
		builder.conditions = append(builder.conditions, "status = 'waiting_user'")
	case "ended":
		builder.conditions = append(builder.conditions, "status IN ('resolved', 'closed')")
	}
	if filters.Status != "" {
		builder.add("status =", string(filters.Status))
	}
	if filters.Category != "" {
		builder.add("category =", string(filters.Category))
	}
	if filters.Search != "" {
		pattern := builder.placeholder("%" + escapeTicketSearch(filters.Search) + "%")
		builder.conditions = append(builder.conditions, "(ticket_no ILIKE "+pattern+" ESCAPE '\\' OR subject ILIKE "+pattern+" ESCAPE '\\')")
	}
}

func applyAdminTicketFilters(builder *ticketListBuilder, filters service.AdminTicketListFilters) {
	switch filters.Bucket {
	case "action_required":
		builder.conditions = append(builder.conditions, "status IN ('open', 'in_progress')")
	case "in_progress":
		builder.conditions = append(builder.conditions, "status = 'in_progress'")
	case "waiting_user":
		builder.conditions = append(builder.conditions, "status = 'waiting_user'")
	case "ended":
		builder.conditions = append(builder.conditions, "status IN ('resolved', 'closed')")
	}
	if filters.Status != "" {
		builder.add("status =", string(filters.Status))
	}
	if filters.Category != "" {
		builder.add("category =", string(filters.Category))
	}
	if filters.Impact != "" {
		builder.add("impact =", string(filters.Impact))
	}
	if filters.Priority != "" {
		builder.add("priority =", string(filters.Priority))
	}
	if filters.AssigneeID != nil {
		builder.add("assignee_id =", *filters.AssigneeID)
	}
	if filters.Unassigned {
		builder.conditions = append(builder.conditions, "assignee_id IS NULL")
	}
	if filters.CreatedFrom != nil {
		builder.add("created_at >=", *filters.CreatedFrom)
	}
	if filters.CreatedTo != nil {
		builder.add("created_at <", *filters.CreatedTo)
	}
	if filters.Search != "" {
		pattern := builder.placeholder("%" + escapeTicketSearch(filters.Search) + "%")
		parts := []string{
			"ticket_no ILIKE " + pattern + " ESCAPE '\\'",
			"subject ILIKE " + pattern + " ESCAPE '\\'",
			"requester_email ILIKE " + pattern + " ESCAPE '\\'",
			"request_id ILIKE " + pattern + " ESCAPE '\\'",
			"payment_order_no ILIKE " + pattern + " ESCAPE '\\'",
		}
		if userID, err := strconv.ParseInt(filters.Search, 10, 64); err == nil && userID > 0 {
			parts = append(parts, "user_id = "+builder.placeholder(userID))
		}
		builder.conditions = append(builder.conditions, "("+strings.Join(parts, " OR ")+")")
	}
}

func (r *ticketRepository) queryTicketList(ctx context.Context, page pagination.PaginationParams, builder *ticketListBuilder, orderSQL string) ([]service.Ticket, *pagination.PaginationResult, error) {
	whereSQL := builder.whereSQL()
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tickets"+whereSQL, builder.args...).Scan(&total); err != nil {
		return nil, nil, err
	}

	args := append([]any(nil), builder.args...)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, page.Limit())
	offsetPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, page.Offset())
	rows, err := r.db.QueryContext(ctx, "SELECT "+ticketSelectColumns+" FROM tickets"+whereSQL+" "+orderSQL+" LIMIT "+limitPlaceholder+" OFFSET "+offsetPlaceholder, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.Ticket, 0)
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, *ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return items, paginationResultFromTotal(total, page), nil
}

func userTicketOrder(page pagination.PaginationParams) string {
	field := "last_public_message_at"
	switch strings.TrimSpace(page.SortBy) {
	case "created_at":
		field = "created_at"
	case "last_public_message_at", "":
		field = "last_public_message_at"
	}
	order := pagination.NormalizeSortOrder(page.SortOrder, pagination.SortOrderDesc)
	return "ORDER BY " + field + " " + strings.ToUpper(order) + ", id " + strings.ToUpper(order)
}

func adminTicketOrder(page pagination.PaginationParams) string {
	if strings.TrimSpace(page.SortBy) == "" {
		return `ORDER BY
			CASE WHEN status IN ('open', 'in_progress') THEN 0 WHEN status = 'waiting_user' THEN 1 ELSE 2 END ASC,
			CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END ASC,
			action_required_since ASC NULLS LAST,
			id ASC`
	}
	field := "last_activity_at"
	switch strings.TrimSpace(page.SortBy) {
	case "created_at":
		field = "created_at"
	case "last_activity_at":
		field = "last_activity_at"
	case "action_required_since":
		field = "action_required_since"
	case "priority":
		field = "priority"
	}
	order := strings.ToUpper(pagination.NormalizeSortOrder(page.SortOrder, pagination.SortOrderDesc))
	return "ORDER BY " + field + " " + order + " NULLS LAST, id " + order
}

func escapeTicketSearch(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

func (r *ticketRepository) CountUserTickets(ctx context.Context, userID int64) (service.UserTicketCounts, error) {
	var counts service.UserTicketCounts
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE user_notification_seq > user_last_read_seq),
			COUNT(*),
			COUNT(*) FILTER (WHERE status IN ('open', 'in_progress')),
			COUNT(*) FILTER (WHERE status = 'waiting_user'),
			COUNT(*) FILTER (WHERE status IN ('resolved', 'closed')),
			COUNT(*) FILTER (WHERE status = 'open'),
			COUNT(*) FILTER (WHERE status = 'in_progress'),
			COUNT(*) FILTER (WHERE status = 'resolved'),
			COUNT(*) FILTER (WHERE status = 'closed')
		FROM tickets
		WHERE user_id = $1`, userID).Scan(
		&counts.Unread,
		&counts.All,
		&counts.Active,
		&counts.WaitingUser,
		&counts.Ended,
		&counts.Open,
		&counts.InProgress,
		&counts.Resolved,
		&counts.Closed,
	)
	return counts, err
}

func (r *ticketRepository) CountAdminTickets(ctx context.Context) (service.AdminTicketCounts, error) {
	var counts service.AdminTicketCounts
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('open', 'in_progress')),
			COUNT(*) FILTER (WHERE status = 'open'),
			COUNT(*) FILTER (WHERE status = 'in_progress'),
			COUNT(*) FILTER (WHERE status = 'waiting_user'),
			COUNT(*) FILTER (WHERE status = 'resolved'),
			COUNT(*) FILTER (WHERE status = 'closed'),
			COUNT(*) FILTER (WHERE status IN ('resolved', 'closed')),
			COUNT(*)
		FROM tickets`).Scan(
		&counts.ActionRequired,
		&counts.Open,
		&counts.InProgress,
		&counts.WaitingUser,
		&counts.Resolved,
		&counts.Closed,
		&counts.Ended,
		&counts.All,
	)
	return counts, err
}

func (r *ticketRepository) AutoCloseResolvedBatch(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT `+ticketSelectColumns+`
		FROM tickets
		WHERE status = 'resolved' AND reopen_deadline IS NOT NULL AND reopen_deadline <= $1
		ORDER BY reopen_deadline ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $2`, now, limit)
	if err != nil {
		return 0, err
	}
	candidates := make([]service.Ticket, 0)
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, *ticket)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for i := range candidates {
		ticket := &candidates[i]
		transition, err := service.TransitionTicket(service.TicketTransitionInput{Status: ticket.Status, Action: service.TicketActionAutoClose, Now: now, ReopenDeadline: ticket.ReopenDeadline})
		if err != nil {
			return 0, err
		}
		if _, err := insertTicketEvent(ctx, tx, ticket.ID, nil, domain.TicketActorSystem, domain.TicketEventAutoClosed, &transition.From, &transition.To, map[string]any{"reason_code": "reopen_window_expired"}, domain.TicketVisibilityPublic, now); err != nil {
			return 0, err
		}
		if _, err := updateTicketForTransition(ctx, tx, ticket, transition, now, ticketTransitionUpdate{PublicActivity: true, NotifyUser: true}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(candidates), nil
}
