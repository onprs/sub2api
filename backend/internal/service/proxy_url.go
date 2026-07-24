package service

import (
	"net/url"
	"strings"
)

const invalidProxyURLForLog = "<invalid-proxy-url>"

// RedactProxyURL returns a credential-free proxy endpoint suitable for logs.
// Paths, queries, and fragments are omitted because proxy routing only needs
// the scheme and authority, and those fields may contain additional secrets.
func RedactProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" {
		return invalidProxyURLForLog
	}

	authority := parsed.Host
	if parsed.User != nil {
		authority = "<redacted>@" + authority
	}
	return parsed.Scheme + "://" + authority
}
