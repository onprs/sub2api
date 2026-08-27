package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorHTTPTimeoutsAllowSlowResponses(t *testing.T) {
	require.Equal(t, 120*time.Second, monitorRequestTimeout)
	require.Equal(t, 120*time.Second, monitorResponseHeaderTimeout)

	client := newSSRFSafeHTTPClient(monitorRequestTimeout)
	require.Equal(t, 120*time.Second, client.Timeout)
	// transport 可能被 servertiming 包装，底层 http.Transport 的超时配置
	// 由 newSSRFSafeHTTPClient 固定传入，这里验证 client 级超时与 SSRF dial。
	require.NotNil(t, client.Transport)
	dial := NewSSRFSafeDialContext(monitorDialer)
	_, err := dial(context.Background(), "tcp", "127.0.0.1:80")
	require.Error(t, err)
}

func TestChannelMonitorSlowSuccessIsDegradedInsteadOfError(t *testing.T) {
	result := finalizeOperationalOrDegraded(
		&CheckResult{},
		monitorRequestTimeout-time.Second,
		int((monitorRequestTimeout-time.Second)/time.Millisecond),
	)

	require.Equal(t, MonitorStatusDegraded, result.Status)
	require.NotEqual(t, MonitorStatusError, result.Status)
	require.NotEqual(t, MonitorStatusFailed, result.Status)
}
