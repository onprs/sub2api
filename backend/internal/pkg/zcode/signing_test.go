package zcode

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestSigningCrossLanguageVectors(t *testing.T) {
	raw, e := os.ReadFile("testdata/signing.json")
	require.NoError(t, e)
	var f map[string]string
	require.NoError(t, json.Unmarshal(raw, &f))
	require.Equal(t, UpstreamCommit, f["upstream_commit"])
	key, e := derive(f["secret"], "ed25519_priv")
	require.NoError(t, e)
	require.Equal(t, f["hkdf_private_hex"], hex.EncodeToString(key))
	private, e := signingPrivate(f["id"], f["secret"], f["private_cipher"])
	require.NoError(t, e)
	seed, e := hex.DecodeString(f["seed_hex"])
	require.NoError(t, e)
	require.Equal(t, ed25519.NewKeyFromSeed(seed), private)
	require.Equal(t, f["signature"], base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(f["message"]))))
	digest := sha256.Sum256([]byte(f["pow_seed"] + "\n" + f["pow"]))
	require.Zero(t, digest[0])
	_, e = signingPrivate("wrong-id", f["secret"], f["private_cipher"])
	require.Error(t, e)
}
func TestSigningHeadersAndProofOfWork(t *testing.T) {
	seed := make([]byte, 32)
	private := ed25519.NewKeyFromSeed(seed)
	signer := NewSigner()
	client := &Client{Identity: DefaultIdentity("", "synthetic-device")}
	key := "synthetic-id.synthetic-secret"
	origin := "https://api.z.ai"
	signer.states[signingID(origin, key)] = &signingState{enabled: true, gateUntil: time.Now().Add(time.Hour), key: private}
	request, _ := http.NewRequest("POST", origin+"/api/anthropic/v1/messages", nil)
	request.Header.Set("X-Session-Id", "synthetic-session")
	signed, e := signer.Sign(context.Background(), client, request, key)
	require.NoError(t, e)
	require.True(t, signed)
	signature, e := base64.StdEncoding.DecodeString(request.Header.Get("X-Client-Sig"))
	require.NoError(t, e)
	message := "synthetic-id\n" + request.Header.Get("X-Client-Ts") + "\n3.14.0\nsynthetic-session\n" + request.Header.Get("X-Client-Nonce")
	publicKey, ok := private.Public().(ed25519.PublicKey)
	require.True(t, ok)
	require.True(t, ed25519.Verify(publicKey, []byte(message), signature))
	require.Equal(t, "zcode", request.Header.Get("X-App-Id"))
	_, e = strconv.ParseInt(request.Header.Get("X-Client-Ts"), 10, 64)
	require.NoError(t, e)
	pow, e := proofOfWork(context.Background(), "synthetic-id", "synthetic-session", "1700000000123")
	require.NoError(t, e)
	seedHash := sha256.Sum256([]byte("synthetic-id\nzcode\nsynthetic-session\n1700000000123"))
	hash := sha256.Sum256([]byte(hex.EncodeToString(seedHash[:])[:32] + "\n" + pow))
	require.Zero(t, hash[0])
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = proofOfWork(ctx, "id", "session", "1")
	require.ErrorIs(t, e, context.Canceled)
}
