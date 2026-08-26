package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

type schedulerSnapshotRetryStateKey struct{}
type schedulerSnapshotAuthoritativeReadKey struct{}

type schedulerSnapshotRetryState struct {
	cacheHit              atomic.Bool
	authoritativeAttempt  atomic.Bool
	authoritativeAccounts sync.Map
}

func withSchedulerSnapshotRetryState(ctx context.Context) (context.Context, *schedulerSnapshotRetryState) {
	if state, ok := ctx.Value(schedulerSnapshotRetryStateKey{}).(*schedulerSnapshotRetryState); ok && state != nil {
		return ctx, state
	}
	state := &schedulerSnapshotRetryState{}
	return context.WithValue(ctx, schedulerSnapshotRetryStateKey{}, state), state
}

func markSchedulerSnapshotCacheHit(ctx context.Context) {
	if state, ok := ctx.Value(schedulerSnapshotRetryStateKey{}).(*schedulerSnapshotRetryState); ok && state != nil {
		state.cacheHit.Store(true)
	}
}

func isSchedulerSnapshotAuthoritativeRead(ctx context.Context) bool {
	requested, _ := ctx.Value(schedulerSnapshotAuthoritativeReadKey{}).(bool)
	return requested
}

func rememberSchedulerAuthoritativeAccounts(ctx context.Context, accounts []Account) {
	state, _ := ctx.Value(schedulerSnapshotRetryStateKey{}).(*schedulerSnapshotRetryState)
	if state == nil {
		return
	}
	for i := range accounts {
		account := accounts[i]
		state.authoritativeAccounts.Store(account.ID, account)
	}
}

func schedulerAuthoritativeAccountFromContext(ctx context.Context, accountID int64) (*Account, bool) {
	state, _ := ctx.Value(schedulerSnapshotRetryStateKey{}).(*schedulerSnapshotRetryState)
	if state == nil {
		return nil, false
	}
	value, ok := state.authoritativeAccounts.Load(accountID)
	if !ok {
		return nil, false
	}
	account, ok := value.(Account)
	if !ok {
		return nil, false
	}
	cloned, err := cloneSchedulerAccounts([]Account{account})
	if err != nil || len(cloned) != 1 {
		return nil, false
	}
	return &cloned[0], true
}

// selectWithSchedulerSnapshotRetry 仅在本次选择确实命中过调度快照且最终无候选时，
// 绕过快照执行一次权威重读。嵌套调度入口共享状态，避免同一选择链重复回源。
func selectWithSchedulerSnapshotRetry[T any](
	ctx context.Context,
	isExhausted func(error) bool,
	selectOnce func(context.Context) (T, error),
) (T, error) {
	ctx, state := withSchedulerSnapshotRetryState(ctx)
	result, err := selectOnce(ctx)
	if err == nil || isExhausted == nil || !isExhausted(err) || !state.cacheHit.Load() {
		return result, err
	}
	if !state.authoritativeAttempt.CompareAndSwap(false, true) {
		return result, err
	}

	retryCtx := context.WithValue(ctx, schedulerSnapshotAuthoritativeReadKey{}, true)
	refreshedResult, refreshedErr := selectOnce(retryCtx)
	if refreshedErr != nil && !isExhausted(refreshedErr) {
		if ctxErr := retryCtx.Err(); ctxErr != nil {
			return refreshedResult, ctxErr
		}
		// 权威重读是旧快照耗尽后的恢复路径；数据库或缓存写入瞬时失败时，
		// 保留首次“无可用账号”语义，避免把可恢复的 503 放大成内部错误。
		slog.Debug("scheduler_snapshot_authoritative_retry_failed", "error", refreshedErr)
		return result, err
	}
	return refreshedResult, refreshedErr
}

func isNoAvailableAccountSelectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrNoAvailableCompactAccounts) {
		return true
	}
	message := err.Error()
	return strings.HasPrefix(message, "no available OpenAI accounts") ||
		strings.HasPrefix(message, "no available Gemini accounts")
}
