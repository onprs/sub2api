package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

type ZCodeClaimState struct {
	CheckedAt   int64           `json:"checked_at"`
	ClaimedAt   int64           `json:"claimed_at,omitempty"`
	Result      string          `json:"result"`
	Campaign    *zcode.Campaign `json:"campaign,omitempty"`
	Plan        *zcode.Plan     `json:"plan,omitempty"`
	Error       string          `json:"last_error,omitempty"`
	NextAttempt int64           `json:"next_attempt"`
}

func (s *ZCodeService) quotaTTL() time.Duration {
	seconds := 60
	if s.cfg != nil && s.cfg.Gateway.ZCode.QuotaCacheSeconds > seconds {
		seconds = s.cfg.Gateway.ZCode.QuotaCacheSeconds
	}
	return time.Duration(seconds) * time.Second
}
func (s *ZCodeService) claimInterval() time.Duration {
	seconds := 300
	if s.cfg != nil && s.cfg.Gateway.ZCode.ClaimIntervalSeconds > seconds {
		seconds = s.cfg.Gateway.ZCode.ClaimIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}
func accountJitter(id int64, now time.Time) time.Duration {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", id, now.Unix()/300)))
	return time.Duration(int(sum[0])%30) * time.Second
}
func (s *ZCodeService) loadAccount(ctx context.Context, id int64) (*Account, error) {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.IsZCodeOAuth() {
		return nil, infraerrors.BadRequest("ZCODE_ACCOUNT_INVALID", "账号不是 ZCode OAuth 智谱账号")
	}
	return a, nil
}
func (s *ZCodeService) QueryQuota(ctx context.Context, id int64) (*zcode.Quota, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	a, err := s.loadAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	if q := zcodeQuotaFromAccount(a); q != nil && time.Now().Unix() < q.UpdatedAt+int64(s.quotaTTL()/time.Second) {
		return q, nil
	}
	key := "quota-lock:" + zcodeAccountKey(id)
	owner := uuid.NewString()
	locked, err := s.store.Acquire(ctx, key, owner, 30*time.Second)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if !locked {
		if q := zcodeQuotaFromAccount(a); q != nil {
			return q, nil
		}
		return nil, infraerrors.New(http.StatusTooManyRequests, "ZCODE_QUOTA_BUSY", "额度正在更新，请稍后再试")
	}
	defer s.release(key, owner)
	// 检查周期锁在失败后仍保留，避免所有实例依次击穿上游。
	pollKey := "quota-period:" + zcodeAccountKey(id)
	permitted, err := s.store.Acquire(ctx, pollKey, owner, s.quotaTTL())
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if !permitted {
		if q := zcodeQuotaFromAccount(a); q != nil {
			return q, nil
		}
		return nil, infraerrors.New(http.StatusTooManyRequests, "ZCODE_QUOTA_COOLDOWN", "请稍后再查询额度")
	}
	tokens, err := s.tokens(a)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if tokens.Expired(time.Now()) {
		e := &zcode.Error{Kind: zcode.ErrLogin}
		s.stateError(ctx, a, e)
		return nil, zcodeAdminError(e)
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	quota, err := s.client(a, proxy).QueryQuota(ctx, a.ZCodeProvider(), a.GetAccountMode(), tokens)
	if err != nil {
		if e, ok := err.(*zcode.Error); ok {
			s.stateError(ctx, a, e)
		}
		return nil, zcodeAdminError(err)
	}
	updates := map[string]any{zcodeQuotaKey: quota, zcodeStateKey: map[string]any{"oauth_status": "authorized", "checked_at": time.Now().Unix()}}
	// GLM Coding Plan 继续提供原有调度阈值窗口字段。
	if a.GetAccountMode() == AccountModeCoding {
		for _, b := range quota.Balances {
			if b.Percent != nil {
				switch b.Window {
				case "5h":
					updates["zhipu_5h_used_percent"] = *b.Percent
					if b.ResetAt > 0 {
						updates["zhipu_5h_reset_at"] = time.Unix(b.ResetAt, 0).UTC().Format(time.RFC3339)
					}
				case "weekly":
					updates["zhipu_weekly_used_percent"] = *b.Percent
					if b.ResetAt > 0 {
						updates["zhipu_weekly_reset_at"] = time.Unix(b.ResetAt, 0).UTC().Format(time.RFC3339)
					}
				}
			}
		}
		updates["zhipu_usage_updated_at"] = time.Unix(quota.UpdatedAt, 0).UTC().Format(time.RFC3339)
	}
	if ok, updateErr := s.updateState(ctx, a, updates, nil, "", true); updateErr != nil || !ok {
		if updateErr != nil {
			return nil, updateErr
		}
		return nil, &zcode.Error{Kind: zcode.ErrUnavailable}
	}
	if quota.Exhausted(time.Now()) {
		s.stateError(ctx, a, &zcode.Error{Kind: zcode.ErrQuota, RetryAt: quota.NextAvailableAt})
	} else {
		s.clearOwnCooldown(ctx, a)
	}
	return &quota, nil
}
func (s *ZCodeService) CheckClaim(ctx context.Context, id int64, execute bool) (*ZCodeClaimState, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	a, err := s.loadAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.GetAccountMode() != AccountModeStart {
		return nil, infraerrors.BadRequest("ZCODE_START_REQUIRED", "领取体验套餐需要 Start Plan 账号")
	}
	previous := &ZCodeClaimState{}
	_ = decodeZCodeExtra(a.Extra[zcodeClaimKey], previous)
	if previous.NextAttempt > time.Now().Unix() && (!execute || previous.Result != "available") {
		return previous, nil
	}
	owner := uuid.NewString()
	key := "claim-lock:" + zcodeAccountKey(id)
	locked, err := s.store.Acquire(ctx, key, owner, 90*time.Second)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if !locked {
		return previous, nil
	}
	defer s.release(key, owner)
	// 单一检查周期只允许一个实例工作；不释放 period 锁，覆盖崩溃/请求中断。
	interval := s.claimInterval() + accountJitter(id, time.Now())
	permitted, err := s.store.Acquire(ctx, claimPeriodKey(id, execute), owner, interval)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if !permitted {
		return previous, nil
	}
	// 持久化下一次尝试先于外部 claim，防止进程退出后的重复领取。
	now := time.Now()
	state := &ZCodeClaimState{CheckedAt: now.Unix(), Result: "checking", Plan: previous.Plan, ClaimedAt: previous.ClaimedAt, NextAttempt: now.Add(interval).Unix()}
	if err = s.persistClaim(ctx, id, state); err != nil {
		return nil, err
	}
	tokens, err := s.tokens(a)
	if err != nil {
		return s.finishClaim(ctx, a, state, "configuration", err)
	}
	if tokens.JWT == "" || tokens.Expired(now) {
		return s.finishClaim(ctx, a, state, "login_required", &zcode.Error{Kind: zcode.ErrLogin})
	}
	proxy, err := s.proxyURL(ctx, a.ProxyID)
	if err != nil {
		return s.finishClaim(ctx, a, state, "configuration", err)
	}
	client := s.client(a, proxy)
	preview, err := client.Preview(ctx, tokens.JWT)
	if err != nil {
		return s.finishClaim(ctx, a, state, string(zcode.KindOf(err)), err)
	}
	state.Campaign = preview.Campaign
	if preview.Plan != nil {
		state.Plan = preview.Plan
	}
	if preview.RetryAt > state.NextAttempt {
		state.NextAttempt = preview.RetryAt
	}
	if preview.Campaign == nil {
		return s.finishClaim(ctx, a, state, "none", nil)
	}
	campaign := preview.Campaign
	if campaign.MinimumVersion != "" && !zcode.VersionAtLeast(client.Identity.Version(), campaign.MinimumVersion) {
		return s.finishClaim(ctx, a, state, "version_too_low", nil)
	}
	if campaign.StartsAt > now.Unix() {
		state.NextAttempt = campaign.StartsAt + int64(accountJitter(id, now)/time.Second)
		return s.finishClaim(ctx, a, state, "not_in_window", nil)
	}
	if (campaign.EndsAt > 0 && campaign.EndsAt <= now.Unix()) || (campaign.ClaimUntil > 0 && campaign.ClaimUntil <= now.Unix()) {
		return s.finishClaim(ctx, a, state, "not_in_window", nil)
	}
	if !preview.Claimable {
		result := preview.Blocked
		if result == "" {
			result = "not_eligible"
		}
		if result == "quota_exhausted" && campaign.EndsAt > now.Unix() {
			state.NextAttempt = campaign.EndsAt + int64(accountJitter(id, now)/time.Second)
		}
		return s.finishClaim(ctx, a, state, result, nil)
	}
	if !execute {
		return s.finishClaim(ctx, a, state, "available", nil)
	}
	planID := ""
	preferred := a.GetCredential("zcode_claim_plan_id")
	for _, plan := range campaign.Plans {
		if preferred == "" || plan.ID == preferred {
			planID = plan.ID
			break
		}
	}
	if planID == "" {
		return s.finishClaim(ctx, a, state, "plan_unavailable", nil)
	}
	token, err := s.pool(proxy).Take(ctx, client, proxy)
	if err != nil {
		return s.finishClaim(ctx, a, state, "captcha", err)
	}
	result, err := client.Claim(ctx, tokens.JWT, planID, token)
	if err != nil {
		return s.finishClaim(ctx, a, state, string(zcode.KindOf(err)), err)
	}
	if result.RetryAt > state.NextAttempt {
		state.NextAttempt = result.RetryAt
	}
	if result.Plan != nil {
		state.Plan = result.Plan
	}
	if result.OK {
		state.ClaimedAt = time.Now().Unix()
		state.Result = "claimed"
		if result.Plan.StartsAt > time.Now().Unix() {
			state.NextAttempt = result.Plan.StartsAt + int64(accountJitter(id, now)/time.Second)
		}
		if err = s.persistClaim(ctx, id, state); err != nil {
			return nil, err
		}
		// 领取后废弃旧余额快照及查询周期，下一次额度查询读取新套餐。
		_ = s.repo.UpdateExtra(ctx, id, map[string]any{zcodeQuotaKey: nil})
		_, _ = s.store.Take(ctx, "quota-period:"+zcodeAccountKey(id))
		_, _ = s.QueryQuota(ctx, id)
		return state, nil
	}
	if result.Result == "quota_exhausted" && campaign.EndsAt > state.NextAttempt {
		state.NextAttempt = campaign.EndsAt + int64(accountJitter(id, now)/time.Second)
	}
	return s.finishClaim(ctx, a, state, result.Result, nil)
}
func claimPeriodKey(id int64, execute bool) string {
	if execute {
		return "claim-period:" + zcodeAccountKey(id)
	}
	return "preview-period:" + zcodeAccountKey(id)
}

func (s *ZCodeService) persistClaim(ctx context.Context, id int64, state *ZCodeClaimState) error {
	a, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	ok, err := s.updateState(ctx, a, map[string]any{zcodeClaimKey: state}, nil, "", false)
	if err != nil {
		return err
	}
	if !ok {
		return &zcode.Error{Kind: zcode.ErrUnavailable}
	}
	return nil
}
func (s *ZCodeService) finishClaim(ctx context.Context, a *Account, state *ZCodeClaimState, result string, err error) (*ZCodeClaimState, error) {
	state.Result = result
	state.Error = ""
	if err != nil {
		state.Error = string(zcode.KindOf(err))
		if e, ok := err.(*zcode.Error); ok {
			if e.RetryAt > state.NextAttempt {
				state.NextAttempt = e.RetryAt
			}
			if e.Kind == zcode.ErrLogin {
				s.stateError(ctx, a, e)
			}
		}
	}
	if err != nil || result == "captcha" || result == "not_eligible" || result == "already_claimed" {
		minimum := time.Now().Add(10 * time.Minute).Unix()
		if state.NextAttempt < minimum {
			state.NextAttempt = minimum
		}
	}
	if result == "login_required" {
		s.stateError(ctx, a, &zcode.Error{Kind: zcode.ErrLogin})
	}
	if e := s.persistClaim(ctx, a.ID, state); e != nil {
		return nil, e
	}
	return state, nil
}
func (s *ZCodeService) Start() {
	s.start.Do(func() {
		if s.available() != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.done = make(chan struct{})
		go func() {
			defer close(s.done)
			timer := time.NewTimer(20 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					s.runCycle(ctx)
					timer.Reset(time.Minute)
				}
			}
		}()
	})
}
func (s *ZCodeService) Stop() {
	s.stop.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
	})
}
func (s *ZCodeService) runCycle(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 50*time.Second)
	defer cancel()
	accounts, err := s.repo.ListByPlatform(ctx, PlatformZhipu)
	if err != nil {
		slog.Warn("zcode_account_scan_failed")
		return
	}
	eligible := []int64{}
	for i := range accounts {
		a := &accounts[i]
		if a.IsZCodeOAuth() && a.Status == StatusActive {
			if state, ok := a.Extra[zcodeStateKey].(map[string]any); ok && state["oauth_status"] == "login_required" {
				continue
			}
			eligible = append(eligible, a.ID)
		}
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i] < eligible[j] })
	if len(eligible) == 0 {
		s.workerCursor = 0
		return
	}
	s.workerCursor %= len(eligible)
	jobs := make(chan int64, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				if ctx.Err() != nil {
					return
				}
				a, err := s.loadAccount(ctx, id)
				if err != nil {
					continue
				}
				quota, quotaErr := s.QueryQuota(ctx, id)
				if quotaErr != nil {
					slog.Debug("zcode_quota_check_failed", "account_id", id)
				}
				if quotaErr == nil && a.GetAccountMode() == AccountModeStart && !quota.Exhausted(time.Now()) {
					if proxy, proxyErr := s.proxyURL(ctx, a.ProxyID); proxyErr == nil {
						_ = s.pool(proxy).Warm(ctx, s.client(a, proxy), proxy)
					}
				}
				enabled, _ := a.Credentials["zcode_auto_claim"].(bool)
				if enabled && a.GetAccountMode() == AccountModeStart {
					if _, err = s.CheckClaim(ctx, id, true); err != nil {
						slog.Debug("zcode_claim_check_failed", "account_id", id)
					}
				}
			}
		}()
	}
	for i := 0; i < len(eligible); i++ {
		index := s.workerCursor
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		case jobs <- eligible[index]:
			s.workerCursor = (index + 1) % len(eligible)
		}
	}
	close(jobs)
	workers.Wait()
}
