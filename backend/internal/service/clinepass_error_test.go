package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeClinePassErrorTopLevelString(t *testing.T) {
	err := decodeClinePassError(http.StatusUnauthorized, http.Header{"X-Request-Id": []string{"req-1"}}, []byte(`{"success":false,"error":"authentication required"}`))
	require.Equal(t, http.StatusUnauthorized, err.EffectiveStatus())
	require.Equal(t, "authentication required", err.Message)
	require.Equal(t, "req-1", err.RequestID)
	require.True(t, err.AccountAffecting)
	require.False(t, err.ClientError)
}

func TestDecodeClinePassErrorNestedProviderClientError(t *testing.T) {
	body := []byte(`{"success":false,"error":"Provider error: {\"status\":400,\"error\":{\"type\":\"invalid_parameter_error\",\"message\":\"developer role is unsupported\"}}"}`)
	err := decodeClinePassError(http.StatusInternalServerError, nil, body)
	require.Equal(t, http.StatusBadRequest, err.EffectiveStatus())
	require.Equal(t, "invalid_parameter_error", err.Type)
	require.Equal(t, "developer role is unsupported", err.Message)
	require.True(t, err.ClientError)
	require.False(t, err.Retryable)
	require.False(t, err.AccountAffecting)
}

func TestDecodeClinePassErrorInvalidModelDoesNotAffectAccount(t *testing.T) {
	err := decodeClinePassError(http.StatusNotFound, nil, []byte(`{"success":false,"error":"model not found"}`))
	require.Equal(t, http.StatusNotFound, err.EffectiveStatus())
	require.True(t, err.ClientError)
	require.False(t, err.AccountAffecting)
	require.False(t, err.Retryable)
}

func TestDecodeClinePassErrorMalformedNestedBodyFallsBackToOuterStatus(t *testing.T) {
	err := decodeClinePassError(http.StatusBadGateway, nil, []byte(`{"error":"provider failed: {broken"}`))
	require.Equal(t, http.StatusBadGateway, err.EffectiveStatus())
	require.True(t, err.Retryable)
}
