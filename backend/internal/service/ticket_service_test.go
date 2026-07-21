package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type ticketRepositoryStub struct {
	TicketRepository
	createParams     *CreateTicketParams
	adminReplyParams *AdminTicketReplyParams
}

func (s *ticketRepositoryStub) CreateTicketWithInitialMessage(_ context.Context, params CreateTicketParams) (*UserTicketDetail, error) {
	s.createParams = &params
	return &UserTicketDetail{Ticket: Ticket{TicketNo: "TK-20260721-ABC234"}}, nil
}

func (s *ticketRepositoryStub) AppendAdminReply(_ context.Context, params AdminTicketReplyParams) (*AdminTicketDetail, error) {
	s.adminReplyParams = &params
	return &AdminTicketDetail{}, nil
}

func TestTicketServiceCreateNormalizesInput(t *testing.T) {
	t.Parallel()

	repo := &ticketRepositoryStub{}
	svc := NewTicketService(repo)
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	usageLogID := int64(42)
	result, err := svc.Create(context.Background(), 7, &CreateTicketInput{
		Category:         domain.TicketCategoryAPIIssue,
		Impact:           domain.TicketImpactBlocked,
		Subject:          "  request repeatedly fails  ",
		Body:             "  failure details  ",
		UsageLogID:       &usageLogID,
		AttachmentTokens: []string{" token-a "},
	})
	require.NoError(t, err)
	require.Equal(t, "TK-20260721-ABC234", result.Ticket.TicketNo)
	require.NotNil(t, repo.createParams)
	require.Equal(t, "request repeatedly fails", repo.createParams.Subject)
	require.Equal(t, "failure details", repo.createParams.Body)
	require.Equal(t, []string{"token-a"}, repo.createParams.AttachmentTokens)
	require.Equal(t, now, repo.createParams.Now)
	require.Equal(t, usageLogID, *repo.createParams.References.UsageLogID)
}

func TestTicketServiceCreateAllowsOptionalReferences(t *testing.T) {
	t.Parallel()

	for _, category := range []domain.TicketCategory{
		domain.TicketCategorySubscription,
		domain.TicketCategoryOther,
	} {
		category := category
		t.Run(string(category), func(t *testing.T) {
			t.Parallel()
			repo := &ticketRepositoryStub{}
			svc := NewTicketService(repo)

			_, err := svc.Create(context.Background(), 7, &CreateTicketInput{
				Category: category,
				Impact:   domain.TicketImpactGeneral,
				Subject:  "question without reference",
				Body:     "reference is optional",
			})

			require.NoError(t, err)
			require.NotNil(t, repo.createParams)
			require.Nil(t, repo.createParams.References.UsageLogID)
			require.Nil(t, repo.createParams.References.APIKeyID)
			require.Nil(t, repo.createParams.References.PaymentOrderID)
			require.Nil(t, repo.createParams.References.UserSubscriptionID)
		})
	}
}

func TestConfiguredTicketServiceUsesAttachmentLimit(t *testing.T) {
	t.Parallel()

	repo := &ticketRepositoryStub{}
	cfg := &config.Config{}
	cfg.Ticketing.Attachments.MaxFilesPerMessage = 1
	svc := NewConfiguredTicketService(repo, cfg)
	_, err := svc.Create(context.Background(), 1, &CreateTicketInput{
		Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "configured limit", Body: "body", AttachmentTokens: []string{"one", "two"},
	})
	require.ErrorIs(t, err, domain.ErrTicketAttachmentLimitExceeded)
	require.Nil(t, repo.createParams)
}

func TestTicketServiceCreateRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	svc := NewTicketService(&ticketRepositoryStub{})
	tests := []struct {
		name    string
		input   *CreateTicketInput
		wantErr error
	}{
		{name: "nil", input: nil, wantErr: ErrTicketInputRequired},
		{name: "category", input: &CreateTicketInput{Category: "invalid", Impact: domain.TicketImpactGeneral, Subject: "s", Body: "b"}, wantErr: domain.ErrTicketInvalidCategory},
		{name: "impact", input: &CreateTicketInput{Category: domain.TicketCategoryOther, Impact: "invalid", Subject: "s", Body: "b"}, wantErr: domain.ErrTicketInvalidImpact},
		{name: "subject", input: &CreateTicketInput{Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral, Subject: " ", Body: "b"}, wantErr: ErrTicketSubjectInvalid},
		{name: "body", input: &CreateTicketInput{Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral, Subject: "s", Body: " "}, wantErr: ErrTicketBodyInvalid},
		{name: "duplicate token", input: &CreateTicketInput{Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral, Subject: "s", Body: "b", AttachmentTokens: []string{"same", " same "}}, wantErr: domain.ErrTicketAttachmentAlreadyClaimed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.Create(context.Background(), 1, tt.input)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestTicketServiceAdminReplyMapsNextAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		nextAction string
		want       TicketTransitionAction
	}{
		{nextAction: "wait_user", want: TicketActionAdminReplyWaitUser},
		{nextAction: "keep_processing", want: TicketActionAdminReplyKeep},
		{nextAction: "resolve", want: TicketActionAdminReplyResolve},
	}
	for _, tt := range tests {
		t.Run(tt.nextAction, func(t *testing.T) {
			t.Parallel()
			repo := &ticketRepositoryStub{}
			svc := NewTicketService(repo)
			_, err := svc.ReplyAsAdmin(context.Background(), 9, "TK-1", &AdminTicketReplyInput{Body: "reply", NextAction: tt.nextAction, ExpectedVersion: 1})
			require.NoError(t, err)
			require.Equal(t, tt.want, repo.adminReplyParams.NextAction)
		})
	}
}

func TestNewTicketNumber(t *testing.T) {
	t.Parallel()

	number, err := NewTicketNumber(time.Date(2026, 7, 21, 0, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60)))
	require.NoError(t, err)
	require.Regexp(t, `^TK-20260721-[A-HJ-NP-Z2-9]{6}$`, number)
}
