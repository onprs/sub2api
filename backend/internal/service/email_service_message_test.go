//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessage_StandardHeadersAndMultipart(t *testing.T) {
	config := &SMTPConfig{
		Host:     "smtp.qiye.aliyun.com",
		From:     "onprs-server@onprs.top",
		FromName: "Onprs",
	}
	htmlBody := `<html><body><p>Hello <b>World</b></p><p>Code: 123456</p></body></html>`

	message, err := buildSMTPMessage(config, "user@example.com", "[Site] Verify", htmlBody)
	require.NoError(t, err)
	msg := string(message.data)

	// RFC 5322 必需头
	assert.Contains(t, msg, "From: \"Onprs\" <onprs-server@onprs.top>\r\n")
	assert.Contains(t, msg, "To: <user@example.com>\r\n")
	assert.Contains(t, msg, "Subject: [Site] Verify\r\n")
	assert.Contains(t, msg, "Date: ")
	assert.Contains(t, msg, "Message-ID: <")
	assert.Contains(t, msg, "@onprs.top>")

	// multipart/alternative 结构
	assert.Contains(t, msg, "Content-Type: multipart/alternative")
	assert.Contains(t, msg, "Content-Type: text/plain; charset=UTF-8")
	assert.Contains(t, msg, "Content-Type: text/html; charset=UTF-8")
	assert.Contains(t, msg, "Content-Transfer-Encoding: base64")

	// 无明文未转义的控制字符注入（头注入防护由 sanitize 负责，此处校验消息结构）
	assert.NotContains(t, msg, "<html>")
}

func TestBuildMessage_Base64DecodesToOriginalContent(t *testing.T) {
	config := &SMTPConfig{
		Host:     "smtp.qiye.aliyun.com",
		From:     "server@onprs.top",
		FromName: "Server",
	}
	htmlBody := `<p>验证码：<strong>123456</strong></p><a href="https://example.com/reset?a=1&b=2">重置</a>`
	plainWant := "验证码：123456\n重置"

	message, err := buildSMTPMessage(config, "user@example.com", "[站点] 验证码", htmlBody)
	require.NoError(t, err)
	msg := string(message.data)

	// 提取 text/plain 与 text/html 的 base64 内容并解码
	plainPart := extractBase64Part(t, msg, "text/plain")
	htmlPart := extractBase64Part(t, msg, "text/html")

	assert.Contains(t, htmlPart, htmlBody)
	assert.Equal(t, plainWant, plainPart)
}

func TestHtmlToPlainText(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{"strips_script_and_style", `<script>alert(1)</script><style>.x{}</style><p>Hello</p>`, "Hello"},
		{"block_tags_to_newlines", `<div>a</div><p>b</p>`, "a\nb"},
		{"decodes_entities", `<p>A&amp;B&lt;C&gt;</p>`, "A&B<C>"},
		{"collapses_blank_lines", "<p>one</p>\n\n\n<p>two</p>", "one\ntwo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, htmlToPlainText(tc.html))
		})
	}
}

func TestIsPermanentSMTPSendError(t *testing.T) {
	// 网络类错误（DNS 超时、连接重置等）为临时错误，应重试
	assert.False(t, isPermanentSMTPSendError(context.DeadlineExceeded))
	assert.False(t, isPermanentSMTPSendError(errors.New("dial tcp: lookup smtp.qiye.aliyun.com: i/o timeout")))
}

func TestCheckVerifyCodeCooldown(t *testing.T) {
	t.Run("in_cooldown_returns_too_frequent", func(t *testing.T) {
		svc := &EmailService{cache: &emailCacheStub{data: &VerificationCodeData{CreatedAt: time.Now().Add(-10 * time.Second)}}}
		err := svc.CheckVerifyCodeCooldown(context.Background(), "user@example.com")
		assert.ErrorIs(t, err, ErrVerifyCodeTooFrequent)
	})

	t.Run("no_existing_code_allows_send", func(t *testing.T) {
		svc := &EmailService{cache: &emailCacheStub{data: nil}}
		assert.NoError(t, svc.CheckVerifyCodeCooldown(context.Background(), "user@example.com"))
	})

	t.Run("expired_code_allows_send", func(t *testing.T) {
		svc := &EmailService{cache: &emailCacheStub{data: &VerificationCodeData{CreatedAt: time.Now().Add(-2 * time.Minute)}}}
		assert.NoError(t, svc.CheckVerifyCodeCooldown(context.Background(), "user@example.com"))
	})

	t.Run("cache_error_allows_send", func(t *testing.T) {
		svc := &EmailService{cache: &emailCacheStub{err: errors.New("redis down")}}
		assert.NoError(t, svc.CheckVerifyCodeCooldown(context.Background(), "user@example.com"))
	})
}

// extractBase64Part 从 multipart 消息中提取指定 Content-Type 的 base64 内容并解码。
func extractBase64Part(t *testing.T, msg, contentType string) string {
	t.Helper()
	idx := strings.Index(msg, "Content-Type: "+contentType)
	require.Greater(t, idx, -1, "missing part %s", contentType)

	bodyStart := strings.Index(msg[idx:], "\r\n\r\n")
	require.Greater(t, bodyStart, -1)
	body := msg[idx+bodyStart+4:]

	partEnd := strings.Index(body, "\r\n--")
	if partEnd == -1 {
		partEnd = len(body)
	}
	encoded := strings.ReplaceAll(body[:partEnd], "\r\n", "")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	return string(decoded)
}
