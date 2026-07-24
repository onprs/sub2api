package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetSetupBindAddress_DefaultsLoopback(t *testing.T) {
	t.Setenv("SERVER_HOST", "")
	t.Setenv("SERVER_PORT", "")
	host, port := GetSetupBindAddress()
	require.Equal(t, "127.0.0.1", host, "wizard must default to loopback (H-3)")
	require.Equal(t, 8080, port)
}

func TestGetSetupBindAddress_RespectsEnv(t *testing.T) {
	t.Setenv("SERVER_HOST", "0.0.0.0")
	t.Setenv("SERVER_PORT", "9090")
	host, port := GetSetupBindAddress()
	require.Equal(t, "0.0.0.0", host)
	require.Equal(t, 9090, port)
}

func TestGetSetupBindAddress_FallsBackOnInvalidPort(t *testing.T) {
	t.Setenv("SERVER_HOST", "localhost")
	t.Setenv("SERVER_PORT", "not-an-int")
	host, port := GetSetupBindAddress()
	require.Equal(t, "localhost", host)
	require.Equal(t, 8080, port, "invalid port should fall back to 8080")
}

func TestIsLoopbackBind(t *testing.T) {
	for _, h := range []string{"", "127.0.0.1", "127.1.2.3", "localhost", "::1", "[::1]"} {
		require.True(t, IsLoopbackBind(h), "expected loopback for %q", h)
	}
	for _, h := range []string{"0.0.0.0", "10.0.0.1", "example.com", "::", "[::]", "169.254.1.1"} {
		require.False(t, IsLoopbackBind(h), "expected non-loopback for %q", h)
	}
}

func TestValidateSetupToken_UnconfiguredAllowsAll(t *testing.T) {
	t.Setenv("SETUP_TOKEN", "")
	require.True(t, ValidateSetupToken("anything"), "unset token must not enforce")
	require.True(t, ValidateSetupToken(""), "unset token must not enforce even on empty input")
}

func TestValidateSetupToken_ConfiguredEnforces(t *testing.T) {
	t.Setenv("SETUP_TOKEN", "s3cr3t-token")
	require.True(t, ValidateSetupToken("s3cr3t-token"), "matching token must be accepted")
	require.False(t, ValidateSetupToken("wrong"), "wrong token must be rejected")
	require.False(t, ValidateSetupToken(""), "missing token must be rejected")
	require.False(t, ValidateSetupToken("s3cr3t"), "prefix must not match")
	require.False(t, ValidateSetupToken("s3cr3t-token-extra"), "longer input must not match")
}

func TestAcquireSetupLock_ExclusiveThenRelease(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	r1, err := AcquireSetupLock()
	require.NoError(t, err)
	require.NotNil(t, r1)

	// A second process attempting to start a wizard must fail fast (pending exists).
	_, err2 := AcquireSetupLock()
	require.Error(t, err2)

	// After the first holder releases, a new holder can acquire again.
	r1()
	r2, err3 := AcquireSetupLock()
	require.NoError(t, err3)
	require.NotNil(t, r2)
	r2()
}

func TestCreateInstallLock_PromotesPendingLock(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	release, err := AcquireSetupLock()
	require.NoError(t, err)
	defer release()

	// Pending marker must exist while the wizard holds the lock.
	_, statPending := os.Stat(GetInstallPendingLockPath())
	require.NoError(t, statPending)

	// Completing installation atomically promotes pending -> .installed.
	require.NoError(t, createInstallLock())

	_, statInstall := os.Stat(GetInstallLockPath())
	require.NoError(t, statInstall, ".installed must exist after install success")
	_, statPendingAfter := os.Stat(GetInstallPendingLockPath())
	require.Error(t, statPendingAfter, "pending marker must be consumed by createInstallLock")
}

func TestCreateInstallLock_WritesLockWithoutPending(t *testing.T) {
	// CLI / AUTO_SETUP paths never create a pending lock; createInstallLock must
	// still write a fresh .installed file in that case.
	t.Setenv("DATA_DIR", t.TempDir())

	require.NoError(t, createInstallLock())

	_, statInstall := os.Stat(GetInstallLockPath())
	require.NoError(t, statInstall)
	_, statPending := os.Stat(GetInstallPendingLockPath())
	require.Error(t, statPending, "pending marker must not be left behind")
}

func TestSetupGuard_TokenEnforcement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name     string
		tokenEnv string
		header   string
		wantCode int
	}{
		{"unconfigured passes", "", "", http.StatusOK},
		{"configured missing header rejected", "secret", "", http.StatusForbidden},
		{"configured wrong header rejected", "secret", "nope", http.StatusForbidden},
		{"configured correct header passes", "secret", "secret", http.StatusOK},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Empty DATA_DIR => NeedsSetup() is true (no config, no .installed).
			t.Setenv("DATA_DIR", t.TempDir())
			if tc.tokenEnv == "" {
				t.Setenv("SETUP_TOKEN", "")
			} else {
				t.Setenv("SETUP_TOKEN", tc.tokenEnv)
			}

			r := gin.New()
			r.Use(setupGuard())
			r.POST("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

			req := httptest.NewRequest(http.MethodPost, "/x", nil)
			if tc.header != "" {
				req.Header.Set(SetupTokenHeader, tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.wantCode, w.Code, "token=%q header=%q", tc.tokenEnv, tc.header)
		})
	}

	t.Run("already installed rejected regardless of token", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("DATA_DIR", dir)
		t.Setenv("SETUP_TOKEN", "")
		require.NoError(t, os.WriteFile(filepath.Join(dir, InstallLockFile), []byte("installed"), 0400))

		r := gin.New()
		r.Use(setupGuard())
		r.POST("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusForbidden, w.Code, "post-install /setup/* must be blocked even without SETUP_TOKEN")
	})
}
