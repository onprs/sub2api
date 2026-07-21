package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserTicketDetailDTOCannotSerializeInternalVisibility(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	detail := UserTicketDetailFromService(&service.UserTicketDetail{
		Ticket: service.Ticket{TicketNo: "TK-20260721-ABC234", Status: domain.TicketStatusOpen},
		Messages: []service.TicketMessage{
			{ID: 1, AuthorRole: domain.TicketActorAdmin, Visibility: domain.TicketVisibilityPublic, AuthorName: "support", Body: "public reply", CreatedAt: now},
			{ID: 2, AuthorRole: domain.TicketActorAdmin, Visibility: domain.TicketVisibilityInternal, AuthorName: "support", Body: "private note", CreatedAt: now},
		},
		Events: []service.TicketEvent{
			{ID: 3, ActorRole: domain.TicketActorAdmin, EventType: domain.TicketEventStatusChanged, Visibility: domain.TicketVisibilityPublic, Payload: map[string]any{}, CreatedAt: now},
			{ID: 4, ActorRole: domain.TicketActorAdmin, EventType: domain.TicketEventAssigned, Visibility: domain.TicketVisibilityInternal, Payload: map[string]any{"private": true}, CreatedAt: now},
		},
	})
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "visibility")
	require.NotContains(t, string(raw), "private note")
	require.NotContains(t, string(raw), `"private":true`)
	require.Len(t, detail.Messages, 1)
	require.Len(t, detail.Events, 1)
}

func TestAdminTicketDetailDTOCarriesVisibility(t *testing.T) {
	t.Parallel()

	detail := AdminTicketDetailFromService(&service.AdminTicketDetail{
		Ticket:   service.Ticket{TicketNo: "TK-20260721-ABC234", Status: domain.TicketStatusOpen},
		Messages: []service.TicketMessage{{ID: 1, Visibility: domain.TicketVisibilityInternal}},
		Events:   []service.TicketEvent{{ID: 2, Visibility: domain.TicketVisibilityInternal, Payload: map[string]any{}}},
	})
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"visibility":"internal"`)
}
