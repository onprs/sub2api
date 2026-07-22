package service

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestClinePassLiveContract is intentionally opt-in because it consumes real
// ClinePass quota. Inject a newly issued key through CLINEPASS_TEST_API_KEY.
func TestClinePassLiveContract(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("CLINEPASS_TEST_API_KEY"))
	if apiKey == "" {
		t.Skip("CLINEPASS_TEST_API_KEY is not set")
	}
	model := strings.TrimSpace(os.Getenv("CLINEPASS_TEST_MODEL"))
	if model == "" {
		model = "cline-pass/glm-5.2"
	}
	require.True(t, isClinePassModelID(model), "CLINEPASS_TEST_MODEL must be a full cline-pass/... slug")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	transport := &clinePassLiveHTTPUpstream{client: &http.Client{Timeout: 90 * time.Second}}
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	client := NewClinePassClient(transport, cfg, nil)
	gateway := NewClinePassGatewayService(client, cfg, nil)
	account := &Account{
		ID: 1, Name: "clinepass-live", Platform: PlatformClinePass, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": apiKey, "base_url": DefaultClinePassBaseURL},
	}

	t.Run("official usage limits", func(t *testing.T) {
		snapshot, err := client.FetchUsage(ctx, account)
		require.NoError(t, err)
		require.NotNil(t, snapshot)
		for name := range snapshot.Windows {
			require.Contains(t, clinePassOfficialUsageQuotaWindows, name)
		}
	})

	t.Run("public model catalog", func(t *testing.T) {
		catalog := NewClinePassCatalog(transport.client)
		models, err := catalog.ForceRefresh(ctx)
		require.NoError(t, err)
		require.Contains(t, models, model)
	})

	t.Run("channel monitor streaming challenge", func(t *testing.T) {
		result := runCheckForModel(ctx, MonitorProviderClinePass, DefaultClinePassBaseURL, apiKey, model, nil)
		require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
	})

	t.Run("four buffered ingress protocols", func(t *testing.T) {
		tests := []struct {
			name       string
			forward    func(*testing.T) string
			shapeQuery string
		}{
			{
				name: "chat with developer normalization",
				forward: func(t *testing.T) string {
					recorder, ginContext := newClinePassTestContext()
					_, err := gateway.ForwardChatCompletions(ctx, ginContext, account, []byte(`{"model":"`+model+`","messages":[{"role":"developer","content":"Reply briefly."},{"role":"user","content":"Reply with OK."}],"max_tokens":4096}`))
					require.NoError(t, err)
					return recorder.Body.String()
				},
				shapeQuery: "choices.0.message",
			},
			{
				name: "responses",
				forward: func(t *testing.T) string {
					recorder, ginContext := newClinePassTestContext()
					_, err := gateway.ForwardResponses(ctx, ginContext, account, []byte(`{"model":"`+model+`","input":"Reply with OK.","max_output_tokens":4096}`), model)
					require.NoError(t, err)
					return recorder.Body.String()
				},
				shapeQuery: "output",
			},
			{
				name: "anthropic messages",
				forward: func(t *testing.T) string {
					recorder, ginContext := newClinePassTestContext()
					_, err := gateway.ForwardMessages(ctx, ginContext, account, []byte(`{"model":"`+model+`","max_tokens":4096,"messages":[{"role":"user","content":"Reply with OK."}]}`))
					require.NoError(t, err)
					return recorder.Body.String()
				},
				shapeQuery: "content",
			},
			{
				name: "google genai",
				forward: func(t *testing.T) string {
					recorder, ginContext := newClinePassTestContext()
					_, err := gateway.ForwardGoogleGenAI(ctx, ginContext, account, []byte(`{"contents":[{"role":"user","parts":[{"text":"Reply with OK."}]}],"generationConfig":{"maxOutputTokens":4096}}`), model, model, false)
					require.NoError(t, err)
					return recorder.Body.String()
				},
				shapeQuery: "candidates",
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				body := test.forward(t)
				require.True(t, gjson.Valid(body))
				require.True(t, gjson.Get(body, test.shapeQuery).Exists(), body)
			})
		}
	})

	t.Run("chat streaming usage and terminal", func(t *testing.T) {
		recorder, ginContext := newClinePassTestContext()
		result, err := gateway.ForwardChatCompletions(ctx, ginContext, account, []byte(`{"model":"`+model+`","messages":[{"role":"user","content":"Reply with OK."}],"max_tokens":4096,"stream":true}`))
		require.NoError(t, err)
		require.Contains(t, recorder.Body.String(), "data: [DONE]")
		require.Positive(t, result.Usage.InputTokens+result.Usage.OutputTokens)
	})

	t.Run("buffered and streaming tool calls", func(t *testing.T) {
		for _, stream := range []bool{false, true} {
			name := "buffered"
			if stream {
				name = "streaming"
			}
			t.Run(name, func(t *testing.T) {
				recorder, ginContext := newClinePassTestContext()
				body := `{"model":"` + model + `","messages":[{"role":"user","content":"Look up id 1."}],"max_tokens":4096,"stream":` + boolJSON(stream) + `,"tools":[{"type":"function","function":{"name":"lookup","description":"Lookup an id","parameters":{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`
				_, err := gateway.ForwardChatCompletions(ctx, ginContext, account, []byte(body))
				require.NoError(t, err)
				require.Contains(t, recorder.Body.String(), "tool_calls")
			})
		}
	})

	t.Run("invalid model remains a client error", func(t *testing.T) {
		recorder, ginContext := newClinePassTestContext()
		_, err := gateway.ForwardChatCompletions(ctx, ginContext, account, []byte(`{"model":"cline-pass/definitely-invalid","messages":[{"role":"user","content":"hi"}]}`))
		require.Error(t, err)
		var failover *UpstreamFailoverError
		require.NotErrorAs(t, err, &failover)
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

type clinePassLiveHTTPUpstream struct {
	client *http.Client
}

func (u *clinePassLiveHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func (u *clinePassLiveHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.client.Do(req)
}

var _ HTTPUpstream = (*clinePassLiveHTTPUpstream)(nil)
