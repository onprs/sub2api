package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const opencodeGoConsoleHTMLFixture = `<html><head><script>
self.$R=self.$R||[];
_$HY.r["go.referral.get[\"wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ\"]"]=$R[13];
_$HY.r["lite.subscription.get[\"wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ\"]"]=$R[17];
($R[28]=(r,d)=>{r.s(d),r.p.s=1,r.p.v=d})($R[18],$R[33]={mine:!0,useBalance:!1,rollingUsage:$R[34]={status:"ok",resetInSec:5590,usagePercent:19},weeklyUsage:$R[35]={status:"ok",resetInSec:588490,usagePercent:7},monthlyUsage:$R[36]={status:"ok",resetInSec:2265176,usagePercent:10}});
$R[28]($R[14],$R[37]={referralCode:"M5N4TCC0GA",hasReferral:!0,rewardAmount:500,rewards:$R[38]=[$R[39]={id:"ref_01KVD1PJPG6B1GWZYZNAQZBQ2T",source:"invitee",status:"available",email:"ljb061121@gmail.com",amount:500,timeCreated:$R[40]=new Date("2026-06-18T09:44:47.000Z"),timeApplied:null}]});
</script></head><body></body></html>`

const opencodeGoConsoleJSFixture = `
const queryGoReferralUsagePreview_query = createServerReference("46625df0aecf05f270f7ae4612cde374d11350c8abaf8649027572228b8af150");
const queryGoReferralUsagePreview = query(queryGoReferralUsagePreview_query, "go.referral.usagePreview");
const applyGoReferralReward_action = createServerReference("f386778c1b78eade3e6acff87c9284e02fcd86826463c080526143c4fe8fff23");
const applyGoReferralReward = action(applyGoReferralReward_action, "go.referral.reward.apply");
`

func TestParseOpenCodeGoConsolePageExtractsUsageAndReferral(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 22, 4, 50, 0, 0, time.UTC)

	summary, err := ParseOpenCodeGoConsolePage(opencodeGoConsoleHTMLFixture, fetchedAt)

	require.NoError(t, err)
	require.Equal(t, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ", summary.WorkspaceID)
	require.Equal(t, fetchedAt, summary.FetchedAt)
	require.Equal(t, 19.0, summary.Usage.FiveHour.UsagePercent)
	require.Equal(t, 5590, summary.Usage.FiveHour.ResetInSec)
	require.Equal(t, fetchedAt.Add(5590*time.Second), *summary.Usage.FiveHour.ResetsAt)
	require.Equal(t, 7.0, summary.Usage.SevenDay.UsagePercent)
	require.Equal(t, 588490, summary.Usage.SevenDay.ResetInSec)
	require.Equal(t, 10.0, summary.Usage.ThirtyDay.UsagePercent)
	require.Equal(t, 2265176, summary.Usage.ThirtyDay.ResetInSec)

	require.Equal(t, "M5N4TCC0GA", summary.Referral.ReferralCode)
	require.Equal(t, 500, summary.Referral.RewardAmountCents)
	require.Len(t, summary.Referral.Rewards, 1)
	reward := summary.Referral.Rewards[0]
	require.Equal(t, "ref_01KVD1PJPG6B1GWZYZNAQZBQ2T", reward.ID)
	require.Equal(t, "invitee", reward.Source)
	require.Equal(t, "available", reward.Status)
	require.Equal(t, "l********@gmail.com", reward.MaskedEmail)
	require.Empty(t, reward.Email, "plain reward email must not be exposed by the parser result")
	require.Equal(t, 500, reward.AmountCents)
	require.Equal(t, time.Date(2026, 6, 18, 9, 44, 47, 0, time.UTC), *reward.TimeCreated)
	require.Nil(t, reward.TimeApplied)
}

func TestParseOpenCodeGoServerActionsFromAssets(t *testing.T) {
	actions, err := ParseOpenCodeGoServerActions(map[string]string{
		"/_build/assets/index-DtPYjwk4.js": opencodeGoConsoleJSFixture,
	})

	require.NoError(t, err)
	require.Equal(t, "46625df0aecf05f270f7ae4612cde374d11350c8abaf8649027572228b8af150", actions.ReferralUsagePreview)
	require.Equal(t, "f386778c1b78eade3e6acff87c9284e02fcd86826463c080526143c4fe8fff23", actions.ReferralRewardApply)
}

func TestMaskOpenCodeGoReferralEmail(t *testing.T) {
	require.Equal(t, "l********@gmail.com", MaskOpenCodeGoReferralEmail("ljb061121@gmail.com"))
	require.Equal(t, "a*@example.com", MaskOpenCodeGoReferralEmail("ab@example.com"))
	require.Equal(t, "", MaskOpenCodeGoReferralEmail(""))
}
