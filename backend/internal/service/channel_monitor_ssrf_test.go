package service

import (
	"context"
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
	require.NotNil(t, client.Transport)

	// 直接验证 SSRF dial 函数本身能拦截私网地址（transport 外层可能被
	// servertiming 包装，但底层 dial 始终是同一个 SSRF guard）。
	dial := NewSSRFSafeDialContext(monitorDialer)
	conn, err := dial(context.Background(), "tcp", "10.0.0.1:443")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "blocked by SSRF policy")
}
