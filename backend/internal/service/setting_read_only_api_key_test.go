package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type readOnlyKeyRepo struct {
	SettingRepository
	values    map[string]string
	getErr    error
	setErr    error
	deleteErr error
}

func (r *readOnlyKeyRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *readOnlyKeyRepo) Set(_ context.Context, key, value string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	return nil
}

func (r *readOnlyKeyRepo) Delete(_ context.Context, key string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.values, key)
	return nil
}

func TestAdminReadOnlyAPIKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := &readOnlyKeyRepo{values: map[string]string{SettingKeyAdminAPIKey: "现有管理员密钥"}}
	settings := NewSettingService(repo, nil)
	masked, exists, err := settings.GetAdminReadOnlyAPIKeyStatus(ctx)
	require.NoError(t, err)
	require.False(t, exists)
	require.Empty(t, masked)

	key, err := settings.GenerateAdminReadOnlyAPIKey(ctx)
	require.NoError(t, err)
	require.Len(t, key, len(AdminReadOnlyAPIKeyPrefix)+64)
	require.True(t, strings.HasPrefix(key, AdminReadOnlyAPIKeyPrefix))
	require.NotContains(t, repo.values[SettingKeyAdminReadOnlyAPIKey], key)
	var record adminReadOnlyAPIKeyRecord
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyAdminReadOnlyAPIKey]), &record))
	require.Len(t, record.Hash, 64)
	masked, exists, err = settings.GetAdminReadOnlyAPIKeyStatus(ctx)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, key[:10]+"..."+key[len(key)-4:], masked)
	valid, err := settings.ValidateAdminReadOnlyAPIKey(ctx, key)
	require.NoError(t, err)
	require.True(t, valid)

	replacement, err := settings.GenerateAdminReadOnlyAPIKey(ctx)
	require.NoError(t, err)
	require.NotEqual(t, key, replacement)
	valid, err = settings.ValidateAdminReadOnlyAPIKey(ctx, key)
	require.NoError(t, err)
	require.False(t, valid)
	valid, err = settings.ValidateAdminReadOnlyAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.True(t, valid)
	require.NoError(t, settings.DeleteAdminReadOnlyAPIKey(ctx))
	valid, err = settings.ValidateAdminReadOnlyAPIKey(ctx, replacement)
	require.NoError(t, err)
	require.False(t, valid)
	require.NoError(t, settings.DeleteAdminReadOnlyAPIKey(ctx))
	require.Equal(t, "现有管理员密钥", repo.values[SettingKeyAdminAPIKey])
}

func TestAdminReadOnlyAPIKeyFailures(t *testing.T) {
	ctx := context.Background()
	repo := &readOnlyKeyRepo{values: make(map[string]string)}
	settings := NewSettingService(repo, nil)
	key, err := settings.GenerateAdminReadOnlyAPIKey(ctx)
	require.NoError(t, err)
	original := repo.values[SettingKeyAdminReadOnlyAPIKey]

	for _, invalid := range []string{"", key[:len(key)-1], key + "x", AdminReadOnlyAPIKeyPrefix + strings.Repeat("z", 64), "s2a_" + strings.Repeat("0", 64)} {
		valid, err := settings.ValidateAdminReadOnlyAPIKey(ctx, invalid)
		require.NoError(t, err)
		require.False(t, valid)
	}
	repo.setErr = errors.New("写入失败")
	generated, err := settings.GenerateAdminReadOnlyAPIKey(ctx)
	require.Error(t, err)
	require.Empty(t, generated)
	require.Equal(t, original, repo.values[SettingKeyAdminReadOnlyAPIKey])
	repo.setErr = nil
	repo.getErr = errors.New("数据库错误")
	valid, err := settings.ValidateAdminReadOnlyAPIKey(ctx, key)
	require.Error(t, err)
	require.False(t, valid)
	repo.getErr = nil

	for _, value := range []string{"broken", `{}`, `{"hash":"00","masked_key":"s2ro_00000...0000"}`, `{"hash":"` + strings.Repeat("0", 64) + `","masked_key":"s2ro_"}`} {
		repo.values[SettingKeyAdminReadOnlyAPIKey] = value
		valid, err := settings.ValidateAdminReadOnlyAPIKey(ctx, key)
		require.Error(t, err)
		require.False(t, valid)
	}
	repo.values[SettingKeyAdminReadOnlyAPIKey] = original
	repo.deleteErr = errors.New("删除失败")
	require.Error(t, settings.DeleteAdminReadOnlyAPIKey(ctx))
	valid, err = settings.ValidateAdminReadOnlyAPIKey(ctx, key)
	require.NoError(t, err)
	require.True(t, valid)
}
