package service

import (
	"strings"
	"testing"
)

func TestRedactProxyURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "whitespace", raw: "   ", want: ""},
		{
			name: "without credentials",
			raw:  "http://proxy.example.com:8080",
			want: "http://proxy.example.com:8080",
		},
		{
			name: "username and password",
			raw:  "http://alice:secret@proxy.example.com:8080",
			want: "http://<redacted>@proxy.example.com:8080",
		},
		{
			name: "username only",
			raw:  "https://alice@proxy.example.com:443",
			want: "https://<redacted>@proxy.example.com:443",
		},
		{
			name: "socks5 credentials",
			raw:  "socks5://user:p%40ss@socks.example.com:1080",
			want: "socks5://<redacted>@socks.example.com:1080",
		},
		{
			name: "IPv6",
			raw:  "socks5h://user:secret@[2001:db8::1]:1080",
			want: "socks5h://<redacted>@[2001:db8::1]:1080",
		},
		{
			name: "omit path query and fragment",
			raw:  "http://proxy.example.com:8080/path?token=secret#fragment",
			want: "http://proxy.example.com:8080",
		},
		{name: "missing scheme", raw: "user:secret@proxy.example.com:8080", want: invalidProxyURLForLog},
		{name: "missing host", raw: "http://user:secret@", want: invalidProxyURLForLog},
		{name: "invalid escape", raw: "http://user:%zz@proxy.example.com", want: invalidProxyURLForLog},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := RedactProxyURL(test.raw)
			if got != test.want {
				t.Fatalf("RedactProxyURL(%q) = %q, want %q", test.raw, got, test.want)
			}
			for _, secret := range []string{"alice", "user", "secret", "p%40ss", "token=secret"} {
				if test.want != invalidProxyURLForLog && strings.Contains(got, secret) {
					t.Fatalf("RedactProxyURL(%q) leaked %q in %q", test.raw, secret, got)
				}
			}
		})
	}
}
