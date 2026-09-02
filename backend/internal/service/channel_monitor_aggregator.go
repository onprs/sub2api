package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// 渠道监控聚合层：把 latest + availability 拼成 admin/user 视图所需的 summary / detail。
// 所有方法都遵守"失败仅日志，返回零值"的原则，避免 N+1 查询失败拖垮列表渲染。

const (
	channelMonitorRoutingHealthCacheTTL     = 15 * time.Second
	channelMonitorRoutingHealthQueryTimeout = 3 * time.Second
)

type channelMonitorRoutingHealthCacheEntry struct {
	snapshot  APIKeyRoutingHealthSnapshot
	expiresAt time.Time
}

func (s *ChannelMonitorService) GetAPIKeyRoutingHealthSnapshots(
	ctx context.Context,
	groupIDs []int64,
) []APIKeyRoutingHealthSnapshot {
	orderedIDs := uniquePositiveGroupIDs(groupIDs)
	if len(orderedIDs) == 0 {
		return []APIKeyRoutingHealthSnapshot{}
	}
	if s == nil || s.repo == nil {
		return makeUnknownRoutingHealthSnapshots(orderedIDs)
	}

	cached, missing := s.readRoutingHealthCache(orderedIDs, time.Now())
	if len(missing) == 0 {
		return orderedRoutingHealthSnapshots(orderedIDs, cached)
	}

	// 刷新中的请求不等待数据库聚合，直接按当前缓存返回，路由按 unknown 基线继续。
	if !s.routingHealthRefreshMu.TryLock() {
		return orderedRoutingHealthSnapshots(orderedIDs, cached)
	}
	defer s.routingHealthRefreshMu.Unlock()

	cached, missing = s.readRoutingHealthCache(orderedIDs, time.Now())
	if len(missing) > 0 {
		queryCtx, cancel := context.WithTimeout(ctx, channelMonitorRoutingHealthQueryTimeout)
		loaded := s.loadAPIKeyRoutingHealthSnapshots(queryCtx, missing)
		cancel()
		s.storeRoutingHealthCache(missing, loaded, time.Now().Add(channelMonitorRoutingHealthCacheTTL))
		cached, _ = s.readRoutingHealthCache(orderedIDs, time.Now())
	}
	return orderedRoutingHealthSnapshots(orderedIDs, cached)
}

func (s *ChannelMonitorService) loadAPIKeyRoutingHealthSnapshots(
	ctx context.Context,
	orderedIDs []int64,
) []APIKeyRoutingHealthSnapshot {
	snapshots := makeUnknownRoutingHealthSnapshots(orderedIDs)
	monitors, err := s.repo.ListEnabledLocalByGroupIDs(ctx, orderedIDs)
	if err != nil || len(monitors) == 0 {
		if err != nil {
			slog.Warn("channel_monitor: load routing health monitors failed", "error", err)
		}
		return snapshots
	}

	monitors = validLocalRoutingHealthMonitors(monitors)
	if len(monitors) == 0 {
		return snapshots
	}
	ids, primaryByID, _ := collectMonitorIndexes(monitors)
	latestMap, latestErr := s.repo.ListLatestForMonitorIDs(ctx, ids)
	availabilityMap, availabilityErr := s.repo.ComputeAvailabilityForMonitors(ctx, ids, APIKeyRoutingHealthWindowDays)
	if latestErr != nil || availabilityErr != nil {
		slog.Warn("channel_monitor: aggregate routing health failed",
			"latest_error", latestErr,
			"availability_error", availabilityErr,
		)
		return snapshots
	}

	monitorByGroup := make(map[int64]*ChannelMonitor, len(monitors))
	for _, monitor := range monitors {
		if monitor != nil && monitor.GroupID != nil {
			monitorByGroup[*monitor.GroupID] = monitor
		}
	}
	for index, groupID := range orderedIDs {
		monitor := monitorByGroup[groupID]
		if monitor == nil {
			continue
		}
		latest := pickLatest(latestMap[monitor.ID], primaryByID[monitor.ID])
		availability := indexAvailabilityByModel(availabilityMap[monitor.ID])[primaryByID[monitor.ID]]
		if latest == nil || availability == nil || availability.TotalChecks <= 0 {
			continue
		}
		snapshots[index] = monitorRoutingHealthSnapshot(groupID, latest, availability)
	}
	return snapshots
}

func validLocalRoutingHealthMonitors(monitors []*ChannelMonitor) []*ChannelMonitor {
	valid := make([]*ChannelMonitor, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor == nil || !monitor.Enabled || monitor.TargetType != ChannelMonitorTargetLocal || monitor.GroupID == nil || monitor.Group == nil {
			continue
		}
		if monitor.Group.ID != *monitor.GroupID || !monitor.Group.IsActive() {
			continue
		}
		if monitor.Group.Platform != monitorProviderPlatform(monitor.Provider) {
			continue
		}
		valid = append(valid, monitor)
	}
	return valid
}

func (s *ChannelMonitorService) readRoutingHealthCache(
	groupIDs []int64,
	now time.Time,
) (map[int64]APIKeyRoutingHealthSnapshot, []int64) {
	cached := make(map[int64]APIKeyRoutingHealthSnapshot, len(groupIDs))
	missing := make([]int64, 0, len(groupIDs))
	if s == nil {
		return cached, append(missing, groupIDs...)
	}
	s.routingHealthCacheMu.RLock()
	defer s.routingHealthCacheMu.RUnlock()
	for _, groupID := range groupIDs {
		entry, ok := s.routingHealthCache[groupID]
		if !ok || !entry.expiresAt.After(now) {
			missing = append(missing, groupID)
			continue
		}
		cached[groupID] = entry.snapshot
	}
	return cached, missing
}

func (s *ChannelMonitorService) storeRoutingHealthCache(
	groupIDs []int64,
	loaded []APIKeyRoutingHealthSnapshot,
	expiresAt time.Time,
) {
	loadedByID := make(map[int64]APIKeyRoutingHealthSnapshot, len(loaded))
	for _, snapshot := range loaded {
		loadedByID[snapshot.GroupID] = snapshot
	}
	s.routingHealthCacheMu.Lock()
	defer s.routingHealthCacheMu.Unlock()
	if s.routingHealthCache == nil {
		s.routingHealthCache = make(map[int64]channelMonitorRoutingHealthCacheEntry)
	}
	for _, groupID := range groupIDs {
		snapshot, ok := loadedByID[groupID]
		if !ok {
			snapshot = APIKeyRoutingHealthSnapshot{GroupID: groupID, Status: APIKeyRoutingHealthStatusUnknown}
		}
		s.routingHealthCache[groupID] = channelMonitorRoutingHealthCacheEntry{
			snapshot:  snapshot,
			expiresAt: expiresAt,
		}
	}
}

func (s *ChannelMonitorService) invalidateAPIKeyRoutingHealth(groupIDs ...int64) {
	if s == nil {
		return
	}
	s.routingHealthCacheMu.Lock()
	defer s.routingHealthCacheMu.Unlock()
	if len(groupIDs) == 0 {
		s.routingHealthCache = make(map[int64]channelMonitorRoutingHealthCacheEntry)
		return
	}
	for _, groupID := range groupIDs {
		delete(s.routingHealthCache, groupID)
	}
}

func orderedRoutingHealthSnapshots(
	groupIDs []int64,
	byID map[int64]APIKeyRoutingHealthSnapshot,
) []APIKeyRoutingHealthSnapshot {
	out := make([]APIKeyRoutingHealthSnapshot, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		snapshot, ok := byID[groupID]
		if !ok {
			snapshot = APIKeyRoutingHealthSnapshot{GroupID: groupID, Status: APIKeyRoutingHealthStatusUnknown}
		}
		out = append(out, snapshot)
	}
	return out
}

func uniquePositiveGroupIDs(groupIDs []int64) []int64 {
	out := make([]int64, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		out = append(out, groupID)
	}
	return out
}

func makeUnknownRoutingHealthSnapshots(groupIDs []int64) []APIKeyRoutingHealthSnapshot {
	out := make([]APIKeyRoutingHealthSnapshot, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		out = append(out, APIKeyRoutingHealthSnapshot{
			GroupID: groupID,
			Status:  APIKeyRoutingHealthStatusUnknown,
		})
	}
	return out
}

func monitorRoutingHealthSnapshot(
	groupID int64,
	latest *ChannelMonitorLatest,
	availability *ChannelMonitorAvailability,
) APIKeyRoutingHealthSnapshot {
	status := APIKeyRoutingHealthStatusFailed
	switch latest.Status {
	case MonitorStatusOperational:
		status = APIKeyRoutingHealthStatusOperational
	case MonitorStatusDegraded:
		status = APIKeyRoutingHealthStatusDegraded
	}
	successRate := availability.AvailabilityPct
	observedAt := latest.CheckedAt.UTC()
	snapshot := APIKeyRoutingHealthSnapshot{
		GroupID:        groupID,
		Status:         status,
		SuccessRate:    &successRate,
		SampleCount:    int64(availability.TotalChecks),
		LastObservedAt: &observedAt,
	}
	if availability.AvgLatencyMs != nil {
		latency := int64(*availability.AvgLatencyMs)
		snapshot.AverageLatencyMs = &latency
	}
	return snapshot
}

// BatchMonitorStatusSummary 批量聚合多个监控的 latest + 7d 可用率（admin/user list 用，消除 N+1）。
// 失败时返回空 map，错误仅日志，不影响列表渲染。
//
// 参数：
//   - ids: 要聚合的 monitor ID 列表
//   - primaryByID: monitor ID -> primary model（用于读 7d 可用率与 latest 状态）
//   - extrasByID: monitor ID -> extra models 列表（用于读 latest 状态填充 ExtraModels）
func (s *ChannelMonitorService) BatchMonitorStatusSummary(
	ctx context.Context,
	ids []int64,
	primaryByID map[int64]string,
	extrasByID map[int64][]string,
) map[int64]MonitorStatusSummary {
	out := make(map[int64]MonitorStatusSummary, len(ids))
	if len(ids) == 0 {
		return out
	}
	latestMap, err := s.repo.ListLatestForMonitorIDs(ctx, ids)
	if err != nil {
		slog.Warn("channel_monitor: batch load latest failed", "error", err)
		latestMap = map[int64][]*ChannelMonitorLatest{}
	}
	availMap, err := s.repo.ComputeAvailabilityForMonitors(ctx, ids, monitorAvailability7Days)
	if err != nil {
		slog.Warn("channel_monitor: batch compute availability failed", "error", err)
		availMap = map[int64][]*ChannelMonitorAvailability{}
	}

	for _, id := range ids {
		out[id] = buildStatusSummary(
			indexLatestByModel(latestMap[id]),
			indexAvailabilityByModel(availMap[id]),
			primaryByID[id],
			extrasByID[id],
		)
	}
	return out
}

// ListUserView 用户只读视图：列出所有 enabled 监控的概览。
// 使用批量聚合接口避免 N+1：
//
//	1 次查 monitors；
//	1 次批量 latest（含 ping_latency_ms）；
//	1 次批量 7d availability；
//	1 次批量 timeline（主模型最近 N 条）。
func (s *ChannelMonitorService) ListUserView(ctx context.Context) ([]*UserMonitorView, error) {
	monitors, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	if len(monitors) == 0 {
		return []*UserMonitorView{}, nil
	}

	ids, primaryByID, extrasByID := collectMonitorIndexes(monitors)
	summaries := s.BatchMonitorStatusSummary(ctx, ids, primaryByID, extrasByID)
	latestMap := s.batchLatest(ctx, ids)
	timelineMap := s.batchTimeline(ctx, ids, primaryByID)

	views := make([]*UserMonitorView, 0, len(monitors))
	for _, m := range monitors {
		primaryLatest := pickLatest(latestMap[m.ID], m.PrimaryModel)
		views = append(views, buildUserViewFromSummary(m, summaries[m.ID], primaryLatest, timelineMap[m.ID]))
	}
	return views, nil
}

// collectMonitorIndexes 把 monitors 列表按 ID 展开为聚合查询所需的三个索引结构。
func collectMonitorIndexes(monitors []*ChannelMonitor) ([]int64, map[int64]string, map[int64][]string) {
	ids := make([]int64, 0, len(monitors))
	primaryByID := make(map[int64]string, len(monitors))
	extrasByID := make(map[int64][]string, len(monitors))
	for _, m := range monitors {
		ids = append(ids, m.ID)
		primaryByID[m.ID] = m.PrimaryModel
		extrasByID[m.ID] = m.ExtraModels
	}
	return ids, primaryByID, extrasByID
}

// batchLatest 批量取 latest per model，失败仅日志（与现有 BatchMonitorStatusSummary 一致，不阻断列表渲染）。
func (s *ChannelMonitorService) batchLatest(ctx context.Context, ids []int64) map[int64][]*ChannelMonitorLatest {
	latestMap, err := s.repo.ListLatestForMonitorIDs(ctx, ids)
	if err != nil {
		slog.Warn("channel_monitor: user view batch latest failed", "error", err)
		return map[int64][]*ChannelMonitorLatest{}
	}
	return latestMap
}

// batchTimeline 批量取每个 monitor 主模型最近 monitorTimelineMaxPoints 条历史。
func (s *ChannelMonitorService) batchTimeline(
	ctx context.Context,
	ids []int64,
	primaryByID map[int64]string,
) map[int64][]*ChannelMonitorHistoryEntry {
	timelineMap, err := s.repo.ListRecentHistoryForMonitors(ctx, ids, primaryByID, monitorTimelineMaxPoints)
	if err != nil {
		slog.Warn("channel_monitor: user view batch timeline failed", "error", err)
		return map[int64][]*ChannelMonitorHistoryEntry{}
	}
	return timelineMap
}

// pickLatest 从 latest 切片中挑出指定 model 对应项，未命中返回 nil。
func pickLatest(rows []*ChannelMonitorLatest, model string) *ChannelMonitorLatest {
	if model == "" {
		return nil
	}
	for _, r := range rows {
		if r.Model == model {
			return r
		}
	}
	return nil
}

// GetUserDetail 用户只读视图：单个监控详情（每个模型 7d/15d/30d 可用率与平均延迟）。
// 不暴露 api_key。
func (s *ChannelMonitorService) GetUserDetail(ctx context.Context, id int64) (*UserMonitorDetail, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !m.Enabled {
		return nil, ErrChannelMonitorNotFound
	}

	latest, err := s.repo.ListLatestPerModel(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list latest per model: %w", err)
	}
	availMap, err := s.collectAvailabilityWindows(ctx, id)
	if err != nil {
		return nil, err
	}

	models := mergeModelDetails(m, latest, availMap)
	return &UserMonitorDetail{
		ID:        m.ID,
		Name:      m.Name,
		Provider:  m.Provider,
		GroupName: m.GroupName,
		Models:    models,
	}, nil
}

// collectAvailabilityWindows 一次性查询 7/15/30 天三个窗口，按模型组织。
func (s *ChannelMonitorService) collectAvailabilityWindows(ctx context.Context, monitorID int64) (map[int]map[string]*ChannelMonitorAvailability, error) {
	out := make(map[int]map[string]*ChannelMonitorAvailability, 3)
	windows := []int{monitorAvailability7Days, monitorAvailability15Days, monitorAvailability30Days}
	for _, w := range windows {
		rows, err := s.repo.ComputeAvailability(ctx, monitorID, w)
		if err != nil {
			return nil, fmt.Errorf("compute availability %dd: %w", w, err)
		}
		out[w] = indexAvailabilityByModel(rows)
	}
	return out, nil
}

// ---------- 纯函数 helper（无 IO，可在 batch / 单 monitor / detail 路径复用）----------

// indexLatestByModel 把 latest 切片按 model 索引（小工具，避免在 hot path 重复写）。
func indexLatestByModel(rows []*ChannelMonitorLatest) map[string]*ChannelMonitorLatest {
	m := make(map[string]*ChannelMonitorLatest, len(rows))
	for _, r := range rows {
		m[r.Model] = r
	}
	return m
}

// indexAvailabilityByModel 把 availability 切片按 model 索引。
func indexAvailabilityByModel(rows []*ChannelMonitorAvailability) map[string]*ChannelMonitorAvailability {
	m := make(map[string]*ChannelMonitorAvailability, len(rows))
	for _, r := range rows {
		m[r.Model] = r
	}
	return m
}

// buildStatusSummary 由 latest + availability 字典构造 MonitorStatusSummary。
// 不做任何 IO，纯组装，便于在 batch 与单 monitor 路径复用。
func buildStatusSummary(
	latestByModel map[string]*ChannelMonitorLatest,
	availByModel map[string]*ChannelMonitorAvailability,
	primary string,
	extras []string,
) MonitorStatusSummary {
	summary := MonitorStatusSummary{ExtraModels: make([]ExtraModelStatus, 0, len(extras))}
	if primary != "" {
		if l, ok := latestByModel[primary]; ok {
			summary.PrimaryStatus = l.Status
			summary.PrimaryLatencyMs = l.LatencyMs
			// 配额快照只挂主模型行（quota 模式唯一行 / quota_probe 的主行）。
			summary.LatestQuota = l.Quota
		}
		if a, ok := availByModel[primary]; ok {
			summary.Availability7d = a.AvailabilityPct
		}
	}
	for _, model := range extras {
		entry := ExtraModelStatus{Model: model}
		if l, ok := latestByModel[model]; ok {
			entry.Status = l.Status
			entry.LatencyMs = l.LatencyMs
		}
		summary.ExtraModels = append(summary.ExtraModels, entry)
	}
	return summary
}

// buildUserViewFromSummary 用预聚合好的 MonitorStatusSummary + 主模型 latest + timeline 装填 UserMonitorView（无 IO）。
// primaryLatest 可能为 nil（该监控尚无历史）；timelineEntries 可能为空。
func buildUserViewFromSummary(
	m *ChannelMonitor,
	summary MonitorStatusSummary,
	primaryLatest *ChannelMonitorLatest,
	timelineEntries []*ChannelMonitorHistoryEntry,
) *UserMonitorView {
	view := &UserMonitorView{
		ID:               m.ID,
		Name:             m.Name,
		Provider:         m.Provider,
		GroupName:        m.GroupName,
		PrimaryModel:     m.PrimaryModel,
		PrimaryStatus:    summary.PrimaryStatus,
		PrimaryLatencyMs: summary.PrimaryLatencyMs,
		Availability7d:   summary.Availability7d,
		ExtraModels:      summary.ExtraModels,
		Timeline:         buildTimelinePoints(timelineEntries),
	}
	if primaryLatest != nil {
		view.PrimaryPingLatencyMs = primaryLatest.PingLatencyMs
		view.LatestQuota = primaryLatest.Quota
	}
	return view
}

// buildTimelinePoints 把 history entry 裁剪为 timeline 点（去除 message/ID/Model，减小响应体）。
func buildTimelinePoints(entries []*ChannelMonitorHistoryEntry) []UserMonitorTimelinePoint {
	out := make([]UserMonitorTimelinePoint, 0, len(entries))
	for _, e := range entries {
		out = append(out, UserMonitorTimelinePoint{
			Status:        e.Status,
			LatencyMs:     e.LatencyMs,
			PingLatencyMs: e.PingLatencyMs,
			CheckedAt:     e.CheckedAt,
		})
	}
	return out
}

// mergeModelDetails 合并 latest + availability 三个窗口为 ModelDetail 列表。
// 复用 indexLatestByModel，避免在多处重复写 build map 逻辑。
func mergeModelDetails(
	m *ChannelMonitor,
	latest []*ChannelMonitorLatest,
	availMap map[int]map[string]*ChannelMonitorAvailability,
) []ModelDetail {
	all := append([]string{m.PrimaryModel}, m.ExtraModels...)
	latestByModel := indexLatestByModel(latest)
	out := make([]ModelDetail, 0, len(all))
	for _, model := range all {
		d := ModelDetail{Model: model}
		if l, ok := latestByModel[model]; ok {
			d.LatestStatus = l.Status
			d.LatestLatencyMs = l.LatencyMs
		}
		if a, ok := availMap[monitorAvailability7Days][model]; ok {
			d.Availability7d = a.AvailabilityPct
			d.AvgLatency7dMs = a.AvgLatencyMs
		}
		if a, ok := availMap[monitorAvailability15Days][model]; ok {
			d.Availability15d = a.AvailabilityPct
		}
		if a, ok := availMap[monitorAvailability30Days][model]; ok {
			d.Availability30d = a.AvailabilityPct
		}
		out = append(out, d)
	}
	return out
}
