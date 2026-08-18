package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorHTTPTimeoutsAllowSlowResponses(t *testing.T) {
	require.Equal(t, 120*time.Second, monitorRequestTimeout)
	require.Equal(t, 120*time.Second, monitorResponseHeaderTimeout)

	client := newSSRFSafeHTTPClient(monitorRequestTimeout)
	require.Equal(t, 120*time.Second, client.Timeout)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Equal(t, 120*time.Second, transport.ResponseHeaderTimeout)
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
