//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

func applyAntigravitySubscriptionResult(account *Account, result AntigravitySubscriptionResult) (map[string]any, map[string]any) {
	credentials := make(map[string]any)
	for k, v := range account.Credentials {
		credentials[k] = v
	}
	credentials["plan_type"] = result.PlanType

	extra := make(map[string]any)
	for k, v := range account.Extra {
		extra[k] = v
	}
	if result.SubscriptionStatus != "" {
		extra["subscription_status"] = result.SubscriptionStatus
	} else {
		delete(extra, "subscription_status")
	}
	if result.SubscriptionError != "" {
		extra["subscription_error"] = result.SubscriptionError
	} else {
		delete(extra, "subscription_error")
	}
	return credentials, extra
}

func TestApplyAntigravityPrivacyMode_SetsInMemoryExtra(t *testing.T) {
	account := &Account{}

	applyAntigravityPrivacyMode(account, AntigravityPrivacySet)

	if account.Extra == nil {
		t.Fatal("expected account.Extra to be initialized")
	}
	if got := account.Extra["privacy_mode"]; got != AntigravityPrivacySet {
		t.Fatalf("expected privacy_mode %q, got %v", AntigravityPrivacySet, got)
	}
}

func TestApplyAntigravityPrivacyMode_PreservedBySubscriptionResult(t *testing.T) {
	account := &Account{
		Credentials: map[string]any{
			"access_token": "token",
		},
		Extra: map[string]any{
			"existing": "value",
		},
	}
	applyAntigravityPrivacyMode(account, AntigravityPrivacySet)

	_, extra := applyAntigravitySubscriptionResult(account, AntigravitySubscriptionResult{
		PlanType: "Pro",
	})

	if got := extra["privacy_mode"]; got != AntigravityPrivacySet {
		t.Fatalf("expected subscription writeback to keep privacy_mode %q, got %v", AntigravityPrivacySet, got)
	}
	if got := extra["existing"]; got != "value" {
		t.Fatalf("expected existing extra fields to be preserved, got %v", got)
	}
}

type stubAntigravityPrivacyClient struct {
	fetchResponses []*antigravity.FetchUserInfoResponse
	fetchErr       error
	setResponse    *antigravity.SetUserSettingsResponse
	setErr         error

	fetchCalls  int
	setCalls    int
	setExisting map[string]any
}

func (s *stubAntigravityPrivacyClient) FetchUserInfo(_ context.Context, _, _ string) (*antigravity.FetchUserInfoResponse, error) {
	s.fetchCalls++
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	idx := s.fetchCalls - 1
	if idx >= len(s.fetchResponses) {
		idx = len(s.fetchResponses) - 1
	}
	return s.fetchResponses[idx], nil
}

func (s *stubAntigravityPrivacyClient) SetUserSettingsWithExisting(_ context.Context, _ string, existing map[string]any) (*antigravity.SetUserSettingsResponse, error) {
	s.setCalls++
	s.setExisting = existing
	if s.setErr != nil {
		return nil, s.setErr
	}
	return s.setResponse, nil
}

func TestSetAntigravityPrivacyWithClient_SkipsWriteWhenAlreadyPrivate(t *testing.T) {
	client := &stubAntigravityPrivacyClient{
		fetchResponses: []*antigravity.FetchUserInfoResponse{
			{UserSettings: map[string]any{"consumerTermsAccepted": true}},
		},
	}

	got := setAntigravityPrivacyWithClient(context.Background(), client, "tok", "proj-1")
	if got != AntigravityPrivacySet {
		t.Fatalf("got %q, want %q", got, AntigravityPrivacySet)
	}
	if client.fetchCalls != 1 {
		t.Fatalf("expected 1 fetchUserInfo call, got %d", client.fetchCalls)
	}
	if client.setCalls != 0 {
		t.Fatalf("expected setUserSettings to be skipped when already private, got %d calls", client.setCalls)
	}
}

func TestSetAntigravityPrivacyWithClient_ReadFirstClonesExistingAndVerifies(t *testing.T) {
	client := &stubAntigravityPrivacyClient{
		fetchResponses: []*antigravity.FetchUserInfoResponse{
			{UserSettings: map[string]any{
				"consumerTermsAccepted": true,
				"telemetryEnabled":      true,
			}},
			{UserSettings: map[string]any{
				"consumerTermsAccepted": true,
			}},
		},
		setResponse: &antigravity.SetUserSettingsResponse{
			UserSettings: map[string]any{
				"consumerTermsAccepted": true,
			},
		},
	}

	got := setAntigravityPrivacyWithClient(context.Background(), client, "tok", "proj-1")
	if got != AntigravityPrivacySet {
		t.Fatalf("got %q, want %q", got, AntigravityPrivacySet)
	}
	if client.fetchCalls != 2 {
		t.Fatalf("expected 2 fetchUserInfo calls (read-first + verify), got %d", client.fetchCalls)
	}
	if client.setCalls != 1 {
		t.Fatalf("expected 1 setUserSettings call, got %d", client.setCalls)
	}
	if accepted, _ := client.setExisting["consumerTermsAccepted"].(bool); !accepted {
		t.Fatalf("expected existing consumerTermsAccepted=true passed to SetUserSettingsWithExisting, got %v", client.setExisting)
	}
}
