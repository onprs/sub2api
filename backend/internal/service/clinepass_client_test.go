package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type clinePassHTTPUpstreamStub struct {
	response *http.Response
	err      error
	request  *http.Request
}

func (s *clinePassHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	s.request = req
	return s.response, s.err
}

func (s *clinePassHTTPUpstreamStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, "", 0, 0)
}

func TestAppendClinePassPathPreservesAPIRoot(t *testing.T) {
	got, err := appendClinePassPath("https://api.cline.bot/api/v1", "/chat/completions")
	require.NoError(t, err)
	require.Equal(t, "https://api.cline.bot/api/v1/chat/completions", got)

	got, err = appendClinePassPath(got, "/chat/completions")
	require.NoError(t, err)
	require.Equal(t, "https://api.cline.bot/api/v1/chat/completions", got)
}

func TestClinePassClientFetchUsageMapsOptionalWindows(t *testing.T) {
	upstream := &clinePassHTTPUpstreamStub{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"success":true,
			"data":{"limits":[
				{"type":"five_hour","percentUsed":12.5},
				{"type":"weekly","percentUsed":101,"resetsAt":"2026-07-23T01:02:03.123456789Z"},
				{"type":"monthly","percentUsed":-2},
				{"type":"future_window","percentUsed":33}
			]}
		}`)),
	}}
	client := NewClinePassClient(upstream, &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}, nil)
	account := &Account{ID: 7, Platform: PlatformClinePass, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://api.cline.bot/api/v1"}}

	snapshot, err := client.FetchUsage(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 12.5, snapshot.Windows["5h"].PercentUsed)
	require.Nil(t, snapshot.Windows["5h"].ResetsAt)
	require.Equal(t, 100.0, snapshot.Windows["7d"].PercentUsed)
	require.Equal(t, 0.0, snapshot.Windows["30d"].PercentUsed)
	require.Equal(t, "Bearer test-key", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "https://api.cline.bot/api/v1/users/me/plan/usage-limits", upstream.request.URL.String())
	require.Equal(t, time.UTC, snapshot.Windows["7d"].ResetsAt.Location())
}

func TestNormalizeClinePassChatRequestConvertsDeveloperAndKeepsExtensions(t *testing.T) {
	body, err := normalizeClinePassChatRequest([]byte(`{
		"model":"client-model",
		"reasoning":{"enabled":false},
		"messages":[{"role":"developer","content":[{"type":"text","text":"rules","cache_control":{"type":"ephemeral"}}]}]
	}`), "cline-pass/glm-5.2")
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, `"role":"system"`)
	require.Contains(t, text, `"reasoning":{"enabled":false}`)
	require.Contains(t, text, `"cache_control":{"type":"ephemeral"}`)
	require.Contains(t, text, `"model":"cline-pass/glm-5.2"`)
}
