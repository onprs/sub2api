//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCommandCodeChannelEmptyModelListDoesNotRestoreCatalog(t *testing.T) {
	fallback := []string{"gpt-5.6-sol"}
	require.Empty(t, modelListingSource(service.PlatformCommandCode, []string{}, fallback))
	require.Equal(t, fallback, modelListingSource(service.PlatformCommandCode, nil, fallback))
	// OpenAI 的空目录是退役过滤后的权威结论，不回落默认列表；nil 才表示使用默认目录。
	require.Empty(t, modelListingSource(service.PlatformOpenAI, []string{}, fallback))
	require.Equal(t, fallback, modelListingSource(service.PlatformOpenAI, nil, fallback))

	handler := &AvailableChannelHandler{modelPricingModels: &modelPricingGatewayStub{
		modelsByGroup: map[int64][]string{1: {}},
	}}
	group := service.Group{ID: 1, Name: "Command Code", Platform: service.PlatformCommandCode}
	require.Empty(t, handler.modelIDsForPricingGroup(context.Background(), &group))
	channels := handler.buildModelPricingChannels(context.Background(), []service.Group{group}, nil)
	require.Len(t, channels, 1)
	require.Len(t, channels[0].Platforms, 1)
	require.Empty(t, channels[0].Platforms[0].SupportedModels)
	require.Len(t, channels[0].Platforms[0].Groups, 1)
}
