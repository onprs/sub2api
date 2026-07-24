package repository

import "testing"

func TestRedactProxyKeyForLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "direct", raw: directProxyKey, want: directProxyKey},
		{
			name: "credentials",
			raw:  "http://user:secret@proxy.example.com:8080",
			want: "http://<redacted>@proxy.example.com:8080",
		},
		{name: "invalid", raw: "://user:secret@proxy", want: "<invalid-proxy-url>"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := redactProxyKeyForLog(test.raw); got != test.want {
				t.Fatalf("redactProxyKeyForLog(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
