package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	// antigravityRefreshWindow Antigravity token 提前刷新窗口：8分钟。
	// Google OAuth token 存库已预留 5 分钟安全裕量，配合 5 分钟巡检周期，
	// 活跃账号在真实过期前约 8–13 分钟刷新，避免每 35 分钟机械刷新的异常脉冲。
	antigravityRefreshWindow = 8 * time.Minute
	// antigravityIdleRefreshCutoff 闲置账号跳过后台主动刷新的阈值：超过 2 小时无流量
	// 不再 24/7 定时刷新，待真实请求到达时由 AntigravityTokenProvider 按需懒刷新。
	antigravityIdleRefreshCutoff = 2 * time.Hour

	antigravityForceTokenRefreshExtraKey       = "antigravity_force_token_refresh"
	antigravityForceTokenRefreshReasonExtraKey = "antigravity_force_token_refresh_reason"
	antigravityForceTokenRefreshAtExtraKey     = "antigravity_force_token_refresh_at"
)

// AntigravityTokenRefresher 实现 TokenRefresher 接口
type AntigravityTokenRefresher struct {
	antigravityOAuthService *AntigravityOAuthService
}

func NewAntigravityTokenRefresher(antigravityOAuthService *AntigravityOAuthService) *AntigravityTokenRefresher {
	return &AntigravityTokenRefresher{
		antigravityOAuthService: antigravityOAuthService,
	}
}

// CacheKey 返回用于分布式锁的缓存键
func (r *AntigravityTokenRefresher) CacheKey(account *Account) string {
	return AntigravityTokenCacheKey(account)
}

// CanRefresh 检查是否可以刷新此账户
func (r *AntigravityTokenRefresher) CanRefresh(account *Account) bool {
	return account.Platform == PlatformAntigravity && account.Type == AccountTypeOAuth
}

// NeedsRefresh 检查账户是否需要刷新
// Antigravity 使用固定刷新窗口，并跳过长期闲置账号的 24/7 后台刷新（请求到达时由 TokenProvider 按需刷新）。
func (r *AntigravityTokenRefresher) NeedsRefresh(account *Account, _ time.Duration) bool {
	if !r.CanRefresh(account) {
		return false
	}
	if accountNeedsAntigravityForceTokenRefresh(account) {
		return true
	}
	if isAntigravityAccountIdleForBackgroundRefresh(account) {
		return false
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return false
	}
	timeUntilExpiry := time.Until(*expiresAt)
	needsRefresh := timeUntilExpiry < antigravityRefreshWindow
	if needsRefresh {
		fmt.Printf("[AntigravityTokenRefresher] Account %d needs refresh: expires_at=%s, time_until_expiry=%v, window=%v\n",
			account.ID, expiresAt.Format("2006-01-02 15:04:05"), timeUntilExpiry, antigravityRefreshWindow)
	}
	return needsRefresh
}

func isAntigravityAccountIdleForBackgroundRefresh(account *Account) bool {
	if account == nil {
		return false
	}
	// 缺少 project_id 或处于临时不可调度恢复期的账号允许后台刷新完成自愈。
	if strings.TrimSpace(account.GetCredential("project_id")) == "" {
		return false
	}
	if account.TempUnschedulableUntil != nil && time.Now().Before(*account.TempUnschedulableUntil) {
		return false
	}
	if account.LastUsedAt != nil {
		return time.Since(*account.LastUsedAt) > antigravityIdleRefreshCutoff
	}
	if !account.CreatedAt.IsZero() {
		return time.Since(account.CreatedAt) > antigravityIdleRefreshCutoff
	}
	return false
}

func accountNeedsAntigravityForceTokenRefresh(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformAntigravity &&
		account.Type == AccountTypeOAuth &&
		account.getExtraBool(antigravityForceTokenRefreshExtraKey)
}

func antigravityForceTokenRefreshExtra(reason string) map[string]any {
	return map[string]any{
		antigravityForceTokenRefreshExtraKey:       true,
		antigravityForceTokenRefreshReasonExtraKey: reason,
		antigravityForceTokenRefreshAtExtraKey:     time.Now().UTC().Format(time.RFC3339),
	}
}

func clearAntigravityForceTokenRefreshExtra() map[string]any {
	return map[string]any{
		antigravityForceTokenRefreshExtraKey:       false,
		antigravityForceTokenRefreshReasonExtraKey: "",
		antigravityForceTokenRefreshAtExtraKey:     "",
	}
}

// Refresh 执行 token 刷新
func (r *AntigravityTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	tokenInfo, err := r.antigravityOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}

	newCredentials := r.antigravityOAuthService.BuildAccountCredentials(tokenInfo)
	// 合并旧的 credentials，保留新 credentials 中不存在的字段
	newCredentials = MergeCredentials(account.Credentials, newCredentials)

	// 特殊处理 project_id：如果新值为空但旧值非空，保留旧值
	// 这确保了即使 LoadCodeAssist 失败，project_id 也不会丢失
	if newProjectID, _ := newCredentials["project_id"].(string); newProjectID == "" {
		if oldProjectID := strings.TrimSpace(account.GetCredential("project_id")); oldProjectID != "" {
			newCredentials["project_id"] = oldProjectID
		}
	}

	// 如果 project_id 获取失败，只记录警告，不返回错误
	// LoadCodeAssist 失败可能是临时网络问题，应该允许重试而不是立即标记为不可重试错误
	// Token 刷新本身是成功的（access_token 和 refresh_token 已更新）
	if tokenInfo.ProjectIDMissing {
		if tokenInfo.ProjectID != "" {
			// 有旧的 project_id，本次获取失败，保留旧值
			log.Printf("[AntigravityTokenRefresher] Account %d: LoadCodeAssist 临时失败，保留旧 project_id", account.ID)
		} else {
			// 从未获取过 project_id，本次也失败，但不返回错误以允许下次重试
			log.Printf("[AntigravityTokenRefresher] Account %d: LoadCodeAssist 失败，project_id 缺失，但 token 已更新，将在下次刷新时重试", account.ID)
		}
	}

	return newCredentials, nil
}
