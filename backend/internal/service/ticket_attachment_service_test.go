package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type ticketAttachmentRepoStub struct {
	mu             sync.Mutex
	created        *TicketAttachment
	createParams   *CreatePendingTicketAttachmentParams
	createErr      error
	deletePending  []int64
	usage          TicketAttachmentUsage
	hasS3          bool
	expired        []TicketAttachment
	deletedRecords []int64
}

func (r *ticketAttachmentRepoStub) CreatePending(_ context.Context, params CreatePendingTicketAttachmentParams) (*TicketAttachment, error) {
	r.createParams = &params
	if r.createErr != nil {
		return nil, r.createErr
	}
	created := &TicketAttachment{
		ID: 1, UploadToken: params.UploadToken, UploadedBy: &params.UploadedBy, UploaderRole: params.UploaderRole,
		State: "pending", StorageProvider: params.StorageProvider, ObjectKey: params.ObjectKey,
		OriginalName: params.OriginalName, ContentType: params.ContentType, ByteSize: params.ByteSize,
		SHA256: params.SHA256, ExpiresAt: &params.ExpiresAt, CreatedAt: params.CreatedAt,
	}
	r.created = created
	return created, nil
}
func (r *ticketAttachmentRepoStub) DeletePending(_ context.Context, id int64) error {
	r.deletePending = append(r.deletePending, id)
	return nil
}
func (r *ticketAttachmentRepoStub) GetForUserDownload(context.Context, int64, string, int64) (*TicketAttachment, error) {
	return nil, domain.ErrTicketAttachmentNotFound
}
func (r *ticketAttachmentRepoStub) GetForAdminDownload(context.Context, string, int64) (*TicketAttachment, error) {
	return nil, domain.ErrTicketAttachmentNotFound
}
func (r *ticketAttachmentRepoStub) GetUsage(context.Context) (TicketAttachmentUsage, error) {
	return r.usage, nil
}
func (r *ticketAttachmentRepoStub) HasS3Attachments(context.Context) (bool, error) {
	return r.hasS3, nil
}
func (r *ticketAttachmentRepoStub) ListExpiredForDeletion(context.Context, time.Time, int) ([]TicketAttachment, error) {
	return append([]TicketAttachment(nil), r.expired...), nil
}
func (r *ticketAttachmentRepoStub) MarkDeleting(context.Context, int64) (bool, error) {
	return true, nil
}
func (r *ticketAttachmentRepoStub) DeleteRecord(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deletedRecords = append(r.deletedRecords, id)
	return nil
}

type ticketAttachmentStoreStub struct {
	provider string
	probeErr error
	putErr   error
	mu       sync.Mutex
	objects  map[string][]byte
	deleted  []string
}

func newTicketAttachmentStoreStub(provider string) *ticketAttachmentStoreStub {
	return &ticketAttachmentStoreStub{provider: provider, objects: make(map[string][]byte)}
}
func (s *ticketAttachmentStoreStub) Provider() string { return s.provider }
func (s *ticketAttachmentStoreStub) Put(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	s.objects[key] = append([]byte(nil), data...)
	return nil
}
func (s *ticketAttachmentStoreStub) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *ticketAttachmentStoreStub) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}
func (s *ticketAttachmentStoreStub) Probe(context.Context) error { return s.probeErr }

type ticketAttachmentRegistryStub struct {
	local        *ticketAttachmentStoreStub
	s3           *ticketAttachmentStoreStub
	lastS3Config TicketAttachmentS3Config
}

func (r *ticketAttachmentRegistryStub) LocalStore() TicketAttachmentStore { return r.local }
func (r *ticketAttachmentRegistryStub) S3Store(_ context.Context, cfg TicketAttachmentS3Config) (TicketAttachmentStore, error) {
	r.lastS3Config = cfg
	if r.s3 == nil {
		return nil, domain.ErrTicketStorageProviderUnavailable
	}
	return r.s3, nil
}

type ticketSettingRepoStub struct {
	values   map[string]string
	setCalls int
}

func newTicketSettingRepoStub() *ticketSettingRepoStub {
	return &ticketSettingRepoStub{values: make(map[string]string)}
}
func (r *ticketSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (r *ticketSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (r *ticketSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	r.setCalls++
	return nil
}
func (r *ticketSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r *ticketSettingRepoStub) SetMultiple(context.Context, map[string]string) error { return nil }
func (r *ticketSettingRepoStub) GetAll(context.Context) (map[string]string, error)    { return nil, nil }
func (r *ticketSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

type ticketEncryptorStub struct{}

func (ticketEncryptorStub) Encrypt(value string) (string, error) { return "encrypted:" + value, nil }
func (ticketEncryptorStub) Decrypt(value string) (string, error) {
	const prefix = "encrypted:"
	if len(value) < len(prefix) || value[:len(prefix)] != prefix {
		return "", errors.New("invalid ciphertext")
	}
	return value[len(prefix):], nil
}

func ticketAttachmentTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Ticketing.Enabled = true
	cfg.Ticketing.LocalStorageRoot = "./test-data"
	cfg.Ticketing.ResolvedAutoCloseDays = 7
	cfg.Ticketing.PollingHintSeconds = 30
	cfg.Ticketing.DetailPollingSeconds = 15
	cfg.Ticketing.Attachments.MaxFileBytes = 1024 * 1024
	cfg.Ticketing.Attachments.MaxFilesPerMessage = 3
	cfg.Ticketing.Attachments.MaxTicketBytes = 5 * 1024 * 1024
	cfg.Ticketing.Attachments.MaxImagePixels = 100
	cfg.Ticketing.Attachments.PendingExpiryHours = 24
	cfg.Ticketing.Attachments.CleanupBatchSize = 100
	return cfg
}

func newLocalTicketAttachmentServices(repo *ticketAttachmentRepoStub, store *ticketAttachmentStoreStub) (*TicketStorageSettingsService, *TicketAttachmentService) {
	settingsRepo := newTicketSettingRepoStub()
	settingsRepo.values[TicketAttachmentStorageSettingKey] = `{"mode":"local","s3":{}}`
	registry := &ticketAttachmentRegistryStub{local: store, s3: newTicketAttachmentStoreStub("s3")}
	settings := NewTicketStorageSettingsService(settingsRepo, repo, ticketEncryptorStub{}, registry, ticketAttachmentTestConfig())
	return settings, NewTicketAttachmentService(repo, settings, ticketAttachmentTestConfig())
}

func TestTicketAttachmentServiceUploadValidatesAndReencodesImage(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	store := newTicketAttachmentStoreStub("local")
	_, svc := newLocalTicketAttachmentServices(repo, store)
	svc.now = func() time.Time { return time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC) }

	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, imageData))

	attachment, err := svc.Upload(context.Background(), 11, domain.TicketActorUser, TicketAttachmentUploadInput{
		OriginalName: "../evidence.png", Reader: bytes.NewReader(input.Bytes()),
	})
	require.NoError(t, err)
	require.Equal(t, "evidence.png", attachment.OriginalName)
	require.Equal(t, "image/png", attachment.ContentType)
	require.Equal(t, "local", attachment.StorageProvider)
	require.Len(t, attachment.UploadToken, 64)
	require.NotEmpty(t, attachment.SHA256)
	require.Contains(t, attachment.ObjectKey, "2026/07/")
	require.Contains(t, store.objects, attachment.ObjectKey)
	require.Equal(t, domain.TicketAttachmentDailyBytesLimit, repo.createParams.DailyLimitBytes)
	require.Equal(t, svc.now().Add(24*time.Hour), *attachment.ExpiresAt)
}

func TestTicketAttachmentServiceRejectsUnsafeContentAndLimits(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		err  error
	}{
		{name: "html", data: []byte("<!doctype html><script>alert(1)</script>"), err: domain.ErrTicketAttachmentInvalidType},
		{name: "binary", data: []byte{0, 1, 2, 3}, err: domain.ErrTicketAttachmentInvalidType},
		{name: "too large", data: bytes.Repeat([]byte("a"), 1024*1024+1), err: domain.ErrTicketAttachmentTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &ticketAttachmentRepoStub{}
			_, svc := newLocalTicketAttachmentServices(repo, newTicketAttachmentStoreStub("local"))
			_, err := svc.Upload(context.Background(), 11, domain.TicketActorUser, TicketAttachmentUploadInput{OriginalName: "x.txt", Reader: bytes.NewReader(test.data)})
			require.ErrorIs(t, err, test.err)
		})
	}
}

func TestTicketAttachmentServiceRejectsImagePixelBomb(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	_, svc := newLocalTicketAttachmentServices(repo, newTicketAttachmentStoreStub("local"))
	svc.cfg.Ticketing.Attachments.MaxImagePixels = 3
	var input bytes.Buffer
	require.NoError(t, png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	_, err := svc.Upload(context.Background(), 11, domain.TicketActorUser, TicketAttachmentUploadInput{OriginalName: "large.png", Reader: &input})
	require.ErrorIs(t, err, domain.ErrTicketAttachmentInvalidType)
}

func TestTicketAttachmentServiceDoesNotWriteWhenPendingReservationFails(t *testing.T) {
	repo := &ticketAttachmentRepoStub{createErr: errors.New("database unavailable")}
	store := newTicketAttachmentStoreStub("local")
	_, svc := newLocalTicketAttachmentServices(repo, store)
	_, err := svc.Upload(context.Background(), 11, domain.TicketActorUser, TicketAttachmentUploadInput{OriginalName: "evidence.txt", Reader: bytes.NewBufferString("plain evidence")})
	require.Error(t, err)
	require.Empty(t, store.objects)
	require.Empty(t, store.deleted)
}

func TestTicketAttachmentServiceCompensatesPendingReservationWhenStoreWriteFails(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	store := newTicketAttachmentStoreStub("local")
	store.putErr = errors.New("storage unavailable")
	_, svc := newLocalTicketAttachmentServices(repo, store)

	_, err := svc.Upload(context.Background(), 11, domain.TicketActorUser, TicketAttachmentUploadInput{OriginalName: "evidence.txt", Reader: bytes.NewBufferString("plain evidence")})

	require.ErrorIs(t, err, domain.ErrTicketStorageProviderUnavailable)
	require.Equal(t, []int64{1}, repo.deletePending)
	require.Len(t, store.deleted, 1)
}

func TestTicketAttachmentServiceCleanupRoutesByPersistedProvider(t *testing.T) {
	repo := &ticketAttachmentRepoStub{expired: []TicketAttachment{
		{ID: 1, State: "pending", StorageProvider: "local", ObjectKey: "local-key"},
		{ID: 2, State: "deleting", StorageProvider: "s3", ObjectKey: "s3-key"},
	}}
	local := newTicketAttachmentStoreStub("local")
	s3Store := newTicketAttachmentStoreStub("s3")
	local.objects["local-key"] = []byte("local")
	s3Store.objects["s3-key"] = []byte("s3")
	settingsRepo := newTicketSettingRepoStub()
	stored := TicketAttachmentStorageConfig{Mode: TicketAttachmentModeDisabled, S3: TicketAttachmentS3Config{
		Region: "auto", Bucket: "bucket", AccessKeyID: "access", SecretAccessKey: "encrypted:secret", Prefix: "tickets/",
	}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	settingsRepo.values[TicketAttachmentStorageSettingKey] = string(raw)
	registry := &ticketAttachmentRegistryStub{local: local, s3: s3Store}
	settings := NewTicketStorageSettingsService(settingsRepo, repo, ticketEncryptorStub{}, registry, ticketAttachmentTestConfig())
	svc := NewTicketAttachmentService(repo, settings, ticketAttachmentTestConfig())

	deleted, err := svc.CleanupExpired(context.Background(), time.Now(), 100)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)
	require.Equal(t, []string{"local-key"}, local.deleted)
	require.Equal(t, []string{"s3-key"}, s3Store.deleted)
	require.ElementsMatch(t, []int64{1, 2}, repo.deletedRecords)
	require.Equal(t, "secret", registry.lastS3Config.SecretAccessKey)
}
