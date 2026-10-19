//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorLocalAntigravityProtocolsShareGroup(t *testing.T) {
	groupID := int64(81)
	group := activeMonitorTargetGroup(groupID, "Antigravity", PlatformAntigravity)
	for _, mode := range []string{MonitorAPIModeMessages, MonitorAPIModeGenerateContent} {
		t.Run(mode, func(t *testing.T) {
			monitor := &ChannelMonitor{
				ID: 82, Provider: MonitorProviderAntigravity, APIMode: mode,
				TargetType: ChannelMonitorTargetLocal, GroupID: &groupID, Group: group,
				PrimaryModel: "custom-model-alias",
			}
			svc := NewChannelMonitorService(nil, nil)
			capture := &antigravityCaptureHandler{}
			svc.SetLocalRequestHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiKey, ok := InternalChannelMonitorAPIKey(r.Context())
				require.True(t, ok)
				require.True(t, apiKey.IsInternalChannelMonitor())
				require.Equal(t, groupID, *apiKey.GroupID)
				require.Equal(t, PlatformAntigravity, apiKey.Group.Platform)
				require.Equal(t, PlatformAntigravity, apiKey.RoutingPlatform)
				// 两种协议均使用内部探测身份，余额校验与用户记账都应直接跳过。
				require.NoError(t, (&BillingCacheService{}).CheckBillingEligibilityStrict(r.Context(), nil, apiKey, nil, nil, PlatformAntigravity))
				require.NoError(t, (&GatewayService{}).RecordUsage(r.Context(), &RecordUsageInput{APIKey: apiKey}))
				capture.ServeHTTP(w, r)
			}))
			result := svc.runLocalCheckForModel(context.Background(), monitor, monitor.PrimaryModel, &CheckOptions{APIMode: mode})
			require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
			if mode == MonitorAPIModeGenerateContent {
				require.Equal(t, "/antigravity/v1beta/models/custom-model-alias:generateContent", capture.lastPath)
			} else {
				require.Equal(t, "/antigravity/v1/messages", capture.lastPath)
				require.Equal(t, monitor.PrimaryModel, capture.lastBody["model"])
			}
		})
	}
}

func TestAntigravityQuotaMonitorRequiresRealProbeModel(t *testing.T) {
	for _, mode := range []string{MonitorCheckModeProbe, MonitorCheckModeQuotaProbe} {
		t.Run(mode, func(t *testing.T) {
			newMonitor := func() *ChannelMonitor {
				return &ChannelMonitor{
					Provider: MonitorProviderAntigravity, APIMode: MonitorAPIModeMessages,
					CheckMode: MonitorCheckModeQuota, PrimaryModel: MonitorDefaultQuotaModel,
				}
			}
			require.ErrorIs(t, applyMonitorUpdate(newMonitor(), ChannelMonitorUpdateParams{
				CheckMode: &mode,
			}), ErrChannelMonitorMissingPrimaryModel)
			model := "gemini-3.1-pro-high"
			monitor := newMonitor()
			require.NoError(t, applyMonitorUpdate(monitor, ChannelMonitorUpdateParams{
				CheckMode: &mode, PrimaryModel: &model,
			}))
			require.Equal(t, model, monitor.PrimaryModel)
		})
	}
}

func TestGatewayRecordUsageUnifiedAntigravityChannelPricing(t *testing.T) {
	const groupID = int64(100)
	for _, model := range []struct{ requested, wire string }{
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"gemini-3.1-pro-high", "gemini-pro-agent"},
	} {
		for _, source := range []string{BillingModelSourceRequested, BillingModelSourceUpstream, BillingModelSourceChannelMapped} {
			t.Run(model.requested+"/"+source, func(t *testing.T) {
				pricing := []ChannelModelPricing{}
				for _, platform := range []string{PlatformAntigravity, PlatformAnthropic, PlatformGemini} {
					inputPrice, outputPrice, cachePrice := 41e-6, 42e-6, 43e-6
					if platform == PlatformAntigravity {
						inputPrice, outputPrice, cachePrice = 5e-6, 17e-6, 0.7e-6
					}
					pricing = append(pricing, ChannelModelPricing{
						Platform: platform, Models: []string{model.requested}, BillingMode: BillingModeToken,
						InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cachePrice,
					})
				}
				repo := &mockChannelRepository{
					listAllFn: func(context.Context) ([]Channel, error) {
						return []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{groupID}, ModelPricing: pricing}}, nil
					},
					getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
						return map[int64]string{groupID: PlatformAntigravity}, nil
					},
				}
				usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
				userRepo := &openAIRecordUsageUserRepoStub{}
				svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
				svc.resolver = NewModelPricingResolver(NewChannelService(repo, nil, nil, nil, nil), svc.billingService)
				group := &Group{ID: groupID, Platform: PlatformAntigravity, RateMultiplier: 1.25}
				err := svc.RecordUsage(context.Background(), &RecordUsageInput{
					Result: &ForwardResult{
						RequestID: "antigravity-channel-billing", Model: model.requested, UpstreamModel: model.wire,
						UpstreamResponseModel: model.requested,
						Usage:                 ClaudeUsage{InputTokens: 1000, OutputTokens: 1000, CacheReadInputTokens: 1000},
					},
					APIKey: &APIKey{ID: 501, GroupID: &group.ID, Group: group}, User: &User{ID: 601},
					Account: &Account{ID: 701, Platform: PlatformAntigravity, Type: AccountTypeOAuth},
					ChannelUsageFields: ChannelUsageFields{
						OriginalModel: model.requested, ChannelMappedModel: model.requested, BillingModelSource: source,
					},
				})
				require.NoError(t, err)
				log := usageRepo.lastLog
				require.NotNil(t, log)
				require.InDelta(t, 0.005, log.InputCost, 1e-9)
				require.InDelta(t, 0.017, log.OutputCost, 1e-9)
				require.InDelta(t, 0.0007, log.CacheReadCost, 1e-9)
				require.InDelta(t, 0.0227, log.TotalCost, 1e-9)
				require.InDelta(t, 0.028375, log.ActualCost, 1e-9)
				require.Equal(t, 1, userRepo.deductCalls)
				require.InDelta(t, log.ActualCost, userRepo.lastAmount, 1e-9)
			})
		}
	}
}
