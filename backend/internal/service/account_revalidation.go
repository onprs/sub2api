package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/config"
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
	// simple 模式下选号主路径（ListSchedulableByPlatform）不校验分组归属，
	// 复核保持一致，避免无分组账号被误拒（官方测试账号无分组绑定）。
	// cfg 为 nil（测试/误配）时仍保留 group 检查，与 HEAD 既有语义一致。
	groupMatched := (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) || openAIStickyAccountMatchesGroup(account, req.GroupID)
	if !isOpenAICompatibleAccountEligibleForRequest(ctx, account, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability) ||
		!account.SupportsOpenAIImageCapability(req.RequiredImageCapability) ||
		!groupMatched ||
		!s.isOpenAIAccountTransportCompatible(account, req.RequiredTransport) ||
		!parentHealthyForShadow(account, s.parentAccountLookup(ctx)) ||
		s.isOpenAIAccountRuntimeBlocked(account) {
		invalidateSticky()
		return nil, ErrNoAvailableAccounts
	}
	return account, nil
}
