package service

import (
	"context"
	"fmt"
)

// AccountEligibilityRequest describes the routing contract that must still be
// true after an account concurrency wait completes.
type AccountEligibilityRequest struct {
	GroupID          *int64
	SessionHash      string
	RequestedModel   string
	ExpectedPlatform string
}

func (s *GatewayService) RevalidateSelectedAccount(ctx context.Context, accountID int64, req AccountEligibilityRequest) (*Account, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, ErrNoAvailableAccounts
	}
	invalidateSticky := func() {
		if s.cache != nil && req.SessionHash != "" {
			_ = s.cache.DeleteSessionAccountID(ctx, derefGroupID(req.GroupID), req.SessionHash)
		}
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		invalidateSticky()
		return nil, fmt.Errorf("revalidate gateway account %d: %w", accountID, err)
	}
	expectedPlatform := req.ExpectedPlatform
	if expectedPlatform == "" {
		expectedPlatform = PlatformAnthropic
	}
	useMixed := expectedPlatform == PlatformAnthropic || expectedPlatform == PlatformGemini
	if account == nil ||
		!s.isAccountAllowedForPlatform(account, expectedPlatform, useMixed) ||
		!s.isAccountInGroup(account, req.GroupID) ||
		!account.IsSchedulableForModelWithContext(ctx, req.RequestedModel) ||
		!parentHealthyForShadow(account, func(id int64) *Account {
			parent, _ := s.accountRepo.GetByID(ctx, id)
			return parent
		}) {
		invalidateSticky()
		return nil, ErrNoAvailableAccounts
	}
	return account, nil
}

// OpenAIAccountEligibilityRequest adds endpoint and transport requirements to
// the common post-wait account check.
type OpenAIAccountEligibilityRequest struct {
	GroupID                 *int64
	SessionHash             string
	Platform                string
	RequestedModel          string
	RequireCompact          bool
	RequiredCapability      OpenAIEndpointCapability
	RequiredImageCapability OpenAIImagesCapability
	RequiredTransport       OpenAIUpstreamTransport
}

func (s *OpenAIGatewayService) RevalidateSelectedAccount(ctx context.Context, accountID int64, req OpenAIAccountEligibilityRequest) (*Account, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, ErrNoAvailableAccounts
	}
	invalidateSticky := func() {
		if req.SessionHash != "" {
			_ = s.deleteStickySessionAccountID(ctx, req.GroupID, req.SessionHash)
		}
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		invalidateSticky()
		return nil, fmt.Errorf("revalidate openai account %d: %w", accountID, err)
	}
	if !isOpenAICompatibleAccountEligibleForRequest(ctx, account, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability) ||
		!account.SupportsOpenAIImageCapability(req.RequiredImageCapability) ||
		!openAIStickyAccountMatchesGroup(account, req.GroupID) ||
		!s.isOpenAIAccountTransportCompatible(account, req.RequiredTransport) ||
		!parentHealthyForShadow(account, s.parentAccountLookup(ctx)) ||
		s.isOpenAIAccountRuntimeBlocked(account) {
		invalidateSticky()
		return nil, ErrNoAvailableAccounts
	}
	return account, nil
}
