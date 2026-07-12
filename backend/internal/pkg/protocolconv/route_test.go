package protocolconv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRouteRequiresExplicitProtocols(t *testing.T) {
	route := Route{
		Source:         ProtocolGoogleGenAI,
		IntendedTarget: ProtocolOpenAIResponses,
		ClientModel:    "client-model",
		UpstreamModel:  "upstream-model",
		Provider:       "openai",
		AccountID:      42,
	}
	require.NoError(t, route.Validate())

	route.Source = ""
	require.Error(t, route.Validate())
	route.Source = ProtocolGoogleGenAI
	route.IntendedTarget = ""
	require.Error(t, route.Validate())
}

func TestRouteRejectsNegativeAccountID(t *testing.T) {
	route := Route{Source: ProtocolAnthropic, IntendedTarget: ProtocolOpenAIChat, AccountID: -1}
	require.ErrorContains(t, route.Validate(), "must not be negative")
}
