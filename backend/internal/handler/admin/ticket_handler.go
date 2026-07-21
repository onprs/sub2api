package admin

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TicketHandler struct {
	service     *service.TicketService
	attachments *service.TicketAttachmentService
	settings    *service.TicketStorageSettingsService
}

func NewTicketHandler(
	ticketService *service.TicketService,
	attachments *service.TicketAttachmentService,
	settings *service.TicketStorageSettingsService,
) *TicketHandler {
	return &TicketHandler{service: ticketService, attachments: attachments, settings: settings}
}

type adminTicketVersionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type adminTicketAssigneeRequest struct {
	AssigneeID      *int64 `json:"assignee_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type adminTicketPriorityRequest struct {
	Priority        domain.TicketPriority `json:"priority"`
	ExpectedVersion int64                 `json:"expected_version"`
}

type adminTicketMessageRequest struct {
	Visibility       domain.TicketVisibility `json:"visibility"`
	Body             string                  `json:"body"`
	NextAction       string                  `json:"next_action"`
	ExpectedVersion  int64                   `json:"expected_version"`
	AttachmentTokens []string                `json:"attachment_tokens"`
}

type adminTicketCloseRequest struct {
	Reason          string `json:"reason"`
	ExpectedVersion int64  `json:"expected_version"`
}

type adminTicketIdempotencyPayload struct {
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

func (h *TicketHandler) Counts(c *gin.Context) {
	counts, err := h.service.CountForAdmin(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AdminTicketCountsFromService(counts))
}

func (h *TicketHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filters := service.AdminTicketListFilters{
		Bucket: strings.TrimSpace(c.Query("bucket")), Status: domain.TicketStatus(strings.TrimSpace(c.Query("status"))),
		Category: domain.TicketCategory(strings.TrimSpace(c.Query("category"))), Impact: domain.TicketImpact(strings.TrimSpace(c.Query("impact"))),
		Priority: domain.TicketPriority(strings.TrimSpace(c.Query("priority"))), Unassigned: parseBoolTicketQuery(c.Query("unassigned")),
		Search: strings.TrimSpace(c.Query("search")),
	}
	if value := strings.TrimSpace(c.Query("assignee_id")); value != "" {
		id, err := parsePositiveTicketInt64(value)
		if err != nil {
			response.ErrorFrom(c, infraerrors.BadRequest("TICKET_ASSIGNEE_INVALID", "invalid assignee_id"))
			return
		}
		filters.AssigneeID = &id
	}
	var err error
	if filters.CreatedFrom, err = parseOptionalTicketTime(c.Query("created_from")); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if filters.CreatedTo, err = parseOptionalTicketTime(c.Query("created_to")); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	items, pageResult, err := h.service.ListForAdmin(c.Request.Context(), pagination.PaginationParams{
		Page: page, PageSize: pageSize, SortBy: c.Query("sort_by"), SortOrder: c.Query("sort_order"),
	}, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.AdminTicket, 0, len(items))
	for i := range items {
		out = append(out, *dto.AdminTicketFromService(&items[i]))
	}
	response.Paginated(c, out, pageResult.Total, page, pageSize)
}

func (h *TicketHandler) Get(c *gin.Context) {
	detail, err := h.service.GetForAdmin(c.Request.Context(), c.Param("ticket_no"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AdminTicketDetailFromService(detail))
}

func (h *TicketHandler) Claim(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketVersionRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_claim", req, func(ctx context.Context) (any, error) {
		detail, err := h.service.Claim(ctx, adminID, c.Param("ticket_no"), req.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		return dto.AdminTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) Assign(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketAssigneeRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_assign", req, func(ctx context.Context) (any, error) {
		detail, err := h.service.Assign(ctx, adminID, c.Param("ticket_no"), req.AssigneeID, req.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		return dto.AdminTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) ChangePriority(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketPriorityRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_priority", req, func(ctx context.Context) (any, error) {
		detail, err := h.service.ChangePriority(ctx, adminID, c.Param("ticket_no"), req.Priority, req.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		return dto.AdminTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) Message(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketMessageRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	if req.Visibility != domain.TicketVisibilityPublic && req.Visibility != domain.TicketVisibilityInternal {
		response.ErrorFrom(c, infraerrors.BadRequest("TICKET_MESSAGE_VISIBILITY_INVALID", "invalid ticket message visibility"))
		return
	}
	if req.Visibility == domain.TicketVisibilityInternal && strings.TrimSpace(req.NextAction) != "" {
		response.ErrorFrom(c, service.ErrTicketNextActionInvalid)
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_message", req, func(ctx context.Context) (any, error) {
		if req.Visibility == domain.TicketVisibilityInternal {
			detail, err := h.service.AddInternalNote(ctx, adminID, c.Param("ticket_no"), &service.InternalTicketNoteInput{
				Body: req.Body, ExpectedVersion: req.ExpectedVersion, AttachmentTokens: req.AttachmentTokens,
			})
			if err != nil {
				return nil, err
			}
			return dto.AdminTicketDetailFromService(detail), nil
		}
		detail, err := h.service.ReplyAsAdmin(ctx, adminID, c.Param("ticket_no"), &service.AdminTicketReplyInput{
			Body: req.Body, NextAction: req.NextAction, ExpectedVersion: req.ExpectedVersion, AttachmentTokens: req.AttachmentTokens,
		})
		if err != nil {
			return nil, err
		}
		return dto.AdminTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) Resolve(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketVersionRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_resolve", req, func(ctx context.Context) (any, error) {
		detail, err := h.service.Resolve(ctx, adminID, c.Param("ticket_no"), req.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		return dto.AdminTicketDetailFromService(detail), nil
	})
}

func (h *TicketHandler) Reopen(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketVersionRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_reopen", req, func(ctx context.Context) (any, error) {
		if err := h.service.ReopenAsAdmin(ctx, adminID, c.Param("ticket_no"), req.ExpectedVersion); err != nil {
			return nil, err
		}
		return gin.H{"message": "ok"}, nil
	})
}

func (h *TicketHandler) Close(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
	if !ok {
		return
	}
	var req adminTicketCloseRequest
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	executeAdminTicketMutation(c, "ticket.admin_close", req, func(ctx context.Context) (any, error) {
		if err := h.service.CloseAsAdmin(ctx, adminID, c.Param("ticket_no"), req.Reason, req.ExpectedVersion); err != nil {
			return nil, err
		}
		return gin.H{"message": "ok"}, nil
	})
}

func (h *TicketHandler) UploadAttachment(c *gin.Context) {
	adminID, ok := adminTicketUserID(c)
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
	attachment, err := h.attachments.Upload(c.Request.Context(), adminID, domain.TicketActorAdmin, service.TicketAttachmentUploadInput{
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
	attachmentID, err := strconv.ParseInt(c.Param("attachment_id"), 10, 64)
	if err != nil || attachmentID <= 0 {
		response.ErrorFrom(c, domain.ErrTicketAttachmentNotFound)
		return
	}
	download, err := h.attachments.DownloadForAdmin(c.Request.Context(), c.Param("ticket_no"), attachmentID)
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

func (h *TicketHandler) GetStorageSettings(c *gin.Context) {
	settings, err := h.settings.Get(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *TicketHandler) TestStorageSettings(c *gin.Context) {
	var req service.TicketAttachmentStorageUpdate
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	if err := h.settings.Test(c.Request.Context(), req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}

func (h *TicketHandler) UpdateStorageSettings(c *gin.Context) {
	var req service.TicketAttachmentStorageUpdate
	if !bindAdminTicketJSON(c, &req) {
		return
	}
	settings, err := h.settings.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func executeAdminTicketMutation(c *gin.Context, scope string, request any, execute func(context.Context) (any, error)) {
	payload := adminTicketIdempotencyPayload{TicketNo: c.Param("ticket_no"), Request: request}
	executeAdminIdempotentJSON(c, scope, payload, service.DefaultWriteIdempotencyTTL(), execute)
}

func bindAdminTicketJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		response.BadRequest(c, "Invalid request")
		return false
	}
	return true
}

func adminTicketUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return 0, false
	}
	return subject.UserID, true
}

func parseBoolTicketQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parsePositiveTicketInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, service.ErrTicketAssigneeInvalid
	}
	return value, nil
}

func parseOptionalTicketTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, infraerrors.BadRequest("TICKET_TIME_FILTER_INVALID", "invalid ticket time filter")
	}
	return &value, nil
}
