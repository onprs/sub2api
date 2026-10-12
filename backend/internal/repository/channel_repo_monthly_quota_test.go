//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonthlyQuotaPersistsAndCanBeCleared(t *testing.T) {
	starts := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	policy := &service.MonthlyQuotaPolicy{BaseCreditsUSD: 20, Periods: []service.MonthlyQuotaPeriod{
		{CreditsUSD: 40, StartsAt: &starts, ExpiresAt: &expires},
	}}
	for _, operation := range []string{"create", "update", "clear"} {
		t.Run(operation, func(t *testing.T) {
			repo, mock := newChannelModelPricingTimePricingRepo(t)
			pricing := &service.ChannelModelPricing{ID: 11, ChannelID: 7, Platform: service.PlatformCommandCode,
				Models: []string{"mistral/mistral-large-4"}, MonthlyQuota: policy}
			var stored any
			if operation == "clear" {
				pricing.MonthlyQuota = nil
			} else {
				value, err := marshalChannelMonthlyQuota(policy)
				require.NoError(t, err)
				stored = value
			}
			if operation == "create" {
				mock.ExpectQuery(`INSERT INTO channel_model_pricing .*monthly_quota`).
					WithArgs(int64(7), service.PlatformCommandCode, []byte(`["mistral/mistral-large-4"]`), service.BillingModeToken,
						nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, stored).
					WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), time.Time{}, time.Time{}))
				require.NoError(t, repo.CreateModelPricing(context.Background(), pricing))
			} else {
				mock.ExpectExec(`(?s)UPDATE channel_model_pricing.*monthly_quota = \$16.*WHERE id = \$17`).
					WithArgs([]byte(`["mistral/mistral-large-4"]`), service.BillingModeToken,
						nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, service.PlatformCommandCode, stored, int64(11)).
					WillReturnResult(sqlmock.NewResult(0, 1))
				require.NoError(t, repo.UpdateModelPricing(context.Background(), pricing))
			}
			mock.ExpectQuery(`SELECT .*monthly_quota.*FROM channel_model_pricing`).WithArgs(int64(7)).
				WillReturnRows(sqlmock.NewRows(channelModelPricingTimePricingColumns).AddRow(
					int64(11), int64(7), service.PlatformCommandCode, `["mistral/mistral-large-4"]`, service.BillingModeToken,
					nil, nil, nil, nil, nil, nil, nil, "{}", nil, nil, nil, nil, stored, time.Time{}, time.Time{},
				))
			expectEmptyModelPricingIntervals(mock)
			loaded, err := repo.ListModelPricing(context.Background(), 7)
			require.NoError(t, err)
			require.Len(t, loaded, 1)
			require.Equal(t, pricing.MonthlyQuota, loaded[0].MonthlyQuota)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
