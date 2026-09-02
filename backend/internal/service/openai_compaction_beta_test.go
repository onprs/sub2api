package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenAICompactionHeaderContext(t *testing.T, beta string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if beta != "" {
		c.Request.Header.Set("x-codex-beta-features", beta)
	}
	return c
}

func TestApplyOpenAICodexBetaFeatures_CompactionHeaderContext(t *testing.T) {
	oauthAccount := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	apiKeyAccount := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	t.Run("OAuth 普通请求补默认能力", func(t *testing.T) {
		h := http.Header{}
		applyOpenAICodexBetaFeatures(newOpenAICompactionHeaderContext(t, ""), oauthAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("客户端声明保持不变", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(newOpenAICompactionHeaderContext(t, "some_other_feature"), oauthAccount, h)
		require.Equal(t, "some_other_feature", h.Get("x-codex-beta-features"))
	})

	t.Run("原生 v2 强制补齐能力", func(t *testing.T) {
		c := newOpenAICompactionHeaderContext(t, "some_other_feature")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(c, apiKeyAccount, h)
		require.Contains(t, h.Get("x-codex-beta-features"), "some_other_feature")
		require.Contains(t, h.Get("x-codex-beta-features"), "remote_compaction_v2")
	})

	t.Run("API Key 普通请求不注入", func(t *testing.T) {
		h := http.Header{}
		applyOpenAICodexBetaFeatures(newOpenAICompactionHeaderContext(t, ""), apiKeyAccount, h)
		require.Empty(t, h.Get("x-codex-beta-features"))
	})
}

func TestBuildOpenAIWSHeaders_CarriesSessionBetaFeatures_MinimalContext(t *testing.T) {
	svc := &OpenAIGatewayService{}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}
	oauthAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "test-account",
		},
	}

	build := func(t *testing.T, account *Account, clientBeta string) http.Header {
		t.Helper()
		c := newOpenAICompactionHeaderContext(t, clientBeta)
		headers, _, err := svc.buildOpenAIWSHeaders(
			context.Background(), c, account, "test-token", decision,
			true, "", "", "", "", "",
		)
		require.NoError(t, err)
		return headers
	}

	require.Equal(t, "remote_compaction_v2", build(t, oauthAccount, "").Get("x-codex-beta-features"))
	require.Equal(t, "some_other_feature", build(t, oauthAccount, "some_other_feature").Get("x-codex-beta-features"))
	require.Empty(t, build(t, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "").Get("x-codex-beta-features"))
}
