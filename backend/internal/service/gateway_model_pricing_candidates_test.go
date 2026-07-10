package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAvailableModelPricingCandidates_UsesAccountMappingTargets(t *testing.T) {
	groupID := int64(8)
	repo := &modelsListAccountRepoStub{
		byGroup: map[int64][]Account{
			groupID: {
				{
					ID:       101,
					Platform: PlatformAntigravity,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"gemini-3.5-flash-low": "gemini-3.1-pro-low",
						},
					},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	got := svc.GetAvailableModelPricingCandidates(
		context.Background(),
		&groupID,
		PlatformAntigravity,
		[]string{"gemini-3.5-flash-low"},
	)

	require.Equal(t, []string{"gemini-3.1-pro-low", "gemini-3.5-flash-low"}, got["gemini-3.5-flash-low"])
	require.Equal(t, int64(1), repo.listByGroupCalls.Load())
}

func TestGetAvailableModelPricingCandidates_FallsBackToDisplayModel(t *testing.T) {
	groupID := int64(10)
	repo := &modelsListAccountRepoStub{
		byGroup: map[int64][]Account{
			groupID: {
				{
					ID:       201,
					Platform: PlatformOpenCodeGo,
					Credentials: map[string]any{
						"model_mapping": map[string]any{
							"kimi-k2.7-code": "kimi-k2.7-code",
						},
					},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	got := svc.GetAvailableModelPricingCandidates(
		context.Background(),
		&groupID,
		PlatformOpenCodeGo,
		[]string{"kimi-k2.7-code"},
	)

	require.Equal(t, []string{"kimi-k2.7-code"}, got["kimi-k2.7-code"])
	require.Equal(t, int64(1), repo.listByGroupCalls.Load())
}
