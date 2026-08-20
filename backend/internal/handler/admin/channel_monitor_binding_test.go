package admin

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func bindChannelMonitorJSON(t *testing.T, body string, target any) error {
	t.Helper()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/test", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	return c.ShouldBindJSON(target)
}

func TestChannelMonitorBindingAcceptsLocalGroupWithoutCredentials(t *testing.T) {
	body := `{
		"name":"local-openai",
		"provider":"openai",
		"target_type":"local",
		"group_id":20,
		"primary_model":"gpt-5.4",
		"interval_seconds":60
	}`
	var request channelMonitorCreateRequest
	require.NoError(t, bindChannelMonitorJSON(t, body, &request))
	require.Equal(t, "local", request.TargetType)
	require.NotNil(t, request.GroupID)
	require.Equal(t, int64(20), *request.GroupID)
	require.Empty(t, request.Endpoint)
	require.Empty(t, request.APIKey)
}

func TestChannelMonitorBindingAcceptsOpenCodeGoMessages(t *testing.T) {
	createBody := `{
		"name":"opencode-go",
		"provider":"opencode_go",
		"api_mode":"messages",
		"endpoint":"https://opencode.ai/zen/go/v1",
		"api_key":"sk-test",
		"primary_model":"qwen3.7-plus",
		"interval_seconds":60
	}`
	require.NoError(t, bindChannelMonitorJSON(t, createBody, &channelMonitorCreateRequest{}))

	updateBody := `{"provider":"opencode_go","api_mode":"messages"}`
	require.NoError(t, bindChannelMonitorJSON(t, updateBody, &channelMonitorUpdateRequest{}))

	templateCreateBody := `{
		"name":"opencode-go messages",
		"provider":"opencode_go",
		"api_mode":"messages"
	}`
	require.NoError(t, bindChannelMonitorJSON(t, templateCreateBody, &channelMonitorTemplateCreateRequest{}))

	templateUpdateBody := `{"api_mode":"messages"}`
	require.NoError(t, bindChannelMonitorJSON(t, templateUpdateBody, &channelMonitorTemplateUpdateRequest{}))
}
