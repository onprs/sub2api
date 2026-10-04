package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/zcode"
	"github.com/google/uuid"
	"net/http"
	"sync"
	"time"
)

type ZCodeLoginInput struct {
	Provider  zcode.Provider `json:"provider"`
	Plan      string         `json:"plan"`
	ProxyID   *int64         `json:"proxy_id"`
	AccountID int64          `json:"account_id"`
}
type ZCodeLoginResult struct {
	SessionID    string `json:"session_id"`
	AuthorizeURL string `json:"authorize_url,omitempty"`
	Status       string `json:"status"`
	ExpiresAt    int64  `json:"expires_at"`
	PollInterval int    `json:"poll_interval"`
	Plan         string `json:"plan"`
}
type zcodeLoginSession struct {
	Flow      zcode.LoginFlow `json:"flow"`
	Actor     string          `json:"actor"`
	AccountID int64           `json:"account_id"`
	Plan      string          `json:"plan"`
	ProxyID   *int64          `json:"proxy_id"`
	Device    string          `json:"device"`
	NextPoll  int64           `json:"next_poll"`
	Tokens    *zcode.Tokens   `json:"tokens,omitempty"`
}

func sessionStoreKey(id string) string {
	digest := sha256.Sum256([]byte(id))
	return "oauth:" + hex.EncodeToString(digest[:])
}
func (s *ZCodeService) saveSession(ctx context.Context, id string, session *zcodeLoginSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	encrypted, err := s.cipher.Encrypt(string(data))
	if err != nil {
		return &zcode.Error{Kind: zcode.ErrConfiguration}
	}
	ttl := time.Until(time.Unix(session.Flow.ExpiresAt, 0))
	if ttl <= 0 {
		return &zcode.Error{Kind: zcode.ErrLogin}
	}
	return s.store.Put(ctx, sessionStoreKey(id), encrypted, ttl)
}
func (s *ZCodeService) readSession(ctx context.Context, id, actor string, take bool) (*zcodeLoginSession, error) {
	if len(id) != 64 {
		return nil, infraerrors.BadRequest("ZCODE_SESSION_INVALID", "授权会话无效或已过期")
	}
	var encrypted string
	var err error
	if take {
		encrypted, err = s.store.Take(ctx, sessionStoreKey(id))
	} else {
		encrypted, err = s.store.Get(ctx, sessionStoreKey(id))
	}
	if err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZCODE_SESSION_UNAVAILABLE", "授权会话存储暂时不可用")
	}
	plain, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, infraerrors.BadRequest("ZCODE_SESSION_INVALID", "授权会话无效或已过期")
	}
	var session zcodeLoginSession
	if json.Unmarshal([]byte(plain), &session) != nil || session.Actor != actor || session.Flow.ExpiresAt <= time.Now().Unix() {
		return nil, infraerrors.BadRequest("ZCODE_SESSION_INVALID", "授权会话无效或已过期")
	}
	return &session, nil
}
func (s *ZCodeService) StartLogin(ctx context.Context, actor string, input ZCodeLoginInput) (*ZCodeLoginResult, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	if !input.Provider.Valid() || (input.Plan != AccountModeStart && input.Plan != AccountModeCoding) {
		return nil, infraerrors.BadRequest("ZCODE_LOGIN_INVALID", "请选择有效的授权来源和套餐")
	}
	if input.AccountID > 0 {
		a, err := s.repo.GetByID(ctx, input.AccountID)
		if err != nil {
			return nil, err
		}
		if a.Platform != PlatformZhipu {
			return nil, infraerrors.BadRequest("ZCODE_ACCOUNT_INVALID", "ZCode OAuth 仅用于智谱账号")
		}
	}
	actorDigest := sha256.Sum256([]byte(actor))
	ok, err := s.store.Acquire(ctx, "oauth-start:"+hex.EncodeToString(actorDigest[:]), uuid.NewString(), 10*time.Second)
	if err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZCODE_SESSION_UNAVAILABLE", "授权会话存储暂时不可用")
	}
	if !ok {
		return nil, infraerrors.New(http.StatusTooManyRequests, "ZCODE_LOGIN_RATE_LIMIT", "请稍后再发起授权")
	}
	proxy, err := s.proxyURL(ctx, input.ProxyID)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	device := uuid.NewString()
	a := &Account{Credentials: map[string]any{"zcode_device_id": device}}
	client := s.client(a, proxy)
	flow, authorize, err := client.StartLogin(ctx, input.Provider)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	id, err := zcode.RandomToken()
	if err != nil {
		return nil, err
	}
	session := &zcodeLoginSession{Flow: flow, Actor: actor, AccountID: input.AccountID, Plan: input.Plan, ProxyID: input.ProxyID, Device: device}
	if err = s.saveSession(ctx, id, session); err != nil {
		return nil, zcodeAdminError(err)
	}
	return &ZCodeLoginResult{SessionID: id, AuthorizeURL: authorize, Status: "pending", ExpiresAt: flow.ExpiresAt, PollInterval: flow.Interval, Plan: input.Plan}, nil
}
func (s *ZCodeService) PollLogin(ctx context.Context, actor, id string) (*ZCodeLoginResult, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	owner := uuid.NewString()
	lockKey := "poll:" + sessionStoreKey(id)
	locked, err := s.store.Acquire(ctx, lockKey, owner, time.Minute)
	if err != nil || !locked {
		return nil, infraerrors.New(http.StatusTooManyRequests, "ZCODE_POLL_BUSY", "授权正在检查，请稍后再试")
	}
	defer s.release(lockKey, owner)
	session, err := s.readSession(ctx, id, actor, false)
	if err != nil {
		return nil, err
	}
	result := &ZCodeLoginResult{SessionID: id, Status: "pending", ExpiresAt: session.Flow.ExpiresAt, PollInterval: session.Flow.Interval, Plan: session.Plan}
	if session.Tokens != nil {
		result.Status = "ready"
		return result, nil
	}
	if session.NextPoll > time.Now().Unix() {
		return result, nil
	}
	session.NextPoll = time.Now().Add(time.Duration(session.Flow.Interval) * time.Second).Unix()
	if err = s.saveSession(ctx, id, session); err != nil {
		return nil, zcodeAdminError(err)
	}
	proxy, err := s.proxyURL(ctx, session.ProxyID)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	client := s.client(&Account{Credentials: map[string]any{"zcode_device_id": session.Device}}, proxy)
	tokens, err := client.PollLogin(ctx, session.Flow)
	if err != nil {
		return nil, zcodeAdminError(err)
	}
	if tokens == nil {
		return result, nil
	}
	if session.Plan == AccountModeStart && tokens.JWT == "" {
		return nil, infraerrors.BadRequest("ZCODE_JWT_MISSING", "此授权未返回 Start Plan JWT，请重新登录")
	}
	if session.Plan == AccountModeCoding {
		tokens.CodingKey, err = client.ResolveCodingKey(ctx, session.Flow.Provider, tokens.AccessToken)
		if err != nil {
			return nil, zcodeAdminError(err)
		}
	}
	session.Tokens = tokens
	if err = s.saveSession(ctx, id, session); err != nil {
		return nil, zcodeAdminError(err)
	}
	result.Status = "ready"
	return result, nil
}
func (s *ZCodeService) release(key, owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.store.Release(ctx, key, owner)
}

// PrepareCredentials 为创建/编辑保留会话；数据库提交成功才消费，失败后可重试。
func (s *ZCodeService) PrepareCredentials(ctx context.Context, actor string, accountID int64, creds map[string]any) (map[string]any, func(bool), error) {
	id, _ := creds["zcode_oauth_session_id"].(string)
	if id == "" {
		return creds, func(bool) {}, nil
	}
	if err := s.available(); err != nil {
		return nil, nil, err
	}
	owner := uuid.NewString()
	key := "consume:" + sessionStoreKey(id)
	locked, err := s.store.Acquire(ctx, key, owner, 2*time.Minute)
	if err != nil || !locked {
		return nil, nil, infraerrors.New(http.StatusConflict, "ZCODE_SESSION_BUSY", "授权会话正在保存，请稍后再试")
	}
	fail := func(err error) (map[string]any, func(bool), error) { s.release(key, owner); return nil, nil, err }
	session, err := s.readSession(ctx, id, actor, false)
	if err != nil {
		return fail(err)
	}
	if session.AccountID != accountID || session.Tokens == nil {
		return fail(infraerrors.BadRequest("ZCODE_SESSION_INVALID", "授权会话未就绪或目标账号不匹配"))
	}
	raw, _ := json.Marshal(session.Tokens)
	encrypted, err := s.cipher.Encrypt(string(raw))
	if err != nil {
		return fail(zcodeAdminError(&zcode.Error{Kind: zcode.ErrConfiguration}))
	}
	out := shallowCopyMap(creds)
	delete(out, "zcode_oauth_session_id")
	out["auth_mode"] = "zcode_oauth"
	out["account_mode"] = session.Plan
	out["zcode_provider"] = string(session.Flow.Provider)
	out["zcode_device_id"] = session.Device
	out[zcodeTokensKey] = encrypted
	out["api_protocol"] = APIProtocolAnthropic
	if _, ok := out["zcode_auto_claim"]; !ok {
		out["zcode_auto_claim"] = false
	}
	once := sync.Once{}
	finish := func(committed bool) {
		once.Do(func() {
			if committed {
				cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, _ = s.store.Take(cleanCtx, sessionStoreKey(id))
			}
			s.release(key, owner)
		})
	}
	return out, finish, nil
}
func (s *ZCodeService) OnAuthorized(ctx context.Context, a *Account) error {
	if !a.IsZCodeOAuth() {
		return nil
	}
	if err := s.repo.UpdateExtra(ctx, a.ID, map[string]any{zcodeStateKey: map[string]any{"oauth_status": "authorized", "checked_at": time.Now().Unix()}, zcodeQuotaKey: nil, zcodeClaimKey: nil}); err != nil {
		return err
	}
	s.clearOwnCooldown(ctx, a)
	return nil
}
