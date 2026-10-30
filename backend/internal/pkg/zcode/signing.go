package zcode

// 来源：src/proxy/client-signing.ts，V4 的签名字段使用换行拼接，HKDF/AES-GCM/Ed25519。
// 3.15.1 起签名是静态客户端规则，不再探测 agent/configs 的 codingPlanSignature；
// 凭据支持 {apiKeyId}.{secret} 与 project_access JWT 两种形态，bigmodel OAuth
// 账号通过 access_tokens 铸造 project JWT 后签名。
import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/sync/singleflight"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const projectTokenClientType = "zcode"

// SigningCredential 是签名的输入：Key 为 coding-plan 凭据，OAuth 仅 bigmodel 账号可用。
type SigningCredential struct {
	Key      string
	OAuth    string
	Provider Provider
}

type parsedCredential struct {
	apiKeyID       string
	secret         string
	handshakeField string
}

const (
	handshakeFieldAPIKey      = "apiKey"
	handshakeFieldAccessToken = "accessToken"
)

type signingState struct {
	key          ed25519.PrivateKey
	bypass       bool
	projectJWT   string
	projectExp   time.Time
	mintNegUntil time.Time
}
type Signer struct {
	mu     sync.Mutex
	states map[string]*signingState
	flight singleflight.Group
}

func NewSigner() *Signer { return &Signer{states: map[string]*signingState{}} }
func signingID(origin, key string) string {
	digest := sha256.Sum256([]byte(origin + "\n" + key))
	return hex.EncodeToString(digest[:])
}
func derive(secret, info string) ([]byte, error) {
	out := make([]byte, 32)
	_, err := io.ReadFull(hkdf.New(sha256.New, []byte(secret), []byte("WD_CLIENT_SIGN_KDF_SALT"), []byte(info)), out)
	return out, err
}
func decodeJWTPart(value string) (map[string]any, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil, &Error{Kind: ErrFormat}
	}
	return out, nil
}

// parseSigningCredential 对应上游 3.15.1 的 FSr：JWT 形态走 accessToken 握手字段，
// {apiKeyId}.{secret} 形态走 apiKey。
func parseSigningCredential(value string) (parsedCredential, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return parsedCredential{}, false
	}
	if segments := strings.Split(value, "."); len(segments) == 3 && segments[2] != "" {
		header, herr := decodeJWTPart(segments[0])
		payload, perr := decodeJWTPart(segments[1])
		if herr != nil || perr != nil {
			return parsedCredential{}, false
		}
		if header["alg"] != "HS256" || header["sign_type"] != "SIGN" {
			return parsedCredential{}, false
		}
		if payload["token_use"] != "project_access" {
			return parsedCredential{}, false
		}
		apiKey, _ := payload["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			return parsedCredential{}, false
		}
		return parsedCredential{apiKeyID: apiKey, secret: value, handshakeField: handshakeFieldAccessToken}, true
	}
	dot := strings.Index(value, ".")
	if dot <= 0 || dot != strings.LastIndex(value, ".") {
		return parsedCredential{}, false
	}
	apiKeyID, secret := value[:dot], value[dot+1:]
	if strings.TrimSpace(apiKeyID) == "" || strings.TrimSpace(secret) == "" {
		return parsedCredential{}, false
	}
	return parsedCredential{apiKeyID: apiKeyID, secret: secret, handshakeField: handshakeFieldAPIKey}, true
}
func signingPrivate(apiID, secret, value string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) <= 28 {
		return nil, &Error{Kind: ErrFormat}
	}
	key, err := derive(secret, "ed25519_priv")
	if err != nil {
		return nil, err
	}
	defer clear(key)
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(aesBlock)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], []byte(apiID))
	if err != nil {
		return nil, &Error{Kind: ErrFormat}
	}
	defer clear(plain)
	pkcs, err := base64.StdEncoding.DecodeString(string(plain))
	if err != nil {
		return nil, &Error{Kind: ErrFormat}
	}
	defer clear(pkcs)
	parsed, err := x509.ParsePKCS8PrivateKey(pkcs)
	if err != nil {
		return nil, &Error{Kind: ErrFormat}
	}
	private, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, &Error{Kind: ErrFormat}
	}
	return private, nil
}
func randomHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func proofOfWork(ctx context.Context, id, session, ts string) (string, error) {
	digest := sha256.Sum256([]byte(id + "\nzcode\n" + session + "\n" + ts))
	seed := hex.EncodeToString(digest[:])[:32]
	nonce := randomHex(12)
	for i := uint64(0); i <= 0xffffffff; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		answer := fmt.Sprintf("%s%08x", nonce, i)
		hash := sha256.Sum256([]byte(seed + "\n" + answer))
		if hash[0] == 0 {
			return answer, nil
		}
	}
	return "", &Error{Kind: ErrUnavailable}
}
func (s *Signer) Invalidate(origin, key string, bypass bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.states[signingID(origin, key)]; state != nil {
		state.key = nil
		state.bypass = bypass
	}
}

// resolveCredential 优先使用 JWT 形态；bigmodel OAuth 账号会铸造并缓存 project token。
func (s *Signer) resolveCredential(ctx context.Context, c *Client, stateKey string, direct *parsedCredential, cred SigningCredential) *parsedCredential {
	if direct != nil && direct.handshakeField == handshakeFieldAccessToken {
		return direct
	}
	if cred.Provider != BigModel || strings.TrimSpace(cred.OAuth) == "" || direct == nil {
		return direct
	}
	s.mu.Lock()
	state := s.states[stateKey]
	now := time.Now()
	if state.projectJWT != "" {
		if now.Before(state.projectExp.Add(-5 * time.Minute)) {
			if cached, ok := parseSigningCredential(state.projectJWT); ok {
				s.mu.Unlock()
				return &cached
			}
			state.projectJWT = ""
		}
	}
	if now.Before(state.mintNegUntil) {
		s.mu.Unlock()
		return direct
	}
	s.mu.Unlock()
	result, _, _ := s.flight.Do(stateKey+"|mint", func() (any, error) {
		jwt, exp, ok := s.mintProjectToken(ctx, c, cred.OAuth, direct.apiKeyID)
		s.mu.Lock()
		defer s.mu.Unlock()
		state := s.states[stateKey]
		if state == nil {
			return nil, nil
		}
		if !ok {
			state.mintNegUntil = time.Now().Add(time.Minute)
			return nil, nil
		}
		parsed, valid := parseSigningCredential(jwt)
		if !valid {
			state.mintNegUntil = time.Now().Add(time.Minute)
			return nil, nil
		}
		// 握手私钥与旧 secret 绑定，换 token 后必须重新握手。
		state.projectJWT = jwt
		state.projectExp = exp
		state.key = nil
		return parsed, nil
	})
	if minted, ok := result.(parsedCredential); ok {
		return &minted
	}
	return direct
}

// mintProjectToken 依次读取 bigmodel 机构/项目列表并调用 access_tokens 铸造 project JWT。
func (s *Signer) mintProjectToken(ctx context.Context, c *Client, oauth, apiKeyID string) (string, time.Time, bool) {
	bare := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(oauth), "Bearer "))
	if bare == "" || apiKeyID == "" {
		return "", time.Time{}, false
	}
	headers := bearer(bare)
	headers.Set("Content-Type", "application/json")
	data, err := c.request(ctx, "GET", "https://bigmodel.cn/api/biz/customer/getCustomerInfo", headers, nil, 15*time.Second)
	if err != nil {
		return "", time.Time{}, false
	}
	var info struct {
		Organizations []struct {
			OrganizationID   string `json:"organizationId"`
			OrganizationName string `json:"organizationName"`
			Projects         []struct {
				ProjectID   string `json:"projectId"`
				ProjectName string `json:"projectName"`
				ProjectType any    `json:"projectType"`
			} `json:"projects"`
		} `json:"organizations"`
	}
	if json.Unmarshal(data, &info) != nil {
		return "", time.Time{}, false
	}
	type project struct {
		orgID, orgName, id, name string
	}
	var candidates []project
	for _, org := range info.Organizations {
		if org.OrganizationID == "" {
			continue
		}
		for _, item := range org.Projects {
			if item.ProjectID == "" || strings.TrimSpace(fmt.Sprint(item.ProjectType)) == "2" {
				continue
			}
			candidates = append(candidates, project{orgID: org.OrganizationID, orgName: org.OrganizationName, id: item.ProjectID, name: item.ProjectName})
		}
	}
	if len(candidates) == 0 {
		return "", time.Time{}, false
	}
	picked := candidates[0]
	for _, candidate := range candidates {
		if strings.Contains(candidate.orgName, "默认机构") && strings.Contains(candidate.name, "默认项目") {
			picked = candidate
			break
		}
	}
	endpoint := "https://bigmodel.cn/api/biz/v1/organization/" + url.PathEscape(picked.orgID) +
		"/projects/" + url.PathEscape(picked.id) + "/api_keys/" + url.PathEscape(apiKeyID) + "/access_tokens"
	payload, err := c.request(ctx, "POST", endpoint, headers, map[string]string{"clientType": projectTokenClientType, "clientVersion": DefaultAppVersion}, 15*time.Second)
	if err != nil {
		return "", time.Time{}, false
	}
	var token struct {
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(payload, &token) != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", time.Time{}, false
	}
	exp := time.Now().Add(55 * time.Minute)
	if segments := strings.Split(token.AccessToken, "."); len(segments) == 3 {
		if claims, err := decodeJWTPart(segments[1]); err == nil {
			if value, ok := claims["exp"].(float64); ok && value > 0 {
				exp = time.Unix(int64(value), 0)
			}
		}
	}
	return token.AccessToken, exp, true
}

func (s *Signer) Sign(ctx context.Context, c *Client, request *http.Request, cred SigningCredential) (bool, error) {
	if request.URL.Scheme != "https" || request.URL.Path == StartMessagesPath || request.URL.Path == "/api/v1/off-peak/anthropic/v1/messages" {
		return false, nil
	}
	if request.Header.Get("X-Session-Id") == "" || strings.TrimSpace(cred.Key) == "" {
		return false, nil
	}
	parsed, valid := parseSigningCredential(cred.Key)
	if !valid {
		return false, nil
	}
	origin := request.URL.Scheme + "://" + request.URL.Host
	cacheKey := signingID(origin, cred.Key)
	s.mu.Lock()
	state := s.states[cacheKey]
	if state == nil {
		state = &signingState{}
		s.states[cacheKey] = state
	}
	bypass := state.bypass
	s.mu.Unlock()
	if bypass {
		return false, nil
	}
	resolved := s.resolveCredential(ctx, c, cacheKey, &parsed, cred)
	if resolved == nil {
		return false, nil
	}
	parsed = *resolved

	result := s.flight.DoChan(cacheKey, func() (any, error) {
		s.mu.Lock()
		private := state.key
		s.mu.Unlock()
		if len(private) > 0 {
			return private, nil
		}
		ts, nonce := strconv.FormatInt(time.Now().UnixMilli(), 10), randomHex(16)
		signingKey, err := derive(parsed.secret, "getSignKey_hmac")
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, signingKey)
		_, _ = mac.Write([]byte("get_sign_key\n" + parsed.apiKeyID + "\n" + ts + "\n" + nonce))
		clear(signingKey)
		handshake := map[string]string{"nonce": nonce, "sig": base64.StdEncoding.EncodeToString(mac.Sum(nil)), "ts": ts}
		handshake[parsed.handshakeField] = parsed.secret
		h := http.Header{"Authorization": []string{cred.Key}, "Content-Type": []string{"application/json"}}
		data, err := c.request(ctx, "POST", origin+"/api/paas/c1f3a7e2/v2/client", h, handshake, 10*time.Second)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Cipher string `json:"privateCipher"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			return nil, &Error{Kind: ErrFormat}
		}
		private, err = signingPrivate(parsed.apiKeyID, parsed.secret, envelope.Cipher)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		state.key = private
		s.mu.Unlock()
		return private, nil
	})
	var private ed25519.PrivateKey
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case outcome := <-result:
		if outcome.Err != nil {
			return false, nil
		}
		private, _ = outcome.Val.(ed25519.PrivateKey)
	}
	if len(private) == 0 {
		return false, nil
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := randomHex(16)
	session := request.Header.Get("X-Session-Id")
	pow, err := proofOfWork(ctx, parsed.apiKeyID, session, ts)
	if err != nil {
		return false, err
	}
	message := strings.Join([]string{parsed.apiKeyID, ts, c.Identity.Version(), session, nonce}, "\n")
	request.Header.Set("X-Client-Ts", ts)
	request.Header.Set("X-Client-Version", c.Identity.Version())
	request.Header.Set("X-Client-Sig", base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(message))))
	request.Header.Set("X-Client-Nonce", nonce)
	request.Header.Set("X-App-Id", "zcode")
	request.Header.Set("X-Client-Pow", pow)
	return true, nil
}
func VerifyFailure(status int, body []byte) bool {
	if status != 401 {
		return false
	}
	var d map[string]any
	if json.Unmarshal(body, &d) != nil {
		return false
	}
	candidates := []any{d["msg"], d["reason"]}
	if data, ok := d["data"].(map[string]any); ok {
		candidates = append(candidates, data["reason"])
	}
	if e, ok := d["error"].(map[string]any); ok {
		candidates = append(candidates, e["reason"], e["message"])
	}
	for _, v := range candidates {
		if v == "VERIFY_SIGNATURE_INVALID" || v == "VERIFY_APIKEY_EXPIRED" {
			return true
		}
	}
	return false
}
func requestOrigin(u *url.URL) string { return u.Scheme + "://" + u.Host }
