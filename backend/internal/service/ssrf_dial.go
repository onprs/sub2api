package service

import (
	"context"
	"net"
	"strings"
	"time"
)

var ssrfBlockedHostnames = map[string]struct{}{
	"localhost":                  {},
	"localhost.localdomain":      {},
	"metadata":                   {},
	"metadata.google.internal":   {},
	"metadata.goog":              {},
	"instance-data":              {},
	"instance-data.ec2.internal": {},
}

var ssrfBlockedCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",    // IPv4 loopback
	"10.0.0.0/8",     // RFC1918
	"172.16.0.0/12",  // RFC1918
	"192.168.0.0/16", // RFC1918
	"169.254.0.0/16", // link-local and cloud metadata
	"100.64.0.0/10",  // CGNAT
	"0.0.0.0/8",      // this network
	"::1/128",        // IPv6 loopback
	"fc00::/7",       // IPv6 ULA
	"fe80::/10",      // IPv6 link-local
	"::/128",         // IPv6 unspecified
})

var defaultSSRFDialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 30 * time.Second,
}

type ssrfIPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type ssrfDialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// NewSSRFSafeDialContext returns a dial function that resolves a hostname once,
// rejects non-public addresses, and dials the validated IP literal directly.
// Dialing the resolved IP prevents a second resolver lookup from changing the
// destination between validation and socket creation.
func NewSSRFSafeDialContext(dialer *net.Dialer) ssrfDialContextFunc {
	if dialer == nil {
		dialer = defaultSSRFDialer
	}
	return newSSRFSafeDialContext(net.DefaultResolver, dialer.DialContext)
}

func newSSRFSafeDialContext(resolver ssrfIPResolver, dialContext ssrfDialContextFunc) ssrfDialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		if ip := net.ParseIP(host); ip != nil {
			if isPrivateIP(ip) {
				return nil, blockedSSRFAddress(host)
			}
			return dialContext(ctx, network, address)
		}
		if isBlockedHostname(host) {
			return nil, blockedSSRFAddress(host)
		}

		addresses, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(addresses) == 0 {
			return nil, &net.AddrError{Err: "no addresses for host", Addr: host}
		}

		// Fail the whole resolution if any answer is non-public. This keeps split
		// DNS and rebinding responses from depending on resolver answer order.
		for _, address := range addresses {
			if isPrivateIP(address.IP) {
				return nil, blockedSSRFAddress(host)
			}
		}

		var lastErr error
		for _, resolved := range addresses {
			resolvedAddress := net.JoinHostPort(resolved.IP.String(), port)
			conn, err := dialContext(ctx, network, resolvedAddress)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = &net.AddrError{Err: "no usable addresses", Addr: host}
		}
		return nil, lastErr
	}
}

func blockedSSRFAddress(host string) error {
	return &net.AddrError{Err: "blocked by SSRF policy", Addr: host}
}

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("ssrf_dial: invalid CIDR " + cidr + ": " + err.Error())
		}
		out = append(out, network)
	}
	return out
}

func isBlockedHostname(hostname string) bool {
	if hostname == "" {
		return true
	}
	_, blocked := ssrfBlockedHostnames[strings.ToLower(hostname)]
	return blocked
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range ssrfBlockedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
