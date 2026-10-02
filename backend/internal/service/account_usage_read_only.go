package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// getAnthropicOAuthUsage 共用官方用量缓存；只读调用仅查询，不写账号状态。
func (s *AccountUsageService) getAnthropicOAuthUsage(ctx context.Context, account *Account, readOnly bool) (*UsageInfo, error) {
	accountID := account.ID
	loadCache := func() *apiUsageCache {
		cached, ok := s.cache.apiCache.Load(accountID)
		if !ok {
			return nil
		}
		entry, ok := cached.(*apiUsageCache)
		if !ok {
			return nil
		}
		age := time.Since(entry.timestamp)
		if (entry.err != nil && age < apiErrorCacheTTL) || (entry.response != nil && age < apiCacheTTL) {
			return entry
		}
		return nil
	}
	entry := loadCache()
	if entry == nil {
		jitter := time.Duration(rand.Int64N(int64(apiQueryMaxJitter)))
		select {
		case <-time.After(jitter):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		result, flightErr, _ := s.cache.apiFlight.Do(fmt.Sprintf("usage:%d", accountID), func() (any, error) {
			if cached := loadCache(); cached != nil {
				return cached, nil
			}
			resp, fetchErr := s.fetchOAuthUsageRaw(ctx, account)
			cached := &apiUsageCache{response: resp, err: fetchErr, timestamp: time.Now()}
			s.cache.apiCache.Store(accountID, cached)
			return cached, nil
		})
		if flightErr != nil {
			return nil, flightErr
		}
		entry, _ = result.(*apiUsageCache)
	}
	if entry == nil || (entry.response == nil && entry.err == nil) {
		return nil, fmt.Errorf("账号用量响应为空")
	}
	if entry.err != nil {
		return nil, entry.err
	}
	now := time.Now()
	updatedAt := now
	if readOnly {
		updatedAt = entry.timestamp
	}
	usage := s.buildUsageInfo(entry.response, &updatedAt)
	if !readOnly {
		s.addWindowStats(ctx, account, usage)
		s.syncActiveToPassive(ctx, account.ID, usage)
	}
	if usage.SevenDayFable == nil {
		usage.SevenDayFable = buildPassiveUsageWindow(account.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset")
	}
	if readOnly {
		usage.Source = "active"
	} else {
		s.tryClearRecoverableAccountError(ctx, account)
	}
	return usage, nil
}

// GetReadOnlyUsage 主动读取 Anthropic OAuth 用量，其它账号只读取已有快照。
func (s *AccountUsageService) GetReadOnlyUsage(ctx context.Context, accountID int64, source string) (*UsageInfo, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if source != "passive" && account.Platform == PlatformAnthropic && account.Type == AccountTypeOAuth && !account.IsSyntheticUITest() {
		usage, err := s.getAnthropicOAuthUsage(ctx, account, true)
		if err != nil {
			// 查询响应不公开上游错误原文或网络连接信息。
			return nil, infraerrors.ServiceUnavailable("ACCOUNT_USAGE_UNAVAILABLE", "账号用量暂时不可用")
		}
		return usage, nil
	}
	usage, err := s.getReadOnlyStoredUsage(account, time.Now())
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("ACCOUNT_USAGE_UNAVAILABLE", "账号额度快照暂时不可用")
	}
	return usage, nil
}

// getReadOnlyStoredUsage 将缺失的窗口保留为未知，避免用空对象表示完整剩余额度。
func (s *AccountUsageService) getReadOnlyStoredUsage(account *Account, now time.Time) (*UsageInfo, error) {
	if account.Platform == PlatformAnthropic && !account.IsAnthropicOAuthOrSetupToken() {
		return nil, ErrObserverQuotaUnavailable
	}
	usage, err := s.GetStoredUsageSnapshot(account, now)
	if err != nil {
		return nil, err
	}
	if account.Platform == PlatformAnthropic && account.SessionWindowEnd == nil {
		usage.FiveHour = nil
		if value, ok := account.Extra["session_window_utilization"]; ok {
			usage.FiveHour = &UsageProgress{Utilization: parseExtraFloat64(value) * 100}
		}
	}
	if observerUsageEmpty(usage) {
		return nil, ErrObserverQuotaUnavailable
	}
	return usage, nil
}

// GetReadOnlyUsageBatch 读取快照，不探测或兑换任何上游奖励。
func (s *AccountUsageService) GetReadOnlyUsageBatch(ctx context.Context, accountIDs []int64) (map[int64]*UsageInfo, map[int64]string, error) {
	ids := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	usage := make(map[int64]*UsageInfo, len(ids))
	failures := make(map[int64]string)
	if len(ids) == 0 {
		return usage, failures, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	now := time.Now()
	for _, id := range ids {
		account := byID[id]
		if account == nil {
			failures[id] = "账号不存在"
			continue
		}
		snapshot, err := s.getReadOnlyStoredUsage(account, now)
		if err != nil {
			failures[id] = "账号额度快照暂时不可用"
			continue
		}
		usage[id] = snapshot
	}
	return usage, failures, nil
}
