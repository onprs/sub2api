package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
)

const (
	AntigravityPrivacySet    = "privacy_set"
	AntigravityPrivacyFailed = "privacy_set_failed"
)

type antigravityPrivacyClient interface {
	FetchUserInfo(ctx context.Context, accessToken, projectID string) (*antigravity.FetchUserInfoResponse, error)
	SetUserSettingsWithExisting(ctx context.Context, accessToken string, existing map[string]any) (*antigravity.SetUserSettingsResponse, error)
}

// setAntigravityPrivacy 调用 Antigravity API 关闭遥测并验证结果。
// 对齐官方 agy CLI persistTelemetryDisabled 语义：
//  1. 先通过 fetchUserInfo 读取既有 userSettings；若已关闭遥测则直接跳过写入；
//  2. 若开启了遥测，则克隆既有 userSettings（保留 consumerTermsAccepted / gcpTermsAccepted 等字段），
//     仅关闭 telemetryEnabled 后调用 setUserSettings；
//  3. 再通过 fetchUserInfo 二次验证隐私状态已生效。
//
// 返回 privacy_mode 值："privacy_set" 成功，"privacy_set_failed" 失败，空串表示无法执行。
func setAntigravityPrivacy(ctx context.Context, accessToken, projectID, proxyURL string) string {
	if accessToken == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := antigravity.NewClient(proxyURL)
	if err != nil {
		slog.Warn("antigravity_privacy_client_error", "error", err.Error())
		return AntigravityPrivacyFailed
	}

	return setAntigravityPrivacyWithClient(ctx, client, accessToken, projectID)
}

func setAntigravityPrivacyWithClient(ctx context.Context, client antigravityPrivacyClient, accessToken, projectID string) string {
	if accessToken == "" {
		return ""
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		slog.Warn("antigravity_privacy_missing_project_id")
		return AntigravityPrivacyFailed
	}

	// 第 1 步：先读取既有 userSettings，避免盲发空对象清空条款同意等既有字段。
	existingInfo, err := client.FetchUserInfo(ctx, accessToken, projectID)
	if err != nil {
		slog.Warn("antigravity_privacy_fetch_existing_failed", "error", err.Error())
		return AntigravityPrivacyFailed
	}
	if existingInfo.IsPrivate() {
		slog.Info("antigravity_privacy_already_private")
		return AntigravityPrivacySet
	}

	// 第 2 步：克隆既有设置并仅关闭 telemetryEnabled。
	setResp, err := client.SetUserSettingsWithExisting(ctx, accessToken, existingInfo.UserSettings)
	if err != nil {
		slog.Warn("antigravity_privacy_set_failed", "error", err.Error())
		return AntigravityPrivacyFailed
	}
	if !setResp.IsSuccess() {
		slog.Warn("antigravity_privacy_set_response_telemetry_still_enabled",
			"user_settings", setResp.UserSettings,
		)
		return AntigravityPrivacyFailed
	}

	// 第 3 步：调用 fetchUserInfo 二次验证隐私是否已生效。
	userInfo, err := client.FetchUserInfo(ctx, accessToken, projectID)
	if err != nil {
		slog.Warn("antigravity_privacy_verify_failed", "error", err.Error())
		return AntigravityPrivacyFailed
	}
	if !userInfo.IsPrivate() {
		slog.Warn("antigravity_privacy_verify_not_private",
			"user_settings", userInfo.UserSettings,
		)
		return AntigravityPrivacyFailed
	}

	slog.Info("antigravity_privacy_set_success")
	return AntigravityPrivacySet
}

func applyAntigravityPrivacyMode(account *Account, mode string) {
	if account == nil || strings.TrimSpace(mode) == "" {
		return
	}
	extra := make(map[string]any, len(account.Extra)+1)
	for k, v := range account.Extra {
		extra[k] = v
	}
	extra["privacy_mode"] = mode
	account.Extra = extra
}
