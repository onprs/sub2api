# 套餐维度独立订阅 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让绑定同一分组的不同订阅套餐成为相互独立的用户权益，并让请求用量真实扣到“最早过期且额度足够”的一条权益上。

**Architecture:** `user_subscriptions` 增加可空 `plan_id`。支付和套餐兑换码创建的权益按 `user_id + group_id + plan_id` 续期或新建，旧版分组权益继续用 `plan_id IS NULL`。请求前只做“有可用订阅”的轻量校验；真实消费成本出来后，由 `usage_billing_repo.Apply` 在同一个数据库事务内选择并扣减“最早过期且额度足够”的一条权益，并把返回的 `subscription_id` 写回 usage log。

**Tech Stack:** Go、Ent、PostgreSQL SQL migrations、Gin gateway handlers、Vue 3、Pinia、Vitest、pnpm。

---

## File Structure

- Modify: `backend/ent/schema/user_subscription.go`
  - 增加 `plan_id` 字段、套餐 edge、索引。
- Modify: generated Ent files under `backend/ent/`
  - 运行 `go generate ./ent` 后更新 `usersubscription` 常量、create/update/query/mutation 等生成代码。
- Create: `backend/migrations/146_plan_scoped_user_subscriptions.sql`
  - 增加 `plan_id`，替换旧的 active 唯一索引为套餐权益和 legacy 权益两个部分唯一索引。
- Modify: `backend/internal/service/user_subscription.go`
  - 服务层模型增加 `PlanID *int64`、`PlanName string`、`Plan *SubscriptionPlan`。
- Modify: `backend/internal/service/user_subscription_port.go`
  - Repository 接口增加 plan-aware 查询和 active candidate list。
- Modify: `backend/internal/repository/user_subscription_repo.go`
  - `Create/Update/Get/List` 映射 `plan_id` 和 plan edge；新增按 `plan_id` 查询、按 user+group 列出活跃候选。
- Modify: `backend/internal/service/subscription_service.go`
  - `AssignOrExtendSubscription` 按 plan-aware 身份续期；新增 `SelectBillableSubscription`。
- Modify: `backend/internal/service/payment_fulfillment.go`
  - 支付履约把订单 `PlanID` 传入订阅分配。
- Modify: `backend/internal/service/redeem_service.go`
  - 套餐兑换码把 `SubscriptionPlanID` 传入订阅分配。
- Modify: `backend/internal/service/gateway_service.go`
  - 后扣命令携带 `group_id`，由 billing repo 返回真实扣费 `subscription_id` 后再写 usage log。
- Modify: `backend/internal/service/openai_gateway_service.go`
  - OpenAI 专用记录用量路径同样等待 billing repo 返回真实扣费 `subscription_id`。
- Modify: `backend/internal/service/usage_billing.go`
  - `UsageBillingCommand` 增加 `GroupID`，`UsageBillingApplyResult` 返回实际扣费 `SubscriptionID`。
- Modify: `backend/internal/repository/usage_billing_repo.go`
  - 在事务内行锁选择并扣减可承担成本的订阅权益，避免并发超扣。
- Modify: `backend/internal/service/billing_cache_service.go`
  - 请求前订阅模式不再依赖单条 `user+group` 订阅缓存做套餐额度裁决；保留余额、API key、RPM、platform quota 逻辑。
- Modify: `backend/internal/handler/dto/types.go`
  - 用户订阅 DTO 增加 `plan_id`、`plan_name`。
- Modify: `backend/internal/handler/dto/mappers.go`
  - 映射服务层 plan 字段到 DTO。
- Modify: `frontend/src/types/index.ts`
  - `UserSubscription` 增加 `plan_id`、`plan_name`。
- Modify: `frontend/src/components/payment/SubscriptionPlanCard.vue`
  - 续费判断由 `group_id` 改为 `plan_id`。
- Modify: `frontend/src/views/user/PaymentView.vue`
  - 当前订阅列表展示套餐名，允许同组多条订阅可读。
- Modify: `frontend/src/views/user/SubscriptionsView.vue`
  - 订阅列表展示套餐名。
- Modify: `frontend/src/views/admin/SubscriptionsView.vue`
  - 管理员订阅列表展示套餐身份。
- Test: backend unit tests in `backend/internal/service/*_test.go`
- Test: repository/migration integration tests in `backend/internal/repository/*_test.go`
- Test: frontend Vitest specs in `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts` and `frontend/src/views/user/__tests__/PaymentView.spec.ts`

---

### Task 1: Schema And Repository Identity

**Files:**
- Modify: `backend/ent/schema/user_subscription.go`
- Modify: generated files under `backend/ent/`
- Create: `backend/migrations/146_plan_scoped_user_subscriptions.sql`
- Modify: `backend/internal/service/user_subscription.go`
- Modify: `backend/internal/service/user_subscription_port.go`
- Modify: `backend/internal/repository/user_subscription_repo.go`
- Test: `backend/internal/repository/user_subscription_repo_integration_test.go`

- [ ] **Step 1: Write failing repository integration tests**

Add tests that insert two plans on one group, then create two active subscriptions for one user/group with different `plan_id` values. The test must verify both rows can coexist and legacy `plan_id IS NULL` still has only one active row.

```go
func TestUserSubscriptionRepository_AllowsPlanScopedSubscriptionsForSameGroup(t *testing.T) {
    client := newUserSubscriptionRepoIntegrationClient(t)
    ctx := context.Background()
    user := createIntegrationUser(t, client, "plan-scoped@example.com")
    group := createIntegrationSubscriptionGroup(t, client, "plan-scoped-group")
    planA := createIntegrationSubscriptionPlan(t, client, group.ID, "Plan A")
    planB := createIntegrationSubscriptionPlan(t, client, group.ID, "Plan B")
    repo := repository.NewUserSubscriptionRepository(client)

    require.NoError(t, repo.Create(ctx, &service.UserSubscription{
        UserID: user.ID, GroupID: group.ID, PlanID: &planA.ID,
        StartsAt: time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour),
        Status: service.SubscriptionStatusActive,
    }))
    require.NoError(t, repo.Create(ctx, &service.UserSubscription{
        UserID: user.ID, GroupID: group.ID, PlanID: &planB.ID,
        StartsAt: time.Now(), ExpiresAt: time.Now().Add(48 * time.Hour),
        Status: service.SubscriptionStatusActive,
    }))

    gotA, err := repo.GetByUserIDGroupIDAndPlanID(ctx, user.ID, group.ID, &planA.ID)
    require.NoError(t, err)
    require.Equal(t, planA.ID, *gotA.PlanID)
    require.Equal(t, "Plan A", gotA.PlanName)

    gotB, err := repo.GetByUserIDGroupIDAndPlanID(ctx, user.ID, group.ID, &planB.ID)
    require.NoError(t, err)
    require.Equal(t, planB.ID, *gotB.PlanID)

    candidates, err := repo.ListActiveByUserIDAndGroupID(ctx, user.ID, group.ID)
    require.NoError(t, err)
    require.Len(t, candidates, 2)
    require.Equal(t, gotA.ID, candidates[0].ID)
    require.Equal(t, gotB.ID, candidates[1].ID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```powershell
cd backend
go test ./internal/repository -run TestUserSubscriptionRepository_AllowsPlanScopedSubscriptionsForSameGroup -count=1
```

Expected: FAIL because `plan_id` and plan-aware repo methods do not exist yet.

- [ ] **Step 3: Add migration**

Create `backend/migrations/146_plan_scoped_user_subscriptions.sql`:

```sql
-- 146_plan_scoped_user_subscriptions.sql
-- Make plan-backed subscriptions independent while preserving legacy group subscriptions.

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_plan_id
    ON user_subscriptions(plan_id);

ALTER TABLE user_subscriptions DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS usersubscription_user_id_group_id;
DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_plan_unique_active
    ON user_subscriptions(user_id, group_id, plan_id)
    WHERE deleted_at IS NULL AND plan_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_legacy_unique_active
    ON user_subscriptions(user_id, group_id)
    WHERE deleted_at IS NULL AND plan_id IS NULL;
```

- [ ] **Step 4: Update Ent schema and generate code**

In `backend/ent/schema/user_subscription.go`, add:

```go
field.Int64("plan_id").
    Optional().
    Nillable(),
```

Add edge:

```go
edge.From("plan", SubscriptionPlan.Type).
    Ref("user_subscriptions").
    Field("plan_id").
    Unique(),
```

Add index:

```go
index.Fields("plan_id"),
index.Fields("user_id", "group_id", "plan_id"),
```

In `backend/ent/schema/subscription_plan.go`, add reverse edge:

```go
edge.To("user_subscriptions", UserSubscription.Type),
```

Run:

```powershell
cd backend
go generate ./ent
```

Expected: generated Ent code includes `FieldPlanID`, `PlanID`, `SetNillablePlanID`, `WithPlan`, and plan predicates.

- [ ] **Step 5: Update service model and repository port**

Add fields to `backend/internal/service/user_subscription.go`:

```go
PlanID   *int64
PlanName string
Plan     *SubscriptionPlan
```

Add methods to `backend/internal/service/user_subscription_port.go`:

```go
GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*UserSubscription, error)
ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]UserSubscription, error)
ExistsByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (bool, error)
```

- [ ] **Step 6: Implement repository methods and mapping**

In `Create`, set:

```go
SetNillablePlanID(sub.PlanID)
```

In `Update`, set or clear plan:

```go
if sub.PlanID == nil {
    builder.ClearPlanID()
} else {
    builder.SetPlanID(*sub.PlanID)
}
```

Load plan in `GetByID`, `ListByUserID`, `ListActiveByUserID`, `ListByGroupID`, and `List`.

Implement helper predicate:

```go
func userSubscriptionPlanPredicate(planID *int64) predicate.UserSubscription {
    if planID == nil {
        return usersubscription.PlanIDIsNil()
    }
    return usersubscription.PlanIDEQ(*planID)
}
```

Implement plan-aware lookup:

```go
func (r *userSubscriptionRepository) GetByUserIDGroupIDAndPlanID(ctx context.Context, userID, groupID int64, planID *int64) (*service.UserSubscription, error) {
    client := clientFromContext(ctx, r.client)
    m, err := client.UserSubscription.Query().
        Where(
            usersubscription.UserIDEQ(userID),
            usersubscription.GroupIDEQ(groupID),
            userSubscriptionPlanPredicate(planID),
        ).
        WithGroup().
        WithPlan().
        Only(ctx)
    if err != nil {
        return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
    }
    return userSubscriptionEntityToService(m), nil
}
```

Implement active candidate list ordered by expiry:

```go
func (r *userSubscriptionRepository) ListActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) ([]service.UserSubscription, error) {
    client := clientFromContext(ctx, r.client)
    subs, err := client.UserSubscription.Query().
        Where(
            usersubscription.UserIDEQ(userID),
            usersubscription.GroupIDEQ(groupID),
            usersubscription.StatusEQ(service.SubscriptionStatusActive),
            usersubscription.ExpiresAtGT(time.Now()),
        ).
        WithGroup().
        WithPlan().
        Order(dbent.Asc(usersubscription.FieldExpiresAt), dbent.Asc(usersubscription.FieldID)).
        All(ctx)
    if err != nil {
        return nil, err
    }
    return userSubscriptionEntitiesToService(subs), nil
}
```

- [ ] **Step 7: Run repository test**

Run:

```powershell
cd backend
go test ./internal/repository -run TestUserSubscriptionRepository_AllowsPlanScopedSubscriptionsForSameGroup -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit task**

```powershell
git add backend/ent backend/migrations/146_plan_scoped_user_subscriptions.sql backend/internal/service/user_subscription.go backend/internal/service/user_subscription_port.go backend/internal/repository/user_subscription_repo.go backend/internal/repository/user_subscription_repo_integration_test.go
git commit -m "feat: add plan-scoped subscription identity"
```

---

### Task 2: Plan-Aware Assignment, Payment, And Redeem

**Files:**
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/payment_fulfillment.go`
- Modify: `backend/internal/service/redeem_service.go`
- Test: `backend/internal/service/subscription_rolling_quota_test.go`
- Test: `backend/internal/service/redeem_service_plan_snapshot_test.go`
- Test: payment fulfillment tests if existing stubs cover them

- [ ] **Step 1: Write failing service tests**

Add tests:

```go
func TestAssignOrExtendSubscription_DifferentPlansSameGroupCreateIndependentSubscriptions(t *testing.T) {
    planA := int64(101)
    planB := int64(102)
    groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}}
    subRepo := newSubscriptionUserSubRepoStub()
    svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

    first, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
        UserID: 10, GroupID: 2, PlanID: &planA, ValidityDays: 30,
        HasRollingQuotaSnapshot: true,
    })
    require.NoError(t, err)
    require.False(t, reused)

    second, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
        UserID: 10, GroupID: 2, PlanID: &planB, ValidityDays: 30,
        HasRollingQuotaSnapshot: true,
    })
    require.NoError(t, err)
    require.False(t, reused)
    require.NotEqual(t, first.ID, second.ID)
}

func TestAssignOrExtendSubscription_SamePlanRenewsSameSubscription(t *testing.T) {
    planID := int64(101)
    groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}}
    subRepo := newSubscriptionUserSubRepoStub()
    svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

    first, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
        UserID: 10, GroupID: 2, PlanID: &planID, ValidityDays: 30,
        HasRollingQuotaSnapshot: true,
    })
    require.NoError(t, err)
    require.False(t, reused)

    second, reused, err := svc.AssignOrExtendSubscription(context.Background(), &AssignSubscriptionInput{
        UserID: 10, GroupID: 2, PlanID: &planID, ValidityDays: 7,
        HasRollingQuotaSnapshot: true,
    })
    require.NoError(t, err)
    require.True(t, reused)
    require.Equal(t, first.ID, second.ID)
}
```

Update redeem test to assert `AssignSubscriptionInput.PlanID` equals code `SubscriptionPlanID`.

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
cd backend
go test -tags unit ./internal/service -run "TestAssignOrExtendSubscription_(DifferentPlansSameGroupCreateIndependentSubscriptions|SamePlanRenewsSameSubscription)|TestRedeemService_RedeemSubscriptionPlanSnapshotAppliesRollingLimits" -count=1
```

Expected: FAIL because `AssignSubscriptionInput.PlanID` is not present and stubs are keyed only by group.

- [ ] **Step 3: Add plan id to assignment input**

In `AssignSubscriptionInput` add:

```go
PlanID *int64
```

In `createSubscription`, set:

```go
PlanID: input.PlanID,
```

- [ ] **Step 4: Make assignment lookup plan-aware**

Replace `GetByUserIDAndGroupID` in `AssignOrExtendSubscription`, conflict fallback, and `assignSubscriptionWithReuse` with:

```go
existingSub, err := s.userSubRepo.GetByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
```

Replace `ExistsByUserIDAndGroupID` with:

```go
exists, err := s.userSubRepo.ExistsByUserIDGroupIDAndPlanID(ctx, input.UserID, input.GroupID, input.PlanID)
```

Legacy/manual assignments pass `PlanID: nil`, so they still reuse only the legacy `plan_id IS NULL` entitlement.

- [ ] **Step 5: Pass plan id from payment fulfillment**

In subscription order fulfillment, when building `AssignSubscriptionInput`, add:

```go
PlanID: order.PlanID,
```

The order already carries quota snapshots, so do not query live plan for quota values.

- [ ] **Step 6: Pass plan id from redeem**

In `RedeemService.Redeem` subscription branch, add:

```go
PlanID: redeemCode.SubscriptionPlanID,
```

- [ ] **Step 7: Update test stubs to key by plan-aware identity**

In `subscriptionUserSubRepoStub`, replace `byUserGroup` with a key including nullable plan:

```go
func (s *subscriptionUserSubRepoStub) key(userID, groupID int64, planID *int64) string {
    plan := "legacy"
    if planID != nil {
        plan = strconv.FormatInt(*planID, 10)
    }
    return strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(groupID, 10) + ":" + plan
}
```

Use this key in `seed`, `Create`, `GetByUserIDGroupIDAndPlanID`, and `ExistsByUserIDGroupIDAndPlanID`. Keep old `GetByUserIDAndGroupID` for legacy/no-plan callers by delegating to `nil`.

- [ ] **Step 8: Run service tests**

Run:

```powershell
cd backend
go test -tags unit ./internal/service -run "TestAssignOrExtendSubscription_|TestRedeemService_RedeemSubscriptionPlanSnapshotAppliesRollingLimits" -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit task**

```powershell
git add backend/internal/service/subscription_service.go backend/internal/service/payment_fulfillment.go backend/internal/service/redeem_service.go backend/internal/service/*subscription*_test.go backend/internal/service/redeem_service_plan_snapshot_test.go
git commit -m "feat: fulfill subscriptions per plan"
```

---

### Task 3: Transactional Billable Subscription Selection

**Files:**
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/billing_cache_service.go`
- Modify: `backend/internal/service/usage_billing.go`
- Modify: `backend/internal/repository/usage_billing_repo.go`
- Test: `backend/internal/service/subscription_rolling_quota_test.go`
- Test: `backend/internal/service/gateway_service_subscription_billing_test.go`
- Test: `backend/internal/repository/usage_billing_repo_integration_test.go`
- Test: `backend/internal/service/openai_gateway_record_usage_test.go`

- [ ] **Step 1: Write failing selection tests**

Add tests:

```go
func TestSelectBillableSubscription_SkipsEarliestWhenQuotaCannotCoverCost(t *testing.T) {
    now := time.Now()
    low := 1.0
    high := 20.0
    groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}}
    subRepo := newSubscriptionUserSubRepoStub()
    subRepo.seed(&UserSubscription{
        ID: 1, UserID: 10, GroupID: 2, Status: SubscriptionStatusActive,
        StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
        FiveHourLimitUSD: &low, FiveHourUsageUSD: 0.5,
    })
    subRepo.seed(&UserSubscription{
        ID: 2, UserID: 10, GroupID: 2, Status: SubscriptionStatusActive,
        StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(48 * time.Hour),
        FiveHourLimitUSD: &high, FiveHourUsageUSD: 0,
    })
    svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

    selected, err := svc.SelectBillableSubscription(context.Background(), 10, 2, 2)

    require.NoError(t, err)
    require.Equal(t, int64(2), selected.ID)
}

func TestSelectBillableSubscription_UsesEarliestWhenItCanCoverCost(t *testing.T) {
    now := time.Now()
    limit := 5.0
    groupRepo := &subscriptionGroupRepoStub{group: &Group{ID: 2, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive}}
    subRepo := newSubscriptionUserSubRepoStub()
    subRepo.seed(&UserSubscription{
        ID: 1, UserID: 10, GroupID: 2, Status: SubscriptionStatusActive,
        StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour),
        FiveHourLimitUSD: &limit, FiveHourUsageUSD: 1,
    })
    subRepo.seed(&UserSubscription{
        ID: 2, UserID: 10, GroupID: 2, Status: SubscriptionStatusActive,
        StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(48 * time.Hour),
        FiveHourLimitUSD: &limit, FiveHourUsageUSD: 0,
    })
    svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

    selected, err := svc.SelectBillableSubscription(context.Background(), 10, 2, 1)

    require.NoError(t, err)
    require.Equal(t, int64(1), selected.ID)
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```powershell
cd backend
go test -tags unit ./internal/service -run "TestSelectBillableSubscription_" -count=1
```

Expected: FAIL because `SelectBillableSubscription` does not exist.

- [ ] **Step 3: Add command/result fields for transactional selection**

In `backend/internal/service/usage_billing.go`, add:

```go
GroupID *int64
```

to `UsageBillingCommand`, and add:

```go
SubscriptionID *int64
```

to `UsageBillingApplyResult`.

For automatic plan-scoped selection, `SubscriptionID` in the command may be nil while `GroupID` is set. The repository then chooses and returns the actual subscription id.

- [ ] **Step 4: Implement transactional select-and-increment in repository**

In `backend/internal/repository/usage_billing_repo.go`, when `cmd.SubscriptionCost > 0` and `cmd.GroupID != nil`, select one candidate inside the existing `Apply` transaction:

```sql
SELECT us.id
FROM user_subscriptions us
WHERE us.user_id = $1
  AND us.group_id = $2
  AND us.status = 'active'
  AND us.expires_at > NOW()
  AND us.deleted_at IS NULL
  AND (
    us.five_hour_limit_usd IS NULL
    OR (
      us.five_hour_limit_usd > 0
      AND CASE
        WHEN us.five_hour_window_start IS NULL
          OR us.five_hour_window_start + INTERVAL '5 hours' <= NOW()
        THEN 0
        ELSE us.five_hour_usage_usd
      END + $3 <= us.five_hour_limit_usd
    )
  )
  AND (
    us.seven_day_limit_usd IS NULL
    OR (
      us.seven_day_limit_usd > 0
      AND CASE
        WHEN us.seven_day_window_start IS NULL
          OR us.seven_day_window_start + INTERVAL '7 days' <= NOW()
        THEN 0
        ELSE us.seven_day_usage_usd
      END + $3 <= us.seven_day_limit_usd
    )
  )
  AND (
    us.thirty_day_limit_usd IS NULL
    OR (
      us.thirty_day_limit_usd > 0
      AND CASE
        WHEN us.thirty_day_window_start IS NULL
          OR us.thirty_day_window_start + INTERVAL '30 days' <= NOW()
        THEN 0
        ELSE us.thirty_day_usage_usd
      END + $3 <= us.thirty_day_limit_usd
    )
  )
ORDER BY us.expires_at ASC, us.id ASC
FOR UPDATE
LIMIT 1
```

Then call the existing increment SQL for that selected id and set `result.SubscriptionID`.

- [ ] **Step 5: Keep optional service selector for tests/admin tools**

Add to `SubscriptionService`:

```go
func (s *SubscriptionService) SelectBillableSubscription(ctx context.Context, userID, groupID int64, costUSD float64) (*UserSubscription, error) {
    subs, err := s.userSubRepo.ListActiveByUserIDAndGroupID(ctx, userID, groupID)
    if err != nil {
        return nil, err
    }
    if len(subs) == 0 {
        return nil, ErrSubscriptionNotFound
    }

    var firstErr error
    for i := range subs {
        sub := &subs[i]
        NormalizeSubscriptionWindowsForDisplay(sub)
        if err := s.ValidateSubscription(ctx, sub); err != nil {
            if firstErr == nil {
                firstErr = err
            }
            continue
        }
        if err := s.CheckUsageLimits(ctx, sub, sub.Group, costUSD); err != nil {
            if firstErr == nil {
                firstErr = err
            }
            continue
        }
        return sub, nil
    }
    if firstErr != nil {
        return nil, firstErr
    }
    return nil, ErrSubscriptionNotFound
}
```

Use candidate list ordering from repository; do not split one request across subscriptions.

- [ ] **Step 6: Update request post-billing path**

In `backend/internal/service/gateway_service.go`, when creating `UsageBillingCommand` for subscription mode, set `GroupID` from `p.APIKey.GroupID` and do not depend on middleware's preloaded subscription as the final deduction target:

```go
if p.APIKey.GroupID != nil {
    cmd.GroupID = p.APIKey.GroupID
}
```

After `repo.Apply` returns, set the in-memory usage log before writing it:

```go
if result.SubscriptionID != nil {
    usageLog.SubscriptionID = result.SubscriptionID
}
```

Do the same in `OpenAIGatewayService.RecordUsage`. For zero-cost subscription requests, skip quota selection and leave `SubscriptionID` empty or use the preflight id only as a display hint.

- [ ] **Step 7: Keep preflight lightweight**

In `BillingCacheService.CheckBillingEligibility`, for subscription mode:

- Keep balance bypass.
- Keep API key rate limits and RPM checks.
- Replace single-subscription cache quota decision with validation of passed subscription or a DB existence check. Do not use `GetSubscriptionStatus` as an authoritative plan quota checker because it only stores one subscription per `user+group`.

Minimum safe behavior:

```go
if isSubscriptionMode {
    if subscription == nil || subscription.Status != SubscriptionStatusActive || subscription.IsExpired() {
        return ErrSubscriptionInvalid
    }
}
```

The real cost-aware quota decision is now Task 3 Step 4.

- [ ] **Step 8: Stop writing stale single-subscription usage cache for plan selection**

In `finalizePostUsageBilling`, avoid `UpdateSubscriptionUsage(ctx, userID, groupID, cost)` for subscription mode. Replace it with `InvalidateSubscription(ctx, userID, groupID)`. This keeps old cache from approving a future request based on the wrong plan entitlement.

- [ ] **Step 9: Run targeted service tests**

Run:

```powershell
cd backend
go test -tags unit ./internal/service -run "TestSelectBillableSubscription_|TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier|TestRecordUsage" -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit task**

```powershell
git add backend/internal/service/subscription_service.go backend/internal/service/gateway_service.go backend/internal/service/openai_gateway_service.go backend/internal/service/billing_cache_service.go backend/internal/service/usage_billing.go backend/internal/repository/usage_billing_repo.go backend/internal/service/*billing*_test.go backend/internal/service/*usage*_test.go backend/internal/repository/usage_billing_repo_integration_test.go
git commit -m "feat: select subscription entitlement for billing"
```

---

### Task 4: API DTO And Frontend Display

**Files:**
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/payment/SubscriptionPlanCard.vue`
- Modify: `frontend/src/views/user/PaymentView.vue`
- Modify: `frontend/src/views/user/SubscriptionsView.vue`
- Modify: `frontend/src/views/admin/SubscriptionsView.vue`
- Test: `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts`
- Test: `frontend/src/views/user/__tests__/PaymentView.spec.ts`

- [ ] **Step 1: Write failing frontend tests**

In `SubscriptionPlanCard.spec.ts`, add:

```ts
it("shows renewal only for the same plan id", () => {
  const wrapper = mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 2,
        group_id: 10,
        group_platform: "openai",
        name: "Plan B",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: [],
        is_active: true,
      },
      activeSubscriptions: [
        { id: 1, user_id: 1, group_id: 10, plan_id: 1, status: "active" },
      ],
    },
    global: { plugins: [i18n] },
  })

  expect(wrapper.text()).toContain("Subscribe now")
  expect(wrapper.text()).not.toContain("payment.renewNow")
})
```

Add the inverse case where `plan_id: 2` displays renewal.

- [ ] **Step 2: Run frontend tests to verify failure**

Run:

```powershell
corepack pnpm --dir frontend exec vitest run src/components/payment/__tests__/SubscriptionPlanCard.spec.ts
```

Expected: FAIL because `isRenewal` still checks `group_id`.

- [ ] **Step 3: Add DTO fields**

In backend DTO:

```go
PlanID   *int64  `json:"plan_id,omitempty"`
PlanName string  `json:"plan_name,omitempty"`
```

Map from service:

```go
PlanID: sub.PlanID,
PlanName: sub.PlanName,
```

- [ ] **Step 4: Add frontend type fields**

In `UserSubscription`:

```ts
plan_id?: number | null
plan_name?: string
```

- [ ] **Step 5: Update renewal logic**

In `SubscriptionPlanCard.vue`, replace group-based `isRenewal` with:

```ts
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(
    (s) => s.plan_id === props.plan.id && s.status === 'active',
  ) ?? false,
)
```

Subscriptions with `plan_id == null` must not mark every same-group plan as renewal.

- [ ] **Step 6: Display plan identity**

In user/admin subscription list rows, show `sub.plan_name` when present. Use fallback text such as existing group name when `plan_name` is empty.

```vue
<span v-if="sub.plan_name" class="text-xs text-gray-500">{{ sub.plan_name }}</span>
```

- [ ] **Step 7: Run frontend tests**

Run:

```powershell
corepack pnpm --dir frontend exec vitest run src/components/payment/__tests__/SubscriptionPlanCard.spec.ts src/views/user/__tests__/PaymentView.spec.ts
corepack pnpm --dir frontend run typecheck
```

Expected: PASS.

- [ ] **Step 8: Commit task**

```powershell
git add backend/internal/handler/dto/types.go backend/internal/handler/dto/mappers.go frontend/src/types/index.ts frontend/src/components/payment/SubscriptionPlanCard.vue frontend/src/views/user/PaymentView.vue frontend/src/views/user/SubscriptionsView.vue frontend/src/views/admin/SubscriptionsView.vue frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts frontend/src/views/user/__tests__/PaymentView.spec.ts
git commit -m "feat: show subscriptions per plan"
```

---

### Task 5: Full Verification And Review

**Files:**
- All files changed by Tasks 1-4.

- [ ] **Step 1: Run backend targeted tests**

```powershell
cd backend
go test -tags unit ./internal/service ./internal/server ./internal/server/middleware
go test ./internal/repository -run "UserSubscription|UsageBilling|Redeem" -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests if time permits**

```powershell
cd backend
go test ./...
```

Expected: PASS or document unrelated existing failures.

- [ ] **Step 3: Run frontend verification**

```powershell
corepack pnpm --dir frontend exec vitest run src/components/payment/__tests__/SubscriptionPlanCard.spec.ts src/views/user/__tests__/PaymentView.spec.ts
corepack pnpm --dir frontend run typecheck
corepack pnpm --dir frontend run build
```

Expected: PASS.

- [ ] **Step 4: Diff hygiene**

```powershell
git diff --check
git status --short
```

Expected: no whitespace errors. Dirty legal-doc files may remain uncommitted and must not be included in this feature commit unless explicitly requested.

- [ ] **Step 5: Final review**

Review requirements against `docs/superpowers/specs/2026-05-28-plan-scoped-subscriptions-design.md`:

- Same plan renews the same entitlement.
- Different plans under the same group create independent entitlements.
- Legacy `plan_id IS NULL` subscription remains compatible.
- API usage selects one earliest-expiring affordable entitlement.
- No cross-plan split billing.
- User UI renewal button is plan-based.
- Admin/user lists expose plan identity.

- [ ] **Step 6: Final commit**

If Tasks 1-4 were not committed independently, commit the complete implementation with:

```powershell
git add backend frontend docs/superpowers/plans/2026-05-28-plan-scoped-subscriptions.md
git commit -m "feat: support plan-scoped subscriptions"
```

Do not stage `docs/legal/` or unrelated `.gitignore` legal-doc changes unless the user asks.

---

## Deployment Notes

Do not deploy from Codex automatically because this Codex session may depend on the currently running Sub2API service.

When the user is ready, provide a Termius/tmux deployment guide that:

1. backs up PostgreSQL,
2. builds Go + frontend assets on the server,
3. stops service only after the new binary is ready,
4. starts the new binary and lets migration `146` run,
5. verifies `/api/v1/payment/checkout-info`, purchase page, subscription list, redeem, and one real API request,
6. keeps the old binary and DB backup for rollback.

Rollback warning: after users create multiple plan-backed entitlements under one group, old code cannot safely assume one active `user_id + group_id` subscription. Roll back binary directly only before any multi-plan entitlement is created; otherwise consolidate/soft-delete duplicate entitlements first.
