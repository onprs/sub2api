package zcode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Send     Sender
	Origin   string
	Identity Identity
}
type envelope struct {
	Code    *int            `json:"code"`
	Success *bool           `json:"success"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) OriginURL() string {
	if c.Origin != "" {
		return strings.TrimRight(c.Origin, "/")
	}
	return DefaultOrigin
}
func (c *Client) request(ctx context.Context, method, url string, headers http.Header, body any, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var data []byte
	if body != nil {
		var e error
		data, e = json.Marshal(body)
		if e != nil {
			return nil, &Error{Kind: ErrFormat}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data)) //nolint:gosec // G704: URL 已由 ZCodeService.validateURL / routing 限定为官方域名白名单，拒绝私有地址与用户信息
	if err != nil {
		return nil, &Error{Kind: ErrConfiguration}
	}
	req.Header = headers.Clone()
	resp, err := c.Send(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Kind: ErrNetwork}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, &Error{Kind: ErrNetwork}
	}
	var env envelope
	if json.Unmarshal(raw, &env) != nil {
		return nil, &Error{Kind: Classify(resp.StatusCode, 0), Status: resp.StatusCode}
	}
	code := 0
	if env.Code != nil {
		code = *env.Code
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (env.Code != nil && code != 0 && code != 200) || (env.Success != nil && !*env.Success) {
		return nil, &Error{Kind: Classify(resp.StatusCode, code), Status: resp.StatusCode, Code: code, RetryAt: retryAt(resp.Header, raw, time.Now())}
	}
	if len(env.Data) == 0 {
		return nil, &Error{Kind: ErrFormat, Status: resp.StatusCode}
	}
	return env.Data, nil
}
func retryAt(h http.Header, body []byte, now time.Time) int64 {
	if seconds, err := strconv.Atoi(h.Get("Retry-After")); err == nil && seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second).Unix()
	}
	if t, err := http.ParseTime(h.Get("Retry-After")); err == nil {
		return t.Unix()
	}
	var info struct {
		Data struct {
			RetryAfter int64 `json:"retry_after"`
			Next       int64 `json:"next_available_at"`
			RetryAt    int64 `json:"retry_at"`
			Plan       struct {
				EndsAt int64 `json:"ends_at"`
			} `json:"plan"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &info)
	for _, t := range []int64{info.Data.Next, info.Data.RetryAt, info.Data.Plan.EndsAt} {
		if t > now.Unix() {
			return t
		}
	}
	if info.Data.RetryAfter > 0 {
		return now.Unix() + info.Data.RetryAfter
	}
	return 0
}
func (c *Client) rawZaiLogin(ctx context.Context, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.z.ai/api/auth/z/login", bytes.NewReader(body))
	if err != nil {
		return "", &Error{Kind: ErrConfiguration}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Send(req)
	if err != nil {
		return "", &Error{Kind: ErrNetwork}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", &Error{Kind: ErrNetwork}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &Error{Kind: Classify(resp.StatusCode, 0), Status: resp.StatusCode}
	}
	var result struct {
		Token string `json:"access_token"`
		Camel string `json:"accessToken"`
		Data  struct {
			Token string `json:"access_token"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "", &Error{Kind: ErrFormat}
	}
	value := result.Token
	if value == "" {
		value = result.Camel
	}
	if value == "" {
		value = result.Data.Token
	}
	if value == "" {
		return "", &Error{Kind: ErrFormat}
	}
	return value, nil
}
func bearer(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h
}
