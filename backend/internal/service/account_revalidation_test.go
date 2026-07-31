package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountRevalidationRepoStub struct {
	AccountRepository
	accounts map[int64]*Account
}

func (s *accountRevalidationRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account := s.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	copy := *account
	return &copy, nil
}

func TestGatewayRevalidateSelectedAccountRejectsDisabledAccountAfterWait(t *testing.T) {
	groupID := int64(9)
	repo := &accountRevalidationRepoStub{accounts: map[int64]*Account{
		3: {
			ID:            3,
			Platform:      PlatformAnthropic,
			Type:          AccountTypeAPIKey,
			Status:        StatusDisabled,
			Schedulable:   true,
			AccountGroups: []AccountGroup{{AccountID: 3, GroupID: groupID}},
		},
	}}
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"sticky-session": 3}}
	svc := &GatewayService{accountRepo: repo, cache: cache}

	_, err := svc.RevalidateSelectedAccount(context.Background(), 3, AccountEligibilityRequest{
		GroupID:          &groupID,
		SessionHash:      "sticky-session",
		RequestedModel:   "claude-sonnet-4-5",
		ExpectedPlatform: PlatformAnthropic,
	})

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Equal(t, 1, cache.deletedSessions["sticky-session"])
}

func TestGatewayRevalidateSelectedAccountReturnsFreshSchedulableAccount(t *testing.T) {
	groupID := int64(9)
	repo := &accountRevalidationRepoStub{accounts: map[int64]*Account{
		3: {
			ID:            3,
			Name:          "fresh-account",
			Platform:      PlatformAnthropic,
			Type:          AccountTypeAPIKey,
			Status:        StatusActive,
			Schedulable:   true,
			AccountGroups: []AccountGroup{{AccountID: 3, GroupID: groupID}},
		},
	}}
	svc := &GatewayService{accountRepo: repo}

	account, err := svc.RevalidateSelectedAccount(context.Background(), 3, AccountEligibilityRequest{
		GroupID:          &groupID,
		RequestedModel:   "claude-sonnet-4-5",
		ExpectedPlatform: PlatformAnthropic,
	})

	require.NoError(t, err)
	require.Equal(t, "fresh-account", account.Name)
}

func TestOpenAIRevalidateSelectedAccountRejectsAccountRemovedFromGroup(t *testing.T) {
	groupID := int64(9)
	repo := &accountRevalidationRepoStub{accounts: map[int64]*Account{
		4: {
			ID:          4,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"api_key":       "sk-test",
				"model_mapping": map[string]any{"gpt-5.1": "gpt-5.1"},
			},
		},
	}}
	cache := &stubGatewayCache{sessionBindings: map[string]int64{"sticky-session": 4}}
	svc := &OpenAIGatewayService{accountRepo: repo, cache: cache}

	_, err := svc.RevalidateSelectedAccount(context.Background(), 4, OpenAIAccountEligibilityRequest{
		GroupID:            &groupID,
		SessionHash:        "sticky-session",
		Platform:           PlatformOpenAI,
		RequestedModel:     "gpt-5.1",
		RequiredCapability: OpenAIEndpointCapabilityChatCompletions,
		RequiredTransport:  OpenAIUpstreamTransportAny,
	})

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.NotEmpty(t, cache.deletedSessions)
}
