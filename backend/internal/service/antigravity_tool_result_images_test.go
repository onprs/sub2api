package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAntigravityResponsesToolResultImages(t *testing.T) {
	for _, model := range []string{"gemini-3.8-flash", "gemini-4-argon"} {
		t.Run(model, func(t *testing.T) {
			raw := []byte(`{"model":"client-alias","input":[
 {"type":"function_call","call_id":"call_A","name":"images","arguments":"{}"},
 {"type":"function_call","call_id":"call_B","name":"images","arguments":"{}"},
 {"type":"function_call_output","call_id":"call_A","output":[{"type":"input_text","text":"first pair"},{"type":"input_image","image_url":"data:image/png;base64,AQID"},{"type":"input_image","image_url":"data:image/webp;base64,BAUG"}]},
 {"type":"function_call_output","call_id":"call_B","output":[{"type":"input_image","image_url":"data:image/jpeg;base64,BwgJ"}]}
 ]}`)
			wire := toolImageCompatWire(t, raw, model)
			responses := []gjson.Result{}
			nativeImages := []gjson.Result{}
			for _, content := range gjson.GetBytes(wire, "request.contents").Array() {
				for _, part := range content.Get("parts").Array() {
					if fr := part.Get("functionResponse"); fr.Exists() {
						responses = append(responses, fr)
						continue
					}
					if image := part.Get("inlineData"); image.Exists() {
						nativeImages = append(nativeImages, image)
					}
				}
			}
			require.Len(t, responses, 2)
			require.Len(t, nativeImages, 3)
			a, b := responses[0], responses[1]
			require.Equal(t, "images", a.Get("name").String())
			require.Equal(t, "images", b.Get("name").String())
			// 定制 pipeline 契约：functionResponse.response.content 为结构化数组，
			// 文本与 media 都保留在其中；原生 inlineData 副本作为独立 part 供 Gemini 读取。
			require.Equal(t, "first pair", a.Get("response.content.0.text").String())
			require.Equal(t, "image/png", a.Get("response.content.1.media_type").String())
			require.Equal(t, "AQID", a.Get("response.content.1.data").String())
			require.Equal(t, "image/webp", a.Get("response.content.2.media_type").String())
			require.Equal(t, "BAUG", a.Get("response.content.2.data").String())
			require.Equal(t, "image/png", nativeImages[0].Get("mimeType").String())
			require.Equal(t, "AQID", nativeImages[0].Get("data").String())
			require.Equal(t, "image/webp", nativeImages[1].Get("mimeType").String())
			require.Equal(t, "BAUG", nativeImages[1].Get("data").String())
			require.Equal(t, "image/jpeg", b.Get("response.content.0.media_type").String())
			require.Equal(t, "BwgJ", b.Get("response.content.0.data").String())
			require.Equal(t, "image/jpeg", nativeImages[2].Get("mimeType").String())
			require.Equal(t, "BwgJ", nativeImages[2].Get("data").String())
		})
	}
}

func toolImageCompatWire(t *testing.T, raw []byte, model string) []byte {
	t.Helper()
	var req apicompat.ResponsesRequest
	require.NoError(t, json.Unmarshal(raw, &req))
	a, err := apicompat.ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	body, err := json.Marshal(a)
	require.NoError(t, err)
	var claude antigravity.ClaudeRequest
	require.NoError(t, json.Unmarshal(body, &claude))
	service := &AntigravityGatewayService{}
	wire, err := service.buildAntigravityCompatGeminiBody(context.Background(), body, &claude, "offline-project", model)
	require.NoError(t, err)
	return wire
}
