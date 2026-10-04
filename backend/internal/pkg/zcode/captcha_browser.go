package zcode

// CAPTCHA 原生适配边界：Go 管理 Chromium 生命周期，通过 CDP 执行官方动态 SDK。
// SDK 初始化契约来源 src/proxy/captcha-happy.ts；不移植上游的 Happy DOM/polyfill。
import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type BrowserSolver struct {
	Executable string
	NoSandbox  bool
	// 仅包内合成测试使用；生产服务无法通过配置指定任意 SDK。
	origin string
	sdkURL string
	trace  func(string)
}

func (s *BrowserSolver) Solve(ctx context.Context, cfg CaptchaConfig, proxy string) (string, error) {
	binary := s.Executable
	if binary == "" {
		for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "chrome"} {
			if path, err := exec.LookPath(name); err == nil {
				binary = path
				break
			}
		}
	}
	if binary == "" {
		return "", &Error{Kind: ErrConfiguration}
	}
	dir, err := os.MkdirTemp("", "sub2api-zcode-captcha-")
	if err != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	args := []string{"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-quic", "--disable-blink-features=AutomationControlled", "--disable-background-timer-throttling", "--disable-renderer-backgrounding", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0", "--user-data-dir=" + dir, "about:blank"}
	if s.NoSandbox {
		args = append(args, "--no-sandbox")
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.User != nil || u.Host == "" {
			return "", &Error{Kind: ErrConfiguration}
		}
		switch u.Scheme {
		case "http", "https", "socks5":
		default:
			return "", &Error{Kind: ErrConfiguration}
		}
		args = append(args, "--proxy-server="+u.String())
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	// 服务的 HOME 可能不可写；Chromium 的配置与 crashpad 也必须使用匿名临时目录。
	cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "XDG_CACHE_HOME="+filepath.Join(dir, "cache"))
	hideProcess(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Start() != nil {
		return "", &Error{Kind: ErrConfiguration}
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	port := 0
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for port == 0 {
		data, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err == nil {
			parts := strings.Split(string(data), "\n")
			port, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			if port < 1 || port > 65535 {
				port = 0
			}
		}
		if port > 0 {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
	control := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer control.CloseIdleConnections()
	origin := s.origin
	if origin == "" {
		origin = DefaultOrigin + "/robots.txt"
	}
	req, _ := http.NewRequestWithContext(ctx, "PUT", fmt.Sprintf("http://127.0.0.1:%d/json/new?about:blank", port), nil)
	resp, err := control.Do(req)
	if err != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	var target struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&target)
	_ = resp.Body.Close()
	if err != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	u, err := url.Parse(target.URL)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != strconv.Itoa(port) || u.Scheme != "ws" {
		return "", &Error{Kind: ErrCaptcha}
	}
	conn, _, err := websocket.Dial(ctx, target.URL, nil)
	if err != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(1 << 20)
	// SDK 使用当前 Chromium 的真实版本，避免另行固定第二套客户端版本。
	versionCall, _ := json.Marshal(map[string]any{"id": 5, "method": "Browser.getVersion"})
	if conn.Write(ctx, websocket.MessageText, versionCall) != nil {
		return "", &Error{Kind: ErrCaptcha, Code: 40010}
	}
	userAgent := ""
	for userAgent == "" {
		_, data, e := conn.Read(ctx)
		if e != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", &Error{Kind: ErrCaptcha, Code: 40010}
		}
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				UserAgent string `json:"userAgent"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &reply) == nil && reply.ID == 5 {
			userAgent = strings.ReplaceAll(reply.Result.UserAgent, "HeadlessChrome/", "Chrome/")
			if userAgent == "" {
				return "", &Error{Kind: ErrConfiguration}
			}
		}
	}
	uaCall, _ := json.Marshal(map[string]any{"id": 9, "method": "Network.setUserAgentOverride", "params": map[string]string{"userAgent": userAgent, "acceptLanguage": "en-US,en"}})
	if conn.Write(ctx, websocket.MessageText, uaCall) != nil {
		return "", &Error{Kind: ErrCaptcha, Code: 40010}
	}
	// 先启用事件、设置临时匿名页面策略，再导航。SDK 不在旧 about:blank execution context 中执行。
	for _, call := range []map[string]any{{"id": 10, "method": "Page.enable"}, {"id": 11, "method": "Page.setBypassCSP", "params": map[string]bool{"enabled": true}}, {"id": 12, "method": "Page.navigate", "params": map[string]string{"url": origin}}} {
		message, _ := json.Marshal(call)
		if conn.Write(ctx, websocket.MessageText, message) != nil {
			return "", &Error{Kind: ErrCaptcha, Code: 40010}
		}
	}
	expected, _ := url.Parse(origin)
	frameReady := false
	for {
		_, data, readErr := conn.Read(ctx)
		if readErr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", &Error{Kind: ErrCaptcha, Code: 40020}
		}
		var event struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				ErrorText string `json:"errorText"`
			} `json:"result"`
			Params struct {
				Frame struct {
					URL    string `json:"url"`
					Parent string `json:"parentId"`
				} `json:"frame"`
			} `json:"params"`
		}
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		if len(event.Error) > 0 || (event.ID == 12 && event.Result.ErrorText != "") {
			return "", &Error{Kind: ErrCaptcha, Code: 40021}
		}
		if event.Method == "Page.frameNavigated" && event.Params.Frame.Parent == "" {
			actual, _ := url.Parse(event.Params.Frame.URL)
			frameReady = actual != nil && actual.Scheme == expected.Scheme && actual.Host == expected.Host
		}
		if s.trace != nil && (event.Method == "Page.frameNavigated" || event.Method == "Page.domContentEventFired" || event.ID == 12) {
			s.trace(fmt.Sprintf("navigation_event=%s nav_reply=%t origin_match=%t", event.Method, event.ID == 12, frameReady))
		}
		if event.Method == "Page.domContentEventFired" && frameReady {
			break
		}
	}
	configJSON, _ := json.Marshal(cfg)
	expression := strings.ReplaceAll(captchaProgram, "__CONFIG__", string(configJSON))
	sdkURL := s.sdkURL
	if sdkURL == "" {
		sdkURL = "https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js"
	}
	encodedSDK, _ := json.Marshal(sdkURL)
	expression = strings.ReplaceAll(expression, "__SDK_URL__", string(encodedSDK))
	payload, _ := json.Marshal(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true, "timeout": 20000}})
	if conn.Write(ctx, websocket.MessageText, payload) != nil {
		return "", &Error{Kind: ErrCaptcha}
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", &Error{Kind: ErrCaptcha}
		}
		var result struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
				Exception json.RawMessage `json:"exceptionDetails"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &result) != nil || result.ID != 1 {
			continue
		}
		if len(result.Error) > 0 || len(result.Result.Exception) > 0 {
			code := 40010
			var detail struct {
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			}
			_ = json.Unmarshal(result.Result.Exception, &detail)
			for label, n := range map[string]int{"zcode-sdk": 40011, "zcode-timeout": 40012, "zcode-rejected": 40013, "zcode-format": 40014, "zcode-init": 40015, "zcode-failure": 40016} {
				if strings.Contains(detail.Exception.Description, label) {
					code = n
					break
				}
			}
			return "", &Error{Kind: ErrCaptcha, Code: code}
		}
		var param string
		if json.Unmarshal(result.Result.Result.Value, &param) != nil {
			return "", &Error{Kind: ErrCaptcha, Code: 40014}
		}
		if _, err = ValidateCaptcha(param); err != nil {
			return "", err
		}
		return param, nil
	}
}

// 配置仅来自受信 client/configs 的标识字段。脚本 URL 固定；无账号 JWT/API Key。
const captchaProgram = `(async()=>{
 const cfg=__CONFIG__;
 if(document.readyState==='loading'){await new Promise(resolve=>document.addEventListener('DOMContentLoaded',resolve,{once:true}));}
 document.body.innerHTML='<div id="cap"></div><button id="btn" type="button">Verify</button>';
 window.AliyunCaptchaConfig={region:cfg.region,prefix:cfg.prefix};
 await new Promise((resolve,reject)=>{const script=document.createElement('script');script.src=__SDK_URL__;script.onload=resolve;script.onerror=()=>reject(new Error('zcode-sdk'));document.head.append(script);});
 return await new Promise((resolve,reject)=>{
  const timer=setTimeout(()=>reject(new Error('zcode-timeout')),18000);
  const finish=(value)=>{clearTimeout(timer);const param=typeof value==='string'?value:value?.verifyParam||value?.data||value?.param;if(value?.verifyResult===false&&!(typeof param==='string'&&param.length)){reject(new Error('zcode-rejected'));return;}if(typeof param!=='string'){reject(new Error('zcode-format'));return;}resolve(param);};
  try { window.initAliyunCaptcha({SceneId:cfg.sceneId,mode:'popup',region:cfg.region,prefix:cfg.prefix,language:'en',element:'#cap',button:'#btn',captchaLogoImg:'',showErrorTip:false,getInstance:(inst)=>{try{(inst.startTracelessVerification||inst.show).call(inst);}catch{clearTimeout(timer);reject(new Error('zcode-init'));}},success:finish,fail:finish,onError:()=>{clearTimeout(timer);reject(new Error('zcode-sdk'));}}); } catch {clearTimeout(timer);reject(new Error('zcode-sdk'));}
 });
})()`
