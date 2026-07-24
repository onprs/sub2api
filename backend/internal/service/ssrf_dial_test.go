package service

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type staticSSRFResolver struct {
	addresses []net.IPAddr
	err       error
	calls     int
}

func (r *staticSSRFResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.calls++
	return r.addresses, r.err
}

func TestSSRFSafeDialRejectsDNSRebindingAnswer(t *testing.T) {
	resolver := &staticSSRFResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}
	dialCalled := false
	dial := newSSRFSafeDialContext(resolver, func(context.Context, string, string) (net.Conn, error) {
		dialCalled = true
		return nil, nil
	})

	conn, err := dial(context.Background(), "tcp", "rebind.example:443")
	require.Nil(t, conn)
	require.ErrorContains(t, err, "blocked by SSRF policy")
	require.False(t, dialCalled, "private rebinding answer must never reach the socket dialer")
	require.Equal(t, 1, resolver.calls)
}

func TestSSRFSafeDialRejectsMixedPublicAndPrivateAnswers(t *testing.T) {
	resolver := &staticSSRFResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("93.184.216.34")},
		{IP: net.ParseIP("10.0.0.8")},
	}}
	dialCalled := false
	dial := newSSRFSafeDialContext(resolver, func(context.Context, string, string) (net.Conn, error) {
		dialCalled = true
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "split.example:443")
	require.ErrorContains(t, err, "blocked by SSRF policy")
	require.False(t, dialCalled)
}

func TestSSRFSafeDialUsesValidatedIPLiteral(t *testing.T) {
	resolver := &staticSSRFResolver{addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}}
	var dialedAddress string
	peerClosed := make(chan struct{})
	dial := newSSRFSafeDialContext(resolver, func(_ context.Context, network, address string) (net.Conn, error) {
		require.Equal(t, "tcp", network)
		dialedAddress = address
		client, peer := net.Pipe()
		go func() {
			_ = peer.Close()
			close(peerClosed)
		}()
		return client, nil
	})

	conn, err := dial(context.Background(), "tcp", "public.example:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, "93.184.216.34:443", dialedAddress)
	require.NoError(t, conn.Close())
	<-peerClosed
}

func TestSSRFSafeDialTriesNextValidatedAddress(t *testing.T) {
	resolver := &staticSSRFResolver{addresses: []net.IPAddr{
		{IP: net.ParseIP("93.184.216.34")},
		{IP: net.ParseIP("8.8.8.8")},
	}}
	var attempts []string
	dial := newSSRFSafeDialContext(resolver, func(_ context.Context, _, address string) (net.Conn, error) {
		attempts = append(attempts, address)
		if len(attempts) == 1 {
			return nil, errors.New("first address unavailable")
		}
		client, peer := net.Pipe()
		_ = peer.Close()
		return client, nil
	})

	conn, err := dial(context.Background(), "tcp", "multi.example:443")
	require.NoError(t, err)
	require.Equal(t, []string{"93.184.216.34:443", "8.8.8.8:443"}, attempts)
	require.NoError(t, conn.Close())
}

func TestSSRFSafeDialRejectsPrivateLiteralWithoutResolving(t *testing.T) {
	resolver := &staticSSRFResolver{}
	dialCalled := false
	dial := newSSRFSafeDialContext(resolver, func(context.Context, string, string) (net.Conn, error) {
		dialCalled = true
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "169.254.169.254:80")
	require.ErrorContains(t, err, "blocked by SSRF policy")
	require.Zero(t, resolver.calls)
	require.False(t, dialCalled)
}

func TestIsPrivateIPPolicy(t *testing.T) {
	tests := []struct {
		address string
		blocked bool
	}{
		{address: "0.0.0.0", blocked: true},
		{address: "127.0.0.1", blocked: true},
		{address: "10.0.0.1", blocked: true},
		{address: "100.64.0.1", blocked: true},
		{address: "169.254.169.254", blocked: true},
		{address: "192.168.1.1", blocked: true},
		{address: "::1", blocked: true},
		{address: "fc00::1", blocked: true},
		{address: "fe80::1", blocked: true},
		{address: "8.8.8.8", blocked: false},
		{address: "2606:4700:4700::1111", blocked: false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			require.Equal(t, test.blocked, isPrivateIP(net.ParseIP(test.address)))
		})
	}
}
