package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTransitionTicket_AllowedActions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	tests := []struct {
		name            string
		status          domain.TicketStatus
		action          TicketTransitionAction
		deadline        *time.Time
		wantStatus      domain.TicketStatus
		wantRequired    domain.TicketActionRequired
		wantRequiredAt  TicketTimeUpdate
		wantResolvedAt  TicketTimeUpdate
		wantReopenUntil TicketTimeUpdate
		wantClosedAt    TicketTimeUpdate
	}{
		{name: "create", action: TicketActionCreate, wantStatus: domain.TicketStatusOpen, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "claim open", status: domain.TicketStatusOpen, action: TicketActionClaim, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "claim in progress", status: domain.TicketStatusInProgress, action: TicketActionClaim, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "assign waiting user", status: domain.TicketStatusWaitingUser, action: TicketActionAssign, wantStatus: domain.TicketStatusWaitingUser, wantRequired: domain.TicketActionRequiredUser, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "priority on resolved", status: domain.TicketStatusResolved, action: TicketActionChangePriority, wantStatus: domain.TicketStatusResolved, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "internal note", status: domain.TicketStatusWaitingUser, action: TicketActionInternalNote, wantStatus: domain.TicketStatusWaitingUser, wantRequired: domain.TicketActionRequiredUser, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "admin waits for user", status: domain.TicketStatusOpen, action: TicketActionAdminReplyWaitUser, wantStatus: domain.TicketStatusWaitingUser, wantRequired: domain.TicketActionRequiredUser, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "admin keeps processing", status: domain.TicketStatusWaitingUser, action: TicketActionAdminReplyKeep, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "admin reply resolves", status: domain.TicketStatusInProgress, action: TicketActionAdminReplyResolve, wantStatus: domain.TicketStatusResolved, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimeClear, wantResolvedAt: TicketTimeSet, wantReopenUntil: TicketTimeSet, wantClosedAt: TicketTimePreserve},
		{name: "user replies", status: domain.TicketStatusWaitingUser, action: TicketActionUserReply, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimePreserve, wantClosedAt: TicketTimePreserve},
		{name: "user closes resolved", status: domain.TicketStatusResolved, action: TicketActionUserClose, wantStatus: domain.TicketStatusClosed, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimeClear, wantClosedAt: TicketTimeSet},
		{name: "admin resolves", status: domain.TicketStatusOpen, action: TicketActionAdminResolve, wantStatus: domain.TicketStatusResolved, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimeClear, wantResolvedAt: TicketTimeSet, wantReopenUntil: TicketTimeSet, wantClosedAt: TicketTimePreserve},
		{name: "user reopens", status: domain.TicketStatusResolved, action: TicketActionUserReopen, deadline: &future, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimeClear, wantReopenUntil: TicketTimeClear, wantClosedAt: TicketTimePreserve},
		{name: "admin reopens", status: domain.TicketStatusResolved, action: TicketActionAdminReopen, deadline: &past, wantStatus: domain.TicketStatusInProgress, wantRequired: domain.TicketActionRequiredAdmin, wantRequiredAt: TicketTimeSet, wantResolvedAt: TicketTimeClear, wantReopenUntil: TicketTimeClear, wantClosedAt: TicketTimePreserve},
		{name: "admin closes", status: domain.TicketStatusWaitingUser, action: TicketActionAdminClose, wantStatus: domain.TicketStatusClosed, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimeClear, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimeClear, wantClosedAt: TicketTimeSet},
		{name: "auto closes", status: domain.TicketStatusResolved, action: TicketActionAutoClose, deadline: &past, wantStatus: domain.TicketStatusClosed, wantRequired: domain.TicketActionRequiredNone, wantRequiredAt: TicketTimePreserve, wantResolvedAt: TicketTimePreserve, wantReopenUntil: TicketTimeClear, wantClosedAt: TicketTimeSet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := TransitionTicket(TicketTransitionInput{
				Status:         tt.status,
				Action:         tt.action,
				Now:            now,
				ReopenDeadline: tt.deadline,
			})
			require.NoError(t, err)
			require.Equal(t, tt.status, result.From)
			require.Equal(t, tt.wantStatus, result.To)
			require.Equal(t, tt.wantRequired, result.ActionRequired)
			require.Equal(t, tt.wantRequiredAt, result.ActionRequiredSinceUpdate)
			require.Equal(t, tt.wantResolvedAt, result.ResolvedAtUpdate)
			require.Equal(t, tt.wantReopenUntil, result.ReopenDeadlineUpdate)
			require.Equal(t, tt.wantClosedAt, result.ClosedAtUpdate)
			if result.ReopenDeadlineUpdate == TicketTimeSet {
				require.NotNil(t, result.ReopenDeadline)
				require.Equal(t, now.Add(DefaultTicketReopenWindow), *result.ReopenDeadline)
			}
		})
	}
}

func TestTransitionTicket_RejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	tests := []struct {
		name     string
		status   domain.TicketStatus
		action   TicketTransitionAction
		deadline *time.Time
		wantErr  error
	}{
		{name: "create from existing", status: domain.TicketStatusOpen, action: TicketActionCreate, wantErr: domain.ErrTicketInvalidTransition},
		{name: "claim while waiting", status: domain.TicketStatusWaitingUser, action: TicketActionClaim, wantErr: domain.ErrTicketInvalidTransition},
		{name: "assign resolved", status: domain.TicketStatusResolved, action: TicketActionAssign, wantErr: domain.ErrTicketInvalidTransition},
		{name: "reply resolved", status: domain.TicketStatusResolved, action: TicketActionUserReply, wantErr: domain.ErrTicketInvalidTransition},
		{name: "resolve resolved", status: domain.TicketStatusResolved, action: TicketActionAdminResolve, wantErr: domain.ErrTicketInvalidTransition},
		{name: "user reopen active", status: domain.TicketStatusOpen, action: TicketActionUserReopen, deadline: &future, wantErr: domain.ErrTicketInvalidTransition},
		{name: "admin reopen active", status: domain.TicketStatusOpen, action: TicketActionAdminReopen, wantErr: domain.ErrTicketInvalidTransition},
		{name: "auto close before deadline", status: domain.TicketStatusResolved, action: TicketActionAutoClose, deadline: &future, wantErr: domain.ErrTicketInvalidTransition},
		{name: "closed is terminal", status: domain.TicketStatusClosed, action: TicketActionAdminReopen, wantErr: domain.ErrTicketInvalidTransition},
		{name: "unknown action", status: domain.TicketStatusOpen, action: "unknown", wantErr: domain.ErrTicketInvalidTransition},
		{name: "unknown status", status: "unknown", action: TicketActionInternalNote, wantErr: domain.ErrTicketInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := TransitionTicket(TicketTransitionInput{Status: tt.status, Action: tt.action, Now: now, ReopenDeadline: tt.deadline})
			require.Error(t, err)
			require.True(t, errors.Is(err, tt.wantErr), "expected %v, got %v", tt.wantErr, err)
		})
	}
}

func TestTransitionTicket_UserReopenDeadlineBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	before := now.Add(time.Nanosecond)
	atDeadline := now

	_, err := TransitionTicket(TicketTransitionInput{Status: domain.TicketStatusResolved, Action: TicketActionUserReopen, Now: now, ReopenDeadline: &before})
	require.NoError(t, err)

	_, err = TransitionTicket(TicketTransitionInput{Status: domain.TicketStatusResolved, Action: TicketActionUserReopen, Now: now, ReopenDeadline: &atDeadline})
	require.ErrorIs(t, err, domain.ErrTicketReopenWindowExpired)

	result, err := TransitionTicket(TicketTransitionInput{Status: domain.TicketStatusResolved, Action: TicketActionAutoClose, Now: now, ReopenDeadline: &atDeadline})
	require.NoError(t, err)
	require.Equal(t, domain.TicketStatusClosed, result.To)
}

func TestTicketActionRequiredFor(t *testing.T) {
	t.Parallel()

	require.Equal(t, domain.TicketActionRequiredAdmin, domain.TicketActionRequiredFor(domain.TicketStatusOpen))
	require.Equal(t, domain.TicketActionRequiredAdmin, domain.TicketActionRequiredFor(domain.TicketStatusInProgress))
	require.Equal(t, domain.TicketActionRequiredUser, domain.TicketActionRequiredFor(domain.TicketStatusWaitingUser))
	require.Equal(t, domain.TicketActionRequiredNone, domain.TicketActionRequiredFor(domain.TicketStatusResolved))
	require.Equal(t, domain.TicketActionRequiredNone, domain.TicketActionRequiredFor(domain.TicketStatusClosed))
}
