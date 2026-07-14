package metadata

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/protocolconv"
	"github.com/stretchr/testify/require"
)

func TestStoreScopesIdenticalToolCallIDs(t *testing.T) {
	store := NewStore(time.Minute, 10)
	base := Scope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: protocolconv.ProtocolGoogleGenAI}
	first := Key{Scope: base, ToolCallID: "call-1"}
	secondScope := base
	secondScope.TenantID = 9
	second := Key{Scope: secondScope, ToolCallID: "call-1"}

	require.NoError(t, store.Put(first, map[string]json.RawMessage{"signature": json.RawMessage(`"tenant-1"`)}))
	require.NoError(t, store.Put(second, map[string]json.RawMessage{"signature": json.RawMessage(`"tenant-9"`)}))

	gotFirst, ok := store.Get(first)
	require.True(t, ok)
	require.JSONEq(t, `"tenant-1"`, string(gotFirst["signature"]))
	gotSecond, ok := store.Get(second)
	require.True(t, ok)
	require.JSONEq(t, `"tenant-9"`, string(gotSecond["signature"]))
}

func TestStoreAccountSwitchAndProtocolSwitchMiss(t *testing.T) {
	store := NewStore(time.Minute, 10)
	scope := Scope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: protocolconv.ProtocolGoogleGenAI}
	key := Key{Scope: scope, ToolCallID: "call-1"}
	require.NoError(t, store.Put(key, map[string]json.RawMessage{"signature": json.RawMessage(`"sig"`)}))

	switchedAccount := key
	switchedAccount.Scope.AccountID = 5
	_, ok := store.Get(switchedAccount)
	require.False(t, ok)

	switchedProtocol := key
	switchedProtocol.Scope.Protocol = protocolconv.ProtocolAnthropic
	_, ok = store.Get(switchedProtocol)
	require.False(t, ok)

	require.Equal(t, 1, store.DeleteScope(scope))
	_, ok = store.Get(key)
	require.False(t, ok)
}

func TestStoreExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Unix(100, 0)
	store := NewStoreWithClock(time.Minute, 2, func() time.Time { return now })
	scope := Scope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: protocolconv.ProtocolGoogleGenAI}
	key := func(id string) Key { return Key{Scope: scope, ToolCallID: id} }
	data := func(value string) map[string]json.RawMessage {
		return map[string]json.RawMessage{"value": json.RawMessage(fmt.Sprintf("%q", value))}
	}

	require.NoError(t, store.Put(key("a"), data("a")))
	require.NoError(t, store.Put(key("b"), data("b")))
	_, ok := store.Get(key("a"))
	require.True(t, ok)
	require.NoError(t, store.Put(key("c"), data("c")))
	_, ok = store.Get(key("b"))
	require.False(t, ok, "b is the least recently used entry")

	now = now.Add(time.Minute)
	_, ok = store.Get(key("a"))
	require.False(t, ok)
	require.Zero(t, store.Len())
}

func TestStoreReturnsDetachedMetadata(t *testing.T) {
	store := NewStore(time.Minute, 1)
	key := Key{Scope: Scope{TenantID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, Protocol: protocolconv.ProtocolGoogleGenAI}, ToolCallID: "call-1"}
	input := map[string]json.RawMessage{"signature": json.RawMessage(`"original"`)}
	require.NoError(t, store.Put(key, input))
	input["signature"][1] = 'X'

	first, ok := store.Get(key)
	require.True(t, ok)
	require.JSONEq(t, `"original"`, string(first["signature"]))
	first["signature"][1] = 'Y'
	second, ok := store.Get(key)
	require.True(t, ok)
	require.JSONEq(t, `"original"`, string(second["signature"]))
}

func TestStoreConcurrentIsolation(t *testing.T) {
	store := NewStore(time.Minute, 512)
	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			key := Key{
				Scope: Scope{
					TenantID:  int64(i + 1),
					APIKeyID:  int64(i + 100),
					GroupID:   int64(i + 200),
					AccountID: int64(i + 300),
					Protocol:  protocolconv.ProtocolGoogleGenAI,
				},
				ToolCallID: "shared-call-id",
			}
			value := fmt.Sprintf("signature-%d", i)
			require.NoError(t, store.Put(key, map[string]json.RawMessage{"signature": json.RawMessage(fmt.Sprintf("%q", value))}))
			got, ok := store.Get(key)
			require.True(t, ok)
			require.JSONEq(t, fmt.Sprintf("%q", value), string(got["signature"]))
		}(i)
	}
	wg.Wait()
	require.Equal(t, workers, store.Len())
}

func TestStoreRejectsIncompleteScope(t *testing.T) {
	store := NewStore(time.Minute, 1)
	err := store.Put(Key{ToolCallID: "call-1"}, map[string]json.RawMessage{"value": json.RawMessage(`1`)})
	require.ErrorContains(t, err, "tenant ID")
}
