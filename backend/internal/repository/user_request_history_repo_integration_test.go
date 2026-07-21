//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRequestHistoryCombinesOwnedSuccessAndErrors(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user := mustCreateUser(t, integrationEntClient, &service.User{Email: "request-history-" + suffix + "@test.local"})
	other := mustCreateUser(t, integrationEntClient, &service.User{Email: "request-history-other-" + suffix + "@test.local"})
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: user.ID, Key: "sk-history-" + suffix, Name: "history-key"})
	otherKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{UserID: other.ID, Key: "sk-history-other-" + suffix, Name: "other-key"})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "history-account-" + suffix})
	requestSuccess := "client:history-success-" + suffix
	requestPlaceholder := "client:history-placeholder-" + suffix
	requestError := "history-error-" + suffix
	requestOther := "history-other-" + suffix

	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE request_id IN ($1, $2)", requestError, requestOther)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE request_id IN ($1, $2)", requestSuccess, requestPlaceholder)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id IN ($1, $2)", apiKey.ID, otherKey.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id IN ($1, $2)", user.ID, other.ID)
	})

	usageRepo := newUsageLogRepositoryWithSQL(integrationEntClient, integrationDB)
	base := time.Now().UTC().Add(-time.Minute)
	_, err := usageRepo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: requestSuccess, Model: "gpt-success", RequestedModel: "gpt-visible",
		InputTokens: 10, OutputTokens: 5, TotalCost: 0.1, ActualCost: 0.1,
		CreatedAt: base,
	})
	require.NoError(t, err)
	_, err = usageRepo.Create(ctx, &service.UsageLog{
		UserID: user.ID, APIKeyID: apiKey.ID, AccountID: account.ID,
		RequestID: requestPlaceholder, Model: "gpt-placeholder", ActualCost: 0,
		CreatedAt: base.Add(time.Second),
	})
	require.NoError(t, err)

	opsRepo := &opsRepository{db: integrationDB}
	userID, apiKeyID := user.ID, apiKey.ID
	otherUserID, otherKeyID := other.ID, otherKey.ID
	_, err = opsRepo.InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
		RequestID: requestError, UserID: &userID, APIKeyID: &apiKeyID,
		Model: "gpt-error", RequestedModel: "gpt-visible", ErrorPhase: "upstream",
		ErrorType: "api_error", StatusCode: 502, ErrorMessage: "safe upstream failure",
		CreatedAt: base.Add(2 * time.Second),
	})
	require.NoError(t, err)
	_, err = opsRepo.InsertErrorLog(ctx, &service.OpsInsertErrorLogInput{
		RequestID: requestOther, UserID: &otherUserID, APIKeyID: &otherKeyID,
		Model: "gpt-other", ErrorPhase: "upstream", ErrorType: "api_error",
		StatusCode: 503, ErrorMessage: "must stay private", CreatedAt: base.Add(3 * time.Second),
	})
	require.NoError(t, err)

	filter := service.UserRequestHistoryFilter{UserID: user.ID, IncludeErrors: true}
	firstPage, total, err := opsRepo.ListUserRequestHistory(ctx, pagination.PaginationParams{Page: 1, PageSize: 1, SortBy: "created_at", SortOrder: "desc"}, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, firstPage, 1)
	require.Equal(t, service.UserRequestRecordError, firstPage[0].RecordType)
	require.Equal(t, requestError, firstPage[0].RequestID)
	require.Equal(t, "upstream", firstPage[0].Category)
	require.Equal(t, 502, firstPage[0].StatusCode)
	require.Equal(t, "history-key", firstPage[0].APIKey.Name)
	require.NotContains(t, firstPage[0].Message, "private")

	secondPage, total, err := opsRepo.ListUserRequestHistory(ctx, pagination.PaginationParams{Page: 2, PageSize: 1, SortBy: "created_at", SortOrder: "desc"}, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, secondPage, 1)
	require.Equal(t, service.UserRequestRecordSuccess, secondPage[0].RecordType)
	require.Equal(t, requestSuccess, secondPage[0].RequestID)
	require.Equal(t, "success", secondPage[0].Category)
	require.Equal(t, 200, secondPage[0].StatusCode)
	require.Equal(t, 10, secondPage[0].InputTokens)

	successOnly := filter
	successOnly.Category = "success"
	items, total, err := opsRepo.ListUserRequestHistory(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, successOnly)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, service.UserRequestRecordSuccess, items[0].RecordType)

	errorsDisabled := filter
	errorsDisabled.IncludeErrors = false
	items, total, err = opsRepo.ListUserRequestHistory(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, errorsDisabled)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, service.UserRequestRecordSuccess, items[0].RecordType)

	status := 502
	errorOnly := filter
	errorOnly.StatusCode = &status
	items, total, err = opsRepo.ListUserRequestHistory(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, errorOnly)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, service.UserRequestRecordError, items[0].RecordType)
}
