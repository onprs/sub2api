package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AdminReadOnlyAccount 只公开监控所需字段，不透传凭据、扩展数据或错误原文。
type AdminReadOnlyAccount struct {
	ID                     int64      `json:"id"`
	Name                   string     `json:"name"`
	Platform               string     `json:"platform"`
	Type                   string     `json:"type"`
	Status                 string     `json:"status"`
	Schedulable            bool       `json:"schedulable"`
	GroupIDs               []int64    `json:"group_ids"`
	ExpiresAt              *int64     `json:"expires_at"`
	LastUsedAt             *time.Time `json:"last_used_at"`
	RateLimitResetAt       *time.Time `json:"rate_limit_reset_at"`
	OverloadUntil          *time.Time `json:"overload_until"`
	TempUnschedulableUntil *time.Time `json:"temp_unschedulable_until"`
	SessionWindowEnd       *time.Time `json:"session_window_end"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func AdminReadOnlyAccountFromService(account *service.Account) *AdminReadOnlyAccount {
	if account == nil {
		return nil
	}
	return &AdminReadOnlyAccount{
		ID: account.ID, Name: account.Name, Platform: account.Platform,
		Type: account.Type, Status: account.Status, Schedulable: account.Schedulable,
		GroupIDs: append([]int64{}, account.GroupIDs...), ExpiresAt: timeToUnixSeconds(account.ExpiresAt),
		LastUsedAt: account.LastUsedAt, RateLimitResetAt: account.RateLimitResetAt,
		OverloadUntil: account.OverloadUntil, TempUnschedulableUntil: account.TempUnschedulableUntil,
		SessionWindowEnd: account.SessionWindowEnd, UpdatedAt: account.UpdatedAt,
	}
}

type AdminReadOnlyGroup struct {
	ID                      int64  `json:"id"`
	Name                    string `json:"name"`
	Platform                string `json:"platform"`
	Status                  string `json:"status"`
	AccountCount            int64  `json:"account_count"`
	ActiveAccountCount      int64  `json:"active_account_count"`
	RateLimitedAccountCount int64  `json:"rate_limited_account_count"`
}

func AdminReadOnlyGroupFromService(group *service.Group) *AdminReadOnlyGroup {
	if group == nil {
		return nil
	}
	return &AdminReadOnlyGroup{
		ID: group.ID, Name: group.Name, Platform: group.Platform, Status: group.Status,
		AccountCount: group.AccountCount, ActiveAccountCount: group.ActiveAccountCount,
		RateLimitedAccountCount: group.RateLimitedAccountCount,
	}
}
