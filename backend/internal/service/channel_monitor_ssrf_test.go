package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorEndpointRejectsPrivateAndMetadataTargets(t *testing.T) {
	tests := []string{
		"https://127.0.0.1",
		"https://[::1]",
		"https://169.254.169.254",
		"https://metadata.google.internal",
		"https://instance-data.ec2.internal",
	}
	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			require.Error(t, validateEndpoint(endpoint))
		})
	}
}

func TestChannelMonitorHTTPClientUsesSharedSSRFDialGuard(t *testing.T) {
	client := newSSRFSafeHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)

	conn, err := transport.DialContext(context.Background(), "tcp", "10.0.0.1:443")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "blocked by SSRF policy")
}
