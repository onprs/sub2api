package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

func normalizeMonitorTargetType(targetType, endpoint, apiKey string) string {
	targetType = strings.TrimSpace(targetType)
	if targetType != "" {
		return targetType
	}
	// 兼容旧客户端：未传 target_type 但携带外部连接信息时按外站处理。
	if strings.TrimSpace(endpoint) != "" || strings.TrimSpace(apiKey) != "" {
		return ChannelMonitorTargetExternal
	}
	return ChannelMonitorTargetLocal
}

func validateMonitorTargetType(targetType string) error {
	switch targetType {
	case ChannelMonitorTargetLocal, ChannelMonitorTargetExternal:
		return nil
	default:
		return ErrChannelMonitorInvalidTargetType
	}
}

func monitorProviderPlatform(provider string) string {
	switch provider {
	case MonitorProviderAntigravityClaude, MonitorProviderAntigravityGemini:
		return PlatformAntigravity
	default:
		return provider
	}
}

func (s *ChannelMonitorService) resolveLocalMonitorGroup(
	ctx context.Context,
	provider string,
	groupID *int64,
) (*Group, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, ErrChannelMonitorMissingGroup
	}
	if s == nil || s.groupRepo == nil {
		return nil, fmt.Errorf("channel monitor group repository is unavailable")
	}
	group, err := s.groupRepo.GetByIDLite(ctx, *groupID)
	if err != nil || group == nil || !group.IsActive() {
		return nil, ErrChannelMonitorGroupUnavailable
	}
	if group.Platform != monitorProviderPlatform(provider) {
		return nil, ErrChannelMonitorGroupPlatformMismatch
	}
	return group, nil
}

// MigrateLegacyLocalTargets 把迁移 190 标记出的旧记录映射到原 API Key 的真实分组。
// 所有记录解析成功后才进入仓储事务，任何异常都不会产生部分迁移。
func (s *ChannelMonitorService) MigrateLegacyLocalTargets(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	pending, err := s.repo.ListPendingLocalTargets(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	if s.encryptor == nil || s.groupRepo == nil {
		return fmt.Errorf("channel monitor legacy target migration dependencies are unavailable")
	}

	targets := make([]ChannelMonitorLocalTargetMigration, 0, len(pending))
	seenGroups := make(map[int64]int64, len(pending))
	for _, monitor := range pending {
		if monitor == nil || strings.TrimSpace(monitor.APIKey) == "" {
			return fmt.Errorf("legacy channel monitor %d has no encrypted api key", monitorID(monitor))
		}
		plain, decryptErr := s.encryptor.Decrypt(monitor.APIKey)
		if decryptErr != nil {
			return fmt.Errorf("decrypt legacy channel monitor %d api key: %w", monitor.ID, decryptErr)
		}
		groupID, resolveErr := s.repo.ResolveLegacyAPIKeyGroupID(ctx, plain)
		if resolveErr != nil {
			return fmt.Errorf("resolve legacy channel monitor %d group: %w", monitor.ID, resolveErr)
		}
		groupIDCopy := groupID
		group, groupErr := s.resolveLocalMonitorGroup(ctx, monitor.Provider, &groupIDCopy)
		if groupErr != nil {
			return fmt.Errorf("validate legacy channel monitor %d group %d: %w", monitor.ID, groupID, groupErr)
		}
		if otherMonitorID, exists := seenGroups[groupID]; exists {
			return fmt.Errorf("legacy channel monitors %d and %d resolve to the same group %d", otherMonitorID, monitor.ID, groupID)
		}
		seenGroups[groupID] = monitor.ID
		targets = append(targets, ChannelMonitorLocalTargetMigration{
			MonitorID: monitor.ID,
			GroupID:   groupID,
			GroupName: group.Name,
		})
	}
	if err := s.repo.MigrateLocalTargets(ctx, targets); err != nil {
		return err
	}
	migratedGroupIDs := make([]int64, 0, len(targets))
	for _, target := range targets {
		migratedGroupIDs = append(migratedGroupIDs, target.GroupID)
	}
	s.invalidateAPIKeyRoutingHealth(migratedGroupIDs...)
	slog.Info("channel_monitor: migrated legacy monitors to local groups", "count", len(targets))
	return nil
}

func monitorID(monitor *ChannelMonitor) int64 {
	if monitor == nil {
		return 0
	}
	return monitor.ID
}
