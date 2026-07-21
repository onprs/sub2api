package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type TicketStorageSettingsService struct {
	settings    SettingRepository
	attachments TicketAttachmentRepository
	encryptor   SecretEncryptor
	registry    TicketAttachmentStoreRegistry
	cfg         *config.Config
	mu          sync.Mutex
}

func NewTicketStorageSettingsService(
	settings SettingRepository,
	attachments TicketAttachmentRepository,
	encryptor SecretEncryptor,
	registry TicketAttachmentStoreRegistry,
	cfg *config.Config,
) *TicketStorageSettingsService {
	return &TicketStorageSettingsService{
		settings: settings, attachments: attachments, encryptor: encryptor, registry: registry, cfg: cfg,
	}
}

func (s *TicketStorageSettingsService) Get(ctx context.Context) (*TicketAttachmentStorageView, error) {
	stored, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := s.attachments.GetUsage(ctx)
	if err != nil {
		return nil, err
	}
	root := "./data/ticket-attachments"
	shared := false
	if s.cfg != nil {
		root = s.cfg.Ticketing.LocalStorageRoot
		shared = s.cfg.Ticketing.LocalStorageShared
	}
	localConfigured, localWritable := s.localStatus(ctx)
	return &TicketAttachmentStorageView{
		Mode: stored.Mode, AttachmentsEnabled: s.ticketingEnabled() && stored.Mode != TicketAttachmentModeDisabled,
		Local: TicketAttachmentLocalView{
			Configured: localConfigured, Writable: localWritable,
			DisplayPath: root, SharedVolume: shared,
		},
		S3: TicketAttachmentS3View{
			Endpoint: stored.S3.Endpoint, Region: stored.S3.Region, Bucket: stored.S3.Bucket,
			AccessKeyIDMasked: maskTicketAccessKey(stored.S3.AccessKeyID), SecretConfigured: stored.S3.SecretAccessKey != "",
			Prefix: stored.S3.Prefix, ForcePathStyle: stored.S3.ForcePathStyle,
		},
		Usage: usage,
	}, nil
}

func (s *TicketStorageSettingsService) Test(ctx context.Context, update TicketAttachmentStorageUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.loadStored(ctx)
	if err != nil {
		return err
	}
	candidate, _, err := s.prepareCandidate(update, stored, false)
	if err != nil {
		return err
	}
	return s.probe(ctx, candidate)
}

func (s *TicketStorageSettingsService) Update(ctx context.Context, update TicketAttachmentStorageUpdate) (*TicketAttachmentStorageView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	candidate, storedSecret, err := s.prepareCandidate(update, stored, true)
	if err != nil {
		return nil, err
	}
	inUse, err := s.attachments.HasS3Attachments(ctx)
	if err != nil {
		return nil, err
	}
	if inUse && ticketS3DestinationChanged(stored.S3, candidate.S3) {
		return nil, domain.ErrTicketStorageDestinationInUse
	}
	if err := s.probe(ctx, candidate); err != nil {
		return nil, domain.ErrTicketStorageTestFailed.WithCause(err)
	}
	persisted := candidate
	persisted.S3.SecretAccessKey = storedSecret
	raw, err := json.Marshal(persisted)
	if err != nil {
		return nil, err
	}
	if err := s.settings.Set(ctx, TicketAttachmentStorageSettingKey, string(raw)); err != nil {
		return nil, err
	}
	return s.viewFromStored(ctx, persisted)
}

func (s *TicketStorageSettingsService) StoreForNewUpload(ctx context.Context) (TicketAttachmentStore, error) {
	if !s.ticketingEnabled() {
		return nil, domain.ErrTicketAttachmentsDisabled
	}
	stored, err := s.loadStored(ctx)
	if err != nil {
		return nil, err
	}
	switch stored.Mode {
	case TicketAttachmentModeLocal:
		if s.registry == nil || s.registry.LocalStore() == nil {
			return nil, domain.ErrTicketLocalStorageUnavailable
		}
		return s.registry.LocalStore(), nil
	case TicketAttachmentModeS3:
		resolved, err := s.resolveStoredS3(stored.S3)
		if err != nil {
			return nil, err
		}
		return s.registry.S3Store(ctx, resolved)
	default:
		return nil, domain.ErrTicketAttachmentsDisabled
	}
}

func (s *TicketStorageSettingsService) StoreForProvider(ctx context.Context, provider string) (TicketAttachmentStore, error) {
	switch provider {
	case "local":
		if s.registry == nil || s.registry.LocalStore() == nil {
			return nil, domain.ErrTicketLocalStorageUnavailable
		}
		return s.registry.LocalStore(), nil
	case "s3":
		stored, err := s.loadStored(ctx)
		if err != nil {
			return nil, err
		}
		resolved, err := s.resolveStoredS3(stored.S3)
		if err != nil {
			return nil, err
		}
		return s.registry.S3Store(ctx, resolved)
	default:
		return nil, domain.ErrTicketStorageProviderUnavailable
	}
}

func (s *TicketStorageSettingsService) AttachmentsEnabled(ctx context.Context) bool {
	if !s.ticketingEnabled() {
		return false
	}
	stored, err := s.loadStored(ctx)
	return err == nil && stored.Mode != TicketAttachmentModeDisabled
}

func (s *TicketStorageSettingsService) loadStored(ctx context.Context) (TicketAttachmentStorageConfig, error) {
	if s.settings == nil {
		return TicketAttachmentStorageConfig{Mode: TicketAttachmentModeDisabled}, nil
	}
	raw, err := s.settings.GetValue(ctx, TicketAttachmentStorageSettingKey)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return TicketAttachmentStorageConfig{Mode: TicketAttachmentModeDisabled}, nil
		}
		return TicketAttachmentStorageConfig{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return TicketAttachmentStorageConfig{Mode: TicketAttachmentModeDisabled}, nil
	}
	var stored TicketAttachmentStorageConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return TicketAttachmentStorageConfig{}, domain.ErrTicketStorageProviderUnavailable.WithCause(err)
	}
	if stored.Mode == "" {
		stored.Mode = TicketAttachmentModeDisabled
	}
	return stored, nil
}

func (s *TicketStorageSettingsService) prepareCandidate(
	update TicketAttachmentStorageUpdate,
	stored TicketAttachmentStorageConfig,
	encryptNewSecret bool,
) (TicketAttachmentStorageConfig, string, error) {
	mode := update.Mode
	if mode != TicketAttachmentModeDisabled && mode != TicketAttachmentModeLocal && mode != TicketAttachmentModeS3 {
		return TicketAttachmentStorageConfig{}, "", domain.ErrTicketStorageModeInvalid
	}
	candidate := TicketAttachmentStorageConfig{Mode: mode}
	if ticketS3ConfigEmpty(update.S3) && !ticketS3ConfigEmpty(stored.S3) {
		candidate.S3 = stored.S3
	} else {
		candidate.S3 = normalizeTicketS3Config(update.S3)
	}
	plainSecret := strings.TrimSpace(update.S3.SecretAccessKey)
	storedSecret := stored.S3.SecretAccessKey
	if plainSecret == "" && storedSecret != "" {
		decrypted, err := s.encryptor.Decrypt(storedSecret)
		if err != nil {
			return TicketAttachmentStorageConfig{}, "", domain.ErrTicketStorageProviderUnavailable.WithCause(err)
		}
		plainSecret = decrypted
	}
	if strings.TrimSpace(update.S3.AccessKeyID) == "" && stored.S3.AccessKeyID != "" {
		candidate.S3.AccessKeyID = stored.S3.AccessKeyID
	}
	candidate.S3.SecretAccessKey = plainSecret
	if mode == TicketAttachmentModeS3 {
		if err := validateTicketS3Config(candidate.S3); err != nil {
			return TicketAttachmentStorageConfig{}, "", err
		}
	}
	if strings.TrimSpace(update.S3.SecretAccessKey) != "" && encryptNewSecret {
		encrypted, err := s.encryptor.Encrypt(plainSecret)
		if err != nil {
			return TicketAttachmentStorageConfig{}, "", domain.ErrTicketStorageProviderUnavailable.WithCause(err)
		}
		storedSecret = encrypted
	}
	return candidate, storedSecret, nil
}

func (s *TicketStorageSettingsService) probe(ctx context.Context, candidate TicketAttachmentStorageConfig) error {
	switch candidate.Mode {
	case TicketAttachmentModeDisabled:
		return nil
	case TicketAttachmentModeLocal:
		if s.registry == nil || s.registry.LocalStore() == nil {
			return domain.ErrTicketLocalStorageUnavailable
		}
		return s.registry.LocalStore().Probe(ctx)
	case TicketAttachmentModeS3:
		store, err := s.registry.S3Store(ctx, candidate.S3)
		if err != nil {
			return err
		}
		return store.Probe(ctx)
	default:
		return domain.ErrTicketStorageModeInvalid
	}
}

func (s *TicketStorageSettingsService) resolveStoredS3(stored TicketAttachmentS3Config) (TicketAttachmentS3Config, error) {
	if stored.SecretAccessKey == "" {
		return TicketAttachmentS3Config{}, domain.ErrTicketS3ConfigInvalid
	}
	plain, err := s.encryptor.Decrypt(stored.SecretAccessKey)
	if err != nil {
		return TicketAttachmentS3Config{}, domain.ErrTicketStorageProviderUnavailable.WithCause(err)
	}
	stored.SecretAccessKey = plain
	if err := validateTicketS3Config(stored); err != nil {
		return TicketAttachmentS3Config{}, err
	}
	return stored, nil
}

func (s *TicketStorageSettingsService) viewFromStored(ctx context.Context, stored TicketAttachmentStorageConfig) (*TicketAttachmentStorageView, error) {
	usage, err := s.attachments.GetUsage(ctx)
	if err != nil {
		return nil, err
	}
	root := "./data/ticket-attachments"
	shared := false
	if s.cfg != nil {
		root = s.cfg.Ticketing.LocalStorageRoot
		shared = s.cfg.Ticketing.LocalStorageShared
	}
	localConfigured, localWritable := s.localStatus(ctx)
	return &TicketAttachmentStorageView{
		Mode: stored.Mode, AttachmentsEnabled: s.ticketingEnabled() && stored.Mode != TicketAttachmentModeDisabled,
		Local: TicketAttachmentLocalView{
			Configured: localConfigured, Writable: localWritable,
			DisplayPath: root, SharedVolume: shared,
		},
		S3: TicketAttachmentS3View{
			Endpoint: stored.S3.Endpoint, Region: stored.S3.Region, Bucket: stored.S3.Bucket,
			AccessKeyIDMasked: maskTicketAccessKey(stored.S3.AccessKeyID), SecretConfigured: stored.S3.SecretAccessKey != "",
			Prefix: stored.S3.Prefix, ForcePathStyle: stored.S3.ForcePathStyle,
		},
		Usage: usage,
	}, nil
}

func (s *TicketStorageSettingsService) localStatus(ctx context.Context) (configured, writable bool) {
	if s.registry == nil || s.registry.LocalStore() == nil {
		return false, false
	}
	return true, s.registry.LocalStore().Probe(ctx) == nil
}

func (s *TicketStorageSettingsService) ticketingEnabled() bool {
	return s.cfg == nil || s.cfg.Ticketing.Enabled
}

func normalizeTicketS3Config(cfg TicketAttachmentS3Config) TicketAttachmentS3Config {
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Region = strings.TrimSpace(cfg.Region)
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.SecretAccessKey = strings.TrimSpace(cfg.SecretAccessKey)
	cfg.Prefix = strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	if cfg.Prefix != "" {
		cfg.Prefix += "/"
	}
	return cfg
}

func validateTicketS3Config(cfg TicketAttachmentS3Config) error {
	if cfg.Endpoint != "" {
		if err := config.ValidateAbsoluteHTTPURL(cfg.Endpoint); err != nil {
			return domain.ErrTicketS3ConfigInvalid.WithCause(err)
		}
	}
	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || cfg.Region == "" {
		return domain.ErrTicketS3ConfigInvalid
	}
	if strings.Contains(cfg.Prefix, "..") || strings.ContainsAny(cfg.Prefix, "\\\r\n") {
		return domain.ErrTicketS3ConfigInvalid
	}
	return nil
}

func ticketS3DestinationChanged(old, candidate TicketAttachmentS3Config) bool {
	old = normalizeTicketS3Config(old)
	candidate = normalizeTicketS3Config(candidate)
	return old.Endpoint != candidate.Endpoint || old.Region != candidate.Region || old.Bucket != candidate.Bucket ||
		old.Prefix != candidate.Prefix || old.ForcePathStyle != candidate.ForcePathStyle
}

func ticketS3ConfigEmpty(cfg TicketAttachmentS3Config) bool {
	return strings.TrimSpace(cfg.Endpoint) == "" && strings.TrimSpace(cfg.Region) == "" && strings.TrimSpace(cfg.Bucket) == "" &&
		strings.TrimSpace(cfg.AccessKeyID) == "" && strings.TrimSpace(cfg.SecretAccessKey) == "" && strings.TrimSpace(cfg.Prefix) == "" && !cfg.ForcePathStyle
}

func maskTicketAccessKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		if value == "" {
			return ""
		}
		return strings.Repeat("*", len(value))
	}
	return fmt.Sprintf("%s...%s", value[:4], value[len(value)-4:])
}
