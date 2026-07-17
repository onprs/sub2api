package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	AccountObserverReadScope = "account_observer:read"
	accountObserverTokenTag  = "s2obs_"
	observerDefaultPageSize  = 100
	observerMaxPageSize      = 500
)

var (
	ErrAccountObserverUnauthorized  = errors.New("invalid account observer token")
	ErrAccountObserverForbidden     = errors.New("account observer token is not allowed from this address")
	ErrAccountObserverNotFound      = errors.New("account observer resource not found")
	ErrAccountObserverInvalidInput  = errors.New("invalid account observer input")
	ErrAccountObserverInvalidCursor = errors.New("invalid account observer cursor")
	ErrObserverQuotaUnavailable     = errors.New("stored quota snapshot is unavailable")
)

type AccountObserverToken struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	TokenPrefix  string     `json:"token_prefix"`
	Scope        string     `json:"scope"`
	AllowedCIDRs []string   `json:"allowed_cidrs"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CreateAccountObserverTokenInput struct {
	Name         string
	AllowedCIDRs []string
	ExpiresAt    *time.Time
}

type CreatedAccountObserverToken struct {
	AccountObserverToken
	Token string `json:"token"`
}

type ObserverListParams struct {
	Cursor       string
	UpdatedSince *time.Time
	Limit        int
}

type ObserverQuotaWindow struct {
	Utilization      float64    `json:"utilization"`
	ResetsAt         *time.Time `json:"resets_at,omitempty"`
	RemainingSeconds int        `json:"remaining_seconds"`
	Estimated        bool       `json:"estimated"`
}

type ObserverQuotaSnapshot struct {
	Source    string                         `json:"source,omitempty"`
	UpdatedAt *time.Time                     `json:"updated_at,omitempty"`
	Windows   map[string]ObserverQuotaWindow `json:"windows"`
}

type ObserverQuotaError struct {
	Code string `json:"code"`
}

type ObserverAccount struct {
	ID                     int64                  `json:"id"`
	Name                   string                 `json:"name"`
	Platform               string                 `json:"platform"`
	Type                   string                 `json:"type"`
	Status                 string                 `json:"status"`
	Availability           string                 `json:"availability"`
	Available              bool                   `json:"available"`
	LastUsedAt             *time.Time             `json:"last_used_at,omitempty"`
	ExpiresAt              *time.Time             `json:"expires_at,omitempty"`
	RateLimitResetAt       *time.Time             `json:"rate_limit_reset_at,omitempty"`
	OverloadUntil          *time.Time             `json:"overload_until,omitempty"`
	TempUnschedulableUntil *time.Time             `json:"temp_unschedulable_until,omitempty"`
	SessionWindowEnd       *time.Time             `json:"session_window_end,omitempty"`
	UpdatedAt              time.Time              `json:"updated_at"`
	Quota                  *ObserverQuotaSnapshot `json:"quota,omitempty"`
	QuotaError             *ObserverQuotaError    `json:"quota_error,omitempty"`
	SanitizedErrorCode     string                 `json:"error_code,omitempty"`
}

type ObserverAccountsPage struct {
	InstanceID  string            `json:"instance_id"`
	Items       []ObserverAccount `json:"items"`
	NextCursor  string            `json:"next_cursor,omitempty"`
	GeneratedAt time.Time         `json:"generated_at"`
	ETag        string            `json:"-"`
}

type accountObserverRow struct {
	ID                     int64
	Name                   string
	Platform               string
	Type                   string
	Status                 string
	Schedulable            bool
	ErrorMessage           sql.NullString
	LastUsedAt             sql.NullTime
	ExpiresAt              sql.NullTime
	AutoPauseOnExpired     bool
	RateLimitResetAt       sql.NullTime
	OverloadUntil          sql.NullTime
	TempUnschedulableUntil sql.NullTime
	SessionWindowEnd       sql.NullTime
	CreatedAt              time.Time
	UpdatedAt              time.Time
	Extra                  map[string]any
}

type AccountObserverService struct {
	db           *sql.DB
	usageService *AccountUsageService
	now          func() time.Time
}

func NewAccountObserverService(db *sql.DB, usageService *AccountUsageService) *AccountObserverService {
	return &AccountObserverService{db: db, usageService: usageService, now: time.Now}
}

func (s *AccountObserverService) CreateToken(ctx context.Context, input CreateAccountObserverTokenInput) (*CreatedAccountObserverToken, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: token name must contain 1 to 100 characters", ErrAccountObserverInvalidInput)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return nil, fmt.Errorf("%w: token expiry must be in the future", ErrAccountObserverInvalidInput)
	}
	cidrs, err := normalizeObserverCIDRs(input.AllowedCIDRs)
	if err != nil {
		return nil, err
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate observer token: %w", err)
	}
	plaintext := accountObserverTokenTag + base64.RawURLEncoding.EncodeToString(secret)
	prefix := observerTokenPrefix(plaintext)
	hash := observerTokenHash(plaintext)

	var record AccountObserverToken
	var allowed pq.StringArray
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO account_observer_tokens
			(name, token_prefix, token_hash, scope, allowed_cidrs, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, token_prefix, scope, allowed_cidrs, expires_at,
			revoked_at, last_used_at, created_at`,
		name, prefix, hash, AccountObserverReadScope, pq.Array(cidrs), input.ExpiresAt,
	).Scan(
		&record.ID, &record.Name, &record.TokenPrefix, &record.Scope, &allowed,
		&record.ExpiresAt, &record.RevokedAt, &record.LastUsedAt, &record.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create account observer token: %w", err)
	}
	record.AllowedCIDRs = []string(allowed)
	return &CreatedAccountObserverToken{AccountObserverToken: record, Token: plaintext}, nil
}

func (s *AccountObserverService) ListTokens(ctx context.Context) ([]AccountObserverToken, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_prefix, scope, allowed_cidrs, expires_at,
			revoked_at, last_used_at, created_at
		FROM account_observer_tokens
		ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list account observer tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tokens := make([]AccountObserverToken, 0)
	for rows.Next() {
		var token AccountObserverToken
		var allowed pq.StringArray
		if err := rows.Scan(
			&token.ID, &token.Name, &token.TokenPrefix, &token.Scope, &allowed,
			&token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt, &token.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan account observer token: %w", err)
		}
		token.AllowedCIDRs = []string(allowed)
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account observer tokens: %w", err)
	}
	return tokens, nil
}

func (s *AccountObserverService) RevokeToken(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE account_observer_tokens
		SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("revoke account observer token: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revoke result: %w", err)
	}
	if rows == 0 {
		return ErrAccountObserverNotFound
	}
	return nil
}

func (s *AccountObserverService) Authenticate(ctx context.Context, plaintext string, remoteIP net.IP) error {
	plaintext = strings.TrimSpace(plaintext)
	if !strings.HasPrefix(plaintext, accountObserverTokenTag) || len(plaintext) < len(accountObserverTokenTag)+16 {
		return ErrAccountObserverUnauthorized
	}
	prefix := observerTokenPrefix(plaintext)
	presentedHash := observerTokenHash(plaintext)

	var id int64
	var storedHash, scope string
	var expiresAt, revokedAt sql.NullTime
	var allowed pq.StringArray
	err := s.db.QueryRowContext(ctx, `
		SELECT id, token_hash, scope, allowed_cidrs, expires_at, revoked_at
		FROM account_observer_tokens
		WHERE token_prefix = $1
		ORDER BY id DESC
		LIMIT 1`, prefix,
	).Scan(&id, &storedHash, &scope, &allowed, &expiresAt, &revokedAt)
	if err != nil {
		return ErrAccountObserverUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(presentedHash)) != 1 || scope != AccountObserverReadScope {
		return ErrAccountObserverUnauthorized
	}
	now := s.now()
	if revokedAt.Valid || (expiresAt.Valid && !now.Before(expiresAt.Time)) {
		return ErrAccountObserverUnauthorized
	}
	if !observerIPAllowed(remoteIP, []string(allowed)) {
		return ErrAccountObserverForbidden
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE account_observer_tokens SET last_used_at = $2, updated_at = $2 WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("update account observer token usage: %w", err)
	}
	return nil
}

func (s *AccountObserverService) ListAccounts(ctx context.Context, params ObserverListParams) (*ObserverAccountsPage, error) {
	cursorID, err := decodeObserverCursor(params.Cursor)
	if err != nil {
		return nil, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = observerDefaultPageSize
	}
	if limit > observerMaxPageSize {
		limit = observerMaxPageSize
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, platform, type, status, schedulable, error_message,
			last_used_at, expires_at, auto_pause_on_expired, rate_limit_reset_at,
			overload_until, temp_unschedulable_until, session_window_end,
			created_at, updated_at, extra
		FROM accounts
		WHERE deleted_at IS NULL
			AND platform IN ('anthropic', 'openai', 'opencode_go', 'gemini', 'antigravity', 'grok')
			AND id > $1
			AND ($2::timestamptz IS NULL OR updated_at >= $2)
		ORDER BY id ASC
		LIMIT $3`, cursorID, params.UpdatedSince, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list observer accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	now := s.now().UTC()
	items := make([]ObserverAccount, 0, limit)
	var nextCursor string
	for rows.Next() {
		row, err := scanAccountObserverRow(rows)
		if err != nil {
			return nil, err
		}
		if len(items) == limit {
			nextCursor = encodeObserverCursor(items[len(items)-1].ID)
			break
		}
		items = append(items, s.mapObserverAccount(row, now))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate observer accounts: %w", err)
	}

	var instanceID string
	if err := s.db.QueryRowContext(ctx, `SELECT instance_id::text FROM account_observer_instances WHERE singleton = TRUE`).Scan(&instanceID); err != nil {
		return nil, fmt.Errorf("read account observer instance id: %w", err)
	}

	page := &ObserverAccountsPage{
		InstanceID:  instanceID,
		Items:       items,
		NextCursor:  nextCursor,
		GeneratedAt: now,
	}
	page.ETag, err = observerPageETag(page)
	if err != nil {
		return nil, err
	}
	return page, nil
}

func scanAccountObserverRow(rows *sql.Rows) (*accountObserverRow, error) {
	var row accountObserverRow
	var rawExtra []byte
	if err := rows.Scan(
		&row.ID, &row.Name, &row.Platform, &row.Type, &row.Status, &row.Schedulable,
		&row.ErrorMessage, &row.LastUsedAt, &row.ExpiresAt, &row.AutoPauseOnExpired,
		&row.RateLimitResetAt, &row.OverloadUntil, &row.TempUnschedulableUntil,
		&row.SessionWindowEnd, &row.CreatedAt, &row.UpdatedAt, &rawExtra,
	); err != nil {
		return nil, fmt.Errorf("scan observer account: %w", err)
	}
	if len(rawExtra) > 0 {
		if err := json.Unmarshal(rawExtra, &row.Extra); err != nil {
			row.Extra = nil
		}
	}
	return &row, nil
}

func (s *AccountObserverService) mapObserverAccount(row *accountObserverRow, now time.Time) ObserverAccount {
	availability, available := observerAvailability(row, now)
	account := ObserverAccount{
		ID:                     row.ID,
		Name:                   row.Name,
		Platform:               row.Platform,
		Type:                   row.Type,
		Status:                 row.Status,
		Availability:           availability,
		Available:              available,
		LastUsedAt:             nullTimePointer(row.LastUsedAt),
		ExpiresAt:              nullTimePointer(row.ExpiresAt),
		RateLimitResetAt:       nullTimePointer(row.RateLimitResetAt),
		OverloadUntil:          nullTimePointer(row.OverloadUntil),
		TempUnschedulableUntil: nullTimePointer(row.TempUnschedulableUntil),
		SessionWindowEnd:       nullTimePointer(row.SessionWindowEnd),
		UpdatedAt:              row.UpdatedAt.UTC(),
	}
	if row.ErrorMessage.Valid && strings.TrimSpace(row.ErrorMessage.String) != "" {
		account.SanitizedErrorCode = "upstream_account_error"
	}

	usageAccount := &Account{
		ID:               row.ID,
		Platform:         row.Platform,
		Type:             row.Type,
		Extra:            row.Extra,
		ExpiresAt:        account.ExpiresAt,
		SessionWindowEnd: account.SessionWindowEnd,
		UpdatedAt:        row.UpdatedAt,
	}
	var usage *UsageInfo
	var err error
	if s.usageService == nil {
		err = ErrObserverQuotaUnavailable
	} else {
		usage, err = s.usageService.GetStoredUsageSnapshot(usageAccount, now)
	}
	if err != nil {
		account.QuotaError = &ObserverQuotaError{Code: "snapshot_unavailable"}
	} else {
		account.Quota = observerQuotaFromUsage(usage)
	}
	return account
}

func observerAvailability(row *accountObserverRow, now time.Time) (string, bool) {
	if row.Status != StatusActive {
		return row.Status, false
	}
	if row.AutoPauseOnExpired && row.ExpiresAt.Valid && !now.Before(row.ExpiresAt.Time) {
		return "expired", false
	}
	if row.TempUnschedulableUntil.Valid && now.Before(row.TempUnschedulableUntil.Time) {
		return "temporarily_unavailable", false
	}
	if row.RateLimitResetAt.Valid && now.Before(row.RateLimitResetAt.Time) {
		return "rate_limited", false
	}
	if row.OverloadUntil.Valid && now.Before(row.OverloadUntil.Time) {
		return "overloaded", false
	}
	if !row.Schedulable {
		return "paused", false
	}
	return "available", true
}

func observerQuotaFromUsage(usage *UsageInfo) *ObserverQuotaSnapshot {
	if usage == nil {
		return nil
	}
	windows := make(map[string]ObserverQuotaWindow)
	add := func(name string, progress *UsageProgress) {
		if progress == nil {
			return
		}
		windows[name] = ObserverQuotaWindow{
			Utilization:      progress.Utilization,
			ResetsAt:         progress.ResetsAt,
			RemainingSeconds: progress.RemainingSeconds,
			Estimated:        progress.Estimated,
		}
	}
	add("five_hour", usage.FiveHour)
	add("seven_day", usage.SevenDay)
	add("thirty_day", usage.ThirtyDay)
	add("seven_day_sonnet", usage.SevenDaySonnet)
	add("seven_day_fable", usage.SevenDayFable)
	add("gemini_shared_daily", usage.GeminiSharedDaily)
	add("gemini_pro_daily", usage.GeminiProDaily)
	add("gemini_flash_daily", usage.GeminiFlashDaily)
	if len(windows) == 0 {
		return nil
	}
	return &ObserverQuotaSnapshot{Source: usage.Source, UpdatedAt: usage.UpdatedAt, Windows: windows}
}

func normalizeObserverCIDRs(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{}, nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid allowed CIDR %q", ErrAccountObserverInvalidInput, value)
		}
		canonical := network.String()
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}

func observerIPAllowed(ip net.IP, cidrs []string) bool {
	if len(cidrs) == 0 {
		return true
	}
	if ip == nil {
		return false
	}
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func observerTokenPrefix(token string) string {
	const suffixLength = 10
	if len(token) <= len(accountObserverTokenTag)+suffixLength {
		return token
	}
	return token[:len(accountObserverTokenTag)+suffixLength]
}

func observerTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func encodeObserverCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("v1:%d", id)))
}

func decodeObserverCursor(cursor string) (int64, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, ErrAccountObserverInvalidCursor
	}
	var id int64
	if _, err := fmt.Sscanf(string(raw), "v1:%d", &id); err != nil || id < 0 {
		return 0, ErrAccountObserverInvalidCursor
	}
	return id, nil
}

func observerPageETag(page *ObserverAccountsPage) (string, error) {
	canonical := struct {
		InstanceID string            `json:"instance_id"`
		Items      []ObserverAccount `json:"items"`
		NextCursor string            `json:"next_cursor"`
	}{page.InstanceID, page.Items, page.NextCursor}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode observer etag: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return `"` + hex.EncodeToString(sum[:]) + `"`, nil
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
