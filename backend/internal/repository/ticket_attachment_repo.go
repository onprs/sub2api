package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type ticketAttachmentRepository struct {
	db *sql.DB
}

func NewTicketAttachmentRepository(db *sql.DB) service.TicketAttachmentRepository {
	return &ticketAttachmentRepository{db: db}
}

func (r *ticketAttachmentRepository) CreatePending(ctx context.Context, params service.CreatePendingTicketAttachmentParams) (*service.TicketAttachment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if params.UploaderRole == domain.TicketActorUser && params.DailyLimitBytes > 0 {
		var userID int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, params.UploadedBy).Scan(&userID); err != nil {
			return nil, err
		}
		var uploadedBytes int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(byte_size), 0)
			FROM ticket_attachments
			WHERE uploaded_by = $1 AND uploader_role = 'user' AND created_at >= $2`,
			params.UploadedBy, params.CreatedAt.Add(-24*time.Hour),
		).Scan(&uploadedBytes); err != nil {
			return nil, err
		}
		if uploadedBytes > params.DailyLimitBytes-params.ByteSize {
			return nil, domain.ErrTicketAttachmentDailyLimit
		}
	}

	attachment, err := scanTicketAttachment(tx.QueryRowContext(ctx, `
		INSERT INTO ticket_attachments (
			upload_token, uploaded_by, uploader_role, state, storage_provider, object_key,
			original_name, content_type, byte_size, sha256, expires_at, created_at
		) VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+ticketAttachmentSelectColumns,
		params.UploadToken, params.UploadedBy, string(params.UploaderRole), params.StorageProvider,
		params.ObjectKey, params.OriginalName, params.ContentType, params.ByteSize, params.SHA256,
		params.ExpiresAt, params.CreatedAt,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return attachment, nil
}

func (r *ticketAttachmentRepository) DeletePending(ctx context.Context, attachmentID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ticket_attachments WHERE id = $1 AND state = 'pending'`, attachmentID)
	return err
}

func (r *ticketAttachmentRepository) GetForUserDownload(ctx context.Context, userID int64, ticketNo string, attachmentID int64) (*service.TicketAttachment, error) {
	attachment, err := scanTicketAttachment(r.db.QueryRowContext(ctx, `
		SELECT `+prefixedTicketAttachmentSelectColumns("a")+`
		FROM ticket_attachments a
		JOIN tickets t ON t.id = a.ticket_id
		JOIN ticket_messages m ON m.id = a.message_id AND m.ticket_id = t.id
		WHERE a.id = $1 AND t.ticket_no = $2 AND t.user_id = $3
		  AND a.state = 'attached' AND m.visibility = 'public'`, attachmentID, ticketNo, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketAttachmentNotFound
	}
	return attachment, err
}

func (r *ticketAttachmentRepository) GetForAdminDownload(ctx context.Context, ticketNo string, attachmentID int64) (*service.TicketAttachment, error) {
	attachment, err := scanTicketAttachment(r.db.QueryRowContext(ctx, `
		SELECT `+prefixedTicketAttachmentSelectColumns("a")+`
		FROM ticket_attachments a
		JOIN tickets t ON t.id = a.ticket_id
		WHERE a.id = $1 AND t.ticket_no = $2 AND a.state = 'attached'`, attachmentID, ticketNo))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrTicketAttachmentNotFound
	}
	return attachment, err
}

func (r *ticketAttachmentRepository) GetUsage(ctx context.Context) (service.TicketAttachmentUsage, error) {
	var usage service.TicketAttachmentUsage
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE storage_provider = 'local' AND state <> 'deleting'),
			COALESCE(SUM(byte_size) FILTER (WHERE storage_provider = 'local' AND state <> 'deleting'), 0),
			COUNT(*) FILTER (WHERE storage_provider = 's3' AND state <> 'deleting'),
			COALESCE(SUM(byte_size) FILTER (WHERE storage_provider = 's3' AND state <> 'deleting'), 0)
		FROM ticket_attachments`).Scan(&usage.Local.Files, &usage.Local.Bytes, &usage.S3.Files, &usage.S3.Bytes)
	return usage, err
}

func (r *ticketAttachmentRepository) HasS3Attachments(ctx context.Context) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM ticket_attachments WHERE storage_provider = 's3')`).Scan(&exists)
	return exists, err
}

func (r *ticketAttachmentRepository) ListExpiredForDeletion(ctx context.Context, now time.Time, limit int) ([]service.TicketAttachment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+ticketAttachmentSelectColumns+`
		FROM ticket_attachments
		WHERE state IN ('pending', 'deleting') AND expires_at IS NOT NULL AND expires_at <= $1
		ORDER BY expires_at ASC, id ASC
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.TicketAttachment, 0)
	for rows.Next() {
		item, err := scanTicketAttachment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *ticketAttachmentRepository) MarkDeleting(ctx context.Context, attachmentID int64) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE ticket_attachments SET state = 'deleting'
		WHERE id = $1 AND state IN ('pending', 'deleting')`, attachmentID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (r *ticketAttachmentRepository) DeleteRecord(ctx context.Context, attachmentID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM ticket_attachments WHERE id = $1 AND state = 'deleting'`, attachmentID)
	return err
}

const ticketAttachmentSelectColumns = `
	id, upload_token, uploaded_by, uploader_role, ticket_id, message_id, state,
	storage_provider, object_key, original_name, content_type, byte_size, sha256,
	expires_at, created_at`

func prefixedTicketAttachmentSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.upload_token, ` + alias + `.uploaded_by, ` + alias + `.uploader_role, ` +
		alias + `.ticket_id, ` + alias + `.message_id, ` + alias + `.state, ` + alias + `.storage_provider, ` +
		alias + `.object_key, ` + alias + `.original_name, ` + alias + `.content_type, ` + alias + `.byte_size, ` +
		alias + `.sha256, ` + alias + `.expires_at, ` + alias + `.created_at`
}

func scanTicketAttachment(row ticketRowScanner) (*service.TicketAttachment, error) {
	var (
		attachment   service.TicketAttachment
		uploadedBy   sql.NullInt64
		ticketID     sql.NullInt64
		messageID    sql.NullInt64
		expiresAt    sql.NullTime
		uploaderRole string
	)
	err := row.Scan(
		&attachment.ID, &attachment.UploadToken, &uploadedBy, &uploaderRole,
		&ticketID, &messageID, &attachment.State, &attachment.StorageProvider,
		&attachment.ObjectKey, &attachment.OriginalName, &attachment.ContentType,
		&attachment.ByteSize, &attachment.SHA256, &expiresAt, &attachment.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	attachment.UploadedBy = nullInt64Pointer(uploadedBy)
	attachment.TicketID = nullInt64Pointer(ticketID)
	attachment.MessageID = nullInt64Pointer(messageID)
	attachment.ExpiresAt = nullTimePointer(expiresAt)
	attachment.UploaderRole = domain.TicketActorRole(uploaderRole)
	return &attachment, nil
}
