package admin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

func TestSanitizeAdminPaymentOrderForResponseAddsCurrency(t *testing.T) {
	now := time.Now()
	order := &dbent.PaymentOrder{
		ID:          1,
		UserID:      2,
		Amount:      100,
		PayAmount:   108,
		FeeRate:     8,
		OutTradeNo:  "sub2_202606250001",
		PaymentType: "stripe",
		OrderType:   "subscription",
		Status:      "COMPLETED",
		ExpiresAt:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"currency":       "USD",
		},
	}

	got := sanitizeAdminPaymentOrderForResponse(order)
	if got == nil {
		t.Fatal("expected sanitized order")
	}
	if got.Currency != "USD" {
		t.Fatalf("expected currency USD, got %q", got.Currency)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal sanitized order: %v", err)
	}
	if strings.Contains(string(body), "provider_snapshot") {
		t.Fatalf("expected provider_snapshot to be omitted, got %s", string(body))
	}
}

func TestSubscriptionPlanResponseFromEntKeepsRollingQuotaLimits(t *testing.T) {
	fiveHour := 5.0
	sevenDay := 70.0
	thirtyDay := 300.0
	plan := &dbent.SubscriptionPlan{
		ID:                7,
		FiveHourLimitUsd:  &fiveHour,
		SevenDayLimitUsd:  &sevenDay,
		ThirtyDayLimitUsd: &thirtyDay,
	}

	got := subscriptionPlanResponseFromEnt(plan)
	if got == nil {
		t.Fatal("expected plan response")
	}
	if got.FiveHourLimitUSD == nil || *got.FiveHourLimitUSD != fiveHour {
		t.Fatalf("expected five_hour_limit_usd %v, got %v", fiveHour, got.FiveHourLimitUSD)
	}
	if got.SevenDayLimitUSD == nil || *got.SevenDayLimitUSD != sevenDay {
		t.Fatalf("expected seven_day_limit_usd %v, got %v", sevenDay, got.SevenDayLimitUSD)
	}
	if got.ThirtyDayLimitUSD == nil || *got.ThirtyDayLimitUSD != thirtyDay {
		t.Fatalf("expected thirty_day_limit_usd %v, got %v", thirtyDay, got.ThirtyDayLimitUSD)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal plan response: %v", err)
	}
	for _, field := range []string{"five_hour_limit_usd", "seven_day_limit_usd", "thirty_day_limit_usd"} {
		if !strings.Contains(string(body), `"`+field+`"`) {
			t.Fatalf("expected %s in response JSON, got %s", field, body)
		}
	}
}
