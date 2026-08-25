package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const opencodeGoConsoleHTMLFixture = `<html><head><script>
self.$R=self.$R||[];
_$HY.r["go.referral.get[\"wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ\"]"]=$R[13];
_$HY.r["lite.subscription.get[\"wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ\"]"]=$R[17];
($R[28]=(r,d)=>{r.s(d),r.p.s=1,r.p.v=d})($R[18],$R[33]={mine:!0,useBalance:!1,rollingUsage:$R[34]={status:"ok",resetInSec:5590,usagePercent:19,usage:950,limit:5000},weeklyUsage:$R[35]={status:"ok",resetInSec:588490,usagePercent:7,usage:350,limit:5000},monthlyUsage:$R[36]={status:"ok",resetInSec:2265176,usagePercent:10,usage:1000,limit:10000}});
$R[28]($R[19],$R[41]={balance:0,monthlyUsage:null});
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

func TestParseOpenCodeGoConsolePageAcceptsUsageObjectShapeVariations(t *testing.T) {
	fetchedAt := time.Date(2026, 6, 22, 4, 50, 0, 0, time.UTC)
	legacyHTML := strings.NewReplacer(
		",usage:950,limit:5000", "",
		",usage:350,limit:5000", "",
		",usage:1000,limit:10000", "",
	).Replace(opencodeGoConsoleHTMLFixture)
	reorderedHTML := strings.NewReplacer(
		`status:"ok",resetInSec:5590,usagePercent:19,usage:950,limit:5000`,
		`limit:5000,usagePercent:19,status:"ok",usage:950,resetInSec:5590`,
		`status:"ok",resetInSec:588490,usagePercent:7,usage:350,limit:5000`,
		`resetInSec:588490,limit:5000,status:"ok",usagePercent:7,usage:350`,
		`status:"ok",resetInSec:2265176,usagePercent:10,usage:1000,limit:10000`,
		`usage:1000,status:"ok",resetInSec:2265176,limit:10000,usagePercent:10`,
	).Replace(opencodeGoConsoleHTMLFixture)
	directObjectHTML := strings.NewReplacer(
		`rollingUsage:$R[34]=`, `rollingUsage:`,
		`weeklyUsage:$R[35]=`, `weeklyUsage:`,
		`monthlyUsage:$R[36]=`, `monthlyUsage:`,
	).Replace(opencodeGoConsoleHTMLFixture)
	decimalPercentHTML := strings.Replace(opencodeGoConsoleHTMLFixture, "usagePercent:19", "usagePercent:19.4", 1)

	for _, tt := range []struct {
		name            string
		html            string
		fiveHourPercent float64
	}{
		{name: "legacy fields", html: legacyHTML, fiveHourPercent: 19},
		{name: "reordered fields", html: reorderedHTML, fiveHourPercent: 19},
		{name: "direct objects", html: directObjectHTML, fiveHourPercent: 19},
		{name: "decimal percentage", html: decimalPercentHTML, fiveHourPercent: 19.4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := ParseOpenCodeGoConsolePage(tt.html, fetchedAt)

			require.NoError(t, err)
			require.Equal(t, tt.fiveHourPercent, summary.Usage.FiveHour.UsagePercent)
			require.Equal(t, 7.0, summary.Usage.SevenDay.UsagePercent)
			require.Equal(t, 10.0, summary.Usage.ThirtyDay.UsagePercent)
		})
	}
}

func TestParseOpenCodeGoConsolePageSkipsInvalidUsageCandidate(t *testing.T) {
	invalidCandidate := `({rollingUsage:{status:"ok",resetInSec:1,usagePercent:99},weeklyUsage:{status:"ok",resetInSec:2,usagePercent:99},monthlyUsage:{resetInSec:3,usagePercent:99}});`
	html := strings.Replace(opencodeGoConsoleHTMLFixture, `<script>`, `<script>`+invalidCandidate, 1)

	summary, err := ParseOpenCodeGoConsolePage(html, time.Now())

	require.NoError(t, err)
	require.Equal(t, 19.0, summary.Usage.FiveHour.UsagePercent)
	require.Equal(t, 7.0, summary.Usage.SevenDay.UsagePercent)
	require.Equal(t, 10.0, summary.Usage.ThirtyDay.UsagePercent)
}

func TestParseOpenCodeGoConsolePageRejectsAmbiguousUsageCandidates(t *testing.T) {
	secondCandidate := `({rollingUsage:{status:"ok",resetInSec:10,usagePercent:20},weeklyUsage:{status:"ok",resetInSec:20,usagePercent:30},monthlyUsage:{status:"ok",resetInSec:30,usagePercent:40}});`
	html := strings.Replace(opencodeGoConsoleHTMLFixture, `</script>`, secondCandidate+`</script>`, 1)

	_, err := ParseOpenCodeGoConsolePage(html, time.Now())

	require.ErrorContains(t, err, "multiple opencode go usage summaries found")
}

func TestParseOpenCodeGoConsolePageDoesNotCombineUsageAcrossParentObjects(t *testing.T) {
	html := `<script>const path="/workspace/wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ/go";({rollingUsage:{status:"ok",resetInSec:1,usagePercent:10},weeklyUsage:{status:"ok",resetInSec:2,usagePercent:20}});({monthlyUsage:{status:"ok",resetInSec:3,usagePercent:30}});</script>`

	_, err := ParseOpenCodeGoConsolePage(html, time.Now())

	require.ErrorContains(t, err, "opencode go usage summary not found")
}

func TestParseOpenCodeGoConsolePageRejectsMalformedUsageFields(t *testing.T) {
	for _, tt := range []struct {
		name         string
		oldValue     string
		invalidValue string
	}{
		{name: "reset seconds suffix", oldValue: "resetInSec:5590", invalidValue: "resetInSec:1e3"},
		{name: "usage percent suffix", oldValue: "usagePercent:19", invalidValue: "usagePercent:19invalid"},
		{name: "duplicate status", oldValue: `status:"ok",resetInSec:5590`, invalidValue: `status:"ok",status:"blocked",resetInSec:5590`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			html := strings.Replace(opencodeGoConsoleHTMLFixture, tt.oldValue, tt.invalidValue, 1)

			_, err := ParseOpenCodeGoConsolePage(html, time.Now())

			require.Error(t, err)
		})
	}
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
