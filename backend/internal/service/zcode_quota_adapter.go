package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"net/http"
	"time"
)

// 原 CN quota 入口仍可消费 ZCode；Start Plan 提供余额对象，不伪造滚动窗口。
func (s *CNProviderQuotaService) queryZCodeQuota(ctx context.Context, a *Account) (*CNProviderQuotaProbeResult, error) {
	if s.zcodeService == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZCODE_UNAVAILABLE", "ZCode 服务不可用")
	}
	quota, err := s.zcodeService.QueryQuota(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	result := &CNProviderQuotaProbeResult{Provider: PlatformZhipu, Source: "zcode_" + a.GetAccountMode(), Success: true, CredentialValid: true, FetchedAt: quota.UpdatedAt, Persisted: true, ZCodeQuota: quota}
	for _, balance := range quota.Balances {
		if balance.Window == "" || balance.Percent == nil {
			continue
		}
		tier := CNQuotaTier{Window: balance.Window, UsedPercent: *balance.Percent}
		if balance.ResetAt > 0 {
			tier.ResetAt = time.Unix(balance.ResetAt, 0).UTC().Format(time.RFC3339)
		}
		result.Tiers = append(result.Tiers, tier)
	}
	return result, nil
}
