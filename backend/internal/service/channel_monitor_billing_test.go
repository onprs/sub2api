package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalChannelMonitorSkipsBillingAndUsagePersistence(t *testing.T) {
	apiKey := &APIKey{InternalChannelMonitor: true}

	billing := &BillingCacheService{}
	require.NoError(t, billing.CheckBillingEligibility(
		context.Background(), nil, apiKey, nil, nil, PlatformOpenAI,
	))
	require.NoError(t, billing.CheckBillingEligibilityStrict(
		context.Background(), nil, apiKey, nil, nil, PlatformOpenAI,
	))

	gateway := &GatewayService{}
	require.NoError(t, gateway.RecordUsage(context.Background(), &RecordUsageInput{APIKey: apiKey}))
	require.NoError(t, gateway.RecordUsageWithLongContext(context.Background(), &RecordUsageLongContextInput{APIKey: apiKey}))

	openAIGateway := &OpenAIGatewayService{}
	require.NoError(t, openAIGateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		APIKey: apiKey,
		Result: &OpenAIForwardResult{},
	}))
}
