package service

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"time"
)

const AccountModeStart = "start"
const zcodeTokensKey = "zcode_tokens_encrypted"
const zcodeStateKey = "zcode_state"
const zcodeQuotaKey = "zcode_quota"
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
		return !quota.Exhausted(now)
	}
	return true
}
func zcodeQuotaFromAccount(a *Account) *zcode.Quota {
	var quota zcode.Quota
	if a == nil || decodeZCodeExtra(a.Extra[zcodeQuotaKey], &quota) != nil || quota.Plan == "" {
		return nil
	}
	return &quota
}
