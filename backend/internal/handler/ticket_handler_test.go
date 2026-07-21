package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ticketHandlerRepositoryStub struct {
	service.TicketRepository
	creates atomic.Int32
}

func (s *ticketHandlerRepositoryStub) CreateTicketWithInitialMessage(_ context.Context, params service.CreateTicketParams) (*service.UserTicketDetail, error) {
	s.creates.Add(1)
	return &service.UserTicketDetail{
		Ticket:   service.Ticket{TicketNo: "TK-20260721-ABC234", Subject: params.Subject, Category: params.Category, Impact: params.Impact, Status: domain.TicketStatusOpen, Version: 1},
		Messages: []service.TicketMessage{{ID: 1, AuthorRole: domain.TicketActorUser, Visibility: domain.TicketVisibilityPublic, Body: params.Body}},
	}, nil
}

func TestTicketHandlerCreateIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &ticketHandlerRepositoryStub{}
	coordinator := service.NewIdempotencyCoordinator(newUserMemoryIdempotencyRepoStub(), service.DefaultIdempotencyConfig())
	service.SetDefaultIdempotencyCoordinator(coordinator)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })

	handler := NewTicketHandler(service.NewTicketService(repo), nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/tickets", handler.Create)

	body := []byte(`{"category":"other","impact":"general","subject":"help","body":"details"}`)
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/tickets", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "ticket-create-intent")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}

	first := call()
	require.Equal(t, http.StatusOK, first.Code)
	second := call()
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, int32(1), repo.creates.Load())
}

func TestTicketHandlerUploadMapsRequestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Ticketing.Enabled = true
	cfg.Ticketing.Attachments.MaxFileBytes = 16
	settings := service.NewTicketStorageSettingsService(nil, nil, nil, nil, cfg)
	attachments := service.NewTicketAttachmentService(nil, settings, cfg)
	handler := NewTicketHandler(service.NewTicketService(&ticketHandlerRepositoryStub{}), attachments)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "oversized.txt")
	require.NoError(t, err)
	_, err = part.Write(make([]byte, 1024*1024+32))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	router.POST("/tickets/attachments", handler.UploadAttachment)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/tickets/attachments", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "TICKET_ATTACHMENT_TOO_LARGE")
}

func TestTicketHandlerRequireEnabledRejectsDisabledDeployment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Ticketing.Enabled = false
	settings := service.NewTicketStorageSettingsService(nil, nil, nil, nil, cfg)
	attachments := service.NewTicketAttachmentService(nil, settings, cfg)
	handler := NewTicketHandler(service.NewTicketService(&ticketHandlerRepositoryStub{}), attachments)
	router := gin.New()
	router.GET("/tickets/counts", handler.RequireEnabled, handler.Counts)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/tickets/counts", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "TICKETING_DISABLED")
}

func TestTicketHandlerRequiresAuthenticatedSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)

	handler := NewTicketHandler(service.NewTicketService(&ticketHandlerRepositoryStub{}), nil)
	router := gin.New()
	router.GET("/tickets/counts", handler.Counts)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/tickets/counts", nil))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
