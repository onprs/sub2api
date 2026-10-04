// Package zcode 封装 ZCode 协议；业务调度、计费和数据库留在 Sub2API 服务层。
// 协议来源：TriDefender/zcode-api，固定版本与源码映射见 upstream.json。
package zcode

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const UpstreamCommit = "0f8788ae02d3a2432b96276f200e7c41bf8f64d7"
const DefaultAppVersion = "3.14.0"
const DefaultOrigin = "https://zcode.z.ai"
const StartBase = DefaultOrigin + "/api/v1/zcode-plan/anthropic"
const StartMessagesPath = "/api/v1/zcode-plan/anthropic/v1/messages"
const CaptchaHeader = "X-Aliyun-Captcha-Verify-Param"
const CaptchaRegionHeader = "X-Aliyun-Captcha-Verify-Region"
const PlanStart = "start"
const PlanCoding = "coding"

type Provider string

const (
	ZAI      Provider = "zai"
	BigModel Provider = "bigmodel"
)

func (p Provider) Valid() bool { return p == ZAI || p == BigModel }
func (p Provider) CodingBase() string {
	if p == ZAI {
		return "https://api.z.ai/api/anthropic"
	}
	return "https://open.bigmodel.cn/api/anthropic"
}
func (p Provider) MonitorOrigin() string {
	if p == ZAI {
		return "https://api.z.ai"
	}
	return "https://open.bigmodel.cn"
}
func (p Provider) BusinessOrigin() string {
	if p == ZAI {
		return "https://api.z.ai"
	}
	return "https://bigmodel.cn"
}

// Tokens 的 JSON 仅用于加密保存和服务内部处理，不能直接作为 API 响应。
type Tokens struct {
	AccessToken string `json:"access_token"`
	JWT         string `json:"jwt,omitempty"`
	CodingKey   string `json:"coding_key,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	IssuedAt    int64  `json:"issued_at,omitempty"`
}

func InspectJWT(token string) (issued, expires int64, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, errors.New("ZCode JWT 格式无效")
	}
	raw, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if e != nil {
		return 0, 0, errors.New("ZCode JWT payload 无效")
	}
	var claims struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return 0, 0, errors.New("ZCode JWT payload 无效")
	}
	return claims.Iat, claims.Exp, nil
}
func (t Tokens) Expired(now time.Time) bool { return t.ExpiresAt > 0 && now.Unix() >= t.ExpiresAt }

type ErrorKind string

const (
	ErrLogin         ErrorKind = "login_required"
	ErrCaptcha       ErrorKind = "captcha"
	ErrQuota         ErrorKind = "quota_exhausted"
	ErrRateLimit     ErrorKind = "rate_limited"
	ErrConfiguration ErrorKind = "configuration"
	ErrNetwork       ErrorKind = "network"
	ErrFormat        ErrorKind = "upstream_format"
	ErrUnavailable   ErrorKind = "unavailable"
)

// Error 永不保留上游原始 msg、URL query 或认证值。
type Error struct {
	Kind    ErrorKind
	Status  int
	Code    int
	RetryAt int64
}

func (e *Error) Error() string {
	return fmt.Sprintf("ZCode %s (http=%d code=%d)", e.Kind, e.Status, e.Code)
}
func Classify(status, code int) ErrorKind {
	switch {
	case status == 401 || code == 401:
		return ErrLogin
	case code == 3007:
		return ErrCaptcha
	case status == 429:
		return ErrRateLimit
	case status == 402 || code == 1005:
		return ErrQuota
	case status == 408 || status >= 500:
		return ErrUnavailable
	case code == 3001 || code == 3012 || status == 403:
		return ErrConfiguration
	default:
		return ErrFormat
	}
}
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrNetwork
	}
	return ErrUnavailable
}

type Sender func(*http.Request) (*http.Response, error)
type Cipher interface {
	Encrypt(string) (string, error)
	Decrypt(string) (string, error)
}

// Store 必须具有跨实例原子语义；不提供生产内存回退。
type Store interface {
	Get(context.Context, string) (string, error)
	Put(context.Context, string, string, time.Duration) error
	Take(context.Context, string) (string, error)
	Acquire(context.Context, string, string, time.Duration) (bool, error)
	Release(context.Context, string, string) error
}
