//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayRecordUsageAntigravityReasoningAggregation(t *testing.T) {
	for _, source := range []string{BillingModelSourceUpstream, BillingModelSourceRequested, BillingModelSourceChannelMapped} {
		for _, effort := range []string{"high", "medium", "low"} {
			t.Run(source+"/"+effort, func(t *testing.T) {
				usageRepo := &openAIRecordUsageLogRepoStub{}
				svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				wire := "gemini-3.8-flash-" + effort
				response := "gemini-3.8-flash"
				err := svc.RecordUsage(context.Background(), &RecordUsageInput{
					Result: &ForwardResult{
						RequestID: "aggregation-billing", Model: wire, UpstreamModel: wire, UpstreamResponseModel: response,
						ReasoningEffort: &effort, Usage: ClaudeUsage{InputTokens: 1000000, OutputTokens: 1000000, CacheReadInputTokens: 1000000},
					},
					APIKey: &APIKey{ID: 501}, User: &User{ID: 601}, Account: &Account{ID: 701, Platform: PlatformAntigravity, Type: AccountTypeOAuth},
					ChannelUsageFields: ChannelUsageFields{OriginalModel: wire, ChannelMappedModel: response, BillingModelSource: source, ModelMappingChain: response + "→" + wire},
				})
				require.NoError(t, err)
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.Equal(t, response, log.Model)
				require.Equal(t, response, log.RequestedModel)
				require.Equal(t, wire, *log.UpstreamModel)
				require.Equal(t, effort, *log.ReasoningEffort)
				require.Equal(t, effort, *log.RequestedReasoningEffort)
				require.Nil(t, log.ModelMappingChain)
				require.False(t, *log.UpstreamModelMismatch)
				require.InDelta(t, 0.75, log.InputCost, 1e-9)
				require.InDelta(t, 3.75, log.OutputCost, 1e-9)
				require.InDelta(t, 0.075, log.CacheReadCost, 1e-9)
			})
		}
	}
}
