package repository

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func strictHTTPUpstreamForSSRFTest(t *testing.T) *httpUpstreamService {
	t.Helper()
	upstream := NewHTTPUpstream(&config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           true,
				AllowPrivateHosts: false,
			},
		},
	})
	result, ok := upstream.(*httpUpstreamService)
	require.True(t, ok)
	return result
}

func TestHTTPUpstreamStrictDirectTransportRejectsPrivateDial(t *testing.T) {
	upstream := strictHTTPUpstreamForSSRFTest(t)
	entry, err := upstream.getClientEntry(
		"",
		1,
		1,
		service.HTTPUpstreamProfileDefault,
		false,
		false,
	)
	require.NoError(t, err)
	transport, ok := entry.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)

	conn, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "blocked by SSRF policy")
}

func TestHTTPUpstreamStrictTLSFingerprintRejectsPrivateDial(t *testing.T) {
	upstream := strictHTTPUpstreamForSSRFTest(t)
	entry, err := upstream.getClientEntryWithTLS(
		"",
		1,
		1,
		&tlsfingerprint.Profile{Name: "ssrf-test"},
		service.HTTPUpstreamProfileDefault,
		false,
		false,
	)
	require.NoError(t, err)
	transport, ok := entry.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialTLSContext)

	conn, err := transport.DialTLSContext(context.Background(), "tcp", "169.254.169.254:443")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "blocked by SSRF policy")
}

func TestHTTPUpstreamProxyModeTrustsConfiguredProxyDialPath(t *testing.T) {
	upstream := strictHTTPUpstreamForSSRFTest(t)
	entry, err := upstream.getClientEntry(
		"http://127.0.0.1:3128",
		1,
		1,
		service.HTTPUpstreamProfileDefault,
		false,
		false,
	)
	require.NoError(t, err)
	transport, ok := entry.client.Transport.(*http.Transport)
	require.True(t, ok)

	// 代理模式仍安装有界超时的普通 dialer；只有直连严格模式才会用
	// SSRF-safe dialer 覆盖它。目标 DNS 与私网约束位于受信代理边界。
	require.NotNil(t, transport.DialContext)
	require.NotNil(t, transport.Proxy)
}

func TestHTTPUpstreamAllowPrivateHostsExplicitlyDisablesDialGuard(t *testing.T) {
	rawUpstream := NewHTTPUpstream(&config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           true,
				AllowPrivateHosts: true,
			},
		},
	})
	upstream, ok := rawUpstream.(*httpUpstreamService)
	require.True(t, ok)
	entry, err := upstream.getClientEntry(
		"",
		1,
		1,
		service.HTTPUpstreamProfileDefault,
		false,
		false,
	)
	require.NoError(t, err)
	transport, ok := entry.client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext, "允许私网时仍应保留普通拨号超时")
}

func TestHTTPUpstreamRedirectCheckerRejectsPrivateTarget(t *testing.T) {
	upstream := strictHTTPUpstreamForSSRFTest(t)
	request, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/metadata", nil)
	require.NoError(t, err)
	require.Error(t, upstream.redirectChecker(request, nil))
}
