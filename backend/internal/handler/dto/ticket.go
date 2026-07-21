package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type PendingTicketAttachment struct {
	UploadToken  string     `json:"upload_token"`
	OriginalName string     `json:"original_name"`
	ContentType  string     `json:"content_type"`
	ByteSize     int64      `json:"byte_size"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type TicketCapabilities struct {
	Enabled              bool  `json:"enabled"`
	AttachmentsEnabled   bool  `json:"attachments_enabled"`
	MaxFileBytes         int64 `json:"max_file_bytes"`
	MaxFilesPerMessage   int   `json:"max_files_per_message"`
	MaxTicketBytes       int64 `json:"max_ticket_bytes"`
	PollingHintSeconds   int   `json:"polling_hint_seconds"`
	DetailPollingSeconds int   `json:"detail_polling_seconds"`
}

type UserTicketCounts struct {
	Unread      int64 `json:"unread"`
	All         int64 `json:"all"`
	Active      int64 `json:"active"`
	WaitingUser int64 `json:"waiting_user"`
	Ended       int64 `json:"ended"`
	Open        int64 `json:"open"`
	InProgress  int64 `json:"in_progress"`
	Resolved    int64 `json:"resolved"`
	Closed      int64 `json:"closed"`
}

type AdminTicketCounts struct {
	ActionRequired int64 `json:"action_required"`
	Open           int64 `json:"open"`
	InProgress     int64 `json:"in_progress"`
	WaitingUser    int64 `json:"waiting_user"`
	Resolved       int64 `json:"resolved"`
	Closed         int64 `json:"closed"`
	Ended          int64 `json:"ended"`
	All            int64 `json:"all"`
}

type UserTicket struct {
	TicketNo            string     `json:"ticket_no"`
	Subject             string     `json:"subject"`
	Category            string     `json:"category"`
	Impact              string     `json:"impact"`
	Status              string     `json:"status"`
	ActionRequired      string     `json:"action_required"`
	RequestID           string     `json:"request_id,omitempty"`
	UsageLogID          *int64     `json:"usage_log_id,omitempty"`
	APIKeyID            *int64     `json:"api_key_id,omitempty"`
	APIKeyName          string     `json:"api_key_name,omitempty"`
	PaymentOrderID      *int64     `json:"payment_order_id,omitempty"`
	PaymentOrderNo      string     `json:"payment_order_no,omitempty"`
	UserSubscriptionID  *int64     `json:"user_subscription_id,omitempty"`
	SubscriptionName    string     `json:"subscription_name,omitempty"`
	LastPublicMessageAt time.Time  `json:"last_public_message_at"`
	ActionRequiredSince *time.Time `json:"action_required_since,omitempty"`
	Unread              bool       `json:"unread"`
	NotificationSeq     int64      `json:"notification_seq"`
	ResolvedAt          *time.Time `json:"resolved_at,omitempty"`
	ReopenDeadline      *time.Time `json:"reopen_deadline,omitempty"`
	ClosedAt            *time.Time `json:"closed_at,omitempty"`
	Version             int64      `json:"version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type AdminTicket struct {
	TicketNo            string     `json:"ticket_no"`
	UserID              *int64     `json:"user_id,omitempty"`
	RequesterEmail      string     `json:"requester_email"`
	RequesterUsername   string     `json:"requester_username"`
	Subject             string     `json:"subject"`
	Category            string     `json:"category"`
	Impact              string     `json:"impact"`
	Priority            string     `json:"priority"`
	Status              string     `json:"status"`
	ActionRequired      string     `json:"action_required"`
	AssigneeID          *int64     `json:"assignee_id,omitempty"`
	RequestID           string     `json:"request_id,omitempty"`
	UsageLogID          *int64     `json:"usage_log_id,omitempty"`
	APIKeyID            *int64     `json:"api_key_id,omitempty"`
	APIKeyName          string     `json:"api_key_name,omitempty"`
	PaymentOrderID      *int64     `json:"payment_order_id,omitempty"`
	PaymentOrderNo      string     `json:"payment_order_no,omitempty"`
	UserSubscriptionID  *int64     `json:"user_subscription_id,omitempty"`
	SubscriptionName    string     `json:"subscription_name,omitempty"`
	LastPublicMessageAt time.Time  `json:"last_public_message_at"`
	LastActivityAt      time.Time  `json:"last_activity_at"`
	ActionRequiredSince *time.Time `json:"action_required_since,omitempty"`
	NotificationSeq     int64      `json:"notification_seq"`
	ResolvedAt          *time.Time `json:"resolved_at,omitempty"`
	ReopenDeadline      *time.Time `json:"reopen_deadline,omitempty"`
	ClosedAt            *time.Time `json:"closed_at,omitempty"`
	Version             int64      `json:"version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type TicketMessageAttachment struct {
	ID           int64     `json:"id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	ByteSize     int64     `json:"byte_size"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserTicketMessage struct {
	ID          int64                     `json:"id"`
	AuthorID    *int64                    `json:"author_id,omitempty"`
	AuthorRole  string                    `json:"author_role"`
	AuthorName  string                    `json:"author_name"`
	Body        string                    `json:"body"`
	Attachments []TicketMessageAttachment `json:"attachments"`
	CreatedAt   time.Time                 `json:"created_at"`
}

type AdminTicketMessage struct {
	ID          int64                     `json:"id"`
	AuthorID    *int64                    `json:"author_id,omitempty"`
	AuthorRole  string                    `json:"author_role"`
	Visibility  string                    `json:"visibility"`
	AuthorName  string                    `json:"author_name"`
	Body        string                    `json:"body"`
	Attachments []TicketMessageAttachment `json:"attachments"`
	CreatedAt   time.Time                 `json:"created_at"`
}

type UserTicketEvent struct {
	ID         int64          `json:"id"`
	ActorID    *int64         `json:"actor_id,omitempty"`
	ActorRole  string         `json:"actor_role"`
	EventType  string         `json:"event_type"`
	FromStatus *string        `json:"from_status,omitempty"`
	ToStatus   *string        `json:"to_status,omitempty"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AdminTicketEvent struct {
	ID         int64          `json:"id"`
	ActorID    *int64         `json:"actor_id,omitempty"`
	ActorRole  string         `json:"actor_role"`
	EventType  string         `json:"event_type"`
	FromStatus *string        `json:"from_status,omitempty"`
	ToStatus   *string        `json:"to_status,omitempty"`
	Payload    map[string]any `json:"payload"`
	Visibility string         `json:"visibility"`
	CreatedAt  time.Time      `json:"created_at"`
}

type UserTicketDetail struct {
	Ticket   UserTicket          `json:"ticket"`
	Messages []UserTicketMessage `json:"messages"`
	Events   []UserTicketEvent   `json:"events"`
}

type AdminTicketDetail struct {
	Ticket   AdminTicket          `json:"ticket"`
	Messages []AdminTicketMessage `json:"messages"`
	Events   []AdminTicketEvent   `json:"events"`
}

func UserTicketCountsFromService(counts service.UserTicketCounts) UserTicketCounts {
	return UserTicketCounts{
		Unread: counts.Unread, All: counts.All, Active: counts.Active, WaitingUser: counts.WaitingUser,
		Ended: counts.Ended, Open: counts.Open, InProgress: counts.InProgress, Resolved: counts.Resolved, Closed: counts.Closed,
	}
}

func AdminTicketCountsFromService(counts service.AdminTicketCounts) AdminTicketCounts {
	return AdminTicketCounts{
		ActionRequired: counts.ActionRequired, Open: counts.Open, InProgress: counts.InProgress,
		WaitingUser: counts.WaitingUser, Resolved: counts.Resolved, Closed: counts.Closed, Ended: counts.Ended, All: counts.All,
	}
}

func UserTicketFromService(ticket *service.Ticket) *UserTicket {
	if ticket == nil {
		return nil
	}
	return &UserTicket{
		TicketNo:            ticket.TicketNo,
		Subject:             ticket.Subject,
		Category:            string(ticket.Category),
		Impact:              string(ticket.Impact),
		Status:              string(ticket.Status),
		ActionRequired:      string(ticket.ActionRequired),
		RequestID:           ticket.RequestID,
		UsageLogID:          ticket.UsageLogID,
		APIKeyID:            ticket.APIKeyID,
		APIKeyName:          ticket.APIKeyName,
		PaymentOrderID:      ticket.PaymentOrderID,
		PaymentOrderNo:      ticket.PaymentOrderNo,
		UserSubscriptionID:  ticket.UserSubscriptionID,
		SubscriptionName:    ticket.SubscriptionName,
		LastPublicMessageAt: ticket.LastPublicMessageAt,
		ActionRequiredSince: ticket.ActionRequiredSince,
		Unread:              ticket.HasUnreadForUser(),
		NotificationSeq:     ticket.UserNotificationSeq,
		ResolvedAt:          ticket.ResolvedAt,
		ReopenDeadline:      ticket.ReopenDeadline,
		ClosedAt:            ticket.ClosedAt,
		Version:             ticket.Version,
		CreatedAt:           ticket.CreatedAt,
		UpdatedAt:           ticket.UpdatedAt,
	}
}

func AdminTicketFromService(ticket *service.Ticket) *AdminTicket {
	if ticket == nil {
		return nil
	}
	return &AdminTicket{
		TicketNo:            ticket.TicketNo,
		UserID:              ticket.UserID,
		RequesterEmail:      ticket.RequesterEmail,
		RequesterUsername:   ticket.RequesterUsername,
		Subject:             ticket.Subject,
		Category:            string(ticket.Category),
		Impact:              string(ticket.Impact),
		Priority:            string(ticket.Priority),
		Status:              string(ticket.Status),
		ActionRequired:      string(ticket.ActionRequired),
		AssigneeID:          ticket.AssigneeID,
		RequestID:           ticket.RequestID,
		UsageLogID:          ticket.UsageLogID,
		APIKeyID:            ticket.APIKeyID,
		APIKeyName:          ticket.APIKeyName,
		PaymentOrderID:      ticket.PaymentOrderID,
		PaymentOrderNo:      ticket.PaymentOrderNo,
		UserSubscriptionID:  ticket.UserSubscriptionID,
		SubscriptionName:    ticket.SubscriptionName,
		LastPublicMessageAt: ticket.LastPublicMessageAt,
		LastActivityAt:      ticket.LastActivityAt,
		ActionRequiredSince: ticket.ActionRequiredSince,
		NotificationSeq:     ticket.UserNotificationSeq,
		ResolvedAt:          ticket.ResolvedAt,
		ReopenDeadline:      ticket.ReopenDeadline,
		ClosedAt:            ticket.ClosedAt,
		Version:             ticket.Version,
		CreatedAt:           ticket.CreatedAt,
		UpdatedAt:           ticket.UpdatedAt,
	}
}

func UserTicketDetailFromService(detail *service.UserTicketDetail) *UserTicketDetail {
	if detail == nil {
		return nil
	}
	out := &UserTicketDetail{
		Ticket:   *UserTicketFromService(&detail.Ticket),
		Messages: make([]UserTicketMessage, 0, len(detail.Messages)),
		Events:   make([]UserTicketEvent, 0, len(detail.Events)),
	}
	for i := range detail.Messages {
		message := detail.Messages[i]
		if message.Visibility != domain.TicketVisibilityPublic {
			continue
		}
		out.Messages = append(out.Messages, UserTicketMessage{
			ID: message.ID, AuthorID: message.AuthorID, AuthorRole: string(message.AuthorRole),
			AuthorName: message.AuthorName, Body: message.Body,
			Attachments: ticketMessageAttachments(message.Attachments), CreatedAt: message.CreatedAt,
		})
	}
	for i := range detail.Events {
		event := detail.Events[i]
		if event.Visibility != domain.TicketVisibilityPublic {
			continue
		}
		out.Events = append(out.Events, UserTicketEvent{
			ID: event.ID, ActorID: event.ActorID, ActorRole: string(event.ActorRole), EventType: string(event.EventType),
			FromStatus: ticketStatusString(event.FromStatus), ToStatus: ticketStatusString(event.ToStatus),
			Payload: event.Payload, CreatedAt: event.CreatedAt,
		})
	}
	return out
}

func AdminTicketDetailFromService(detail *service.AdminTicketDetail) *AdminTicketDetail {
	if detail == nil {
		return nil
	}
	out := &AdminTicketDetail{
		Ticket:   *AdminTicketFromService(&detail.Ticket),
		Messages: make([]AdminTicketMessage, 0, len(detail.Messages)),
		Events:   make([]AdminTicketEvent, 0, len(detail.Events)),
	}
	for i := range detail.Messages {
		message := detail.Messages[i]
		out.Messages = append(out.Messages, AdminTicketMessage{
			ID: message.ID, AuthorID: message.AuthorID, AuthorRole: string(message.AuthorRole), Visibility: string(message.Visibility),
			AuthorName: message.AuthorName, Body: message.Body,
			Attachments: ticketMessageAttachments(message.Attachments), CreatedAt: message.CreatedAt,
		})
	}
	for i := range detail.Events {
		event := detail.Events[i]
		out.Events = append(out.Events, AdminTicketEvent{
			ID: event.ID, ActorID: event.ActorID, ActorRole: string(event.ActorRole), EventType: string(event.EventType),
			FromStatus: ticketStatusString(event.FromStatus), ToStatus: ticketStatusString(event.ToStatus),
			Payload: event.Payload, Visibility: string(event.Visibility), CreatedAt: event.CreatedAt,
		})
	}
	return out
}

func ticketMessageAttachments(items []service.TicketMessageAttachment) []TicketMessageAttachment {
	attachments := make([]TicketMessageAttachment, 0, len(items))
	for _, item := range items {
		attachments = append(attachments, TicketMessageAttachment{
			ID: item.ID, OriginalName: item.OriginalName, ContentType: item.ContentType,
			ByteSize: item.ByteSize, CreatedAt: item.CreatedAt,
		})
	}
	return attachments
}

func ticketStatusString(status *domain.TicketStatus) *string {
	if status == nil {
		return nil
	}
	value := string(*status)
	return &value
}
