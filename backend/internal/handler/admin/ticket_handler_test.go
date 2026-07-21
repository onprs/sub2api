package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminTicketHandlerRepositoryStub struct {
	service.TicketRepository
	internalNotes atomic.Int32
}

func (s *adminTicketHandlerRepositoryStub) AppendInternalNote(_ context.Context, params service.InternalTicketNoteParams) (*service.AdminTicketDetail, error) {
	s.internalNotes.Add(1)
	return &service.AdminTicketDetail{
		Ticket: service.Ticket{TicketNo: params.TicketNo, Status: domain.TicketStatusOpen, Version: params.ExpectedVersion + 1},
		Messages: []service.TicketMessage{{
			ID: 1, AuthorRole: domain.TicketActorAdmin, Visibility: domain.TicketVisibilityInternal, Body: params.Body,
		}},
	}, nil
}

func TestAdminTicketHandlerInternalNoteIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminTicketHandlerRepositoryStub{}
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	handler := NewTicketHandler(service.NewTicketService(repo), nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 9})
		c.Next()
	})
	router.POST("/admin/tickets/:ticket_no/messages", handler.Message)

	body := []byte(`{"visibility":"internal","body":"private note","expected_version":1}`)
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/tickets/TK-20260721-ABC234/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "ticket-note-intent")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := call()
	require.Equal(t, http.StatusOK, first.Code)
	second := call()
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, int32(1), repo.internalNotes.Load())
	require.Contains(t, first.Body.String(), `"visibility":"internal"`)
}

func TestAdminTicketHandlerRejectsNextActionOnInternalNote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	repo := &adminTicketHandlerRepositoryStub{}
	handler := NewTicketHandler(service.NewTicketService(repo), nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 9})
		c.Next()
	})
	router.POST("/admin/tickets/:ticket_no/messages", handler.Message)

	body := []byte(`{"visibility":"internal","body":"private note","next_action":"wait_user","expected_version":1}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/tickets/TK-20260721-ABC234/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, repo.internalNotes.Load())
}
