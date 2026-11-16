package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildChatCompletionsDigestChain_PrefixGrowth(t *testing.T) {
	r1 := []byte(`{"model":"gemini-3.8-flash","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	r2 := []byte(`{"model":"gemini-3.8-flash","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"next"}]}`)

	c1 := BuildChatCompletionsDigestChain(r1)
	c2 := BuildChatCompletionsDigestChain(r2)
	require.NotEmpty(t, c1)
	require.Contains(t, c2, c1+"-")

	store := NewDigestSessionStore()
	defer store.Close()
	prefix := GenerateChatCompletionsPrefixHash(1, 15, "magpie/0.1.4303", "antigravity", "gemini-3.8-flash")
	store.Save(8, prefix, c1, "uuid-1", 8305, "")

	uuid, accID, matched, found := store.Find(8, prefix, c2)
	require.True(t, found)
	require.Equal(t, "uuid-1", uuid)
	require.Equal(t, int64(8305), accID)
	require.Equal(t, c1, matched)
}
