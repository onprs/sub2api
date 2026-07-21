package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type TicketService struct {
	repo               TicketRepository
	now                func() time.Time
	maxFilesPerMessage int
}

func NewTicketService(repo TicketRepository) *TicketService {
	return &TicketService{repo: repo, now: time.Now, maxFilesPerMessage: 3}
}

func NewConfiguredTicketService(repo TicketRepository, cfg *config.Config) *TicketService {
	svc := NewTicketService(repo)
	if cfg != nil && cfg.Ticketing.Attachments.MaxFilesPerMessage > 0 {
		svc.maxFilesPerMessage = cfg.Ticketing.Attachments.MaxFilesPerMessage
	}
	return svc
}

type CreateTicketInput struct {
	Category           domain.TicketCategory
	Impact             domain.TicketImpact
	Subject            string
	Body               string
	UsageLogID         *int64
	APIKeyID           *int64
	PaymentOrderID     *int64
	UserSubscriptionID *int64
	AttachmentTokens   []string
}

type TicketReplyInput struct {
	Body             string
	ExpectedVersion  int64
	AttachmentTokens []string
}

type AdminTicketReplyInput struct {
	Body             string
	NextAction       string
	ExpectedVersion  int64
	AttachmentTokens []string
}

type InternalTicketNoteInput struct {
	Body             string
	ExpectedVersion  int64
	AttachmentTokens []string
}

func (s *TicketService) Create(ctx context.Context, userID int64, input *CreateTicketInput) (*UserTicketDetail, error) {
	if input == nil || userID <= 0 {
		return nil, ErrTicketInputRequired
	}
	if !input.Category.Valid() {
		return nil, domain.ErrTicketInvalidCategory
	}
	if !input.Impact.Valid() {
		return nil, domain.ErrTicketInvalidImpact
	}

	subject, err := normalizeTicketText(input.Subject, domain.TicketMaxSubjectLength, ErrTicketSubjectInvalid)
	if err != nil {
		return nil, err
	}
	body, err := normalizeTicketText(input.Body, domain.TicketMaxMessageLength, ErrTicketBodyInvalid)
	if err != nil {
		return nil, err
	}
	tokens, err := normalizeTicketAttachmentTokens(input.AttachmentTokens, s.maxFilesPerMessage)
	if err != nil {
		return nil, err
	}
	if !validTicketReferenceIDs(input.UsageLogID, input.APIKeyID, input.PaymentOrderID, input.UserSubscriptionID) {
		return nil, domain.ErrTicketReferenceNotFound
	}

	return s.repo.CreateTicketWithInitialMessage(ctx, CreateTicketParams{
		UserID:   userID,
		Category: input.Category,
		Impact:   input.Impact,
		Subject:  subject,
		Body:     body,
		References: TicketReferenceInput{
			UsageLogID:         input.UsageLogID,
			APIKeyID:           input.APIKeyID,
			PaymentOrderID:     input.PaymentOrderID,
			UserSubscriptionID: input.UserSubscriptionID,
		},
		AttachmentTokens: tokens,
		Now:              s.now(),
	})
}

func (s *TicketService) GetForUser(ctx context.Context, userID int64, ticketNo string) (*UserTicketDetail, error) {
	if userID <= 0 || strings.TrimSpace(ticketNo) == "" {
		return nil, ErrTicketNotFound
	}
	return s.repo.GetUserTicket(ctx, userID, strings.TrimSpace(ticketNo))
}

func (s *TicketService) GetForAdmin(ctx context.Context, ticketNo string) (*AdminTicketDetail, error) {
	if strings.TrimSpace(ticketNo) == "" {
		return nil, ErrTicketNotFound
	}
	return s.repo.GetAdminTicket(ctx, strings.TrimSpace(ticketNo))
}

func (s *TicketService) ListForUser(ctx context.Context, userID int64, page pagination.PaginationParams, filters UserTicketListFilters) ([]Ticket, *pagination.PaginationResult, error) {
	if filters.Status != "" && !filters.Status.Valid() {
		return nil, nil, domain.ErrTicketInvalidTransition
	}
	if filters.Category != "" && !filters.Category.Valid() {
		return nil, nil, domain.ErrTicketInvalidCategory
	}
	filters.Search = strings.TrimSpace(filters.Search)
	return s.repo.ListUserTickets(ctx, userID, page, filters)
}

func (s *TicketService) ListForAdmin(ctx context.Context, page pagination.PaginationParams, filters AdminTicketListFilters) ([]Ticket, *pagination.PaginationResult, error) {
	if filters.Status != "" && !filters.Status.Valid() {
		return nil, nil, domain.ErrTicketInvalidTransition
	}
	if filters.Category != "" && !filters.Category.Valid() {
		return nil, nil, domain.ErrTicketInvalidCategory
	}
	if filters.Impact != "" && !filters.Impact.Valid() {
		return nil, nil, domain.ErrTicketInvalidImpact
	}
	if filters.Priority != "" && !filters.Priority.Valid() {
		return nil, nil, domain.ErrTicketInvalidPriority
	}
	filters.Search = strings.TrimSpace(filters.Search)
	return s.repo.ListAdminTickets(ctx, page, filters)
}

func (s *TicketService) CountForUser(ctx context.Context, userID int64) (UserTicketCounts, error) {
	return s.repo.CountUserTickets(ctx, userID)
}

func (s *TicketService) CountForAdmin(ctx context.Context) (AdminTicketCounts, error) {
	return s.repo.CountAdminTickets(ctx)
}

func (s *TicketService) ReplyAsUser(ctx context.Context, userID int64, ticketNo string, input *TicketReplyInput) (*UserTicketDetail, error) {
	if input == nil {
		return nil, ErrTicketInputRequired
	}
	body, err := normalizeTicketText(input.Body, domain.TicketMaxMessageLength, ErrTicketBodyInvalid)
	if err != nil {
		return nil, err
	}
	if err := validateTicketVersion(input.ExpectedVersion); err != nil {
		return nil, err
	}
	tokens, err := normalizeTicketAttachmentTokens(input.AttachmentTokens, s.maxFilesPerMessage)
	if err != nil {
		return nil, err
	}
	return s.repo.AppendUserReply(ctx, UserTicketReplyParams{
		UserID:           userID,
		TicketNo:         strings.TrimSpace(ticketNo),
		Body:             body,
		ExpectedVersion:  input.ExpectedVersion,
		AttachmentTokens: tokens,
		Now:              s.now(),
	})
}

func (s *TicketService) ReplyAsAdmin(ctx context.Context, adminID int64, ticketNo string, input *AdminTicketReplyInput) (*AdminTicketDetail, error) {
	if input == nil {
		return nil, ErrTicketInputRequired
	}
	body, err := normalizeTicketText(input.Body, domain.TicketMaxMessageLength, ErrTicketBodyInvalid)
	if err != nil {
		return nil, err
	}
	if err := validateTicketVersion(input.ExpectedVersion); err != nil {
		return nil, err
	}
	tokens, err := normalizeTicketAttachmentTokens(input.AttachmentTokens, s.maxFilesPerMessage)
	if err != nil {
		return nil, err
	}
	action, err := ticketAdminReplyAction(input.NextAction)
	if err != nil {
		return nil, err
	}
	return s.repo.AppendAdminReply(ctx, AdminTicketReplyParams{
		AdminID:          adminID,
		TicketNo:         strings.TrimSpace(ticketNo),
		Body:             body,
		NextAction:       action,
		ExpectedVersion:  input.ExpectedVersion,
		AttachmentTokens: tokens,
		Now:              s.now(),
	})
}

func (s *TicketService) AddInternalNote(ctx context.Context, adminID int64, ticketNo string, input *InternalTicketNoteInput) (*AdminTicketDetail, error) {
	if input == nil {
		return nil, ErrTicketInputRequired
	}
	body, err := normalizeTicketText(input.Body, domain.TicketMaxMessageLength, ErrTicketBodyInvalid)
	if err != nil {
		return nil, err
	}
	if err := validateTicketVersion(input.ExpectedVersion); err != nil {
		return nil, err
	}
	tokens, err := normalizeTicketAttachmentTokens(input.AttachmentTokens, s.maxFilesPerMessage)
	if err != nil {
		return nil, err
	}
	return s.repo.AppendInternalNote(ctx, InternalTicketNoteParams{
		AdminID:          adminID,
		TicketNo:         strings.TrimSpace(ticketNo),
		Body:             body,
		ExpectedVersion:  input.ExpectedVersion,
		AttachmentTokens: tokens,
		Now:              s.now(),
	})
}

func (s *TicketService) Claim(ctx context.Context, adminID int64, ticketNo string, expectedVersion int64) (*AdminTicketDetail, error) {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return nil, err
	}
	return s.repo.ClaimTicket(ctx, TicketVersionActionParams{ActorID: adminID, TicketNo: strings.TrimSpace(ticketNo), ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) Assign(ctx context.Context, adminID int64, ticketNo string, assigneeID *int64, expectedVersion int64) (*AdminTicketDetail, error) {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return nil, err
	}
	if assigneeID != nil && *assigneeID <= 0 {
		return nil, ErrTicketAssigneeInvalid
	}
	return s.repo.AssignTicket(ctx, AssignTicketParams{AdminID: adminID, TicketNo: strings.TrimSpace(ticketNo), AssigneeID: assigneeID, ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) ChangePriority(ctx context.Context, adminID int64, ticketNo string, priority domain.TicketPriority, expectedVersion int64) (*AdminTicketDetail, error) {
	if !priority.Valid() {
		return nil, domain.ErrTicketInvalidPriority
	}
	if err := validateTicketVersion(expectedVersion); err != nil {
		return nil, err
	}
	return s.repo.ChangePriority(ctx, ChangeTicketPriorityParams{AdminID: adminID, TicketNo: strings.TrimSpace(ticketNo), Priority: priority, ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) Resolve(ctx context.Context, adminID int64, ticketNo string, expectedVersion int64) (*AdminTicketDetail, error) {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return nil, err
	}
	return s.repo.ResolveTicket(ctx, TicketVersionActionParams{ActorID: adminID, TicketNo: strings.TrimSpace(ticketNo), ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) CloseAsUser(ctx context.Context, userID int64, ticketNo string, expectedVersion int64) error {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return err
	}
	return s.repo.CloseTicket(ctx, CloseTicketParams{ActorID: userID, ActorRole: domain.TicketActorUser, TicketNo: strings.TrimSpace(ticketNo), ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) CloseAsAdmin(ctx context.Context, adminID int64, ticketNo, reason string, expectedVersion int64) error {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return err
	}
	reason, err := normalizeTicketText(reason, domain.TicketMaxCloseReasonLength, domain.ErrTicketCloseReasonRequired)
	if err != nil {
		return err
	}
	return s.repo.CloseTicket(ctx, CloseTicketParams{ActorID: adminID, ActorRole: domain.TicketActorAdmin, TicketNo: strings.TrimSpace(ticketNo), Reason: reason, ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) ReopenAsUser(ctx context.Context, userID int64, ticketNo, body string, expectedVersion int64) error {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return err
	}
	body, err := normalizeTicketText(body, domain.TicketMaxMessageLength, ErrTicketBodyInvalid)
	if err != nil {
		return err
	}
	return s.repo.ReopenTicket(ctx, ReopenTicketParams{ActorID: userID, ActorRole: domain.TicketActorUser, TicketNo: strings.TrimSpace(ticketNo), Body: body, ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) ReopenAsAdmin(ctx context.Context, adminID int64, ticketNo string, expectedVersion int64) error {
	if err := validateTicketVersion(expectedVersion); err != nil {
		return err
	}
	return s.repo.ReopenTicket(ctx, ReopenTicketParams{ActorID: adminID, ActorRole: domain.TicketActorAdmin, TicketNo: strings.TrimSpace(ticketNo), ExpectedVersion: expectedVersion, Now: s.now()})
}

func (s *TicketService) MarkRead(ctx context.Context, userID int64, ticketNo string, observedSeq int64) error {
	if observedSeq < 0 {
		return ErrTicketInputRequired
	}
	return s.repo.MarkUserRead(ctx, MarkTicketReadParams{UserID: userID, TicketNo: strings.TrimSpace(ticketNo), ObservedNotificationSeq: observedSeq})
}

func (s *TicketService) AutoCloseResolved(ctx context.Context, limit int) (int, error) {
	return s.repo.AutoCloseResolvedBatch(ctx, s.now(), limit)
}

func normalizeTicketText(value string, maxRunes int, validationErr error) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxRunes {
		return "", validationErr
	}
	return value, nil
}

func validateTicketVersion(version int64) error {
	if version <= 0 {
		return ErrTicketExpectedVersion
	}
	return nil
}

func validTicketReferenceIDs(ids ...*int64) bool {
	for _, id := range ids {
		if id != nil && *id <= 0 {
			return false
		}
	}
	return true
}

func normalizeTicketAttachmentTokens(tokens []string, maxFiles int) ([]string, error) {
	if maxFiles <= 0 {
		maxFiles = 3
	}
	if len(tokens) > maxFiles {
		return nil, domain.ErrTicketAttachmentLimitExceeded
	}
	seen := make(map[string]struct{}, len(tokens))
	normalized := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" || len(token) > 64 {
			return nil, domain.ErrTicketAttachmentNotFound
		}
		if _, ok := seen[token]; ok {
			return nil, domain.ErrTicketAttachmentAlreadyClaimed
		}
		seen[token] = struct{}{}
		normalized = append(normalized, token)
	}
	return normalized, nil
}

func ticketAdminReplyAction(nextAction string) (TicketTransitionAction, error) {
	switch strings.TrimSpace(nextAction) {
	case "wait_user":
		return TicketActionAdminReplyWaitUser, nil
	case "keep_processing":
		return TicketActionAdminReplyKeep, nil
	case "resolve":
		return TicketActionAdminReplyResolve, nil
	default:
		return "", ErrTicketNextActionInvalid
	}
}
