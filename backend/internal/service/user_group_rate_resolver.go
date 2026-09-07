package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

type userGroupRateResolver struct {
	repo         UserGroupRateRepository
	cache        *gocache.Cache
	cacheTTL     time.Duration
	sf           *singleflight.Group
	logComponent string
}

func newUserGroupRateResolver(repo UserGroupRateRepository, cache *gocache.Cache, cacheTTL time.Duration, sf *singleflight.Group, logComponent string) *userGroupRateResolver {
	if cacheTTL <= 0 {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cache == nil {
		cache = gocache.New(cacheTTL, time.Minute)
	}
	if logComponent == "" {
		logComponent = "service.gateway"
	}
	if sf == nil {
		sf = &singleflight.Group{}
	}

	return &userGroupRateResolver{
		repo:         repo,
		cache:        cache,
		cacheTTL:     cacheTTL,
		sf:           sf,
		logComponent: logComponent,
	}
}

type resolvedUserGroupRate struct {
	Multiplier   float64
	HasOverride  bool
	LookupFailed bool
}

func (r *userGroupRateResolver) Resolve(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	return r.ResolveDetail(ctx, userID, groupID, groupDefaultMultiplier).Multiplier
}

func (r *userGroupRateResolver) ResolveDetail(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) resolvedUserGroupRate {
	fallback := resolvedUserGroupRate{Multiplier: groupDefaultMultiplier}
	if r == nil || userID <= 0 || groupID <= 0 {
		return fallback
	}

	key := fmt.Sprintf("%d:%d", userID, groupID)
	if cached, ok := r.cachedRate(key, groupDefaultMultiplier); ok {
		userGroupRateCacheHitTotal.Add(1)
		return cached
	}
	if r.repo == nil {
		return fallback
	}
	userGroupRateCacheMissTotal.Add(1)

	value, err, shared := r.sf.Do(key, func() (any, error) {
		if cached, ok := r.cachedRate(key, groupDefaultMultiplier); ok {
			userGroupRateCacheHitTotal.Add(1)
			return cached, nil
		}

		userGroupRateCacheLoadTotal.Add(1)
		userRate, repoErr := r.repo.GetByUserAndGroup(ctx, userID, groupID)
		if repoErr != nil {
			return nil, repoErr
		}

		resolved := fallback
		if userRate != nil {
			resolved.Multiplier = *userRate
			resolved.HasOverride = true
		}
		if r.cache != nil {
			// 倍率沿用既有 float64 缓存格式；单独标记覆盖状态，兼容进程内现有调用和测试。
			r.cache.Set(key, resolved.Multiplier, r.cacheTTL)
			r.cache.Set(key+":override", resolved.HasOverride, r.cacheTTL)
		}
		return resolved, nil
	})
	if shared {
		userGroupRateCacheSFSharedTotal.Add(1)
	}
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		fallback.LookupFailed = true
		return fallback
	}

	resolved, ok := value.(resolvedUserGroupRate)
	if !ok {
		userGroupRateCacheFallbackTotal.Add(1)
		fallback.LookupFailed = true
		return fallback
	}
	return resolved
}

func (r *userGroupRateResolver) cachedRate(key string, groupDefaultMultiplier float64) (resolvedUserGroupRate, bool) {
	if r == nil || r.cache == nil {
		return resolvedUserGroupRate{}, false
	}
	cached, ok := r.cache.Get(key)
	if !ok {
		return resolvedUserGroupRate{}, false
	}
	multiplier, ok := cached.(float64)
	if !ok {
		return resolvedUserGroupRate{}, false
	}
	hasOverride := multiplier != groupDefaultMultiplier
	if cachedOverride, exists := r.cache.Get(key + ":override"); exists {
		if value, castOK := cachedOverride.(bool); castOK {
			hasOverride = value
		}
	}
	return resolvedUserGroupRate{Multiplier: multiplier, HasOverride: hasOverride}, true
}
