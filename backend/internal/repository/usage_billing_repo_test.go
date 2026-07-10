package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingRepositoryApply_FallsBackToExhaustSubscriptionWhenResponseCostExceedsRemainingQuota(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := NewUsageBillingRepository(nil, db)

	const (
		userID         int64   = 77
		groupID        int64   = 11
		apiKeyID       int64   = 70
		subscriptionID int64   = 19
		requestID              = "overrun-request"
		costUSD        float64 = 0.02
	)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").
		WithArgs(requestID, apiKeyID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery("SELECT request_fingerprint\\s+FROM usage_billing_dedup_archive").
		WithArgs(requestID, apiKeyID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT us.id\\s+FROM user_subscriptions us").
		WithArgs(userID, groupID, costUSD, service.SubscriptionStatusActive).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT us.id\\s+FROM user_subscriptions us").
		WithArgs(userID, groupID, costUSD, service.SubscriptionStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(subscriptionID))
	mock.ExpectExec("UPDATE user_subscriptions us").
		WithArgs(costUSD, subscriptionID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.Apply(context.Background(), &service.UsageBillingCommand{
		RequestID:        requestID,
		APIKeyID:         apiKeyID,
		UserID:           userID,
		GroupID:          ptrInt64(groupID),
		SubscriptionCost: costUSD,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, subscriptionID, *result.SubscriptionID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrInt64(v int64) *int64 {
	return &v
}
