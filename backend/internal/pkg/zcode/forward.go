package zcode

// ZCode 只改变认证、endpoint 与 provider envelope。SSE、工具、计费由调用方原网关处理。
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ForwardOptions struct {
	Provider Provider
	Plan     string
	Tokens   Tokens
	Resolver *Resolver
	Captcha  *CaptchaPool
	Signer   *Signer
	Proxy    string
	Session  string
}

func SessionID(body []byte, device string) string {
	var payload struct {
		Messages []json.RawMessage `json:"messages"`
	}
	_ = json.Unmarshal(body, &payload)
	data := []byte(device)
	if len(payload.Messages) > 0 {
		data = append(data, payload.Messages[0]...)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:16])
}
func (c *Client) Forward(ctx context.Context, input *http.Request, body []byte, o ForwardOptions) (*http.Response, *Error, error) {
	if !o.Provider.Valid() || (o.Plan != PlanStart && o.Plan != PlanCoding) {
		return nil, &Error{Kind: ErrConfiguration}, nil
	}
	if o.Tokens.Expired(time.Now()) {
		return nil, &Error{Kind: ErrLogin}, nil
	}
	credential := o.Tokens.CodingKey
	endpoint := o.Provider.CodingBase() + "/v1/messages"
	if o.Plan == PlanStart {
		credential = o.Tokens.JWT
		endpoint = c.OriginURL() + StartMessagesPath
	}
	if credential == "" {
		return nil, &Error{Kind: ErrLogin}, nil
	}
	if o.Session == "" {
		o.Session = SessionID(body, c.Identity.DeviceMid)
	}
	transformed, err := TransformAnthropic(body, o.Plan == PlanStart, o.Provider, c.Identity, o.Session, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if o.Resolver != nil {
		endpoint = o.Resolver.Resolve(ctx, c, endpoint, credential)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, &Error{Kind: ErrConfiguration}, nil
	}
	headers := c.Identity.Headers(true)
	headers.Set("User-Agent", headers.Get("User-Agent")+" ai-sdk/anthropic/3.0.81")
	headers.Set("Content-Type", "application/json")
	headers.Set("Anthropic-Version", "2023-06-01")
	headers.Set("Authorization", "Bearer "+credential)
	if o.Plan == PlanCoding {
		headers.Set("X-Api-Key", credential)
	}
	headers.Set("X-Request-Id", uuid.NewString())
	headers.Set("X-ZCode-Trace-Id", uuid.NewString())
	headers.Set("X-ZCode-Session-Type", "main")
	headers.Set("X-Session-Id", o.Session)
	headers.Set("X-Query-Id", uuid.NewString())
	if beta := input.Header.Get("Anthropic-Beta"); beta != "" {
		headers.Set("Anthropic-Beta", beta)
	}
	if o.Plan == PlanStart {
		if o.Captcha == nil {
			return nil, &Error{Kind: ErrConfiguration}, nil
		}
		token, err := o.Captcha.Take(ctx, c, o.Proxy)
		if err != nil {
			if e, ok := err.(*Error); ok {
				return nil, e, nil
			}
			return nil, nil, err
		}
		headers.Set(CaptchaHeader, token.Param)
		headers.Set(CaptchaRegionHeader, token.Region)
	}
	captchaRetry, signingRetry := 0, 0
	for {
		req, err := http.NewRequestWithContext(ctx, input.Method, endpoint, bytes.NewReader(transformed))
		if err != nil {
			return nil, &Error{Kind: ErrConfiguration}, nil
		}
		req.Header = headers.Clone()
		signed := false
		if o.Plan == PlanCoding && o.Signer != nil {
			signed, err = o.Signer.Sign(ctx, c, req, credential)
			if err != nil {
				return nil, nil, err
			}
		}
		resp, err := c.Send(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, &Error{Kind: ErrNetwork}, nil
		}
		var peek []byte
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			contentType := strings.ToLower(resp.Header.Get("Content-Type"))
			if !strings.HasPrefix(contentType, "application/json") {
				return resp, nil, nil
			}
			peek, err = io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
			if err != nil {
				_ = resp.Body.Close()
				return nil, &Error{Kind: ErrNetwork}, nil
			}
			var business struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(peek, &business) != nil || business.Code == 0 {
				// 成功正文和大响应保持原始字节，关闭仍传递给原 body。
				resp.Body = struct {
					io.Reader
					io.Closer
				}{io.MultiReader(bytes.NewReader(peek), resp.Body), resp.Body}
				return resp, nil, nil
			}
		} else {
			peek, err = io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		}
		_ = resp.Body.Close()
		if err != nil {
			return nil, &Error{Kind: ErrNetwork}, nil
		}
		if o.Plan == PlanStart && captchaRetry == 0 && IsCaptchaChallenge(resp.StatusCode, resp.Header, peek) {
			captchaRetry++
			token, err := o.Captcha.Take(ctx, c, o.Proxy)
			if err != nil {
				return nil, &Error{Kind: ErrCaptcha}, nil
			}
			headers.Set(CaptchaHeader, token.Param)
			headers.Set(CaptchaRegionHeader, token.Region)
			continue
		}
		if signed && signingRetry < 2 && VerifyFailure(resp.StatusCode, peek) {
			signingRetry++
			o.Signer.Invalidate(requestOrigin(parsed), credential, signingRetry == 2)
			continue
		}
		var env struct {
			Code  int `json:"code"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(peek, &env)
		kind := Classify(resp.StatusCode, env.Code)
		// 上游余额错误兼容固定短语，原始 message 不离开协议模块。
		low := strings.ToLower(string(peek))
		if strings.Contains(low, "insufficient balance") || strings.Contains(low, "insufficient_credit") || strings.Contains(low, "余额不足") || strings.Contains(low, "quota exhausted") {
			kind = ErrQuota
		}
		return nil, &Error{Kind: kind, Status: resp.StatusCode, Code: env.Code, RetryAt: retryAt(resp.Header, peek, time.Now())}, nil
	}
}
