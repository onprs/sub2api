package service

import (
	"context"
	"io"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type TicketAttachmentStorageMode string

const (
	TicketAttachmentModeDisabled TicketAttachmentStorageMode = "disabled"
	TicketAttachmentModeLocal    TicketAttachmentStorageMode = "local"
	TicketAttachmentModeS3       TicketAttachmentStorageMode = "s3"
)

const TicketAttachmentStorageSettingKey = "ticket_attachment_storage_config"

type TicketAttachmentS3Config struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	Prefix          string `json:"prefix"`
	ForcePathStyle  bool   `json:"force_path_style"`
}

type TicketAttachmentStorageConfig struct {
	Mode TicketAttachmentStorageMode `json:"mode"`
	S3   TicketAttachmentS3Config    `json:"s3"`
}

type TicketAttachmentStorageUpdate struct {
	Mode TicketAttachmentStorageMode `json:"mode"`
	S3   TicketAttachmentS3Config    `json:"s3"`
}

type TicketAttachmentProviderUsage struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

type TicketAttachmentUsage struct {
	Local TicketAttachmentProviderUsage `json:"local"`
	S3    TicketAttachmentProviderUsage `json:"s3"`
}

type TicketAttachmentStorageView struct {
	Mode               TicketAttachmentStorageMode `json:"mode"`
	AttachmentsEnabled bool                        `json:"attachments_enabled"`
	Local              TicketAttachmentLocalView   `json:"local"`
	S3                 TicketAttachmentS3View      `json:"s3"`
	Usage              TicketAttachmentUsage       `json:"usage"`
}

type TicketAttachmentLocalView struct {
	Configured   bool   `json:"configured"`
	Writable     bool   `json:"writable"`
	DisplayPath  string `json:"display_path"`
	SharedVolume bool   `json:"shared_volume"`
}

type TicketAttachmentS3View struct {
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	AccessKeyIDMasked string `json:"access_key_id_masked"`
	SecretConfigured  bool   `json:"secret_configured"`
	Prefix            string `json:"prefix"`
	ForcePathStyle    bool   `json:"force_path_style"`
}

type TicketAttachment struct {
	ID              int64
	UploadToken     string
	UploadedBy      *int64
	UploaderRole    domain.TicketActorRole
	TicketID        *int64
	MessageID       *int64
	State           string
	StorageProvider string
	ObjectKey       string
	OriginalName    string
	ContentType     string
	ByteSize        int64
	SHA256          string
	ExpiresAt       *time.Time
	CreatedAt       time.Time
}

type CreatePendingTicketAttachmentParams struct {
	UploadToken     string
	UploadedBy      int64
	UploaderRole    domain.TicketActorRole
	StorageProvider string
	ObjectKey       string
	OriginalName    string
	ContentType     string
	ByteSize        int64
	DailyLimitBytes int64
	SHA256          string
	ExpiresAt       time.Time
	CreatedAt       time.Time
}

type TicketAttachmentUploadInput struct {
	OriginalName string
	Reader       io.Reader
}

type TicketAttachmentDownload struct {
	Attachment TicketAttachment
	Body       io.ReadCloser
}

type TicketAttachmentRepository interface {
	CreatePending(ctx context.Context, params CreatePendingTicketAttachmentParams) (*TicketAttachment, error)
	DeletePending(ctx context.Context, attachmentID int64) error
	GetForUserDownload(ctx context.Context, userID int64, ticketNo string, attachmentID int64) (*TicketAttachment, error)
	GetForAdminDownload(ctx context.Context, ticketNo string, attachmentID int64) (*TicketAttachment, error)
	GetUsage(ctx context.Context) (TicketAttachmentUsage, error)
	HasS3Attachments(ctx context.Context) (bool, error)
	ListExpiredForDeletion(ctx context.Context, now time.Time, limit int) ([]TicketAttachment, error)
	MarkDeleting(ctx context.Context, attachmentID int64) (bool, error)
	DeleteRecord(ctx context.Context, attachmentID int64) error
}

type TicketAttachmentStore interface {
	Provider() string
	Put(ctx context.Context, objectKey string, data []byte, contentType string) error
	Open(ctx context.Context, objectKey string) (io.ReadCloser, error)
	Delete(ctx context.Context, objectKey string) error
	Probe(ctx context.Context) error
}

type TicketAttachmentS3StoreFactory func(ctx context.Context, cfg TicketAttachmentS3Config) (TicketAttachmentStore, error)

type TicketAttachmentStoreRegistry interface {
	LocalStore() TicketAttachmentStore
	S3Store(ctx context.Context, cfg TicketAttachmentS3Config) (TicketAttachmentStore, error)
}
