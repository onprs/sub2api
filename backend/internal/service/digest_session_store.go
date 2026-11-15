package service

import (
	"strconv"
	"strings"
	"time"
)

// digestSessionTTL 摘要会话默认 TTL。
// 必须覆盖 agent 会话的两轮间隔（工具运行、用户阅读常达数分钟）：TTL 过期后
// digest Find 不再命中，粘性绑定丢失，跨账号切换会改写 thoughtSignature 并
// 打断上游隐式缓存前缀链。与 gateway 侧 stickySessionTTL（1 小时）保持一致。
const digestSessionTTL = time.Hour

// sessionEntry flat cache 条目
type sessionEntry struct {
	uuid      string
	accountID int64
	expiresAt time.Time
}

func (e *sessionEntry) sessionExpiresAt() time.Time { return e.expiresAt }
func (e *sessionEntry) sessionCost() int            { return len(e.uuid) }
func (e *sessionEntry) cloneSessionValue() sessionMemoryValue {
	cloned := *e
	cloned.uuid = strings.Clone(e.uuid)
	return &cloned
}

// DigestSessionStore 内存摘要会话存储（有界 TTL 缓存，增量摘要索引）
// key: "{groupID}:{prefixHash}|{digestChain}" → *sessionEntry
type DigestSessionStore struct {
	cache sessionMemoryCache
	ttl   time.Duration
}

// NewDigestSessionStore 创建内存摘要会话存储
func NewDigestSessionStore() *DigestSessionStore {
	return &DigestSessionStore{
		ttl: digestSessionTTL,
	}
}

func (s *DigestSessionStore) Close() { s.cache.Close() }

// Save 保存摘要会话。oldDigestChain 为 Find 返回的 matchedChain，用于删旧 key。
func (s *DigestSessionStore) Save(groupID int64, prefixHash, digestChain, uuid string, accountID int64, oldDigestChain string) {
	if digestChain == "" {
		return
	}
	ns := buildNS(groupID, prefixHash)
	ttl := s.ttl
	if ttl <= 0 {
		ttl = digestSessionTTL
	}
	s.cache.Store(ns+digestChain, &sessionEntry{uuid: uuid, accountID: accountID, expiresAt: time.Now().Add(ttl)})
	if oldDigestChain != "" && oldDigestChain != digestChain {
		s.cache.Delete(ns + oldDigestChain)
	}
}

// Find 查找摘要会话，在消息边界上返回最长匹配及对应 matchedChain。
func (s *DigestSessionStore) Find(groupID int64, prefixHash, digestChain string) (uuid string, accountID int64, matchedChain string, found bool) {
	if digestChain == "" {
		return "", 0, "", false
	}
	ns := buildNS(groupID, prefixHash)
	if val, chain, ok := s.cache.longestDigestPrefix(ns, digestChain); ok {
		if e, ok := val.(*sessionEntry); ok {
			return e.uuid, e.accountID, chain, true
		}
	}
	return "", 0, "", false
}

// buildNS 构建 namespace 前缀
func buildNS(groupID int64, prefixHash string) string {
	return strconv.FormatInt(groupID, 10) + ":" + prefixHash + "|"
}
