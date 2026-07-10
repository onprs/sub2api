package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestMergeBalanceHistoryCodesIncludesAffiliateTransfersByDefault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(time.Hour)

	usedBy := int64(10)
	redeemCodes := []RedeemCode{
		{
			ID:        1,
			Type:      RedeemTypeBalance,
			Value:     8,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        2,
			Type:      RedeemTypeConcurrency,
			Value:     1,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &older,
			CreatedAt: older,
		},
	}
	affiliateCodes := []RedeemCode{
		{
			ID:        -20,
			Type:      RedeemTypeAffiliateBalance,
			Value:     3.5,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &newer,
			CreatedAt: newer,
		},
	}

	got := mergeBalanceHistoryCodes(pagination.PaginationParams{
		Page:     1,
		PageSize: 2,
	}, redeemCodes, affiliateCodes)

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeAffiliateBalance, got[0].Type)
	require.Equal(t, RedeemTypeBalance, got[1].Type)
}

func TestMergeBalanceHistoryCodesPaginatesAfterCombiningSources(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	usedBy := int64(10)
	at := func(hours int) *time.Time {
		v := base.Add(time.Duration(hours) * time.Hour)
		return &v
	}

	got := mergeBalanceHistoryCodes(
		pagination.PaginationParams{Page: 2, PageSize: 2},
		[]RedeemCode{
			{ID: 1, Type: RedeemTypeBalance, UsedBy: &usedBy, UsedAt: at(4), CreatedAt: *at(4)},
			{ID: 2, Type: RedeemTypeConcurrency, UsedBy: &usedBy, UsedAt: at(2), CreatedAt: *at(2)},
		},
		[]RedeemCode{
			{ID: -3, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(3), CreatedAt: *at(3)},
			{ID: -4, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(1), CreatedAt: *at(1)},
		},
	)

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeConcurrency, got[0].Type)
	require.Equal(t, int64(-4), got[1].ID)
}

func TestMergeBalanceHistoryCodesIncludesSubscriptionOrdersByDefault(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	usedBy := int64(10)
	at := func(hours int) *time.Time {
		v := base.Add(time.Duration(hours) * time.Hour)
		return &v
	}

	got := mergeBalanceHistoryCodes(
		pagination.PaginationParams{Page: 1, PageSize: 3},
		[]RedeemCode{
			{ID: 1, Source: "redeem_code", SourceID: 1, Type: RedeemTypeBalance, UsedBy: &usedBy, UsedAt: at(1), CreatedAt: *at(1)},
		},
		[]RedeemCode{
			{ID: -2, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(2), CreatedAt: *at(2)},
		},
		[]RedeemCode{
			{ID: -3, Source: "payment_order", SourceID: 3, Type: RedeemTypeSubscription, UsedBy: &usedBy, UsedAt: at(3), CreatedAt: *at(3)},
		},
	)

	require.Len(t, got, 3)
	require.Equal(t, "payment_order", got[0].Source)
	require.Equal(t, RedeemTypeAffiliateBalance, got[1].Type)
	require.Equal(t, "redeem_code", got[2].Source)
}

func TestGetUserBalanceHistorySubscriptionIncludesCompletedPaymentOrders(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user := client.User.Create().
		SetEmail("subscription-history@example.com").
		SetPasswordHash("hash").
		SetUsername("subscription-history").
		SaveX(ctx)
	group := client.Group.Create().
		SetName("History Group").
		SetStatus(payment.EntityStatusActive).
		SetSubscriptionType(SubscriptionTypeSubscription).
		SaveX(ctx)
	plan := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName("History Plan").
		SetPrice(9.99).
		SetValidityDays(30).
		SetValidityUnit("days").
		SaveX(ctx)
	completedAt := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	days := 30
	order := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(plan.Price).
		SetPayAmount(plan.Price).
		SetRechargeCode("PAY-HISTORY").
		SetOutTradeNo("sub2_history_order").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade_history").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(completedAt.Add(time.Hour)).
		SetCompletedAt(completedAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		SetPlanID(plan.ID).
		SetSubscriptionGroupID(group.ID).
		SetSubscriptionDays(days).
		SaveX(ctx)
	redeemUsedAt := completedAt.Add(-time.Hour)
	redeemRepo := &balanceHistoryRedeemRepoStub{
		codesByType: map[string][]RedeemCode{
			RedeemTypeSubscription: {
				{
					ID:                 7,
					Source:             "redeem_code",
					SourceID:           7,
					Code:               "RC-SUB",
					Type:               RedeemTypeSubscription,
					Value:              15,
					Status:             StatusUsed,
					UsedBy:             &user.ID,
					UsedAt:             &redeemUsedAt,
					CreatedAt:          redeemUsedAt,
					GroupID:            &group.ID,
					SubscriptionPlanID: &plan.ID,
				},
			},
		},
		totalRecharged: 100,
	}
	svc := &adminServiceImpl{entClient: client, redeemCodeRepo: redeemRepo}

	codes, total, totalRecharged, err := svc.GetUserBalanceHistory(ctx, user.ID, 1, 10, RedeemTypeSubscription)

	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Equal(t, 100.0, totalRecharged)
	require.Len(t, codes, 2)
	require.Equal(t, "payment_order", codes[0].Source)
	require.Equal(t, order.ID, codes[0].SourceID)
	require.NotNil(t, codes[0].PaymentOrderID)
	require.Equal(t, order.ID, *codes[0].PaymentOrderID)
	require.Equal(t, RedeemTypeSubscription, codes[0].Type)
	require.Equal(t, float64(days), codes[0].Value)
	require.Equal(t, order.OutTradeNo, codes[0].Code)
	require.Equal(t, &group.ID, codes[0].GroupID)
	require.Equal(t, &plan.ID, codes[0].SubscriptionPlanID)
	require.Equal(t, "redeem_code", codes[1].Source)
}

type balanceHistoryRedeemRepoStub struct {
	codesByType    map[string][]RedeemCode
	totalRecharged float64
}

func (s *balanceHistoryRedeemRepoStub) Create(context.Context, *RedeemCode) error {
	return errors.New("unexpected Create call")
}

func (s *balanceHistoryRedeemRepoStub) CreateBatch(context.Context, []RedeemCode) error {
	return errors.New("unexpected CreateBatch call")
}

func (s *balanceHistoryRedeemRepoStub) GetByID(context.Context, int64) (*RedeemCode, error) {
	return nil, errors.New("unexpected GetByID call")
}

func (s *balanceHistoryRedeemRepoStub) GetByCode(context.Context, string) (*RedeemCode, error) {
	return nil, errors.New("unexpected GetByCode call")
}

func (s *balanceHistoryRedeemRepoStub) Update(context.Context, *RedeemCode) error {
	return errors.New("unexpected Update call")
}

func (s *balanceHistoryRedeemRepoStub) BatchUpdate(context.Context, []int64, RedeemCodeBatchUpdateFields) (int64, error) {
	return 0, errors.New("unexpected BatchUpdate call")
}

func (s *balanceHistoryRedeemRepoStub) Delete(context.Context, int64) error {
	return errors.New("unexpected Delete call")
}

func (s *balanceHistoryRedeemRepoStub) Use(context.Context, int64, int64) error {
	return errors.New("unexpected Use call")
}

func (s *balanceHistoryRedeemRepoStub) List(context.Context, pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	return nil, nil, errors.New("unexpected List call")
}

func (s *balanceHistoryRedeemRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	return nil, nil, errors.New("unexpected ListWithFilters call")
}

func (s *balanceHistoryRedeemRepoStub) ListByUser(context.Context, int64, int) ([]RedeemCode, error) {
	return nil, errors.New("unexpected ListByUser call")
}

func (s *balanceHistoryRedeemRepoStub) ListByUserPaginated(_ context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]RedeemCode, *pagination.PaginationResult, error) {
	codes := append([]RedeemCode(nil), s.codesByType[codeType]...)
	if codeType == "" {
		codes = nil
		for _, source := range s.codesByType {
			codes = append(codes, source...)
		}
	}
	for i := range codes {
		if codes[i].Source == "" {
			codes[i].Source = "redeem_code"
		}
		if codes[i].SourceID == 0 {
			codes[i].SourceID = codes[i].ID
		}
		if codes[i].UsedBy == nil {
			usedBy := userID
			codes[i].UsedBy = &usedBy
		}
	}
	total := int64(len(codes))
	offset := params.Offset()
	if offset >= len(codes) {
		return nil, &pagination.PaginationResult{Total: total}, nil
	}
	end := offset + params.Limit()
	if end > len(codes) {
		end = len(codes)
	}
	return codes[offset:end], &pagination.PaginationResult{Total: total}, nil
}

func (s *balanceHistoryRedeemRepoStub) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	return s.totalRecharged, nil
}
