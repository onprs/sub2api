package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	SettingKeyAdminReadOnlyAPIKey = "admin_read_only_api_key"
	AdminReadOnlyAPIKeyPrefix     = "s2ro_"
	AdminReadOnlyAPIKeyAuthMethod = "admin_read_only_api_key"
)

type adminReadOnlyAPIKeyRecord struct {
	Hash      string `json:"hash"`
	MaskedKey string `json:"masked_key"`
}

// GenerateAdminReadOnlyAPIKey 轮换只读密钥，完整值仅在生成时返回。
func (s *SettingService) GenerateAdminReadOnlyAPIKey(ctx context.Context) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("生成只读密钥: %w", err)
	}
	key := AdminReadOnlyAPIKeyPrefix + hex.EncodeToString(secret)
	digest := sha256.Sum256([]byte(key))
	record := adminReadOnlyAPIKeyRecord{
		Hash:      hex.EncodeToString(digest[:]),
		MaskedKey: key[:10] + "..." + key[len(key)-4:],
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("编码只读密钥记录: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyAdminReadOnlyAPIKey, string(encoded)); err != nil {
		return "", fmt.Errorf("保存只读密钥: %w", err)
	}
	return key, nil
}

func (s *SettingService) adminReadOnlyAPIKeyRecord(ctx context.Context) (*adminReadOnlyAPIKeyRecord, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAdminReadOnlyAPIKey)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record adminReadOnlyAPIKeyRecord
	if err := json.Unmarshal([]byte(value), &record); err != nil {
		return nil, fmt.Errorf("读取只读密钥记录: %w", err)
	}
	digest, err := hex.DecodeString(record.Hash)
	if err != nil || len(digest) != sha256.Size || !strings.HasPrefix(record.MaskedKey, AdminReadOnlyAPIKeyPrefix) || len(record.MaskedKey) != 17 || record.MaskedKey[10:13] != "..." {
		return nil, errors.New("只读密钥记录格式错误")
	}
	return &record, nil
}

// GetAdminReadOnlyAPIKeyStatus 仅返回脱敏状态。
func (s *SettingService) GetAdminReadOnlyAPIKeyStatus(ctx context.Context) (string, bool, error) {
	record, err := s.adminReadOnlyAPIKeyRecord(ctx)
	if err != nil || record == nil {
		return "", false, err
	}
	return record.MaskedKey, true, nil
}

// ValidateAdminReadOnlyAPIKey 每次读取当前摘要，轮换和删除立即生效。
func (s *SettingService) ValidateAdminReadOnlyAPIKey(ctx context.Context, key string) (bool, error) {
	if !strings.HasPrefix(key, AdminReadOnlyAPIKeyPrefix) || len(key) != len(AdminReadOnlyAPIKeyPrefix)+64 {
		return false, nil
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(key, AdminReadOnlyAPIKeyPrefix)); err != nil {
		return false, nil
	}
	record, err := s.adminReadOnlyAPIKeyRecord(ctx)
	if err != nil || record == nil {
		return false, err
	}
	stored, _ := hex.DecodeString(record.Hash)
	digest := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(stored, digest[:]) == 1, nil
}

func (s *SettingService) DeleteAdminReadOnlyAPIKey(ctx context.Context) error {
	err := s.settingRepo.Delete(ctx, SettingKeyAdminReadOnlyAPIKey)
	if errors.Is(err, ErrSettingNotFound) {
		return nil
	}
	return err
}
