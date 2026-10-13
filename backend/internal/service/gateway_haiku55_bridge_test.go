//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHaiku55BridgePreservesDisabledThinking(t *testing.T) {
	for _, chat := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := `{"model":"public-claude","input":"hello","reasoning":{"effort":"none"}}`
		if chat {
			body = `{"model":"public-claude","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"none"}`
		}
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(namespaceToolAnthropicStream())),
		}}
		svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
		account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "fixture-key", "model_mapping": map[string]any{"public-claude": "claude-haiku-5-5"}}}
		var result *ForwardResult
		var err error
		if chat {
			result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(body), nil)
		} else {
			result, err = svc.ForwardAsResponses(context.Background(), c, account, []byte(body), nil)
		}
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, upstream.lastReq)
		require.Equal(t, "claude-haiku-5-5", gjson.GetBytes(upstream.lastBody, "model").String())
		require.Equal(t, "disabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
		require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
		require.False(t, gjson.GetBytes(upstream.lastBody, "thinking.budget_tokens").Exists())
	}
}
