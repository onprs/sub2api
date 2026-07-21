package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const DefaultTicketReopenWindow = 7 * 24 * time.Hour

type TicketTransitionAction string

const (
	TicketActionCreate             TicketTransitionAction = "create"
	TicketActionClaim              TicketTransitionAction = "claim"
	TicketActionAssign             TicketTransitionAction = "assign"
	TicketActionChangePriority     TicketTransitionAction = "change_priority"
	TicketActionInternalNote       TicketTransitionAction = "internal_note"
	TicketActionAdminReplyWaitUser TicketTransitionAction = "admin_reply_wait_user"
	TicketActionAdminReplyKeep     TicketTransitionAction = "admin_reply_keep_processing"
	TicketActionAdminReplyResolve  TicketTransitionAction = "admin_reply_resolve"
	TicketActionUserReply          TicketTransitionAction = "user_reply"
	TicketActionUserClose          TicketTransitionAction = "user_close"
	TicketActionAdminResolve       TicketTransitionAction = "admin_resolve"
	TicketActionUserReopen         TicketTransitionAction = "user_reopen"
	TicketActionAdminReopen        TicketTransitionAction = "admin_reopen"
	TicketActionAdminClose         TicketTransitionAction = "admin_close"
	TicketActionAutoClose          TicketTransitionAction = "auto_close"
)

type TicketTimeUpdate string

const (
	TicketTimePreserve TicketTimeUpdate = "preserve"
	TicketTimeSet      TicketTimeUpdate = "set"
	TicketTimeClear    TicketTimeUpdate = "clear"
)

type TicketTransitionInput struct {
	Status         domain.TicketStatus
	Action         TicketTransitionAction
	Now            time.Time
	ReopenDeadline *time.Time
	ReopenWindow   time.Duration
}

type TicketTransitionResult struct {
	From                      domain.TicketStatus
	To                        domain.TicketStatus
	ActionRequired            domain.TicketActionRequired
	ActionRequiredSinceUpdate TicketTimeUpdate
	ResolvedAtUpdate          TicketTimeUpdate
	ReopenDeadlineUpdate      TicketTimeUpdate
	ClosedAtUpdate            TicketTimeUpdate
	ReopenDeadline            *time.Time
}

func TransitionTicket(input TicketTransitionInput) (TicketTransitionResult, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}

	if input.Action == TicketActionCreate {
		if input.Status != "" {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		return buildTicketTransitionResult("", domain.TicketStatusOpen, input.Action, now, input.ReopenWindow), nil
	}
	if !input.Status.Valid() || input.Status == domain.TicketStatusClosed {
		return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
	}

	var target domain.TicketStatus
	switch input.Action {
	case TicketActionClaim:
		if !ticketStatusIn(input.Status, domain.TicketStatusOpen, domain.TicketStatusInProgress) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusInProgress
	case TicketActionAssign:
		if !ticketStatusIn(input.Status, domain.TicketStatusOpen, domain.TicketStatusInProgress, domain.TicketStatusWaitingUser) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = input.Status
	case TicketActionChangePriority, TicketActionInternalNote:
		target = input.Status
	case TicketActionAdminReplyWaitUser:
		if !ticketStatusIn(input.Status, domain.TicketStatusOpen, domain.TicketStatusInProgress, domain.TicketStatusWaitingUser) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusWaitingUser
	case TicketActionAdminReplyKeep, TicketActionUserReply:
		if !ticketStatusIn(input.Status, domain.TicketStatusOpen, domain.TicketStatusInProgress, domain.TicketStatusWaitingUser) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusInProgress
	case TicketActionAdminReplyResolve, TicketActionAdminResolve:
		if !ticketStatusIn(input.Status, domain.TicketStatusOpen, domain.TicketStatusInProgress, domain.TicketStatusWaitingUser) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusResolved
	case TicketActionUserClose, TicketActionAdminClose:
		target = domain.TicketStatusClosed
	case TicketActionUserReopen:
		if input.Status != domain.TicketStatusResolved {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		if input.ReopenDeadline == nil || !now.Before(*input.ReopenDeadline) {
			return TicketTransitionResult{}, domain.ErrTicketReopenWindowExpired
		}
		target = domain.TicketStatusInProgress
	case TicketActionAdminReopen:
		if input.Status != domain.TicketStatusResolved {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusInProgress
	case TicketActionAutoClose:
		if input.Status != domain.TicketStatusResolved || input.ReopenDeadline == nil || now.Before(*input.ReopenDeadline) {
			return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
		}
		target = domain.TicketStatusClosed
	default:
		return TicketTransitionResult{}, domain.ErrTicketInvalidTransition
	}

	return buildTicketTransitionResult(input.Status, target, input.Action, now, input.ReopenWindow), nil
}

func buildTicketTransitionResult(
	from domain.TicketStatus,
	to domain.TicketStatus,
	action TicketTransitionAction,
	now time.Time,
	reopenWindow time.Duration,
) TicketTransitionResult {
	result := TicketTransitionResult{
		From:                      from,
		To:                        to,
		ActionRequired:            domain.TicketActionRequiredFor(to),
		ActionRequiredSinceUpdate: TicketTimePreserve,
		ResolvedAtUpdate:          TicketTimePreserve,
		ReopenDeadlineUpdate:      TicketTimePreserve,
		ClosedAtUpdate:            TicketTimePreserve,
	}

	fromRequired := domain.TicketActionRequiredFor(from)
	switch {
	case result.ActionRequired == domain.TicketActionRequiredNone && fromRequired != domain.TicketActionRequiredNone:
		result.ActionRequiredSinceUpdate = TicketTimeClear
	case result.ActionRequired != domain.TicketActionRequiredNone && (from == "" || fromRequired != result.ActionRequired):
		result.ActionRequiredSinceUpdate = TicketTimeSet
	}

	switch action {
	case TicketActionAdminReplyResolve, TicketActionAdminResolve:
		if reopenWindow <= 0 {
			reopenWindow = DefaultTicketReopenWindow
		}
		deadline := now.Add(reopenWindow)
		result.ResolvedAtUpdate = TicketTimeSet
		result.ReopenDeadlineUpdate = TicketTimeSet
		result.ReopenDeadline = &deadline
	case TicketActionUserReopen, TicketActionAdminReopen:
		result.ResolvedAtUpdate = TicketTimeClear
		result.ReopenDeadlineUpdate = TicketTimeClear
	case TicketActionUserClose, TicketActionAdminClose, TicketActionAutoClose:
		result.ClosedAtUpdate = TicketTimeSet
		result.ReopenDeadlineUpdate = TicketTimeClear
	}

	return result
}

func ticketStatusIn(status domain.TicketStatus, allowed ...domain.TicketStatus) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}
