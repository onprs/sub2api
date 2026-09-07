//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type commandCodeInsufficientCreditsRepoStub struct {
	mockAccountRepoForGemini
	calls  int
	id     int64
	until  time.Time
	reason string
}

func (r *commandCodeInsufficientCreditsRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.calls++
	r.id = id
	r.until = until
	r.reason = reason
	return nil
}

func TestCommandCodeGatewayInsufficientCreditsFailsOverAndPausesAccount(t *testing.T) {
	body := `{"error":{"message":"You have insufficient credits to make this request. Please purchase more credits to continue using the service.","type":"invalid_request_error","code":"BAD_REQUEST"}}`
	upstream := &commandCodeHTTPUpstreamStub{responses: map[string]*http.Response{
		commandCodeChatCompletionsPath: commandCodeJSONResponse(http.StatusBadRequest, body),
	}}
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	repo := &commandCodeInsufficientCreditsRepoStub{}
	rateLimitService := NewRateLimitService(repo, nil, cfg, nil, nil)
	client := NewCommandCodeClient(upstream, cfg, nil)
	svc := NewCommandCodeGatewayService(client, cfg, rateLimitService)
	account := commandCodeGatewayTestAccount()
	periodEnd := time.Now().UTC().Add(19 * 24 * time.Hour).Truncate(time.Nanosecond)
	account.Extra = map[string]any{"commandcode_usage_period_end": periodEnd.Format(time.RFC3339Nano)}
	_, c := newCommandCodeTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, account, []byte(`{
		"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hi"}],"stream":false
	}`))

	require.Error(t, err)
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusBadRequest, failover.StatusCode)
	require.Equal(t, body, string(failover.ResponseBody))
	require.Equal(t, 1, repo.calls)
	require.Equal(t, account.ID, repo.id)
	require.WithinDuration(t, periodEnd, repo.until, time.Second)
	require.Contains(t, repo.reason, commandCodeInsufficientCreditsReasonPrefix)
	require.Contains(t, repo.reason, "insufficient credits")
	require.False(t, c.Writer.Written())
}

func TestCommandCodeOrdinaryBadRequestDoesNotPauseAccount(t *testing.T) {
	repo := &commandCodeInsufficientCreditsRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := commandCodeGatewayTestAccount()

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"error":{"message":"Invalid option","type":"invalid_request_error","code":"BAD_REQUEST"}}`),
		"deepseek/deepseek-v4-flash",
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.calls)
}
