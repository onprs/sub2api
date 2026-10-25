//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestExplicitOpenAIHeaderSessionNamesAppendsAstrBotConversationHeader 固定
// AstrBot 会话头的优先级位置：既有会话头必须全部排在它前面，列表顺序即优先级。
func TestExplicitOpenAIHeaderSessionNamesAppendsAstrBotConversationHeader(t *testing.T) {
	indexOf := func(header string) int {
		for i, name := range explicitOpenAIHeaderSessionNames {
			if name == header {
				return i
			}
		}
		return -1
	}

	astrBotIndex := indexOf(astrBotConversationHeader)
	require.Equal(t, len(explicitOpenAIHeaderSessionNames)-1, astrBotIndex,
		"新的 AstrBot 会话头必须追加在列表末尾")

	for _, header := range []string{
		"session-id",
		"session_id",
		"conversation_id",
		openCodeSessionAffinityHeader,
		openCodeSessionIDHeader,
		openCodeNativeSessionHeader,
		codeBuddyConversationHeader,
	} {
		require.Less(t, indexOf(header), astrBotIndex, "%s 必须保持更高的优先级", header)
	}
}

func TestExplicitOpenAIHeaderSessionIDReadsAstrBotConversationHeader(t *testing.T) {
	c := newSessionHeaderContext(t, map[string]string{
		astrBotConversationHeader: "  astrbot-conv-abc123  ",
	})
	require.Equal(t, "astrbot-conv-abc123", explicitOpenAIHeaderSessionID(c))
}

func TestExplicitOpenAIHeaderSessionIDKeepsEarlierHeaderPriority(t *testing.T) {
	c := newSessionHeaderContext(t, map[string]string{
		openCodeSessionIDHeader:   "earlier-session-id",
		astrBotConversationHeader: "astrbot-conv-abc123",
	})
	require.Equal(t, "earlier-session-id", explicitOpenAIHeaderSessionID(c))

	c2 := newSessionHeaderContext(t, map[string]string{
		"session_id":              "underscore-session",
		astrBotConversationHeader: "astrbot-conv-abc123",
	})
	require.Equal(t, "underscore-session", explicitOpenAIHeaderSessionID(c2))
}

func TestApplyOpenCodeSessionHeaderUsesAstrBotConversationID(t *testing.T) {
	account := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey}
	const conversationID = "astrbot-conv-abc123"

	firstTurn := newSessionHeaderContext(t, map[string]string{astrBotConversationHeader: conversationID})
	headers := make(http.Header)
	applyOpenCodeSessionHeader(firstTurn, account, "https://opencode.ai/zen/go/v1/chat/completions", headers)
	require.Equal(t, conversationID, headers.Get(openCodeSessionHeader))

	// 后续轮次沿用同一会话头时必须复用同一出站会话 ID，而不是重新生成 UUID。
	secondTurn := newSessionHeaderContext(t, map[string]string{astrBotConversationHeader: conversationID})
	headers2 := make(http.Header)
	applyOpenCodeSessionHeader(secondTurn, account, "https://opencode.ai/zen/go/v1/chat/completions", headers2)
	require.Equal(t, headers.Get(openCodeSessionHeader), headers2.Get(openCodeSessionHeader))
	require.Equal(t, conversationID, headers2.Get(openCodeSessionHeader))
}

func TestApplyOpenCodeSessionHeaderGeneratesUUIDWithoutAstrBotConversationID(t *testing.T) {
	account := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey}
	headers := make(http.Header)
	applyOpenCodeSessionHeader(newOpenCodeSessionTestContext(t, ""), account, "https://opencode.ai/zen/go/v1/chat/completions", headers)

	sessionID := headers.Get(openCodeSessionHeader)
	require.NotEmpty(t, sessionID)
	_, err := uuid.Parse(sessionID)
	require.NoError(t, err, "缺少任何会话标识时仍按原有逻辑生成 UUID")
}

func TestExtractClientSessionIDRecordsAstrBotConversationID(t *testing.T) {
	c := newSessionHeaderContext(t, map[string]string{
		astrBotConversationHeader: "astrbot-conv-abc123",
	})
	require.Equal(t, "astrbot-conv-abc123", ExtractClientSessionID(c))

	// 既有会话头保持更高优先级，不因新增条目改变已持久化的 session_id 语义。
	c2 := newSessionHeaderContext(t, map[string]string{
		"session-id":              "highest-priority",
		astrBotConversationHeader: "astrbot-conv-abc123",
	})
	require.Equal(t, "highest-priority", ExtractClientSessionID(c2))
}

func TestOpenCodeGoBuildUpstreamRequestUsesAstrBotConversationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := openCodeSessionTestService()
	account := &Account{
		ID:       7,
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://opencode.ai/zen/go/v1",
		},
	}
	c := newSessionHeaderContext(t, map[string]string{astrBotConversationHeader: "astrbot-conv-abc123"})

	req, err := svc.buildUpstreamRequest(
		t.Context(), c, account,
		[]byte(`{"model":"glm-5.3","input":"hello"}`), "token", false, "", false,
	)
	require.NoError(t, err)
	require.Equal(t, "astrbot-conv-abc123", req.Header.Get(openCodeSessionHeader))
}
