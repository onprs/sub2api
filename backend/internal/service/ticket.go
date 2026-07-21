package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	TicketStatusOpen        = domain.TicketStatusOpen
	TicketStatusInProgress  = domain.TicketStatusInProgress
	TicketStatusWaitingUser = domain.TicketStatusWaitingUser
	TicketStatusResolved    = domain.TicketStatusResolved
	TicketStatusClosed      = domain.TicketStatusClosed
)

var (
	ErrTicketNotFound            = domain.ErrTicketNotFound
	ErrTicketVersionConflict     = domain.ErrTicketVersionConflict
	ErrTicketInvalidTransition   = domain.ErrTicketInvalidTransition
	ErrTicketReferenceNotFound   = domain.ErrTicketReferenceNotFound
	ErrTicketReopenWindowExpired = domain.ErrTicketReopenWindowExpired
	ErrTicketTooManyOpen         = domain.ErrTicketTooManyOpen
	ErrTicketRateLimited         = domain.ErrTicketRateLimited
	ErrTicketInputRequired       = infraerrors.BadRequest("TICKET_INPUT_REQUIRED", "ticket input is required")
	ErrTicketSubjectInvalid      = infraerrors.BadRequest("TICKET_SUBJECT_INVALID", "ticket subject is invalid")
	ErrTicketBodyInvalid         = infraerrors.BadRequest("TICKET_BODY_INVALID", "ticket message body is invalid")
	ErrTicketExpectedVersion     = infraerrors.BadRequest("TICKET_EXPECTED_VERSION_INVALID", "expected_version must be positive")
	ErrTicketAssigneeInvalid     = infraerrors.BadRequest("TICKET_ASSIGNEE_INVALID", "ticket assignee is invalid")
	ErrTicketNextActionInvalid   = infraerrors.BadRequest("TICKET_NEXT_ACTION_INVALID", "invalid ticket next action")
)

type Ticket struct {
	ID                  int64
	TicketNo            string
	UserID              *int64
	RequesterEmail      string
	RequesterUsername   string
	Subject             string
	Category            domain.TicketCategory
	Impact              domain.TicketImpact
	Priority            domain.TicketPriority
	Status              domain.TicketStatus
	ActionRequired      domain.TicketActionRequired
	AssigneeID          *int64
	RequestID           string
	UsageLogID          *int64
	APIKeyID            *int64
	APIKeyName          string
	PaymentOrderID      *int64
	PaymentOrderNo      string
	UserSubscriptionID  *int64
	SubscriptionName    string
	LastPublicMessageAt time.Time
	LastActivityAt      time.Time
	ActionRequiredSince *time.Time
	UserNotificationSeq int64
	UserLastReadSeq     int64
	ResolvedAt          *time.Time
	ReopenDeadline      *time.Time
	ClosedAt            *time.Time
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (t Ticket) HasUnreadForUser() bool {
	return t.UserNotificationSeq > t.UserLastReadSeq
}

type TicketMessage struct {
	ID          int64
	TicketID    int64
	AuthorID    *int64
	AuthorRole  domain.TicketActorRole
	Visibility  domain.TicketVisibility
	AuthorName  string
	Body        string
	Attachments []TicketMessageAttachment
	CreatedAt   time.Time
}

type TicketMessageAttachment struct {
	ID           int64
	OriginalName string
	ContentType  string
	ByteSize     int64
	CreatedAt    time.Time
}

type TicketEvent struct {
	ID         int64
	TicketID   int64
	ActorID    *int64
	ActorRole  domain.TicketActorRole
	EventType  domain.TicketEventType
	FromStatus *domain.TicketStatus
	ToStatus   *domain.TicketStatus
	Payload    map[string]any
	Visibility domain.TicketVisibility
	CreatedAt  time.Time
}

type UserTicketDetail struct {
	Ticket   Ticket
	Messages []TicketMessage
	Events   []TicketEvent
}

type AdminTicketDetail struct {
	Ticket   Ticket
	Messages []TicketMessage
	Events   []TicketEvent
}

type TicketReferenceInput struct {
	UsageLogID         *int64
	APIKeyID           *int64
	PaymentOrderID     *int64
	UserSubscriptionID *int64
}

type CreateTicketParams struct {
	UserID           int64
	Category         domain.TicketCategory
	Impact           domain.TicketImpact
	Subject          string
	Body             string
	References       TicketReferenceInput
	AttachmentTokens []string
	Now              time.Time
}

type UserTicketReplyParams struct {
	UserID           int64
	TicketNo         string
	Body             string
	ExpectedVersion  int64
	AttachmentTokens []string
	Now              time.Time
}

type AdminTicketReplyParams struct {
	AdminID          int64
	TicketNo         string
	Body             string
	NextAction       TicketTransitionAction
	ExpectedVersion  int64
	AttachmentTokens []string
	Now              time.Time
}

type InternalTicketNoteParams struct {
	AdminID          int64
	TicketNo         string
	Body             string
	ExpectedVersion  int64
	AttachmentTokens []string
	Now              time.Time
}

type TicketVersionActionParams struct {
	ActorID         int64
	TicketNo        string
	ExpectedVersion int64
	Now             time.Time
}

type AssignTicketParams struct {
	AdminID         int64
	TicketNo        string
	AssigneeID      *int64
	ExpectedVersion int64
	Now             time.Time
}

type ChangeTicketPriorityParams struct {
	AdminID         int64
	TicketNo        string
	Priority        domain.TicketPriority
	ExpectedVersion int64
	Now             time.Time
}

type CloseTicketParams struct {
	ActorID         int64
	ActorRole       domain.TicketActorRole
	TicketNo        string
	Reason          string
	ExpectedVersion int64
	Now             time.Time
}

type ReopenTicketParams struct {
	ActorID         int64
	ActorRole       domain.TicketActorRole
	TicketNo        string
	Body            string
	ExpectedVersion int64
	Now             time.Time
}

type MarkTicketReadParams struct {
	UserID                  int64
	TicketNo                string
	ObservedNotificationSeq int64
}

type UserTicketListFilters struct {
	Bucket   string
	Status   domain.TicketStatus
	Category domain.TicketCategory
	Search   string
}

type AdminTicketListFilters struct {
	Bucket      string
	Status      domain.TicketStatus
	Category    domain.TicketCategory
	Impact      domain.TicketImpact
	Priority    domain.TicketPriority
	AssigneeID  *int64
	Unassigned  bool
	Search      string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

type UserTicketCounts struct {
	Unread      int64
	All         int64
	Active      int64
	WaitingUser int64
	Ended       int64
	Open        int64
	InProgress  int64
	Resolved    int64
	Closed      int64
}

type AdminTicketCounts struct {
	ActionRequired int64
	Open           int64
	InProgress     int64
	WaitingUser    int64
	Resolved       int64
	Closed         int64
	Ended          int64
	All            int64
}

type TicketRepository interface {
	CreateTicketWithInitialMessage(ctx context.Context, params CreateTicketParams) (*UserTicketDetail, error)
	AppendUserReply(ctx context.Context, params UserTicketReplyParams) (*UserTicketDetail, error)
	AppendAdminReply(ctx context.Context, params AdminTicketReplyParams) (*AdminTicketDetail, error)
	AppendInternalNote(ctx context.Context, params InternalTicketNoteParams) (*AdminTicketDetail, error)
	ClaimTicket(ctx context.Context, params TicketVersionActionParams) (*AdminTicketDetail, error)
	AssignTicket(ctx context.Context, params AssignTicketParams) (*AdminTicketDetail, error)
	ChangePriority(ctx context.Context, params ChangeTicketPriorityParams) (*AdminTicketDetail, error)
	ResolveTicket(ctx context.Context, params TicketVersionActionParams) (*AdminTicketDetail, error)
	CloseTicket(ctx context.Context, params CloseTicketParams) error
	ReopenTicket(ctx context.Context, params ReopenTicketParams) error
	MarkUserRead(ctx context.Context, params MarkTicketReadParams) error

	GetUserTicket(ctx context.Context, userID int64, ticketNo string) (*UserTicketDetail, error)
	GetAdminTicket(ctx context.Context, ticketNo string) (*AdminTicketDetail, error)
	ListUserTickets(ctx context.Context, userID int64, page pagination.PaginationParams, filters UserTicketListFilters) ([]Ticket, *pagination.PaginationResult, error)
	ListAdminTickets(ctx context.Context, page pagination.PaginationParams, filters AdminTicketListFilters) ([]Ticket, *pagination.PaginationResult, error)
	CountUserTickets(ctx context.Context, userID int64) (UserTicketCounts, error)
	CountAdminTickets(ctx context.Context) (AdminTicketCounts, error)
	AutoCloseResolvedBatch(ctx context.Context, now time.Time, limit int) (int, error)
}
