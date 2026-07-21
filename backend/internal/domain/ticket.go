package domain

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

type TicketStatus string

const (
	TicketStatusOpen        TicketStatus = "open"
	TicketStatusInProgress  TicketStatus = "in_progress"
	TicketStatusWaitingUser TicketStatus = "waiting_user"
	TicketStatusResolved    TicketStatus = "resolved"
	TicketStatusClosed      TicketStatus = "closed"
)

type TicketActionRequired string

const (
	TicketActionRequiredAdmin TicketActionRequired = "ADMIN"
	TicketActionRequiredUser  TicketActionRequired = "USER"
	TicketActionRequiredNone  TicketActionRequired = "NONE"
)

type TicketCategory string

const (
	TicketCategoryAPIIssue       TicketCategory = "api_issue"
	TicketCategorySubscription   TicketCategory = "subscription"
	TicketCategoryPayment        TicketCategory = "payment"
	TicketCategoryAccount        TicketCategory = "account"
	TicketCategoryFeatureRequest TicketCategory = "feature_request"
	TicketCategoryOther          TicketCategory = "other"
)

type TicketImpact string

const (
	TicketImpactBlocked  TicketImpact = "blocked"
	TicketImpactDegraded TicketImpact = "degraded"
	TicketImpactGeneral  TicketImpact = "general"
)

type TicketPriority string

const (
	TicketPriorityUrgent TicketPriority = "urgent"
	TicketPriorityHigh   TicketPriority = "high"
	TicketPriorityNormal TicketPriority = "normal"
	TicketPriorityLow    TicketPriority = "low"
)

type TicketActorRole string

const (
	TicketActorUser   TicketActorRole = "user"
	TicketActorAdmin  TicketActorRole = "admin"
	TicketActorSystem TicketActorRole = "system"
)

type TicketVisibility string

const (
	TicketVisibilityPublic   TicketVisibility = "public"
	TicketVisibilityInternal TicketVisibility = "internal"
)

type TicketEventType string

const (
	TicketEventCreated         TicketEventType = "ticket_created"
	TicketEventClaimed         TicketEventType = "ticket_claimed"
	TicketEventAssigned        TicketEventType = "ticket_assigned"
	TicketEventPriorityChanged TicketEventType = "priority_changed"
	TicketEventStatusChanged   TicketEventType = "status_changed"
	TicketEventResolved        TicketEventType = "ticket_resolved"
	TicketEventReopened        TicketEventType = "ticket_reopened"
	TicketEventClosed          TicketEventType = "ticket_closed"
	TicketEventAutoClosed      TicketEventType = "ticket_auto_closed"
)

const (
	TicketMaxSubjectLength          = 100
	TicketMaxMessageLength          = 5000
	TicketMaxCloseReasonLength      = 500
	TicketMaxOpenPerUser            = 20
	TicketCreateHourlyLimit         = 10
	TicketCreateDailyLimit          = 30
	TicketReplyHourlyLimit          = 60
	TicketAttachmentDailyBytesLimit = int64(100 * 1024 * 1024)
)

var (
	ErrTicketingDisabled                = infraerrors.NotFound("TICKETING_DISABLED", "ticketing is disabled")
	ErrTicketNotFound                   = infraerrors.NotFound("TICKET_NOT_FOUND", "ticket not found")
	ErrTicketInvalidCategory            = infraerrors.BadRequest("TICKET_INVALID_CATEGORY", "invalid ticket category")
	ErrTicketInvalidImpact              = infraerrors.BadRequest("TICKET_INVALID_IMPACT", "invalid ticket impact")
	ErrTicketInvalidPriority            = infraerrors.BadRequest("TICKET_INVALID_PRIORITY", "invalid ticket priority")
	ErrTicketInvalidTransition          = infraerrors.Conflict("TICKET_INVALID_TRANSITION", "ticket action is not allowed in the current status")
	ErrTicketVersionConflict            = infraerrors.Conflict("TICKET_VERSION_CONFLICT", "ticket was updated by another request")
	ErrTicketReferenceNotFound          = infraerrors.NotFound("TICKET_REFERENCE_NOT_FOUND", "ticket reference not found")
	ErrTicketReplyNotAllowed            = infraerrors.Conflict("TICKET_REPLY_NOT_ALLOWED", "ticket does not accept replies")
	ErrTicketReopenWindowExpired        = infraerrors.Conflict("TICKET_REOPEN_WINDOW_EXPIRED", "ticket reopen window has expired")
	ErrTicketCloseReasonRequired        = infraerrors.BadRequest("TICKET_CLOSE_REASON_REQUIRED", "ticket close reason is required")
	ErrTicketTooManyOpen                = infraerrors.Conflict("TICKET_TOO_MANY_OPEN", "too many active tickets")
	ErrTicketRateLimited                = infraerrors.TooManyRequests("TICKET_RATE_LIMITED", "ticket request rate limit exceeded")
	ErrTicketAttachmentsDisabled        = infraerrors.Conflict("TICKET_ATTACHMENTS_DISABLED", "ticket attachments are disabled")
	ErrTicketAttachmentInvalidType      = infraerrors.BadRequest("TICKET_ATTACHMENT_INVALID_TYPE", "unsupported ticket attachment type")
	ErrTicketAttachmentTooLarge         = infraerrors.BadRequest("TICKET_ATTACHMENT_TOO_LARGE", "ticket attachment is too large")
	ErrTicketAttachmentLimitExceeded    = infraerrors.Conflict("TICKET_ATTACHMENT_LIMIT_EXCEEDED", "ticket attachment limit exceeded")
	ErrTicketAttachmentDailyLimit       = infraerrors.TooManyRequests("TICKET_ATTACHMENT_DAILY_LIMIT", "daily ticket attachment upload limit exceeded")
	ErrTicketAttachmentNotFound         = infraerrors.NotFound("TICKET_ATTACHMENT_NOT_FOUND", "ticket attachment not found")
	ErrTicketAttachmentAlreadyClaimed   = infraerrors.Conflict("TICKET_ATTACHMENT_ALREADY_CLAIMED", "ticket attachment has already been claimed")
	ErrTicketStorageModeInvalid         = infraerrors.BadRequest("TICKET_STORAGE_MODE_INVALID", "invalid ticket attachment storage mode")
	ErrTicketStorageTestFailed          = infraerrors.BadRequest("TICKET_STORAGE_TEST_FAILED", "ticket attachment storage test failed")
	ErrTicketLocalStorageUnavailable    = infraerrors.ServiceUnavailable("TICKET_LOCAL_STORAGE_UNAVAILABLE", "local ticket attachment storage is unavailable")
	ErrTicketS3ConfigInvalid            = infraerrors.BadRequest("TICKET_S3_CONFIG_INVALID", "invalid ticket attachment S3 configuration")
	ErrTicketStorageDestinationInUse    = infraerrors.Conflict("TICKET_STORAGE_DESTINATION_IN_USE", "ticket attachment storage destination is in use")
	ErrTicketStorageProviderUnavailable = infraerrors.ServiceUnavailable("TICKET_STORAGE_PROVIDER_UNAVAILABLE", "ticket attachment storage provider is unavailable")
)

func (s TicketStatus) Valid() bool {
	switch s {
	case TicketStatusOpen, TicketStatusInProgress, TicketStatusWaitingUser, TicketStatusResolved, TicketStatusClosed:
		return true
	default:
		return false
	}
}

func (c TicketCategory) Valid() bool {
	switch c {
	case TicketCategoryAPIIssue, TicketCategorySubscription, TicketCategoryPayment, TicketCategoryAccount, TicketCategoryFeatureRequest, TicketCategoryOther:
		return true
	default:
		return false
	}
}

func (i TicketImpact) Valid() bool {
	switch i {
	case TicketImpactBlocked, TicketImpactDegraded, TicketImpactGeneral:
		return true
	default:
		return false
	}
}

func (p TicketPriority) Valid() bool {
	switch p {
	case TicketPriorityUrgent, TicketPriorityHigh, TicketPriorityNormal, TicketPriorityLow:
		return true
	default:
		return false
	}
}

func TicketActionRequiredFor(status TicketStatus) TicketActionRequired {
	switch status {
	case TicketStatusOpen, TicketStatusInProgress:
		return TicketActionRequiredAdmin
	case TicketStatusWaitingUser:
		return TicketActionRequiredUser
	default:
		return TicketActionRequiredNone
	}
}
