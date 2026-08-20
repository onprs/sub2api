package middleware

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func dynamicAuthTestGroup(id int64, status string, exclusive bool) *service.Group {
	return &service.Group{
		ID:               id,
		Platform:         service.PlatformOpenAI,
		Status:           status,
		IsExclusive:      exclusive,
		SubscriptionType: service.SubscriptionTypeStandard,
	}
}

func TestValidateAPIKeyGroupAvailable_DynamicRoutingAcceptsAnyActiveCandidate(t *testing.T) {
	disabled := dynamicAuthTestGroup(1, service.StatusDisabled, false)
	active := dynamicAuthTestGroup(2, service.StatusActive, false)
	primaryID := disabled.ID
	key := &service.APIKey{
		GroupID:         &primaryID,
		Group:           disabled,
		RoutingPlatform: service.PlatformOpenAI,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: disabled.ID, Priority: 0, Group: disabled},
			{GroupID: active.ID, Priority: 1, Group: active},
		},
	}

	code, message, ok := validateAPIKeyGroupAvailable(key)
	require.True(t, ok)
	require.Empty(t, code)
	require.Empty(t, message)

	active.Status = service.StatusDisabled
	code, message, ok = validateAPIKeyGroupAvailable(key)
	require.False(t, ok)
	require.Equal(t, "GROUP_UNAVAILABLE", code)
	require.NotEmpty(t, message)
}

func TestValidateAPIKeyGroupAllowed_DynamicRoutingAcceptsAnyAuthorizedCandidate(t *testing.T) {
	unauthorized := dynamicAuthTestGroup(11, service.StatusActive, true)
	authorized := dynamicAuthTestGroup(12, service.StatusActive, true)
	primaryID := unauthorized.ID
	key := &service.APIKey{
		User:            &service.User{ID: 20, AllowedGroups: []int64{authorized.ID}},
		GroupID:         &primaryID,
		Group:           unauthorized,
		RoutingPlatform: service.PlatformOpenAI,
		RoutingGroups: []service.APIKeyGroupBinding{
			{GroupID: unauthorized.ID, Priority: 0, Group: unauthorized},
			{GroupID: authorized.ID, Priority: 1, Group: authorized},
		},
	}

	require.True(t, validateAPIKeyGroupAllowed(key))
	key.User.AllowedGroups = nil
	require.False(t, validateAPIKeyGroupAllowed(key))

	// 订阅权限的实时额度由模型已知后的路由解析器检查；认证阶段不能因主候选无权限提前拒绝。
	authorized.SubscriptionType = service.SubscriptionTypeSubscription
	require.True(t, validateAPIKeyGroupAllowed(key))
}
