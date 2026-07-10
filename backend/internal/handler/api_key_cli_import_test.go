package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeCLIImportBuilder struct {
	gotInput service.CLIImportScriptInput
	gotUser  int64
	result   *service.CLIImportScriptResult
	err      error
}

func (f *fakeCLIImportBuilder) BuildCLIImportScript(ctx context.Context, input service.CLIImportScriptInput, userID int64, modelProvider service.CLIImportAvailableModelsProvider, capabilityProvider service.CLIImportCapabilityProvider) (*service.CLIImportScriptResult, error) {
	f.gotInput = input
	f.gotUser = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestAPIKeyHandler_DownloadCLIImportScript(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := &fakeCLIImportBuilder{
		result: &service.CLIImportScriptResult{
			Filename:    "sub2api-cli-import.sh",
			ContentType: "application/octet-stream",
			Body:        []byte("#!/usr/bin/env bash\n"),
		},
	}
	h := &APIKeyHandler{cliImportBuilder: builder}
	router := gin.New()
	router.GET("/keys/:id/cli-import/script", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1001})
		h.DownloadCLIImportScript(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/keys/42/cli-import/script?os=linux", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "api.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
	require.Contains(t, w.Header().Get("Content-Disposition"), "sub2api-cli-import.sh")
	require.Equal(t, "#!/usr/bin/env bash\n", w.Body.String())
	require.Equal(t, int64(42), builder.gotInput.APIKey.ID)
	require.Equal(t, "linux", builder.gotInput.OS)
	require.Equal(t, "https://api.example.com", builder.gotInput.APIBaseURL)
	require.Equal(t, int64(1001), builder.gotUser)
}

func TestAPIKeyHandler_DownloadCLIImportScriptRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &APIKeyHandler{cliImportBuilder: &fakeCLIImportBuilder{}}
	router := gin.New()
	router.GET("/keys/:id/cli-import/script", h.DownloadCLIImportScript)

	req := httptest.NewRequest(http.MethodGet, "/keys/42/cli-import/script?os=linux", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKeyHandler_DownloadCLIImportScriptInvalidOS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := &fakeCLIImportBuilder{err: service.ErrCLIImportInvalidOS}
	h := &APIKeyHandler{cliImportBuilder: builder}
	router := gin.New()
	router.GET("/keys/:id/cli-import/script", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1001})
		h.DownloadCLIImportScript(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/keys/42/cli-import/script?os=plan9", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPIKeyHandler_DownloadCLIImportScriptSanitizesForwardedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builder := &fakeCLIImportBuilder{
		result: &service.CLIImportScriptResult{
			Filename:    "sub2api-cli-import.sh",
			ContentType: "application/octet-stream",
			Body:        []byte("#!/usr/bin/env bash\n"),
		},
	}
	h := &APIKeyHandler{cliImportBuilder: builder}
	router := gin.New()
	router.GET("/keys/:id/cli-import/script", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1001})
		h.DownloadCLIImportScript(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/keys/42/cli-import/script?os=linux", nil)
	req.Host = "safe.example.com"
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "api.example.com/path")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "http://safe.example.com", builder.gotInput.APIBaseURL)
}
