package service

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	APIKeyRoutingHealthWindowDays    = 7
	APIKeyRoutingHealthWindowMinutes = APIKeyRoutingHealthWindowDays * 24 * 60

	APIKeyRoutingCapabilityText       = "text"
	APIKeyRoutingCapabilityMessages   = "messages"
	APIKeyRoutingCapabilityImage      = "image"
	APIKeyRoutingCapabilityBatchImage = "batch_image"
	APIKeyRoutingCapabilityVideo      = "video"

	APIKeyRoutingHealthStatusOperational = "operational"
	APIKeyRoutingHealthStatusDegraded    = "degraded"
	APIKeyRoutingHealthStatusFailed      = "failed"
	APIKeyRoutingHealthStatusUnknown     = "unknown"

	apiKeyRoutingSchedulerHealthWindow = 30 * time.Minute
	apiKeyRoutingHealthPrior           = 20.0
	apiKeyRoutingHealthBase            = 0.95
	apiKeyRoutingHealthOperationalRate = 95.0
	apiKeyRoutingHealthDegradedRate    = 80.0
)

var ErrNoAvailableRoutingGroup = infraerrors.ServiceUnavailable(
	"NO_AVAILABLE_ROUTING_GROUP",
	"no configured routing group can serve this request",
)

type APIKeyRoutingResolveInput struct {
	Model                      string
	Capability                 string
	MediaSize                  string
	SessionKey                 string
	ForcePlatform              string
	RequiredEndpointCapability OpenAIEndpointCapability
	RequiredImageCapability    OpenAIImagesCapability
	RequiredTransport          OpenAIUpstreamTransport
}

type APIKeyRoutingHealth struct {
	Success        int64
	Failure        int64
	LatencyTotalMs int64
	LatencySamples int64
	LastSuccess    *bool
	LastObservedAt *time.Time
}

type APIKeyRoutingHealthSnapshot struct {
	GroupID          int64
	Status           string
	SuccessRate      *float64
	AverageLatencyMs *int64
	SampleCount      int64
	LastObservedAt   *time.Time
}

type APIKeyRoutingHealthProvider interface {
	GetAPIKeyRoutingHealthSnapshots(ctx context.Context, groupIDs []int64) []APIKeyRoutingHealthSnapshot
}

type apiKeyRoutingCache interface {
	GetAPIKeyRoutingGroupID(ctx context.Context, apiKeyID int64, sessionKey string) (int64, error)
	SetAPIKeyRoutingGroupID(ctx context.Context, apiKeyID int64, sessionKey string, groupID int64, ttl time.Duration) error
	RecordAPIKeyRoutingOutcome(ctx context.Context, groupID int64, success bool, latencyMs *int64, at time.Time) error
}

type apiKeyRoutingHealthCache interface {
	GetAPIKeyRoutingHealth(ctx context.Context, groupID int64, since time.Time) (APIKeyRoutingHealth, error)
}

type apiKeyRoutingHealthBatchCache interface {
	GetAPIKeyRoutingHealthBatch(ctx context.Context, groupIDs []int64, since time.Time) (map[int64]APIKeyRoutingHealth, error)
}

type apiKeyRoutingCandidate struct {
	binding       APIKeyGroupBinding
	cost          float64
	stability     float64
	capacity      float64
	modelDirect   bool
	manualOrder   int
	balancedScore float64
}

// ResolveAPIKeyRoutingGroup 从 Key 的候选集合中解析本次请求的实际分组。
func (s *GatewayService) ResolveAPIKeyRoutingGroup(ctx context.Context, apiKey *APIKey, input APIKeyRoutingResolveInput) (*APIKey, error) {
	routingPlatform := ""
	if apiKey != nil {
		routingPlatform = apiKey.RoutingPlatformValue()
	}
	if s != nil && s.settingService != nil &&
		(routingPlatform == PlatformOpenAI || strings.TrimSpace(input.ForcePlatform) == PlatformOpenAI) {
		ctx = withOpenAIQuotaAutoPauseSettings(ctx, s.settingService.GetOpenAIQuotaAutoPauseSettings(ctx))
	}
	return selectWithSchedulerSnapshotRetry(ctx, func(err error) bool {
		return err == ErrNoAvailableRoutingGroup
	}, func(attemptCtx context.Context) (*APIKey, error) {
		return s.resolveAPIKeyRoutingGroupOnce(attemptCtx, apiKey, input)
	})
}

func (s *GatewayService) resolveAPIKeyRoutingGroupOnce(ctx context.Context, apiKey *APIKey, input APIKeyRoutingResolveInput) (*APIKey, error) {
	if apiKey == nil {
		return nil, ErrNoAvailableRoutingGroup
	}
	bindings := apiKey.ConfiguredRoutingGroups()
	if len(bindings) == 0 {
		return apiKey, nil
	}

	subscriptionsByGroup := make(map[int64][]UserSubscription)
	if s.userSubRepo != nil {
		if subscriptions, err := s.userSubRepo.ListActiveByUserID(ctx, apiKey.UserID); err == nil {
			for _, subscription := range subscriptions {
				subscriptionsByGroup[subscription.GroupID] = append(subscriptionsByGroup[subscription.GroupID], subscription)
			}
		}
	}
	strategy := apiKey.RoutingStrategyValue()
	healthByGroup := make(map[int64]APIKeyRoutingHealthSnapshot)
	if strategy == APIKeyRoutingStrategyBalanced || strategy == APIKeyRoutingStrategyStabilityFirst {
		healthByGroup = s.apiKeyRoutingHealthSnapshotsByGroup(ctx, bindings)
	}
	candidates := make([]apiKeyRoutingCandidate, 0, len(bindings))
	for index, binding := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate, ok := s.buildAPIKeyRoutingCandidate(ctx, apiKey, binding, input, index, subscriptionsByGroup, healthByGroup)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoAvailableRoutingGroup
	}

	// 只要至少一个分组直接支持请求模型，就不让未知模型能力的候选抢占请求。
	hasDirectModel := false
	for i := range candidates {
		hasDirectModel = hasDirectModel || candidates[i].modelDirect
	}
	if hasDirectModel {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if candidate.modelDirect {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}

	if stickyID := s.cachedAPIKeyRoutingGroupID(ctx, apiKey.ID, input.SessionKey); stickyID > 0 {
		for _, candidate := range candidates {
			if candidate.binding.GroupID == stickyID {
				return apiKey.CloneWithEffectiveGroup(candidate.binding.Group), nil
			}
		}
	}

	selected := selectAPIKeyRoutingCandidate(candidates, strategy)
	if selected.binding.Group == nil {
		return nil, ErrNoAvailableRoutingGroup
	}
	if input.SessionKey != "" {
		s.bindAPIKeyRoutingGroup(ctx, apiKey.ID, input.SessionKey, selected.binding.GroupID)
	}
	return apiKey.CloneWithEffectiveGroup(selected.binding.Group), nil
}

func (s *GatewayService) buildAPIKeyRoutingCandidate(
	ctx context.Context,
	apiKey *APIKey,
	binding APIKeyGroupBinding,
	input APIKeyRoutingResolveInput,
	manualOrder int,
	subscriptionsByGroup map[int64][]UserSubscription,
	healthByGroup map[int64]APIKeyRoutingHealthSnapshot,
) (apiKeyRoutingCandidate, bool) {
	group := binding.Group
	if group == nil || !group.IsActive() || group.Platform != apiKey.RoutingPlatformValue() {
		return apiKeyRoutingCandidate{}, false
	}
	if apiKey.User != nil && !group.IsSubscriptionType() && !apiKey.User.CanBindGroup(group.ID, group.IsExclusive) {
		return apiKeyRoutingCandidate{}, false
	}
	if strings.TrimSpace(input.ForcePlatform) == "" && group.ClaudeCodeOnly && !IsClaudeCodeClient(ctx) {
		return apiKeyRoutingCandidate{}, false
	}
	if !routingGroupSupportsCapability(group, input.Capability) {
		return apiKeyRoutingCandidate{}, false
	}
	if !s.routingGroupBillingPrecheck(apiKey, group, subscriptionsByGroup) {
		return apiKeyRoutingCandidate{}, false
	}

	groupCopy := *group
	if groupCopy.FallbackGroupID != nil && !apiKey.HasConfiguredGroup(*groupCopy.FallbackGroupID) {
		groupCopy.FallbackGroupID = nil
	}
	binding.Group = &groupCopy

	platform := group.Platform
	if strings.TrimSpace(input.ForcePlatform) != "" {
		platform = strings.TrimSpace(input.ForcePlatform)
	}
	accounts, _, err := s.listSchedulableAccounts(ctx, &group.ID, platform, input.ForcePlatform != "")
	if err != nil {
		return apiKeyRoutingCandidate{}, false
	}
	model := strings.TrimSpace(input.Model)
	if input.Capability == APIKeyRoutingCapabilityMessages && (group.Platform == PlatformOpenAI || group.Platform == PlatformGrok) {
		if mapped := group.ResolveMessagesDispatchModel(model); mapped != "" {
			model = mapped
		}
	}
	eligibleAccounts := accounts[:0]
	for i := range accounts {
		account := accounts[i]
		if platform == PlatformOpenAI || platform == PlatformGrok {
			if !isOpenAICompatibleAccountEligibleForRequest(
				ctx,
				&account,
				platform,
				model,
				false,
				input.RequiredEndpointCapability,
			) {
				continue
			}
		} else if !account.IsSchedulableForModelWithContext(ctx, model) {
			continue
		}
		if group.RequireOAuthOnly && account.Type == AccountTypeAPIKey {
			continue
		}
		if group.RequirePrivacySet && !account.IsPrivacySet() {
			continue
		}
		if !account.SupportsOpenAIEndpointCapability(input.RequiredEndpointCapability) ||
			!account.SupportsOpenAIImageCapability(input.RequiredImageCapability) ||
			!s.routingAccountTransportCompatible(&account, input.RequiredTransport) {
			continue
		}
		eligibleAccounts = append(eligibleAccounts, account)
	}
	accounts = eligibleAccounts
	if len(accounts) == 0 {
		return apiKeyRoutingCandidate{}, false
	}

	modelDirect := model == ""
	accountIDs := make([]int64, 0, len(accounts))
	maxConcurrency := 0
	for i := range accounts {
		account := &accounts[i]
		if model == "" || account.IsModelSupported(model) {
			modelDirect = true
		}
		accountIDs = append(accountIDs, account.ID)
		limit := account.Concurrency
		if limit <= 0 {
			limit = 1
		}
		maxConcurrency += limit
	}
	usedConcurrency := 0
	if s.concurrencyService != nil {
		if loads, loadErr := s.concurrencyService.GetAccountConcurrencyBatch(ctx, accountIDs); loadErr == nil {
			for _, used := range loads {
				usedConcurrency += used
			}
		}
	}
	capacity := 1.0
	if maxConcurrency > 0 {
		capacity = 1 - math.Min(1, float64(usedConcurrency)/float64(maxConcurrency))
	}

	baseCost := group.RateMultiplier
	if s.userGroupRateResolver != nil && apiKey.UserID > 0 {
		baseCost = s.userGroupRateResolver.Resolve(ctx, apiKey.UserID, group.ID, baseCost)
	}
	effectiveKey := apiKey.CloneWithEffectiveGroup(&groupCopy)
	cost := baseCost * groupCopy.PeakMultiplierAt(timezone.Now())
	switch input.Capability {
	case APIKeyRoutingCapabilityImage:
		cost = resolveImageRateMultiplier(effectiveKey, baseCost)
		if price := routingImageUnitPrice(&groupCopy, input.MediaSize); price != nil {
			cost *= *price
		}
	case APIKeyRoutingCapabilityBatchImage:
		cost = resolveImageRateMultiplier(effectiveKey, baseCost)
		if price := routingImageUnitPrice(&groupCopy, input.MediaSize); price != nil {
			cost *= *price
		}
		cost *= math.Max(0, groupCopy.BatchImageDiscountMultiplier)
	case APIKeyRoutingCapabilityVideo:
		cost = resolveVideoRateMultiplier(effectiveKey, baseCost)
		if price := groupCopy.GetVideoPrice(input.MediaSize); price != nil && *price >= 0 {
			cost *= *price
		}
	}

	health, hasHealth := healthByGroup[group.ID]
	smoothedSuccess := apiKeyRoutingHealthBase
	if hasHealth && health.SuccessRate != nil && health.SampleCount > 0 {
		samples := float64(health.SampleCount)
		smoothedSuccess = ((*health.SuccessRate/100)*samples + apiKeyRoutingHealthPrior*apiKeyRoutingHealthBase) /
			(samples + apiKeyRoutingHealthPrior)
	}
	stability := 0.8*smoothedSuccess + 0.2*capacity

	return apiKeyRoutingCandidate{
		binding:     binding,
		cost:        cost,
		stability:   stability,
		capacity:    capacity,
		modelDirect: modelDirect,
		manualOrder: manualOrder,
	}, true
}

func (s *GatewayService) routingAccountTransportCompatible(account *Account, required OpenAIUpstreamTransport) bool {
	if required == OpenAIUpstreamTransportAny || required == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	var cfg *config.Config
	if s != nil {
		cfg = s.cfg
	}
	return openAIAccountTransportCompatible(cfg, NewOpenAIWSProtocolResolver(cfg), account, required)
}

func routingImageUnitPrice(group *Group, imageSize string) *float64 {
	if group == nil {
		return nil
	}
	tier, ok := ClassifyImageBillingTier(imageSize)
	if !ok {
		return nil
	}
	return group.GetImagePrice(tier)
}

func routingGroupSupportsCapability(group *Group, capability string) bool {
	if group == nil {
		return false
	}
	switch capability {
	case APIKeyRoutingCapabilityMessages:
		return true
	case APIKeyRoutingCapabilityImage:
		return GroupAllowsImageGeneration(group)
	case APIKeyRoutingCapabilityBatchImage:
		return group.Platform == PlatformGemini && group.AllowBatchImageGeneration
	case APIKeyRoutingCapabilityVideo:
		return GroupAllowsImageGeneration(group)
	}
	return true
}

func (s *GatewayService) routingGroupBillingPrecheck(apiKey *APIKey, group *Group, subscriptionsByGroup map[int64][]UserSubscription) bool {
	if s == nil || apiKey == nil || group == nil || s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return true
	}
	if group.IsSubscriptionType() {
		for i := range subscriptionsByGroup[group.ID] {
			subscription := subscriptionsByGroup[group.ID][i]
			if _, err := (&SubscriptionService{}).ValidateAndCheckLimits(&subscription, group); err == nil {
				return true
			}
		}
		return false
	}
	return apiKey.User != nil && apiKey.User.Balance > 0
}

func selectAPIKeyRoutingCandidate(candidates []apiKeyRoutingCandidate, strategy string) apiKeyRoutingCandidate {
	if len(candidates) == 1 {
		return candidates[0]
	}
	switch strategy {
	case APIKeyRoutingStrategyCostFirst:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].cost == candidates[j].cost {
				return candidates[i].stability > candidates[j].stability
			}
			return candidates[i].cost < candidates[j].cost
		})
		return candidates[0]
	case APIKeyRoutingStrategyStabilityFirst:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].stability == candidates[j].stability {
				return candidates[i].cost < candidates[j].cost
			}
			return candidates[i].stability > candidates[j].stability
		})
		return weightedRoutingCandidate(candidates, func(candidate apiKeyRoutingCandidate) float64 {
			if candidates[0].stability-candidate.stability > 0.05 {
				return 0
			}
			return math.Max(0.01, candidate.stability)
		})
	case APIKeyRoutingStrategyBalanced:
		minCost, maxCost := candidates[0].cost, candidates[0].cost
		minStability, maxStability := candidates[0].stability, candidates[0].stability
		for _, candidate := range candidates[1:] {
			minCost = math.Min(minCost, candidate.cost)
			maxCost = math.Max(maxCost, candidate.cost)
			minStability = math.Min(minStability, candidate.stability)
			maxStability = math.Max(maxStability, candidate.stability)
		}
		for i := range candidates {
			costScore := 1.0
			if maxCost > minCost {
				costScore = (maxCost - candidates[i].cost) / (maxCost - minCost)
			}
			stabilityScore := 1.0
			if maxStability > minStability {
				stabilityScore = (candidates[i].stability - minStability) / (maxStability - minStability)
			}
			candidates[i].balancedScore = 0.5*costScore + 0.5*stabilityScore
		}
		return weightedRoutingCandidate(candidates, func(candidate apiKeyRoutingCandidate) float64 {
			return math.Exp(4 * (candidate.balancedScore - 1))
		})
	default:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].binding.Priority == candidates[j].binding.Priority {
				return candidates[i].manualOrder < candidates[j].manualOrder
			}
			return candidates[i].binding.Priority < candidates[j].binding.Priority
		})
		return candidates[0]
	}
}

func weightedRoutingCandidate(candidates []apiKeyRoutingCandidate, weight func(apiKeyRoutingCandidate) float64) apiKeyRoutingCandidate {
	total := 0.0
	weights := make([]float64, len(candidates))
	for i, candidate := range candidates {
		weights[i] = math.Max(0, weight(candidate))
		total += weights[i]
	}
	if total <= 0 {
		return candidates[0]
	}
	pick := rand.Float64() * total
	for i, candidateWeight := range weights {
		pick -= candidateWeight
		if pick <= 0 {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

func (s *GatewayService) cachedAPIKeyRoutingGroupID(ctx context.Context, apiKeyID int64, sessionKey string) int64 {
	cache, ok := s.cache.(apiKeyRoutingCache)
	if !ok || apiKeyID <= 0 || sessionKey == "" {
		return 0
	}
	groupID, err := cache.GetAPIKeyRoutingGroupID(ctx, apiKeyID, sessionKey)
	if err != nil {
		return 0
	}
	return groupID
}

func (s *GatewayService) bindAPIKeyRoutingGroup(ctx context.Context, apiKeyID int64, sessionKey string, groupID int64) {
	cache, ok := s.cache.(apiKeyRoutingCache)
	if !ok || apiKeyID <= 0 || sessionKey == "" || groupID <= 0 {
		return
	}
	_ = cache.SetAPIKeyRoutingGroupID(ctx, apiKeyID, sessionKey, groupID, stickySessionTTL)
}

func (s *GatewayService) BindAPIKeyRoutingSession(ctx context.Context, apiKeyID int64, sessionKey string, groupID int64) {
	s.bindAPIKeyRoutingGroup(ctx, apiKeyID, sessionKey, groupID)
}

func (s *GatewayService) SetAPIKeyRoutingHealthProvider(provider APIKeyRoutingHealthProvider) {
	if s == nil {
		return
	}
	s.apiKeyRoutingHealthSource = provider
}

func (s *GatewayService) apiKeyRoutingHealthSnapshotsByGroup(
	ctx context.Context,
	bindings []APIKeyGroupBinding,
) map[int64]APIKeyRoutingHealthSnapshot {
	groupIDs := make([]int64, 0, len(bindings))
	for _, binding := range bindings {
		if binding.GroupID > 0 {
			groupIDs = append(groupIDs, binding.GroupID)
		}
	}
	snapshots := s.apiKeyRoutingSchedulerHealthSnapshots(ctx, groupIDs)
	out := make(map[int64]APIKeyRoutingHealthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		out[snapshot.GroupID] = snapshot
	}
	return out
}

func (s *GatewayService) apiKeyRoutingSchedulerHealthSnapshots(
	ctx context.Context,
	groupIDs []int64,
) []APIKeyRoutingHealthSnapshot {
	orderedIDs := uniqueAPIKeyRoutingGroupIDs(groupIDs)
	if len(orderedIDs) == 0 {
		return []APIKeyRoutingHealthSnapshot{}
	}

	healthByID := make(map[int64]APIKeyRoutingHealth, len(orderedIDs))
	since := timezone.Now().Add(-apiKeyRoutingSchedulerHealthWindow)
	if cache, ok := s.cache.(apiKeyRoutingHealthBatchCache); ok {
		if health, err := cache.GetAPIKeyRoutingHealthBatch(ctx, orderedIDs, since); err == nil {
			healthByID = health
		}
	} else if cache, ok := s.cache.(apiKeyRoutingHealthCache); ok {
		for _, groupID := range orderedIDs {
			if health, err := cache.GetAPIKeyRoutingHealth(ctx, groupID, since); err == nil {
				healthByID[groupID] = health
			}
		}
	}

	snapshots := make([]APIKeyRoutingHealthSnapshot, 0, len(orderedIDs))
	for _, groupID := range orderedIDs {
		snapshots = append(snapshots, buildAPIKeyRoutingSchedulerHealthSnapshot(groupID, healthByID[groupID]))
	}
	return snapshots
}

// GetAPIKeyRoutingHealthSnapshots 仅返回编辑器展示用的 Channel Monitor 快照。
// 实际候选排序由 apiKeyRoutingSchedulerHealthSnapshots 继续使用 Redis 近期结果。
func (s *GatewayService) GetAPIKeyRoutingHealthSnapshots(ctx context.Context, groupIDs []int64) []APIKeyRoutingHealthSnapshot {
	orderedIDs := uniqueAPIKeyRoutingGroupIDs(groupIDs)
	if len(orderedIDs) == 0 {
		return []APIKeyRoutingHealthSnapshot{}
	}

	requestedIDs := make(map[int64]struct{}, len(orderedIDs))
	for _, groupID := range orderedIDs {
		requestedIDs[groupID] = struct{}{}
	}
	providedByID := make(map[int64]APIKeyRoutingHealthSnapshot, len(orderedIDs))
	if s != nil && s.apiKeyRoutingHealthSource != nil {
		for _, snapshot := range s.apiKeyRoutingHealthSource.GetAPIKeyRoutingHealthSnapshots(ctx, orderedIDs) {
			if _, requested := requestedIDs[snapshot.GroupID]; requested {
				providedByID[snapshot.GroupID] = snapshot
			}
		}
	}

	snapshots := make([]APIKeyRoutingHealthSnapshot, 0, len(orderedIDs))
	for _, groupID := range orderedIDs {
		if snapshot, ok := providedByID[groupID]; ok {
			snapshots = append(snapshots, snapshot)
			continue
		}
		snapshots = append(snapshots, APIKeyRoutingHealthSnapshot{
			GroupID: groupID,
			Status:  APIKeyRoutingHealthStatusUnknown,
		})
	}
	return snapshots
}

func uniqueAPIKeyRoutingGroupIDs(groupIDs []int64) []int64 {
	orderedIDs := make([]int64, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		orderedIDs = append(orderedIDs, groupID)
	}
	return orderedIDs
}

func buildAPIKeyRoutingSchedulerHealthSnapshot(groupID int64, health APIKeyRoutingHealth) APIKeyRoutingHealthSnapshot {
	snapshot := APIKeyRoutingHealthSnapshot{
		GroupID:        groupID,
		Status:         APIKeyRoutingHealthStatusUnknown,
		SampleCount:    health.Success + health.Failure,
		LastObservedAt: health.LastObservedAt,
	}
	if health.LatencySamples > 0 {
		average := (health.LatencyTotalMs + health.LatencySamples/2) / health.LatencySamples
		snapshot.AverageLatencyMs = &average
	}
	if snapshot.SampleCount <= 0 {
		return snapshot
	}

	successRate := 100 * float64(health.Success) / float64(snapshot.SampleCount)
	snapshot.SuccessRate = &successRate
	if health.LastSuccess != nil {
		if !*health.LastSuccess {
			snapshot.Status = APIKeyRoutingHealthStatusFailed
			return snapshot
		}
		if successRate < apiKeyRoutingHealthOperationalRate {
			snapshot.Status = APIKeyRoutingHealthStatusDegraded
			return snapshot
		}
		snapshot.Status = APIKeyRoutingHealthStatusOperational
		return snapshot
	}

	switch {
	case successRate >= apiKeyRoutingHealthOperationalRate:
		snapshot.Status = APIKeyRoutingHealthStatusOperational
	case successRate >= apiKeyRoutingHealthDegradedRate:
		snapshot.Status = APIKeyRoutingHealthStatusDegraded
	default:
		snapshot.Status = APIKeyRoutingHealthStatusFailed
	}
	return snapshot
}

func (s *GatewayService) RecordAPIKeyRoutingOutcome(ctx context.Context, groupID int64, success bool, latencyMs *int64) {
	cache, ok := s.cache.(apiKeyRoutingCache)
	if !ok || groupID <= 0 {
		return
	}
	_ = cache.RecordAPIKeyRoutingOutcome(ctx, groupID, success, latencyMs, timezone.Now())
}
