// Package metadata provides a bounded cross-request store for opaque provider
// replay metadata such as thought signatures. Callers must supply a tenant and
// account scoped key; the store never infers identity from request content.
package metadata

import (
	"container/list"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
)

const (
	DefaultTTL     = 30 * time.Minute
	DefaultMaxSize = 10_000
)

// Scope and Key alias the root package contract so Store can satisfy the
// Pipeline's narrow metadata interface without creating an import cycle.
type Scope = protocolconv.MetadataScope
type Key = protocolconv.MetadataKey

// Clock allows deterministic expiration tests.
type Clock func() time.Time

// Store is a concurrency-safe TTL-bounded LRU store.
type Store struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	now     Clock
	entries map[Key]*list.Element
	lru     *list.List
}

type entry struct {
	key       Key
	data      map[string]json.RawMessage
	expiresAt time.Time
}

// NewStore creates a bounded store. Non-positive values use conservative
// defaults.
func NewStore(ttl time.Duration, maxSize int) *Store {
	return NewStoreWithClock(ttl, maxSize, time.Now)
}

// NewStoreWithClock creates a store with an injected clock.
func NewStoreWithClock(ttl time.Duration, maxSize int, now Clock) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}
	if now == nil {
		now = time.Now
	}
	return &Store{
		ttl:     ttl,
		maxSize: maxSize,
		now:     now,
		entries: make(map[Key]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// Put stores a detached copy. Existing keys are replaced and refreshed.
func (s *Store) Put(key Key, data map[string]json.RawMessage) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("provider metadata is empty")
	}
	cloned := cloneMetadata(data)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpiredLocked(now)
	if element := s.entries[key]; element != nil {
		if value, ok := element.Value.(*entry); ok {
			value.data = cloned
			value.expiresAt = now.Add(s.ttl)
			s.lru.MoveToBack(element)
			return nil
		}
		s.removeElementLocked(element)
	}
	for len(s.entries) >= s.maxSize {
		s.removeElementLocked(s.lru.Front())
	}
	value := &entry{key: key, data: cloned, expiresAt: now.Add(s.ttl)}
	element := s.lru.PushBack(value)
	s.entries[key] = element
	return nil
}

// Get returns a detached copy. Reads refresh LRU order but do not extend TTL.
func (s *Store) Get(key Key) (map[string]json.RawMessage, bool) {
	if validateKey(key) != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpiredLocked(now)
	element := s.entries[key]
	if element == nil {
		return nil, false
	}
	value, ok := element.Value.(*entry)
	if !ok {
		s.removeElementLocked(element)
		return nil, false
	}
	s.lru.MoveToBack(element)
	return cloneMetadata(value.data), true
}

// DeleteScope removes every entry in an exact scope. Provider failover and
// account invalidation use this to prevent replay against a different account.
func (s *Store) DeleteScope(scope Scope) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for element := s.lru.Front(); element != nil; {
		next := element.Next()
		value, ok := element.Value.(*entry)
		if !ok || value.key.Scope == scope {
			s.removeElementLocked(element)
			removed++
		}
		element = next
	}
	return removed
}

// Len returns the live entry count after expiration.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(s.now())
	return len(s.entries)
}

func (s *Store) evictExpiredLocked(now time.Time) {
	for element := s.lru.Front(); element != nil; {
		next := element.Next()
		value, ok := element.Value.(*entry)
		if !ok || !now.Before(value.expiresAt) {
			s.removeElementLocked(element)
		}
		element = next
	}
}

func (s *Store) removeElementLocked(element *list.Element) {
	if element == nil {
		return
	}
	if value, ok := element.Value.(*entry); ok {
		delete(s.entries, value.key)
	} else {
		for key, candidate := range s.entries {
			if candidate == element {
				delete(s.entries, key)
				break
			}
		}
	}
	s.lru.Remove(element)
}

func validateKey(key Key) error {
	if err := key.Scope.Validate(); err != nil {
		return err
	}
	if key.ToolCallID == "" {
		return errors.New("metadata tool call ID is required")
	}
	return nil
}

func cloneMetadata(data map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(data))
	for key, value := range data {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
