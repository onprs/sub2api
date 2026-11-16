package service

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const chatCompletionsDigestSessionKeyPrefix = "chat:digest:"

// BuildChatCompletionsDigestChain 根据 OpenAI Chat Completions 请求的 messages 生成摘要链。
// 格式: s:<hash>-u:<hash>-a:<hash>-t:<hash>-...
func BuildChatCompletionsDigestChain(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return ""
	}
	var parts []string
	messages.ForEach(func(_, msg gjson.Result) bool {
		prefix := chatRolePrefix(msg.Get("role").String())
		parts = append(parts, prefix+":"+shortHash(canonicalAnthropicDigestJSON([]byte(msg.Raw))))
		return true
	})
	return strings.Join(parts, "-")
}

func chatRolePrefix(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return "s"
	case "assistant":
		return "a"
	case "tool":
		return "t"
	default:
		return "u"
	}
}

// GenerateChatCompletionsPrefixHash 生成 Chat Completions 摘要会话的前缀 hash（不含客户端 IP）。
func GenerateChatCompletionsPrefixHash(userID, apiKeyID int64, userAgent, platform, model string) string {
	normalizedUserAgent := NormalizeSessionUserAgent(userAgent)
	combined := "chat:" +
		strconv.FormatInt(userID, 10) + ":" +
		strconv.FormatInt(apiKeyID, 10) + ":" +
		normalizedUserAgent + ":" +
		platform + ":" +
		model
	hash := sha256.Sum256([]byte(combined))
	return base64.RawURLEncoding.EncodeToString(hash[:12])
}

// GenerateChatCompletionsDigestSessionKey 生成 Chat Completions 摘要 fallback 的 sessionKey。
func GenerateChatCompletionsDigestSessionKey(prefixHash, uuid string) string {
	prefix := prefixHash
	if len(prefixHash) >= 8 {
		prefix = prefixHash[:8]
	}
	uuidPart := uuid
	if len(uuid) >= 8 {
		uuidPart = uuid[:8]
	}
	return chatCompletionsDigestSessionKeyPrefix + prefix + ":" + uuidPart
}
