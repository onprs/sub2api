package anthropic

import (
	"crypto/rand"
	"sync/atomic"
	"time"
)

const anthropicMessageIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var anthropicMessageIDFallbackCounter atomic.Uint64

func generateAnthropicMessageID() string {
	const randomLength = 22

	randomBytes := make([]byte, randomLength)
	if _, err := rand.Read(randomBytes); err != nil {
		seed := uint64(time.Now().UnixNano()) ^ anthropicMessageIDFallbackCounter.Add(1)
		for i := range randomBytes {
			seed ^= seed << 13
			seed ^= seed >> 7
			seed ^= seed << 17
			randomBytes[i] = byte(seed)
		}
	}

	encoded := make([]byte, randomLength)
	for i, value := range randomBytes {
		encoded[i] = anthropicMessageIDAlphabet[int(value)%len(anthropicMessageIDAlphabet)]
	}
	return "msg_01" + string(encoded)
}
