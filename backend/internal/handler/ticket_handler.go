package handler

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TicketHandler struct {
	service     *service.TicketService
	attachments *service.TicketAttachmentService
}

func NewTicketHandler(ticketService *service.TicketService, attachments *service.TicketAttachmentService) *TicketHandler {
	return &TicketHandler{service: ticketService, attachments: attachments}
}

type createTicketRequest struct {
	Category           domain.TicketCategory `json:"category"`
	Impact             domain.TicketImpact   `json:"impact"`
	Subject            string                `json:"subject"`
	Body               string                `json:"body"`
	UsageLogID         *int64                `json:"usage_log_id"`
	APIKeyID           *int64                `json:"api_key_id"`
	PaymentOrderID     *int64                `json:"payment_order_id"`
	UserSubscriptionID *int64                `json:"user_subscription_id"`
	AttachmentTokens   []string              `json:"attachment_tokens"`
}

type ticketMessageRequest struct {
	Body             string   `json:"body"`
	ExpectedVersion  int64    `json:"expected_version"`
	AttachmentTokens []string `json:"attachment_tokens"`
}

type ticketVersionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type ticketReopenRequest struct {
	Body            string `json:"body"`
	ExpectedVersion int64  `json:"expected_version"`
}

type ticketReadRequest struct {
	ObservedNotificationSeq int64 `json:"observed_notification_seq"`
}

type ticketIdempotencyPayload struct {
	TicketNo string `json:"ticket_no"`
	Request  any    `json:"request"`
}

func (h *TicketHandler) RequireEnabled(c *gin.Context) {
	if h == nil || h.attachments == nil || h.attachments.Capabilities(c.Request.Context()).Enabled {
		c.Next()
		return
	}
	response.ErrorFrom(c, domain.ErrTicketingDisabled)
	c.Abort()
}

func (h *TicketHandler) Capabilities(c *gin.Context) {
	capabilities := h.attachments.Capabilities(c.Request.Context())
	response.Success(c, dto.TicketCapabilities{
		Enabled: capabilities.Enabled, AttachmentsEnabled: capabilities.AttachmentsEnabled,
		MaxFileBytes: capabilities.MaxFileBytes, MaxFilesPerMessage: capabilities.MaxFilesPerMessage,
		MaxTicketBytes: capabilities.MaxTicketBytes, PollingHintSeconds: capabilities.PollingHintSeconds,
		DetailPollingSeconds: capabilities.DetailPollingSeconds,
	})
}

func (h *TicketHandler) Counts(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	counts, err := h.service.CountForUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserTicketCountsFromService(counts))
}

func (h *TicketHandler) List(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, pageResult, err := h.service.ListForUser(c.Request.Context(), userID, pagination.PaginationParams{
		Page: page, PageSize: pageSize, SortBy: c.Query("sort_by"), SortOrder: c.Query("sort_order"),
	}, service.UserTicketListFilters{
		Bucket: strings.TrimSpace(c.Query("bucket")), Status: domain.TicketStatus(strings.TrimSpace(c.Query("status"))),
		Category: domain.TicketCategory(strings.TrimSpace(c.Query("category"))), Search: strings.TrimSpace(c.Query("search")),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.UserTicket, 0, len(items))
	for i := range items {
		out = append(out, *dto.UserTicketFromService(&items[i]))
	}
	response.Paginated(c, out, pageResult.Total, page, pageSize)
}

func (h *TicketHandler) Create(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	var req createTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	executeUserIdempotentJSON(c, "ticket.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		created, err := h.service.Create(ctx, userID, &service.CreateTicketInput{
			Category: req.Category, Impact: req.Impact, Subject: req.Subject, Body: req.Body,
			UsageLogID: req.UsageLogID, APIKeyID: req.APIKeyID, PaymentOrderID: req.PaymentOrderID,
			UserSubscriptionID: req.UserSubscriptionID, AttachmentTokens: req.AttachmentTokens,
		})
		if err != nil {
			return nil, err
		}
		return dto.UserTicketDetailFromService(created), nil
	})
}

func (h *TicketHandler) Get(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	detail, err := h.service.GetForUser(c.Request.Context(), userID, c.Param("ticket_no"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserTicketDetailFromService(detail))
}

func (h *TicketHandler) MarkRead(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	var req ticketReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	if err := h.service.MarkRead(c.Request.Context(), userID, c.Param("ticket_no"), req.ObservedNotificationSeq); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

func (h *TicketHandler) Reply(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	var req ticketMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	payload := ticketIdempotencyPayload{TicketNo: c.Param("ticket_no"), Request: req}
	executeUserIdempotentJSON(c, "ticket.user_reply", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		detail, err := h.service.ReplyAsUser(ctx, userID, c.Param("ticket_no"), &service.TicketReplyInput{
			Body: req.Body, ExpectedVersion: req.ExpectedVersion, AttachmentTokens: req.AttachmentTokens,
		})
		if err != nil {
			return nil, err
		}
		return dto.UserTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) Close(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	var req ticketVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	payload := ticketIdempotencyPayload{TicketNo: c.Param("ticket_no"), Request: req}
	executeUserIdempotentJSON(c, "ticket.user_close", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.service.CloseAsUser(ctx, userID, c.Param("ticket_no"), req.ExpectedVersion); err != nil {
			return nil, err
		}
		return gin.H{"message": "ok"}, nil
	})
}

func (h *TicketHandler) Reopen(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	var req ticketReopenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	payload := ticketIdempotencyPayload{TicketNo: c.Param("ticket_no"), Request: req}
	executeUserIdempotentJSON(c, "ticket.user_reopen", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		if err := h.service.ReopenAsUser(ctx, userID, c.Param("ticket_no"), req.Body, req.ExpectedVersion); err != nil {
			return nil, err
		}
		return gin.H{"message": "ok"}, nil
	})
}

func (h *TicketHandler) UploadAttachment(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	capabilities := h.attachments.Capabilities(c.Request.Context())
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, capabilities.MaxFileBytes+1024*1024)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			response.ErrorFrom(c, domain.ErrTicketAttachmentTooLarge)
		} else {
			response.ErrorFrom(c, domain.ErrTicketAttachmentInvalidType)
		}
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.ErrorFrom(c, domain.ErrTicketAttachmentInvalidType)
		return
	}
	defer func() { _ = file.Close() }()
	attachment, err := h.attachments.Upload(c.Request.Context(), userID, domain.TicketActorUser, service.TicketAttachmentUploadInput{
		OriginalName: fileHeader.Filename, Reader: file,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.PendingTicketAttachment{
		UploadToken: attachment.UploadToken, OriginalName: attachment.OriginalName,
		ContentType: attachment.ContentType, ByteSize: attachment.ByteSize, ExpiresAt: attachment.ExpiresAt,
	})
}

func (h *TicketHandler) DownloadAttachment(c *gin.Context) {
	userID, ok := ticketUserID(c)
	if !ok {
		return
	}
	attachmentID, err := strconv.ParseInt(c.Param("attachment_id"), 10, 64)
	if err != nil || attachmentID <= 0 {
		response.ErrorFrom(c, domain.ErrTicketAttachmentNotFound)
		return
	}
	download, err := h.attachments.DownloadForUser(c.Request.Context(), userID, c.Param("ticket_no"), attachmentID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	defer func() { _ = download.Body.Close() }()
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": download.Attachment.OriginalName})
	c.DataFromReader(http.StatusOK, download.Attachment.ByteSize, download.Attachment.ContentType, download.Body, map[string]string{
		"Content-Disposition":    disposition,
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, no-store",
	})
}

func ticketUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return 0, false
	}
	return subject.UserID, true
}
