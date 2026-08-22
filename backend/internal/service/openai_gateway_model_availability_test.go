package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIDiagnoseModelAvailability_EmptyMappingRejectsDisplayName(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}}}}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "GPT-5.6 Luna", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport)
}

func TestOpenAIDiagnoseModelAvailability_EmptyMappingKeepsValidCustomID(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}}}}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "provider/custom-model-v2", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}

func TestOpenAIDiagnoseModelAvailability_ExplicitWhitespaceAliasRemainsSupported(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"Team Model": "provider/custom-model-v2"},
		},
	}}}}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "Team Model", PlatformOpenAI)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport)
}
