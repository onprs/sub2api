package zcode

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForwardHTTP200QuotaBusinessError(t *testing.T) {
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "client/configs") {
			captchaResponse(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":1005,"msg":"synthetic-sensitive","logid":"synthetic"}`)
	})
	request, _ := http.NewRequest("POST", client.OriginURL()+StartMessagesPath, nil)
	response, upstreamError, err := client.Forward(context.Background(), request, fixture(t, "start_request"), ForwardOptions{Provider: ZAI, Plan: PlanStart, Tokens: Tokens{JWT: "synthetic-jwt"}, Captcha: goodPool()})
	require.NoError(t, err)
	require.Nil(t, response)
	require.NotNil(t, upstreamError)
	require.Equal(t, ErrQuota, upstreamError.Kind)
	require.Equal(t, 1005, upstreamError.Code)
	require.NotContains(t, upstreamError.Error(), "synthetic-sensitive")
}

func TestForwardJSONMessagePreservesResponse(t *testing.T) {
	message := `{"type":"message","content":[{"type":"text","text":"OK"}]}`
	client := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "client/configs") {
			captchaResponse(w)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, message)
	})
	request, _ := http.NewRequest("POST", client.OriginURL()+StartMessagesPath, nil)
	response, upstreamError, err := client.Forward(context.Background(), request, fixture(t, "start_request"), ForwardOptions{Provider: ZAI, Plan: PlanStart, Tokens: Tokens{JWT: "synthetic-jwt"}, Captcha: goodPool()})
	require.NoError(t, err)
	require.Nil(t, upstreamError)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, message, string(body))
}
