//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAccountStatsChannelPricingRuleRoundTrip(t *testing.T) {
	repo, mock := newChannelModelPricingTimePricingRepo(t)
	ctx := context.Background()
	rule := &service.AccountStatsPricingRule{ChannelID: 7, Name: "渠道原价 × 模型特有倍率",
		PricingSource: service.AccountStatsPricingSourceChannel, Platform: service.PlatformOpenCodeGo}
	mock.ExpectBegin()
	tx, err := repo.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`INSERT INTO channel_account_stats_pricing_rules .*pricing_source, platform`).
		WithArgs(int64(7), rule.Name, service.AccountStatsPricingSourceChannel, service.PlatformOpenCodeGo,
			pq.Array([]int64{}), pq.Array([]int64{}), 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(9), time.Time{}, time.Time{}))
	require.NoError(t, createAccountStatsPricingRuleTx(ctx, tx, rule))
	mock.ExpectCommit()
	require.NoError(t, tx.Commit())
	mock.ExpectQuery(`(?s)SELECT .*pricing_source, platform.*FROM channel_account_stats_pricing_rules`).
		WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{
		"id", "channel_id", "name", "pricing_source", "platform", "group_ids", "account_ids", "sort_order", "created_at", "updated_at",
	}).AddRow(int64(9), int64(7), rule.Name, service.AccountStatsPricingSourceChannel, service.PlatformOpenCodeGo, "{}", "{}", 0, time.Time{}, time.Time{}))
	mock.ExpectQuery(`(?s)SELECT .*FROM channel_account_stats_model_pricing`).
		WithArgs(pq.Array([]int64{9})).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	loaded, err := repo.batchLoadAccountStatsPricingRules(ctx, []int64{7})
	require.NoError(t, err)
	require.Len(t, loaded[7], 1)
	require.Equal(t, rule.PricingSource, loaded[7][0].PricingSource)
	require.Equal(t, rule.Platform, loaded[7][0].Platform)
	require.Empty(t, loaded[7][0].Pricing, "同步规则只保存来源，模型和价格从渠道读取")
	require.NoError(t, mock.ExpectationsWereMet())
}
