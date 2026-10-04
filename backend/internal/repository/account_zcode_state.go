package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// UpdateZCodeStateIfIdentityUnchanged 复用 scheduler outbox 的原子更新模式。
// 只在原身份仍有效时更新状态/冷却；旧 JWT 的失败不能阻止新登录后的账号。
func (r *accountRepository) UpdateZCodeStateIfIdentityUnchanged(ctx context.Context, id int64, update service.ZCodeStateMutation) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("账号状态 SQL 执行器不可用")
	}
	payload, err := json.Marshal(normalizeJSONMap(update.Extra))
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
 WITH updated AS (
  UPDATE accounts AS a SET
   extra=COALESCE(a.extra,'{}'::jsonb)||$1::jsonb,
   temp_unschedulable_until=CASE WHEN $2::timestamptz IS NOT NULL THEN $2::timestamptz WHEN $4 AND COALESCE(a.temp_unschedulable_reason,'') LIKE 'zcode_%' THEN NULL ELSE a.temp_unschedulable_until END,
   temp_unschedulable_reason=CASE WHEN $2::timestamptz IS NOT NULL THEN $3 WHEN $4 AND COALESCE(a.temp_unschedulable_reason,'') LIKE 'zcode_%' THEN NULL ELSE a.temp_unschedulable_reason END,
   updated_at=NOW()
  WHERE a.id=$5 AND a.deleted_at IS NULL AND a.platform=$6 AND a.type=$7
   AND a.credentials->>'zcode_tokens_encrypted'=$8
   AND a.credentials->>'zcode_provider'=$9
   AND a.credentials->>'account_mode'=$10
   AND a.proxy_id IS NOT DISTINCT FROM $11
  RETURNING a.id
 )
 INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
 SELECT $12,updated.id,NULL,NULL FROM updated
 `, string(payload), update.CooldownUntil, update.CooldownReason, update.ClearOwnCooldown, id, service.PlatformZhipu, service.AccountTypeOAuth, update.CredentialCipher, update.Provider, update.Plan, update.ProxyID, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
