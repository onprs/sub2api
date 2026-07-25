package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"golang.org/x/image/webp"
)

type TicketAttachmentService struct {
	repo     TicketAttachmentRepository
	settings *TicketStorageSettingsService
	cfg      *config.Config
	now      func() time.Time
}

func NewTicketAttachmentService(repo TicketAttachmentRepository, settings *TicketStorageSettingsService, cfg *config.Config) *TicketAttachmentService {
	return &TicketAttachmentService{repo: repo, settings: settings, cfg: cfg, now: time.Now}
}

func (s *TicketAttachmentService) Upload(ctx context.Context, actorID int64, actorRole domain.TicketActorRole, input TicketAttachmentUploadInput) (*TicketAttachment, error) {
	if actorID <= 0 || (actorRole != domain.TicketActorUser && actorRole != domain.TicketActorAdmin) || input.Reader == nil {
		return nil, domain.ErrTicketAttachmentNotFound
	}
	maxBytes := s.maxFileBytes()
	raw, err := io.ReadAll(io.LimitReader(input.Reader, maxBytes+1))
	if err != nil {
		return nil, domain.ErrTicketAttachmentInvalidType.WithCause(err)
	}
	if int64(len(raw)) == 0 {
		return nil, domain.ErrTicketAttachmentInvalidType
	}
	if int64(len(raw)) > maxBytes {
		return nil, domain.ErrTicketAttachmentTooLarge
	}
	processed, contentType, normalizedName, err := s.validateAndNormalize(raw, input.OriginalName)
	if err != nil {
		return nil, err
	}
	if int64(len(processed)) > maxBytes {
		return nil, domain.ErrTicketAttachmentTooLarge
	}
	store, err := s.settings.StoreForNewUpload(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	token, err := randomTicketUploadToken()
	if err != nil {
		return nil, err
	}
	objectKey := now.UTC().Format("2006/01") + "/" + uuid.NewString()
	hash := sha256.Sum256(processed)
	attachment, err := s.repo.CreatePending(ctx, CreatePendingTicketAttachmentParams{
		UploadToken: token, UploadedBy: actorID, UploaderRole: actorRole,
		StorageProvider: store.Provider(), ObjectKey: objectKey, OriginalName: normalizedName,
		ContentType: contentType, ByteSize: int64(len(processed)), DailyLimitBytes: domain.TicketAttachmentDailyBytesLimit,
		SHA256: hex.EncodeToString(hash[:]), ExpiresAt: now.Add(s.pendingExpiry()), CreatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	if err := store.Put(ctx, objectKey, processed, contentType); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := store.Delete(cleanupCtx, objectKey); cleanupErr == nil {
			_ = s.repo.DeletePending(cleanupCtx, attachment.ID)
		}
		return nil, domain.ErrTicketStorageProviderUnavailable.WithCause(err)
	}
	return attachment, nil
}

func (s *TicketAttachmentService) DownloadForUser(ctx context.Context, userID int64, ticketNo string, attachmentID int64) (*TicketAttachmentDownload, error) {
	attachment, err := s.repo.GetForUserDownload(ctx, userID, ticketNo, attachmentID)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, attachment)
}

func (s *TicketAttachmentService) DownloadForAdmin(ctx context.Context, ticketNo string, attachmentID int64) (*TicketAttachmentDownload, error) {
	attachment, err := s.repo.GetForAdminDownload(ctx, ticketNo, attachmentID)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, attachment)
}

func (s *TicketAttachmentService) CleanupExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	items, err := s.repo.ListExpiredForDeletion(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for i := range items {
		item := items[i]
		marked, err := s.repo.MarkDeleting(ctx, item.ID)
		if err != nil {
			return deleted, err
		}
		if !marked {
			continue
		}
		store, err := s.settings.StoreForProvider(ctx, item.StorageProvider)
		if err != nil {
			continue
		}
		if err := store.Delete(ctx, item.ObjectKey); err != nil {
			continue
		}
		if err := s.repo.DeleteRecord(ctx, item.ID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *TicketAttachmentService) Capabilities(ctx context.Context) TicketAttachmentCapabilities {
	return TicketAttachmentCapabilities{
		Enabled:            s.cfg == nil || s.cfg.Ticketing.Enabled,
		AttachmentsEnabled: s.settings.AttachmentsEnabled(ctx),
		MaxFileBytes:       s.maxFileBytes(), MaxFilesPerMessage: s.maxFilesPerMessage(), MaxTicketBytes: s.maxTicketBytes(),
		PollingHintSeconds: s.pollingHintSeconds(), DetailPollingSeconds: s.detailPollingSeconds(),
	}
}

type TicketAttachmentCapabilities struct {
	Enabled              bool
	AttachmentsEnabled   bool
	MaxFileBytes         int64
	MaxFilesPerMessage   int
	MaxTicketBytes       int64
	PollingHintSeconds   int
	DetailPollingSeconds int
}

func (s *TicketAttachmentService) open(ctx context.Context, attachment *TicketAttachment) (*TicketAttachmentDownload, error) {
	store, err := s.settings.StoreForProvider(ctx, attachment.StorageProvider)
	if err != nil {
		return nil, err
	}
	body, err := store.Open(ctx, attachment.ObjectKey)
	if err != nil {
		return nil, domain.ErrTicketStorageProviderUnavailable.WithCause(err)
	}
	return &TicketAttachmentDownload{Attachment: *attachment, Body: body}, nil
}

func (s *TicketAttachmentService) validateAndNormalize(raw []byte, originalName string) ([]byte, string, string, error) {
	name := sanitizeTicketAttachmentName(originalName)
	detected := http.DetectContentType(raw)
	switch detected {
	case "image/png":
		decoded, err := decodeTicketImage(raw, s.maxImagePixels(), png.Decode)
		if err != nil {
			return nil, "", "", err
		}
		var output bytes.Buffer
		if err := png.Encode(&output, decoded); err != nil {
			return nil, "", "", domain.ErrTicketAttachmentInvalidType.WithCause(err)
		}
		return output.Bytes(), "image/png", name, nil
	case "image/jpeg":
		decoded, err := decodeTicketImage(raw, s.maxImagePixels(), jpeg.Decode)
		if err != nil {
			return nil, "", "", err
		}
		var output bytes.Buffer
		if err := jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", "", domain.ErrTicketAttachmentInvalidType.WithCause(err)
		}
		return output.Bytes(), "image/jpeg", name, nil
	case "image/webp":
		decoded, err := decodeTicketImage(raw, s.maxImagePixels(), webp.Decode)
		if err != nil {
			return nil, "", "", err
		}
		var output bytes.Buffer
		if err := png.Encode(&output, decoded); err != nil {
			return nil, "", "", domain.ErrTicketAttachmentInvalidType.WithCause(err)
		}
		return output.Bytes(), "image/png", replaceTicketAttachmentExtension(name, ".png"), nil
	case "text/html; charset=utf-8", "text/xml; charset=utf-8", "application/zip", "application/x-gzip", "application/pdf":
		return nil, "", "", domain.ErrTicketAttachmentInvalidType
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid(trimmed) {
		return raw, "application/json", name, nil
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 || looksLikeBinaryTicketText(raw) {
		return nil, "", "", domain.ErrTicketAttachmentInvalidType
	}
	if !strings.HasPrefix(detected, "text/plain") && detected != "application/octet-stream" {
		return nil, "", "", domain.ErrTicketAttachmentInvalidType
	}
	return raw, "text/plain; charset=utf-8", name, nil
}

type ticketImageDecoder func(io.Reader) (image.Image, error)

func decodeTicketImage(raw []byte, maxPixels int64, decoder ticketImageDecoder) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPixels {
		return nil, domain.ErrTicketAttachmentInvalidType
	}
	decoded, err := decoder(bytes.NewReader(raw))
	if err != nil {
		return nil, domain.ErrTicketAttachmentInvalidType.WithCause(err)
	}
	return decoded, nil
}

func looksLikeBinaryTicketText(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	controls := 0
	for _, r := range string(raw) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			controls++
		}
	}
	return controls*10 > utf8.RuneCount(raw)
}

func sanitizeTicketAttachmentName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	var builder strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			continue
		}
		_, _ = builder.WriteRune(r)
		if builder.Len() >= 240 {
			break
		}
	}
	name = strings.TrimSpace(builder.String())
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

func replaceTicketAttachmentExtension(name, extension string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "attachment"
	}
	return base + extension
}

func randomTicketUploadToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate ticket upload token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func (s *TicketAttachmentService) maxFileBytes() int64 {
	if s.cfg != nil && s.cfg.Ticketing.Attachments.MaxFileBytes > 0 {
		return s.cfg.Ticketing.Attachments.MaxFileBytes
	}
	return 5 * 1024 * 1024
}

func (s *TicketAttachmentService) maxFilesPerMessage() int {
	if s.cfg != nil && s.cfg.Ticketing.Attachments.MaxFilesPerMessage > 0 {
		return s.cfg.Ticketing.Attachments.MaxFilesPerMessage
	}
	return 3
}

func (s *TicketAttachmentService) maxTicketBytes() int64 {
	if s.cfg != nil && s.cfg.Ticketing.Attachments.MaxTicketBytes > 0 {
		return s.cfg.Ticketing.Attachments.MaxTicketBytes
	}
	return 30 * 1024 * 1024
}

func (s *TicketAttachmentService) maxImagePixels() int64 {
	if s.cfg != nil && s.cfg.Ticketing.Attachments.MaxImagePixels > 0 {
		return s.cfg.Ticketing.Attachments.MaxImagePixels
	}
	return 40 * 1000 * 1000
}

func (s *TicketAttachmentService) pendingExpiry() time.Duration {
	if s.cfg != nil && s.cfg.Ticketing.Attachments.PendingExpiryHours > 0 {
		return time.Duration(s.cfg.Ticketing.Attachments.PendingExpiryHours) * time.Hour
	}
	return 24 * time.Hour
}

func (s *TicketAttachmentService) pollingHintSeconds() int {
	if s.cfg != nil && s.cfg.Ticketing.PollingHintSeconds > 0 {
		return s.cfg.Ticketing.PollingHintSeconds
	}
	return 30
}

func (s *TicketAttachmentService) detailPollingSeconds() int {
	if s.cfg != nil && s.cfg.Ticketing.DetailPollingSeconds > 0 {
		return s.cfg.Ticketing.DetailPollingSeconds
	}
	return 15
}
