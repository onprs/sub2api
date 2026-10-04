package zcode

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestCaptchaBrowserSDKContract(t *testing.T) {
	executable := os.Getenv("ZCODE_TEST_CHROMIUM")
	if executable == "" {
		t.Skip("设置 ZCODE_TEST_CHROMIUM 后运行受控浏览器 SDK 合成测试")
	}
	token := captchaParam("synthetic-browser")
	encoded, _ := json.Marshal(token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk.js" {
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprintf(w, "window.initAliyunCaptcha=function(options){if(options.SceneId!=='synthetic-scene'||options.region!=='cn'){throw Error('config');} options.getInstance({startTracelessVerification:function(){options.success({verifyResult:true,verifyParam:%s});}});};", encoded)
			return
		}
		_, _ = fmt.Fprint(w, "<!doctype html><html><body>合成 SDK 页面</body></html>")
	}))
	defer server.Close()
	solver := &BrowserSolver{Executable: executable, origin: server.URL, sdkURL: server.URL + "/sdk.js"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, e := solver.Solve(ctx, CaptchaConfig{Enabled: true, SceneID: "synthetic-scene", Prefix: "synthetic-prefix", Region: "cn"}, "")
	require.NoError(t, e)
	require.Equal(t, token, result)
}

// 人工启用的匿名官方 SDK 检查：不登录、不领取、不打印或保存验证参数。
func TestCaptchaBrowserOfficialSDK(t *testing.T) {
	if os.Getenv("ZCODE_LIVE_CAPTCHA_TEST") != "1" {
		t.Skip("官方 SDK 检查需要显式开启 ZCODE_LIVE_CAPTCHA_TEST")
	}
	executable := os.Getenv("ZCODE_TEST_CHROMIUM")
	require.NotEmpty(t, executable)
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
	testProxy := os.Getenv("ZCODE_TEST_PROXY")
	if testProxy != "" {
		proxyURL, e := url.Parse(testProxy)
		require.NoError(t, e)
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	httpClient := &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	client := &Client{Identity: DefaultIdentity("", "synthetic-anonymous-device"), Send: httpClient.Do}
	pool := NewCaptchaPool(&BrowserSolver{Executable: executable, trace: func(stage string) { t.Log(stage) }})
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cfg, e := pool.configuration(ctx, client)
	require.NoError(t, e, "查询公开 CAPTCHA 配置")
	t.Logf("public_config enabled=%t scene_present=%t prefix_present=%t region_present=%t", cfg.Enabled, cfg.SceneID != "", cfg.Prefix != "", cfg.Region != "")
	param, e := pool.solver.Solve(ctx, cfg, testProxy)
	require.NoError(t, e, "执行官方动态 SDK")
	_, e = ValidateCaptcha(param)
	require.NoError(t, e, "验证返回值格式")
}
func TestCaptchaBrowserCancellationAndUnsupportedProxy(t *testing.T) {
	executable := os.Getenv("ZCODE_TEST_CHROMIUM")
	if executable == "" {
		t.Skip("设置 ZCODE_TEST_CHROMIUM 后运行受控浏览器取消测试")
	}
	solver := &BrowserSolver{Executable: executable}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := solver.Solve(ctx, CaptchaConfig{}, "")
	require.Error(t, e)
	_, e = solver.Solve(context.Background(), CaptchaConfig{}, "http://synthetic-user:synthetic-password@127.0.0.1:9999")
	require.Error(t, e)
	require.Equal(t, ErrConfiguration, KindOf(e))
	require.NotContains(t, e.Error(), "synthetic-password")
}
