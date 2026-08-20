package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

// API Key status constants
const (
	StatusAPIKeyActive         = "active"
	StatusAPIKeyDisabled       = "disabled"
	StatusAPIKeyQuotaExhausted = "quota_exhausted"
	StatusAPIKeyExpired        = "expired"
)

// API Key 分组调度策略。
const (
	APIKeyRoutingStrategyBalanced       = "balanced"
	APIKeyRoutingStrategyStabilityFirst = "stability_first"
	APIKeyRoutingStrategyCostFirst      = "cost_first"
	APIKeyRoutingStrategyManual         = "manual"
	APIKeyRoutingMaxGroups              = 20
)

// APIKeyGroupBinding 表示一个候选分组及其手动调度顺序。
type APIKeyGroupBinding struct {
	GroupID     int64
	Priority    int
	Group       *Group
	RPMOverride *int
}

func IsValidAPIKeyRoutingStrategy(strategy string) bool {
	switch strategy {
	case APIKeyRoutingStrategyBalanced,
		APIKeyRoutingStrategyStabilityFirst,
		APIKeyRoutingStrategyCostFirst,
		APIKeyRoutingStrategyManual:
		return true
	default:
		return false
	}
}

// Rate limit window durations
const (
	RateLimitWindow5h = 5 * time.Hour
	RateLimitWindow1d = 24 * time.Hour
	RateLimitWindow7d = 7 * 24 * time.Hour
)

// IsWindowExpired returns true if the window starting at windowStart has exceeded the given duration.
// A nil windowStart is treated as expired — no initialized window means any accumulated usage is stale.
func IsWindowExpired(windowStart *time.Time, duration time.Duration) bool {
	return windowStart == nil || time.Since(*windowStart) >= duration
}

type APIKey struct {
	ID              int64
	UserID          int64
	Key             string
	Name            string
	GroupID         *int64
	RoutingPlatform string
	RoutingStrategy string
	RoutingGroups   []APIKeyGroupBinding
	Status          string
	IPWhitelist     []string
	IPBlacklist     []string
	// 预编译的 IP 规则，用于认证热路径避免重复 ParseIP/ParseCIDR。
	CompiledIPWhitelist *ip.CompiledIPRules `json:"-"`
	CompiledIPBlacklist *ip.CompiledIPRules `json:"-"`
	LastUsedAt          *time.Time
	LastUsedIP          *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	User                *User
	Group               *Group
	CurrentConcurrency  int

	// Quota fields
	Quota     float64    // Quota limit in USD (0 = unlimited)
	QuotaUsed float64    // Used quota amount
	ExpiresAt *time.Time // Expiration time (nil = never expires)

	// Rate limit fields
	RateLimit5h   float64    // Rate limit in USD per 5h (0 = unlimited)
	RateLimit1d   float64    // Rate limit in USD per 1d (0 = unlimited)
	RateLimit7d   float64    // Rate limit in USD per 7d (0 = unlimited)
	Usage5h       float64    // Used amount in current 5h window
	Usage1d       float64    // Used amount in current 1d window
	Usage7d       float64    // Used amount in current 7d window
	Window5hStart *time.Time // Start of current 5h window
	Window1dStart *time.Time // Start of current 1d window
	Window7dStart *time.Time // Start of current 7d window
}

func (k *APIKey) IsActive() bool {
	return k.Status == StatusActive
}

// ConfiguredRoutingGroups 返回按优先级排序后的候选分组。
// 旧数据或测试对象未填充关联关系时，回退到兼容 group_id/group。
func (k *APIKey) ConfiguredRoutingGroups() []APIKeyGroupBinding {
	if k == nil {
		return nil
	}
	if len(k.RoutingGroups) > 0 {
		groups := make([]APIKeyGroupBinding, len(k.RoutingGroups))
		copy(groups, k.RoutingGroups)
		return groups
	}
	if k.GroupID == nil {
		return nil
	}
	return []APIKeyGroupBinding{{GroupID: *k.GroupID, Priority: 0, Group: k.Group}}
}

func (k *APIKey) RoutingPlatformValue() string {
	if k == nil {
		return ""
	}
	if k.RoutingPlatform != "" {
		return k.RoutingPlatform
	}
	if k.Group != nil {
		return k.Group.Platform
	}
	return ""
}

func (k *APIKey) RoutingStrategyValue() string {
	if k == nil || !IsValidAPIKeyRoutingStrategy(k.RoutingStrategy) {
		return APIKeyRoutingStrategyManual
	}
	return k.RoutingStrategy
}

func (k *APIKey) UsesDynamicGroupRouting() bool {
	return k != nil && len(k.ConfiguredRoutingGroups()) > 1
}

func (k *APIKey) HasConfiguredGroup(groupID int64) bool {
	if k == nil || groupID <= 0 {
		return false
	}
	for _, binding := range k.ConfiguredRoutingGroups() {
		if binding.GroupID == groupID {
			return true
		}
	}
	return false
}

// CloneWithEffectiveGroup 创建当前请求使用的浅拷贝，不修改认证缓存中的配置对象。
func (k *APIKey) CloneWithEffectiveGroup(group *Group) *APIKey {
	if k == nil || group == nil {
		return k
	}
	cloned := *k
	groupID := group.ID
	cloned.GroupID = &groupID
	cloned.Group = group
	if k.User != nil {
		clonedUser := *k.User
		clonedUser.UserGroupRPMOverride = nil
		for _, binding := range k.ConfiguredRoutingGroups() {
			if binding.GroupID == groupID {
				clonedUser.UserGroupRPMOverride = binding.RPMOverride
				break
			}
		}
		cloned.User = &clonedUser
	}
	return &cloned
}

// HasRateLimits returns true if any rate limit window is configured
func (k *APIKey) HasRateLimits() bool {
	return k.RateLimit5h > 0 || k.RateLimit1d > 0 || k.RateLimit7d > 0
}

// IsExpired checks if the API key has expired
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsQuotaExhausted checks if the API key quota is exhausted
func (k *APIKey) IsQuotaExhausted() bool {
	if k.Quota <= 0 {
		return false // unlimited
	}
	return k.QuotaUsed >= k.Quota
}

// GetQuotaRemaining returns remaining quota (-1 for unlimited)
func (k *APIKey) GetQuotaRemaining() float64 {
	if k.Quota <= 0 {
		return -1 // unlimited
	}
	remaining := k.Quota - k.QuotaUsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// GetDaysUntilExpiry returns days until expiry (-1 for never expires)
func (k *APIKey) GetDaysUntilExpiry() int {
	if k.ExpiresAt == nil {
		return -1 // never expires
	}
	duration := time.Until(*k.ExpiresAt)
	if duration < 0 {
		return 0
	}
	return int(duration.Hours() / 24)
}

// EffectiveUsage5h returns the 5h window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage5h() float64 {
	if IsWindowExpired(k.Window5hStart, RateLimitWindow5h) {
		return 0
	}
	return k.Usage5h
}

// EffectiveUsage1d returns the 1d window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage1d() float64 {
	if IsWindowExpired(k.Window1dStart, RateLimitWindow1d) {
		return 0
	}
	return k.Usage1d
}

// EffectiveUsage7d returns the 7d window usage, or 0 if the window has expired.
func (k *APIKey) EffectiveUsage7d() float64 {
	if IsWindowExpired(k.Window7dStart, RateLimitWindow7d) {
		return 0
	}
	return k.Usage7d
}

// APIKeyListFilters holds optional filtering parameters for listing API keys.
type APIKeyListFilters struct {
	Search  string
	Status  string
	GroupID *int64 // nil=不筛选, 0=无分组, >0=指定分组
}
