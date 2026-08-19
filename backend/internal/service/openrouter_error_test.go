package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeOpenRouterError(t *testing.T) {
	// 401 Unauthorized
	body401 := []byte(`{"error":{"code":401,"message":"Missing Authentication header"}}`)
	err401 := decodeOpenRouterError(http.StatusUnauthorized, nil, body401)
	require.Equal(t, 401, err401.EffectiveStatus())
	require.True(t, err401.AccountAffecting)
	require.False(t, err401.ClientError)

	// 429 Rate Limit
	body429 := []byte(`{"error":{"code":429,"message":"Rate limit exceeded"}}`)
	err429 := decodeOpenRouterError(http.StatusTooManyRequests, nil, body429)
	require.Equal(t, 429, err429.EffectiveStatus())
	require.True(t, err429.Retryable)

	// 400 Bad Request
	body400 := []byte(`{"error":{"type":"invalid_request_error","message":"Invalid model provided"}}`)
	err400 := decodeOpenRouterError(http.StatusBadRequest, nil, body400)
	require.Equal(t, 400, err400.EffectiveStatus())
	require.True(t, err400.ClientError)
	require.False(t, err400.Retryable)
}
