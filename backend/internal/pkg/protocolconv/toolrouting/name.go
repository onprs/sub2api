// Package toolrouting defines protocol-neutral naming helpers used when a
// target protocol cannot represent tool namespaces directly.
package toolrouting

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const ChatToolNameMaxLen = 64

// FlattenNamespaceName creates the stable Chat Completions proxy name for one
// namespaced tool. Long names retain a deterministic hash suffix.
func FlattenNamespaceName(namespace, name string) string {
	full := namespace + "__" + name
	if len(full) <= ChatToolNameMaxLen {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	suffix := "__" + hex.EncodeToString(sum[:4])
	prefixLen := ChatToolNameMaxLen - len(suffix)
	var prefix strings.Builder
	for _, ch := range full {
		if prefix.Len()+len(string(ch)) > prefixLen {
			break
		}
		_, _ = prefix.WriteRune(ch)
	}
	return prefix.String() + suffix
}
