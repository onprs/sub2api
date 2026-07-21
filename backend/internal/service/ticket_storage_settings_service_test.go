package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTicketStorageSettingsServiceEncryptsAndMasksS3Secret(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	settingsRepo := newTicketSettingRepoStub()
	registry := &ticketAttachmentRegistryStub{
		local: newTicketAttachmentStoreStub("local"),
		s3:    newTicketAttachmentStoreStub("s3"),
	}
	svc := NewTicketStorageSettingsService(settingsRepo, repo, ticketEncryptorStub{}, registry, ticketAttachmentTestConfig())

	view, err := svc.Update(context.Background(), TicketAttachmentStorageUpdate{
		Mode: TicketAttachmentModeS3,
		S3: TicketAttachmentS3Config{
			Endpoint: "https://objects.example.com/", Region: "auto", Bucket: "private",
			AccessKeyID: "ACCESSKEY123456", SecretAccessKey: "top-secret", Prefix: "/tickets/", ForcePathStyle: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, TicketAttachmentModeS3, view.Mode)
	require.Equal(t, "ACCE...3456", view.S3.AccessKeyIDMasked)
	require.True(t, view.S3.SecretConfigured)
	require.Equal(t, "tickets/", view.S3.Prefix)
	require.Equal(t, "top-secret", registry.lastS3Config.SecretAccessKey, "probe receives only plaintext in memory")

	persistedRaw := settingsRepo.values[TicketAttachmentStorageSettingKey]
	require.NotContains(t, persistedRaw, `"top-secret"`)
	var persisted TicketAttachmentStorageConfig
	require.NoError(t, json.Unmarshal([]byte(persistedRaw), &persisted))
	require.Equal(t, "encrypted:top-secret", persisted.S3.SecretAccessKey)
}

func TestTicketStorageSettingsServiceBlankSecretAndAccessKeyPreserveStoredValues(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	settingsRepo := newTicketSettingRepoStub()
	stored := TicketAttachmentStorageConfig{Mode: TicketAttachmentModeS3, S3: TicketAttachmentS3Config{
		Endpoint: "https://objects.example.com", Region: "auto", Bucket: "private", AccessKeyID: "ACCESSKEY123456",
		SecretAccessKey: "encrypted:old-secret", Prefix: "tickets/", ForcePathStyle: true,
	}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	settingsRepo.values[TicketAttachmentStorageSettingKey] = string(raw)
	registry := &ticketAttachmentRegistryStub{local: newTicketAttachmentStoreStub("local"), s3: newTicketAttachmentStoreStub("s3")}
	svc := NewTicketStorageSettingsService(settingsRepo, repo, ticketEncryptorStub{}, registry, ticketAttachmentTestConfig())

	_, err = svc.Update(context.Background(), TicketAttachmentStorageUpdate{
		Mode: TicketAttachmentModeS3,
		S3: TicketAttachmentS3Config{
			Endpoint: "https://objects.example.com", Region: "auto", Bucket: "private",
			Prefix: "tickets/", ForcePathStyle: true,
		},
	})
	require.NoError(t, err)
	var persisted TicketAttachmentStorageConfig
	require.NoError(t, json.Unmarshal([]byte(settingsRepo.values[TicketAttachmentStorageSettingKey]), &persisted))
	require.Equal(t, "ACCESSKEY123456", persisted.S3.AccessKeyID)
	require.Equal(t, "encrypted:old-secret", persisted.S3.SecretAccessKey)
	require.Equal(t, "old-secret", registry.lastS3Config.SecretAccessKey)
}

func TestTicketStorageSettingsServiceDisabledModePreservesS3Configuration(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	settingsRepo := newTicketSettingRepoStub()
	stored := TicketAttachmentStorageConfig{Mode: TicketAttachmentModeS3, S3: TicketAttachmentS3Config{
		Endpoint: "https://objects.example.com", Region: "auto", Bucket: "private", AccessKeyID: "ACCESSKEY123456",
		SecretAccessKey: "encrypted:old-secret", Prefix: "tickets/",
	}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	settingsRepo.values[TicketAttachmentStorageSettingKey] = string(raw)
	svc := NewTicketStorageSettingsService(
		settingsRepo, repo, ticketEncryptorStub{},
		&ticketAttachmentRegistryStub{local: newTicketAttachmentStoreStub("local"), s3: newTicketAttachmentStoreStub("s3")},
		ticketAttachmentTestConfig(),
	)

	view, err := svc.Update(context.Background(), TicketAttachmentStorageUpdate{Mode: TicketAttachmentModeDisabled})
	require.NoError(t, err)
	require.False(t, view.AttachmentsEnabled)
	var persisted TicketAttachmentStorageConfig
	require.NoError(t, json.Unmarshal([]byte(settingsRepo.values[TicketAttachmentStorageSettingKey]), &persisted))
	require.Equal(t, "private", persisted.S3.Bucket)
	require.Equal(t, "encrypted:old-secret", persisted.S3.SecretAccessKey)
}

func TestTicketStorageSettingsServiceRejectsDestinationChangeWhenS3ObjectsExist(t *testing.T) {
	repo := &ticketAttachmentRepoStub{hasS3: true}
	settingsRepo := newTicketSettingRepoStub()
	stored := TicketAttachmentStorageConfig{Mode: TicketAttachmentModeS3, S3: TicketAttachmentS3Config{
		Endpoint: "https://objects.example.com", Region: "auto", Bucket: "private", AccessKeyID: "access",
		SecretAccessKey: "encrypted:secret", Prefix: "tickets/",
	}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	settingsRepo.values[TicketAttachmentStorageSettingKey] = string(raw)
	svc := NewTicketStorageSettingsService(
		settingsRepo, repo, ticketEncryptorStub{},
		&ticketAttachmentRegistryStub{local: newTicketAttachmentStoreStub("local"), s3: newTicketAttachmentStoreStub("s3")},
		ticketAttachmentTestConfig(),
	)

	_, err = svc.Update(context.Background(), TicketAttachmentStorageUpdate{
		Mode: TicketAttachmentModeS3,
		S3: TicketAttachmentS3Config{
			Endpoint: "https://objects.example.com", Region: "auto", Bucket: "other", AccessKeyID: "access",
			SecretAccessKey: "secret", Prefix: "tickets/",
		},
	})
	require.ErrorIs(t, err, domain.ErrTicketStorageDestinationInUse)
	require.Equal(t, 0, settingsRepo.setCalls)
}

func TestTicketStorageSettingsServiceProbeFailureDoesNotPersist(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	settingsRepo := newTicketSettingRepoStub()
	local := newTicketAttachmentStoreStub("local")
	local.probeErr = domain.ErrTicketLocalStorageUnavailable
	svc := NewTicketStorageSettingsService(
		settingsRepo, repo, ticketEncryptorStub{},
		&ticketAttachmentRegistryStub{local: local, s3: newTicketAttachmentStoreStub("s3")},
		ticketAttachmentTestConfig(),
	)

	_, err := svc.Update(context.Background(), TicketAttachmentStorageUpdate{Mode: TicketAttachmentModeLocal})
	require.ErrorIs(t, err, domain.ErrTicketStorageTestFailed)
	require.Equal(t, 0, settingsRepo.setCalls)
	_, ok := settingsRepo.values[TicketAttachmentStorageSettingKey]
	require.False(t, ok)
}

func TestTicketStorageSettingsServiceCorruptCiphertextFailsClosed(t *testing.T) {
	repo := &ticketAttachmentRepoStub{}
	settingsRepo := newTicketSettingRepoStub()
	stored := TicketAttachmentStorageConfig{Mode: TicketAttachmentModeS3, S3: TicketAttachmentS3Config{
		Region: "auto", Bucket: "private", AccessKeyID: "access", SecretAccessKey: "plaintext-is-rejected",
	}}
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	settingsRepo.values[TicketAttachmentStorageSettingKey] = string(raw)
	svc := NewTicketStorageSettingsService(
		settingsRepo, repo, ticketEncryptorStub{},
		&ticketAttachmentRegistryStub{local: newTicketAttachmentStoreStub("local"), s3: newTicketAttachmentStoreStub("s3")},
		ticketAttachmentTestConfig(),
	)

	_, err = svc.StoreForNewUpload(context.Background())
	require.ErrorIs(t, err, domain.ErrTicketStorageProviderUnavailable)
}
