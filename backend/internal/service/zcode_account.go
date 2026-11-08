package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"strings"
	"time"
)

const AccountModeStart = "start"
const zcodeTokensKey = "zcode_tokens_encrypted"
const zcodeStateKey = "zcode_state"
const zcodeQuotaKey = "zcode_quota"

// ZCodeQuotaExtraKey / ZCodeStateExtraKey 供调度快照投影引用，
// 与 zcodeQuotaKey / zcodeStateKey 保持同步。
const ZCodeQuotaExtraKey = "zcode_quota"
const ZCodeStateExtraKey = "zcode_state"
const zcodeQuotaExhaustedReason = "zcode_quota_exhausted"
const zcodeClaimKey = "zcode_claim"

func (a *Account) zcodeBaseURL() string {
	if a.GetAccountMode() == AccountModeStart {
		return zcode.StartBase
	}
	return a.ZCodeProvider().CodingBase()
}

func (a *Account) IsZCodeOAuth() bool {
	return a != nil && a.Platform == PlatformZhipu && a.Type == AccountTypeOAuth && a.GetCredential("auth_mode") == "zcode_oauth"
}
func (a *Account) ZCodeProvider() zcode.Provider {
	if a == nil {
		return ""
	}
	return zcode.Provider(a.GetCredential("zcode_provider"))
}
func validateZCodeAccount(platform, kind string, creds map[string]any) error {
	auth, _ := creds["auth_mode"].(string)
	if platform != PlatformZhipu {
		if auth == "zcode_oauth" {
			return infraerrors.BadRequest("ZCODE_PLATFORM_INVALID", "ZCode OAuth 仅用于智谱账号")
		}
		return nil
	}
	if kind != AccountTypeOAuth {
		if auth == "zcode_oauth" {
			return infraerrors.BadRequest("ZCODE_AUTH_INVALID", "ZCode OAuth 需要 OAuth 账号类型")
		}
		return nil
	}
	a := &Account{Platform: platform, Type: kind, Credentials: creds}
	if !a.IsZCodeOAuth() || !a.ZCodeProvider().Valid() {
		return infraerrors.BadRequest("ZCODE_AUTH_INVALID", "智谱 OAuth 账号需要有效的 ZCode 授权来源")
	}
	mode := a.GetCredential("account_mode")
	if mode != AccountModeCoding && mode != AccountModeStart {
		return infraerrors.BadRequest("ZCODE_PLAN_INVALID", "ZCode 套餐必须为 Coding Plan 或 Start Plan")
	}
	if a.GetCredential(zcodeTokensKey) == "" {
		return infraerrors.BadRequest("ZCODE_LOGIN_REQUIRED", "请先完成 ZCode 网页授权")
	}
	for _, key := range []string{"api_key", "access_token", "refresh_token", "jwt", "captcha_token", "cookie"} {
		if _, ok := creds[key]; ok {
			return infraerrors.BadRequest("ZCODE_CREDENTIAL_INVALID", "ZCode 凭据必须通过授权流程保存")
		}
	}
	creds["api_protocol"] = APIProtocolAnthropic
	delete(creds, "base_url")
	delete(creds, "api_base_urls")
	if enabled, exists := creds["zcode_auto_claim"]; exists {
		if _, ok := enabled.(bool); !ok {
			return infraerrors.BadRequest("ZCODE_AUTO_CLAIM_INVALID", "自动领取必须为布尔值")
		}
	}
	return nil
}
func (a *Account) zcodeSchedulable(now time.Time) bool {
	if !a.IsZCodeOAuth() {
		return true
	}
	if state, ok := a.Extra[zcodeStateKey].(map[string]any); ok {
		if state["oauth_status"] == "login_required" {
			return false
		}
	}
	if quota := zcodeQuotaFromAccount(a); quota != nil && now.Unix()-quota.UpdatedAt <= 300 {
		// 已领取但尚未生效的计划没有可用额度；到生效时间后即使快照未刷新也允许调度。
		if quota.Status == "scheduled" && (quota.NextAvailableAt == 0 || quota.NextAvailableAt > now.Unix()) {
			return false
		}
		// 模型额度耗尽不再停调整个账号：耗尽的余额由模型级冷却
		// （IsSchedulableForModelWithContext）拦截，其它有剩余额度的模型仍可命中本账号。
		// 账号级停调只保留套餐状态语义（scheduled / no_plan / expired）。
		return true
	}
	return true
}

// zcodeModelSchedulable 按请求模型判定 ZCode 账号可调度性。
// 判定依据额度快照中同名余额的剩余：余额有剩余 → 放行；快照可信且该模型
// 余额已耗尽 → 拦截；快照缺失、过期或无该模型的余额记录 → 放行（不凭空
// 拦截未知模型，由上游错误反馈驱动冷却）。
func (a *Account) zcodeModelSchedulable(ctx context.Context, requestedModel string, now time.Time) bool {
	if !a.IsZCodeOAuth() {
		return true
	}
	quota := zcodeQuotaFromAccount(a)
	if quota == nil || now.Unix()-quota.UpdatedAt > 300 {
		return true
	}
	// 套餐级状态仍然整体拦截。
	if quota.Status == "no_plan" || quota.Status == "expired" {
		return false
	}
	if quota.Status == "scheduled" && (quota.NextAvailableAt == 0 || quota.NextAvailableAt > now.Unix()) {
		return false
	}
	if quota.Plan != zcode.PlanStart || len(quota.Balances) == 0 {
		return true
	}
	name := zcodeModelBalanceKey(requestedModel)
	if name == "" {
		return true
	}
	for _, balance := range quota.Balances {
		if !strings.EqualFold(balance.Name, name) {
			continue
		}
		// 生效前与已过期的余额都不是「耗尽」，交给套餐级状态与上游反馈处理。
		if balance.EffectiveAt > now.Unix() {
			return true
		}
		if balance.ExpiresAt > 0 && balance.ExpiresAt <= now.Unix() {
			return true
		}
		if balance.ResetAt > 0 && balance.ResetAt <= now.Unix() {
			return true
		}
		if balance.Remaining != nil {
			return *balance.Remaining > 0
		}
		if balance.Percent != nil {
			return *balance.Percent < 100
		}
		return true
	}
	return true
}

// zcodeModelBalanceKey 归一化请求模型名，用于与余额 show_name 匹配。
func zcodeModelBalanceKey(requestedModel string) string {
	return strings.ToLower(strings.TrimSpace(requestedModel))
}

// zcodeRateLimitKeepsScheduling 判断 429 是否应保持账号调度：额度快照缺失或
// 未耗尽时视为上游临时限流，不因 429 写入临时停调；只有快照明确耗尽才沿用停调。
func zcodeRateLimitKeepsScheduling(a *Account, now time.Time) bool {
	quota := zcodeQuotaFromAccount(a)
	return quota == nil || !quota.Exhausted(now)
}
func zcodeQuotaFromAccount(a *Account) *zcode.Quota {
	var quota zcode.Quota
	if a == nil || decodeZCodeExtra(a.Extra[zcodeQuotaKey], &quota) != nil || quota.Plan == "" {
		return nil
	}
	return &quota
}
