//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiAccountTestModel_SelectsTierDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		account   *Account
		requested string
		want      string
	}{
		{
			name:      "显式模型优先",
			account:   &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"tier_id": "aistudio_free"}},
			requested: "gemma-4-31b-it",
			want:      "gemma-4-31b-it",
		},
		{
			name:    "Free Tier 使用当前目录首项",
			account: &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"tier_id": "aistudio_free"}},
			want:    "gemini-3-flash-preview",
		},
		{
			name:    "历史 API Key 缺失等级按 Free Tier 处理",
			account: &Account{Type: AccountTypeAPIKey},
			want:    "gemini-3-flash-preview",
		},
		{
			name:    "Paid Tier 保留原有默认模型",
			account: &Account{Type: AccountTypeAPIKey, Credentials: map[string]any{"tier_id": "aistudio_paid"}},
			want:    "gemini-2.0-flash",
		},
		{
			name:    "OAuth 保留原有默认模型",
			account: &Account{Type: AccountTypeOAuth},
			want:    "gemini-2.0-flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, geminiAccountTestModel(tt.account, tt.requested))
		})
	}
}

func TestCreateGeminiTestPayload_ImageModel(t *testing.T) {
	t.Parallel()

	payload := createGeminiTestPayload("gemini-2.5-flash-image", "draw a tiny robot")

	var parsed struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig struct {
			ResponseModalities []string `json:"responseModalities"`
			ImageConfig        struct {
				AspectRatio string `json:"aspectRatio"`
			} `json:"imageConfig"`
		} `json:"generationConfig"`
	}

	require.NoError(t, json.Unmarshal(payload, &parsed))
	require.Len(t, parsed.Contents, 1)
	require.Len(t, parsed.Contents[0].Parts, 1)
	require.Equal(t, "draw a tiny robot", parsed.Contents[0].Parts[0].Text)
	require.Equal(t, []string{"TEXT", "IMAGE"}, parsed.GenerationConfig.ResponseModalities)
	require.Equal(t, "1:1", parsed.GenerationConfig.ImageConfig.AspectRatio)
}

func TestProcessGeminiStream_EmitsImageEvent(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	ctx, recorder := newTestContext()
	svc := &AccountTestService{}

	stream := strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"},{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"QUJD\"}}]}}]}\n\ndata: [DONE]\n\n")

	err := svc.processGeminiStream(ctx, stream)
	require.NoError(t, err)

	body := recorder.Body.String()
	require.Contains(t, body, "\"type\":\"content\"")
	require.Contains(t, body, "\"text\":\"ok\"")
	require.Contains(t, body, "\"type\":\"image\"")
	require.Contains(t, body, "\"image_url\":\"data:image/png;base64,QUJD\"")
	require.Contains(t, body, "\"mime_type\":\"image/png\"")
}
