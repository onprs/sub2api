package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"time"
)

// ZCodeStateMutation 防止在途请求/后台任务覆盖重新授权后的新凭据状态。
// 以密文、供应商、套餐和代理作为账号身份版本；不扩大通用 AccountRepository 接口。
type ZCodeStateMutation struct {
	CredentialCipher string
	Provider         string
	Plan             string
	ProxyID          *int64
	Extra            map[string]any
	CooldownUntil    *time.Time
	CooldownReason   string
	ClearOwnCooldown bool
}
type zcodeConditionalStateRepository interface {
	UpdateZCodeStateIfIdentityUnchanged(context.Context, int64, ZCodeStateMutation) (bool, error)
}

func (s *ZCodeService) updateState(ctx context.Context, a *Account, extra map[string]any, until *time.Time, reason string, clear bool) (bool, error) {
	repo, ok := s.repo.(zcodeConditionalStateRepository)
	if !ok {
		return false, &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	return repo.UpdateZCodeStateIfIdentityUnchanged(ctx, a.ID, ZCodeStateMutation{CredentialCipher: a.GetCredential(zcodeTokensKey), Provider: string(a.ZCodeProvider()), Plan: a.GetAccountMode(), ProxyID: a.ProxyID, Extra: extra, CooldownUntil: until, CooldownReason: reason, ClearOwnCooldown: clear})
}
