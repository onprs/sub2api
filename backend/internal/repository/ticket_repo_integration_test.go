//go:build integration

package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestTicketRepository_InternalNotesAndReadWatermarkAreIsolated(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-user") + "@example.com", Username: "requester"})
	admin := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-admin") + "@example.com", Username: "operator", Role: service.RoleAdmin})
	createdAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	created, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID:   user.ID,
		Category: domain.TicketCategoryAPIIssue,
		Impact:   domain.TicketImpactBlocked,
		Subject:  "request fails",
		Body:     "initial details",
		Now:      createdAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Ticket.Version)
	require.Equal(t, domain.TicketActionRequiredAdmin, created.Ticket.ActionRequired)

	noteAt := createdAt.Add(time.Minute)
	adminDetail, err := repo.AppendInternalNote(ctx, service.InternalTicketNoteParams{
		AdminID:         admin.ID,
		TicketNo:        created.Ticket.TicketNo,
		Body:            "private diagnosis",
		ExpectedVersion: 1,
		Now:             noteAt,
	})
	require.NoError(t, err)
	require.Len(t, adminDetail.Messages, 2)
	require.Equal(t, domain.TicketVisibilityInternal, adminDetail.Messages[1].Visibility)
	require.Equal(t, int64(2), adminDetail.Ticket.Version)
	require.Equal(t, createdAt, adminDetail.Ticket.LastPublicMessageAt)
	require.Equal(t, int64(0), adminDetail.Ticket.UserNotificationSeq)

	userDetail, err := repo.GetUserTicket(ctx, user.ID, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.Len(t, userDetail.Messages, 1)
	for _, message := range userDetail.Messages {
		require.Equal(t, domain.TicketVisibilityPublic, message.Visibility)
	}
	for _, event := range userDetail.Events {
		require.Equal(t, domain.TicketVisibilityPublic, event.Visibility)
	}

	replyAt := noteAt.Add(time.Minute)
	adminDetail, err = repo.AppendAdminReply(ctx, service.AdminTicketReplyParams{
		AdminID:         admin.ID,
		TicketNo:        created.Ticket.TicketNo,
		Body:            "please retry",
		NextAction:      service.TicketActionAdminReplyWaitUser,
		ExpectedVersion: 2,
		Now:             replyAt,
	})
	require.NoError(t, err)
	require.Equal(t, domain.TicketStatusWaitingUser, adminDetail.Ticket.Status)
	require.Equal(t, int64(1), adminDetail.Ticket.UserNotificationSeq)

	require.NoError(t, repo.MarkUserRead(ctx, service.MarkTicketReadParams{UserID: user.ID, TicketNo: created.Ticket.TicketNo, ObservedNotificationSeq: 1}))
	adminDetail, err = repo.AppendAdminReply(ctx, service.AdminTicketReplyParams{
		AdminID:         admin.ID,
		TicketNo:        created.Ticket.TicketNo,
		Body:            "additional information",
		NextAction:      service.TicketActionAdminReplyKeep,
		ExpectedVersion: 3,
		Now:             replyAt.Add(time.Minute),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), adminDetail.Ticket.UserNotificationSeq)

	require.NoError(t, repo.MarkUserRead(ctx, service.MarkTicketReadParams{UserID: user.ID, TicketNo: created.Ticket.TicketNo, ObservedNotificationSeq: 1}))
	userDetail, err = repo.GetUserTicket(ctx, user.ID, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.Equal(t, int64(1), userDetail.Ticket.UserLastReadSeq)
	require.Equal(t, int64(2), userDetail.Ticket.UserNotificationSeq)
	require.Equal(t, int64(4), userDetail.Ticket.Version, "mark-read must not increment the business version")
	require.True(t, userDetail.Ticket.HasUnreadForUser())
}

func TestTicketRepository_InvalidAttachmentRollsBackReplyAndState(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-atomic") + "@example.com", Username: "atomic"})
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, time.UTC)
	created, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "atomicity", Body: "initial", Now: now,
	})
	require.NoError(t, err)

	_, err = repo.AppendUserReply(ctx, service.UserTicketReplyParams{
		UserID: user.ID, TicketNo: created.Ticket.TicketNo, Body: "must roll back",
		ExpectedVersion: 1, AttachmentTokens: []string{"missing-token"}, Now: now.Add(time.Minute),
	})
	require.ErrorIs(t, err, domain.ErrTicketAttachmentNotFound)

	detail, err := repo.GetUserTicket(ctx, user.ID, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.Equal(t, int64(1), detail.Ticket.Version)
	require.Equal(t, domain.TicketStatusOpen, detail.Ticket.Status)
	require.Len(t, detail.Messages, 1)
	require.Len(t, detail.Events, 1)
}

func TestTicketRepository_ConcurrentClaimHasSingleWinner(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-claim-user") + "@example.com"})
	adminA := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-claim-a") + "@example.com", Role: service.RoleAdmin})
	adminB := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-claim-b") + "@example.com", Role: service.RoleAdmin})
	now := time.Date(2026, 7, 21, 14, 0, 0, 0, time.UTC)
	created, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryAccount, Impact: domain.TicketImpactGeneral,
		Subject: "claim race", Body: "initial", Now: now,
	})
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, adminID := range []int64{adminA.ID, adminB.ID} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			<-start
			_, claimErr := repo.ClaimTicket(ctx, service.TicketVersionActionParams{ActorID: id, TicketNo: created.Ticket.TicketNo, ExpectedVersion: 1, Now: now.Add(time.Minute)})
			results <- claimErr
		}(adminID)
	}
	close(start)
	wg.Wait()
	close(results)

	var succeeded, conflicted int
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, domain.ErrTicketVersionConflict):
			conflicted++
		default:
			require.NoError(t, result)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, conflicted)

	detail, err := repo.GetAdminTicket(ctx, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.NotNil(t, detail.Ticket.AssigneeID)
	require.Equal(t, int64(2), detail.Ticket.Version)
}

func TestTicketRepository_ReferenceOwnershipAndCreateRateLimit(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	owner := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-ref-owner") + "@example.com"})
	other := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-ref-other") + "@example.com"})
	key, err := integrationEntClient.APIKey.Create().
		SetUserID(other.ID).
		SetKey("sk-" + uniqueTestValue(t, "ticket-ref-key")).
		SetName("other key").
		Save(ctx)
	require.NoError(t, err)
	now := time.Date(2026, 7, 21, 14, 30, 0, 0, time.UTC)

	_, err = repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: owner.ID, Category: domain.TicketCategoryAPIIssue, Impact: domain.TicketImpactGeneral,
		Subject: "foreign reference", Body: "must be hidden", References: service.TicketReferenceInput{APIKeyID: &key.ID}, Now: now,
	})
	require.ErrorIs(t, err, domain.ErrTicketReferenceNotFound)
	counts, err := repo.CountUserTickets(ctx, owner.ID)
	require.NoError(t, err)
	require.Zero(t, counts.All)

	for i := 0; i < domain.TicketCreateHourlyLimit; i++ {
		_, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
			UserID: owner.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
			Subject: "rate limit", Body: "message", Now: now.Add(time.Duration(i) * time.Minute),
		})
		require.NoError(t, err)
	}
	_, err = repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: owner.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "rate limit exceeded", Body: "message", Now: now.Add(59 * time.Minute),
	})
	require.ErrorIs(t, err, domain.ErrTicketRateLimited)
}

func TestTicketRepository_ConcurrentRepliesSharePerUserRateLimit(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-reply-limit") + "@example.com"})
	now := time.Date(2026, 7, 21, 14, 40, 0, 0, time.UTC)

	first, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "reply limit first", Body: "initial", Now: now.Add(-40 * time.Minute),
	})
	require.NoError(t, err)
	second, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "reply limit second", Body: "initial", Now: now.Add(-39 * time.Minute),
	})
	require.NoError(t, err)

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO ticket_messages (ticket_id, author_id, author_role, visibility, author_name, body, created_at)
		SELECT $1, $2, 'user', 'public', 'rate-test-user', 'prior reply', $3
		FROM generate_series(1, $4)`,
		first.Ticket.ID, user.ID, now.Add(-30*time.Minute), domain.TicketReplyHourlyLimit-1,
	)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, ticketNo := range []string{first.Ticket.TicketNo, second.Ticket.TicketNo} {
		wg.Add(1)
		go func(number string) {
			defer wg.Done()
			<-start
			_, replyErr := repo.AppendUserReply(ctx, service.UserTicketReplyParams{
				UserID: user.ID, TicketNo: number, Body: "concurrent reply", ExpectedVersion: 1, Now: now,
			})
			results <- replyErr
		}(ticketNo)
	}
	close(start)
	wg.Wait()
	close(results)

	var succeeded, rateLimited int
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, domain.ErrTicketRateLimited):
			rateLimited++
		default:
			require.NoError(t, result)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rateLimited)
}

func TestTicketRepository_AttachmentClaimAndDownloadVisibility(t *testing.T) {
	ctx := context.Background()
	ticketRepo := NewTicketRepository(integrationDB, nil)
	attachmentRepo := NewTicketAttachmentRepository(integrationDB)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-attachment-user") + "@example.com"})
	other := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-attachment-other") + "@example.com"})
	admin := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-attachment-admin") + "@example.com", Role: service.RoleAdmin})
	now := time.Date(2026, 7, 21, 14, 45, 0, 0, time.UTC)

	publicAttachment, err := attachmentRepo.CreatePending(ctx, service.CreatePendingTicketAttachmentParams{
		UploadToken: hashedTestValue(t, "public-upload-token"), UploadedBy: user.ID, UploaderRole: domain.TicketActorUser,
		StorageProvider: "local", ObjectKey: uniqueTestValue(t, "public-object"), OriginalName: "evidence.txt",
		ContentType: "text/plain; charset=utf-8", ByteSize: 8, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	})
	require.NoError(t, err)
	created, err := ticketRepo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryAPIIssue, Impact: domain.TicketImpactGeneral,
		Subject: "attachment visibility", Body: "initial", AttachmentTokens: []string{publicAttachment.UploadToken}, Now: now,
	})
	require.NoError(t, err)

	internalAttachment, err := attachmentRepo.CreatePending(ctx, service.CreatePendingTicketAttachmentParams{
		UploadToken: hashedTestValue(t, "internal-upload-token"), UploadedBy: admin.ID, UploaderRole: domain.TicketActorAdmin,
		StorageProvider: "s3", ObjectKey: uniqueTestValue(t, "internal-object"), OriginalName: "diagnosis.json",
		ContentType: "application/json", ByteSize: 12, SHA256: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	})
	require.NoError(t, err)
	adminDetail, err := ticketRepo.AppendInternalNote(ctx, service.InternalTicketNoteParams{
		AdminID: admin.ID, TicketNo: created.Ticket.TicketNo, Body: "private diagnosis", ExpectedVersion: 1,
		AttachmentTokens: []string{internalAttachment.UploadToken}, Now: now.Add(time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, adminDetail.Messages, 2)
	require.Len(t, adminDetail.Messages[0].Attachments, 1)
	require.Len(t, adminDetail.Messages[1].Attachments, 1)

	userDetail, err := ticketRepo.GetUserTicket(ctx, user.ID, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.Len(t, userDetail.Messages, 1)
	require.Len(t, userDetail.Messages[0].Attachments, 1)
	require.Equal(t, publicAttachment.ID, userDetail.Messages[0].Attachments[0].ID)

	_, err = attachmentRepo.GetForUserDownload(ctx, user.ID, created.Ticket.TicketNo, publicAttachment.ID)
	require.NoError(t, err)
	_, err = attachmentRepo.GetForUserDownload(ctx, other.ID, created.Ticket.TicketNo, publicAttachment.ID)
	require.ErrorIs(t, err, domain.ErrTicketAttachmentNotFound)
	_, err = attachmentRepo.GetForUserDownload(ctx, user.ID, created.Ticket.TicketNo, internalAttachment.ID)
	require.ErrorIs(t, err, domain.ErrTicketAttachmentNotFound)
	adminDownload, err := attachmentRepo.GetForAdminDownload(ctx, created.Ticket.TicketNo, internalAttachment.ID)
	require.NoError(t, err)
	require.Equal(t, "s3", adminDownload.StorageProvider)
}

func TestTicketAttachmentRepository_EnforcesRollingUserUploadLimit(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketAttachmentRepository(integrationDB)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-upload-limit") + "@example.com"})
	now := time.Date(2026, 7, 21, 14, 50, 0, 0, time.UTC)

	_, err := repo.CreatePending(ctx, service.CreatePendingTicketAttachmentParams{
		UploadToken: hashedTestValue(t, "upload-limit-first"), UploadedBy: user.ID, UploaderRole: domain.TicketActorUser,
		StorageProvider: "local", ObjectKey: uniqueTestValue(t, "upload-limit-object-first"), OriginalName: "first.txt",
		ContentType: "text/plain; charset=utf-8", ByteSize: 6, DailyLimitBytes: 10, SHA256: strings.Repeat("c", 64),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	})
	require.NoError(t, err)

	_, err = repo.CreatePending(ctx, service.CreatePendingTicketAttachmentParams{
		UploadToken: hashedTestValue(t, "upload-limit-second"), UploadedBy: user.ID, UploaderRole: domain.TicketActorUser,
		StorageProvider: "local", ObjectKey: uniqueTestValue(t, "upload-limit-object-second"), OriginalName: "second.txt",
		ContentType: "text/plain; charset=utf-8", ByteSize: 5, DailyLimitBytes: 10, SHA256: strings.Repeat("d", 64),
		ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(time.Minute),
	})
	require.ErrorIs(t, err, domain.ErrTicketAttachmentDailyLimit)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ticket_attachments WHERE uploaded_by = $1 AND object_key LIKE $2`,
		user.ID, "upload-limit-object-%",
	).Scan(&count))
	require.Equal(t, 1, count)
}

func TestTicketRepository_AutoCloseResolvedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewTicketRepository(integrationDB, nil)
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-close-user") + "@example.com"})
	admin := mustCreateUser(t, integrationEntClient, &service.User{Email: uniqueTestValue(t, "ticket-close-admin") + "@example.com", Role: service.RoleAdmin})
	now := time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)
	created, err := repo.CreateTicketWithInitialMessage(ctx, service.CreateTicketParams{
		UserID: user.ID, Category: domain.TicketCategoryOther, Impact: domain.TicketImpactGeneral,
		Subject: "auto close", Body: "initial", Now: now,
	})
	require.NoError(t, err)
	resolved, err := repo.ResolveTicket(ctx, service.TicketVersionActionParams{ActorID: admin.ID, TicketNo: created.Ticket.TicketNo, ExpectedVersion: 1, Now: now.Add(time.Minute)})
	require.NoError(t, err)
	require.Equal(t, domain.TicketStatusResolved, resolved.Ticket.Status)

	closed, err := repo.AutoCloseResolvedBatch(ctx, now.Add(8*24*time.Hour), 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, closed, 1)
	detail, err := repo.GetAdminTicket(ctx, created.Ticket.TicketNo)
	require.NoError(t, err)
	require.Equal(t, domain.TicketStatusClosed, detail.Ticket.Status)
	require.Equal(t, int64(3), detail.Ticket.Version)

	closed, err = repo.AutoCloseResolvedBatch(ctx, now.Add(9*24*time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, 0, closed)
}
