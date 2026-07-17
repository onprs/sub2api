package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAccountObserverAuthenticateValidReadToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	token := accountObserverTokenTag + strings.Repeat("a", 43)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, token_hash, scope, allowed_cidrs, expires_at, revoked_at")).
		WithArgs(observerTokenPrefix(token)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "token_hash", "scope", "allowed_cidrs", "expires_at", "revoked_at"}).
			AddRow(7, observerTokenHash(token), AccountObserverReadScope, "{127.0.0.0/8}", now.Add(time.Hour), nil))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE account_observer_tokens SET last_used_at = $2, updated_at = $2 WHERE id = $1")).
		WithArgs(int64(7), now).
		WillReturnResult(sqlmock.NewResult(0, 1))

	service := NewAccountObserverService(db, nil)
	service.now = func() time.Time { return now }
	require.NoError(t, service.Authenticate(context.Background(), token, net.ParseIP("127.0.0.1")))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountObserverAuthenticateRejectsRevokedToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	token := accountObserverTokenTag + strings.Repeat("b", 43)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, token_hash, scope, allowed_cidrs, expires_at, revoked_at")).
		WithArgs(observerTokenPrefix(token)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "token_hash", "scope", "allowed_cidrs", "expires_at", "revoked_at"}).
			AddRow(8, observerTokenHash(token), AccountObserverReadScope, "{}", nil, now.Add(-time.Minute)))

	service := NewAccountObserverService(db, nil)
	service.now = func() time.Time { return now }
	require.ErrorIs(t, service.Authenticate(context.Background(), token, net.ParseIP("127.0.0.1")), ErrAccountObserverUnauthorized)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountObserverListAccountsNeverSerializesSecrets(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	updatedAt := now.Add(-time.Minute)
	extra := []byte(`{
		"access_token":"secret-access-token",
		"refresh_token":"secret-refresh-token",
		"cookie":"secret-cookie",
		"proxy_password":"secret-proxy-password",
		"session_window_utilization":0.42,
		"passive_usage_7d_utilization":0.73,
		"passive_usage_7d_reset":1784300000,
		"passive_usage_sampled_at":"2026-07-17T09:59:00Z"
	}`)
	columns := []string{
		"id", "name", "platform", "type", "status", "schedulable", "error_message",
		"last_used_at", "expires_at", "auto_pause_on_expired", "rate_limit_reset_at",
		"overload_until", "temp_unschedulable_until", "session_window_end",
		"created_at", "updated_at", "extra",
	}
	mock.ExpectQuery("SELECT id, name, platform, type, status, schedulable, error_message").
		WithArgs(int64(0), nil, 101).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			42, "Claude primary", PlatformAnthropic, AccountTypeOAuth, StatusActive, true, nil,
			nil, nil, true, nil, nil, nil, now.Add(time.Hour), now.Add(-24*time.Hour), updatedAt, extra,
		))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT instance_id::text FROM account_observer_instances WHERE singleton = TRUE")).
		WillReturnRows(sqlmock.NewRows([]string{"instance_id"}).AddRow("93d9048d-8a5e-497f-9872-80af714eec61"))

	usageService := &AccountUsageService{}
	service := NewAccountObserverService(db, usageService)
	service.now = func() time.Time { return now }
	page, err := service.ListAccounts(context.Background(), ObserverListParams{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.True(t, page.Items[0].Available)
	require.NotNil(t, page.Items[0].Quota)
	require.InDelta(t, 42, page.Items[0].Quota.Windows["five_hour"].Utilization, 0.001)
	require.InDelta(t, 73, page.Items[0].Quota.Windows["seven_day"].Utilization, 0.001)
	require.NotEmpty(t, page.ETag)

	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	body := string(encoded)
	for _, forbidden := range []string{
		"secret-access-token", "secret-refresh-token", "secret-cookie", "secret-proxy-password",
		"access_token", "refresh_token", "credentials", "proxy_password", "session_window_utilization",
	} {
		require.NotContains(t, body, forbidden)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestObserverCursorRoundTripAndValidation(t *testing.T) {
	cursor := encodeObserverCursor(9123)
	decoded, err := decodeObserverCursor(cursor)
	require.NoError(t, err)
	require.Equal(t, int64(9123), decoded)

	_, err = decodeObserverCursor("not-base64!")
	require.Error(t, err)
}

func TestObserverQuotaUnavailableDoesNotFailAccountMapping(t *testing.T) {
	service := &AccountObserverService{usageService: &AccountUsageService{}}
	row := &accountObserverRow{
		ID:          9,
		Name:        "Gemini",
		Platform:    PlatformGemini,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Extra:       map[string]any{"access_token": "must-not-leak"},
	}
	mapped := service.mapObserverAccount(row, time.Now())
	require.True(t, mapped.Available)
	require.Nil(t, mapped.Quota)
	require.Equal(t, "snapshot_unavailable", mapped.QuotaError.Code)
}

func TestObserverAvailabilityPrecedence(t *testing.T) {
	now := time.Now()
	row := &accountObserverRow{
		Status:                 StatusActive,
		Schedulable:            true,
		TempUnschedulableUntil: sql.NullTime{Time: now.Add(time.Minute), Valid: true},
		RateLimitResetAt:       sql.NullTime{Time: now.Add(2 * time.Minute), Valid: true},
	}
	state, available := observerAvailability(row, now)
	require.Equal(t, "temporarily_unavailable", state)
	require.False(t, available)
}
