package zcode

// 来源：src/proxy/client-signing.ts，V4 的签名字段使用换行拼接，HKDF/AES-GCM/Ed25519。
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

type signingState struct {
	gateUntil time.Time
	enabled   bool
	key       ed25519.PrivateKey
	bypass    bool
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
func (s *Signer) Sign(ctx context.Context, c *Client, request *http.Request, key string) (bool, error) {
	if request.URL.Scheme != "https" || request.URL.Path == StartMessagesPath || request.URL.Path == "/api/v1/off-peak/anthropic/v1/messages" {
		return false, nil
	}
	parts := strings.Split(key, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || request.Header.Get("X-Session-Id") == "" {
		return false, nil
	}
	origin := request.URL.Scheme + "://" + request.URL.Host
	cacheKey := signingID(origin, key)
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
	ready := s.flight.DoChan(cacheKey, func() (any, error) {
		s.mu.Lock()
		fresh := time.Now().Before(state.gateUntil)
		enabled := state.enabled
		private := state.key
		s.mu.Unlock()
		if !fresh {
			h := c.Identity.Headers(true)
			h.Set("X-Api-Key", key)
			data, err := c.request(ctx, "GET", c.OriginURL()+"/api/v1/agent/configs", h, nil, 3*time.Second)
			var gate struct {
				Signature struct {
					Enabled bool `json:"enable"`
				} `json:"codingPlanSignature"`
			}
			if err == nil {
				err = json.Unmarshal(data, &gate)
			}
			enabled = err == nil && gate.Signature.Enabled
			ttl := time.Hour
			if err != nil {
				ttl = 30 * time.Second
			}
			s.mu.Lock()
			state.enabled = enabled
			state.gateUntil = time.Now().Add(ttl)
			s.mu.Unlock()
		}
		if !enabled {
			return ed25519.PrivateKey(nil), nil
		}
		if len(private) > 0 {
			return private, nil
		}
		ts, nonce := strconv.FormatInt(time.Now().UnixMilli(), 10), randomHex(16)
		signingKey, err := derive(parts[1], "getSignKey_hmac")
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, signingKey)
		_, _ = mac.Write([]byte("get_sign_key\n" + parts[0] + "\n" + ts + "\n" + nonce))
		clear(signingKey)
		h := http.Header{"Authorization": []string{key}, "Content-Type": []string{"application/json"}}
		data, err := c.request(ctx, "POST", origin+"/api/paas/c1f3a7e2/v2/client", h, map[string]string{"apiKey": key, "nonce": nonce, "sig": base64.StdEncoding.EncodeToString(mac.Sum(nil)), "ts": ts}, 10*time.Second)
		if err != nil {
			return nil, err
		}
		var handshake struct {
			Cipher string `json:"privateCipher"`
		}
		if json.Unmarshal(data, &handshake) != nil {
			return nil, &Error{Kind: ErrFormat}
		}
		private, err = signingPrivate(parts[0], parts[1], handshake.Cipher)
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
	case result := <-ready:
		if result.Err != nil {
			return false, nil
		}
		private, _ = result.Val.(ed25519.PrivateKey)
	}
	if len(private) == 0 {
		return false, nil
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := randomHex(16)
	session := request.Header.Get("X-Session-Id")
	pow, err := proofOfWork(ctx, parts[0], session, ts)
	if err != nil {
		return false, err
	}
	message := strings.Join([]string{parts[0], ts, c.Identity.Version(), session, nonce}, "\n")
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
