//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type adminRedeemPlanSnapshotRepo struct {
	redeemRepoStub

	created []RedeemCode
}

func (r *adminRedeemPlanSnapshotRepo) Create(_ context.Context, code *RedeemCode) error {
	if code == nil {
		return nil
	}
	cp := *code
	if cp.ID == 0 {
		cp.ID = int64(len(r.created) + 1)
		code.ID = cp.ID
	}
	r.created = append(r.created, cp)
	return nil
}

func TestAdminService_GenerateRedeemCodesSnapshotsSubscriptionPlan(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	groupID := int64(42)
	five := 1.25
	seven := 7.5
	thirty := 30.5
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupID).
		SetName("Pro").
		SetDescription("Pro plan").
		SetPrice(19.9).
		SetValidityDays(2).
		SetValidityUnit("weeks").
		SetFeatures("[]").
		SetProductName("Pro").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(five).
		SetSevenDayLimitUsd(seven).
		SetThirtyDayLimitUsd(thirty).
		Save(ctx)
	require.NoError(t, err)

	redeemRepo := &adminRedeemPlanSnapshotRepo{}
	svc := &adminServiceImpl{
		entClient:      client,
		redeemCodeRepo: redeemRepo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription},
		},
	}

	codes, err := svc.GenerateRedeemCodes(ctx, &GenerateRedeemCodesInput{
		Count:              1,
		Type:               RedeemTypeSubscription,
		SubscriptionPlanID: &plan.ID,
	})

	require.NoError(t, err)
	require.Len(t, codes, 1)
	require.Len(t, redeemRepo.created, 1)
	code := codes[0]
	require.NotEmpty(t, code.Code)
	require.Equal(t, &plan.ID, code.SubscriptionPlanID)
	require.Equal(t, &groupID, code.GroupID)
	require.Equal(t, 14, code.ValidityDays)
	require.Equal(t, 1, code.SubscriptionQuotaSnapshotVersion)
	require.Equal(t, five, *code.FiveHourLimitUSD)
	require.Equal(t, seven, *code.SevenDayLimitUSD)
	require.Equal(t, thirty, *code.ThirtyDayLimitUSD)

	stored := redeemRepo.created[0]
	require.Equal(t, code.SubscriptionPlanID, stored.SubscriptionPlanID)
	require.Equal(t, code.GroupID, stored.GroupID)
	require.Equal(t, code.ValidityDays, stored.ValidityDays)
	require.Equal(t, code.SubscriptionQuotaSnapshotVersion, stored.SubscriptionQuotaSnapshotVersion)
	require.Equal(t, *code.FiveHourLimitUSD, *stored.FiveHourLimitUSD)
	require.Equal(t, *code.SevenDayLimitUSD, *stored.SevenDayLimitUSD)
	require.Equal(t, *code.ThirtyDayLimitUSD, *stored.ThirtyDayLimitUSD)
}

func TestAdminService_BuildRedeemCodeLegacySubscriptionPreservesNegativeValidityDays(t *testing.T) {
	ctx := context.Background()
	groupID := int64(42)
	svc := &adminServiceImpl{
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{ID: groupID, SubscriptionType: SubscriptionTypeSubscription},
		},
	}

	code, err := svc.BuildRedeemCode(ctx, RedeemCode{
		Type:         RedeemTypeSubscription,
		GroupID:      &groupID,
		ValidityDays: -7,
	})

	require.NoError(t, err)
	require.Equal(t, -7, code.ValidityDays)
	require.Zero(t, code.SubscriptionQuotaSnapshotVersion)
	require.Nil(t, code.SubscriptionPlanID)
	require.Nil(t, code.FiveHourLimitUSD)
	require.Nil(t, code.SevenDayLimitUSD)
	require.Nil(t, code.ThirtyDayLimitUSD)
}
