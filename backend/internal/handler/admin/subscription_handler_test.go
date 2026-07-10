package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionHandlerAssignAcceptsSubscriptionPlanID(t *testing.T) {
	ctx := context.Background()
	five := 5.0
	seven := 70.0
	thirty := 300.0
	fixture := newSubscriptionHandlerPlanFixture(t, five, seven, thirty)
	client := fixture.client
	groupEnt := fixture.group
	plan := fixture.plan
	targetUser, err := client.User.Create().
		SetEmail("target@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		Save(ctx)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]any{
		"user_id":              targetUser.ID,
		"subscription_plan_id": plan.ID,
		"notes":                "manual admin plan grant",
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/assign", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: fixture.adminUser.ID})

	fixture.handler.Assign(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	sub, err := client.UserSubscription.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, targetUser.ID, sub.UserID)
	require.Equal(t, groupEnt.ID, sub.GroupID)
	require.NotNil(t, sub.PlanID)
	require.Equal(t, plan.ID, *sub.PlanID)
	require.NotNil(t, sub.FiveHourLimitUsd)
	require.Equal(t, five, *sub.FiveHourLimitUsd)
	require.NotNil(t, sub.SevenDayLimitUsd)
	require.Equal(t, seven, *sub.SevenDayLimitUsd)
	require.NotNil(t, sub.ThirtyDayLimitUsd)
	require.Equal(t, thirty, *sub.ThirtyDayLimitUsd)
}

func TestSubscriptionHandlerBulkAssignAcceptsSubscriptionPlanID(t *testing.T) {
	ctx := context.Background()
	five := 5.0
	seven := 70.0
	thirty := 300.0
	fixture := newSubscriptionHandlerPlanFixture(t, five, seven, thirty)
	targetA, err := fixture.client.User.Create().
		SetEmail("bulk-a@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		Save(ctx)
	require.NoError(t, err)
	targetB, err := fixture.client.User.Create().
		SetEmail("bulk-b@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		Save(ctx)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]any{
		"user_ids":             []int64{targetA.ID, targetB.ID},
		"subscription_plan_id": fixture.plan.ID,
		"notes":                "manual bulk plan grant",
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/bulk-assign", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: fixture.adminUser.ID})

	fixture.handler.BulkAssign(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	subs, err := fixture.client.UserSubscription.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, subs, 2)
	byUser := make(map[int64]*dbent.UserSubscription, len(subs))
	for _, sub := range subs {
		byUser[sub.UserID] = sub
	}
	for _, userID := range []int64{targetA.ID, targetB.ID} {
		sub := byUser[userID]
		require.NotNil(t, sub)
		require.Equal(t, fixture.group.ID, sub.GroupID)
		require.NotNil(t, sub.PlanID)
		require.Equal(t, fixture.plan.ID, *sub.PlanID)
		require.NotNil(t, sub.FiveHourLimitUsd)
		require.Equal(t, five, *sub.FiveHourLimitUsd)
		require.NotNil(t, sub.SevenDayLimitUsd)
		require.Equal(t, seven, *sub.SevenDayLimitUsd)
		require.NotNil(t, sub.ThirtyDayLimitUsd)
		require.Equal(t, thirty, *sub.ThirtyDayLimitUsd)
	}
}

func TestSubscriptionHandlerAssignRejectsLegacyGroupPayload(t *testing.T) {
	fixture := newSubscriptionHandlerPlanFixture(t, 5.0, 70.0, 300.0)
	targetUser, err := fixture.client.User.Create().
		SetEmail("legacy-target@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		Save(context.Background())
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]any{
		"user_id":       targetUser.ID,
		"group_id":      fixture.group.ID,
		"validity_days": 7,
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/assign", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: fixture.adminUser.ID})

	fixture.handler.Assign(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	count, err := fixture.client.UserSubscription.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

func TestSubscriptionHandlerBulkAssignRejectsLegacyGroupPayload(t *testing.T) {
	fixture := newSubscriptionHandlerPlanFixture(t, 5.0, 70.0, 300.0)
	targetUser, err := fixture.client.User.Create().
		SetEmail("legacy-bulk-target@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleUser).
		Save(context.Background())
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]any{
		"user_ids":      []int64{targetUser.ID},
		"group_id":      fixture.group.ID,
		"validity_days": 7,
	})
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/subscriptions/bulk-assign", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: fixture.adminUser.ID})

	fixture.handler.BulkAssign(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	count, err := fixture.client.UserSubscription.Query().Count(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, count)
}

type subscriptionHandlerPlanFixture struct {
	client    *dbent.Client
	group     *dbent.Group
	plan      *dbent.SubscriptionPlan
	adminUser *dbent.User
	handler   *SubscriptionHandler
}

func newSubscriptionHandlerPlanFixture(t *testing.T, five, seven, thirty float64) subscriptionHandlerPlanFixture {
	t.Helper()

	ctx := context.Background()
	client, db := newAdminHandlerTestClient(t)
	groupEnt, err := client.Group.Create().
		SetName("Codex Plus").
		SetDescription("Codex Plus group").
		SetPlatform(domain.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetStatus(service.StatusActive).
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)

	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(groupEnt.ID).
		SetName("Codex Plus 7d").
		SetDescription("7 day plan").
		SetPrice(19.9).
		SetValidityDays(1).
		SetValidityUnit("week").
		SetFeatures("[]").
		SetProductName("Codex Plus").
		SetForSale(true).
		SetSortOrder(1).
		SetFiveHourLimitUsd(five).
		SetSevenDayLimitUsd(seven).
		SetThirtyDayLimitUsd(thirty).
		Save(ctx)
	require.NoError(t, err)
	adminUser, err := client.User.Create().
		SetEmail("admin@example.com").
		SetPasswordHash("hash").
		SetRole(domain.RoleAdmin).
		Save(ctx)
	require.NoError(t, err)

	groupRepo := repository.NewGroupRepository(client, db)
	subRepo := repository.NewUserSubscriptionRepository(client)
	subSvc := service.NewSubscriptionService(groupRepo, subRepo, nil, client, nil)
	return subscriptionHandlerPlanFixture{
		client:    client,
		group:     groupEnt,
		plan:      plan,
		adminUser: adminUser,
		handler:   NewSubscriptionHandler(subSvc),
	}
}
