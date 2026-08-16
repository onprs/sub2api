//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAccountConcurrencyDefaultsInvalidGrokOAuthToOne(t *testing.T) {
	require.Equal(t, 1, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, 0))
	require.Equal(t, 1, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, -5))
}

func TestNormalizeAccountConcurrencyPreservesExplicitValues(t *testing.T) {
	require.Equal(t, 50, normalizeAccountConcurrency(PlatformGrok, AccountTypeOAuth, 50))
	require.Equal(t, 2, normalizeAccountConcurrency(PlatformOpenAI, AccountTypeOAuth, 2))
	require.Equal(t, 2, normalizeAccountConcurrency(PlatformGrok, AccountTypeAPIKey, 2))
}

func TestNormalizeFirstOutputFailoverTimeout(t *testing.T) {
	zero := 0
	normalized, err := normalizeFirstOutputFailoverTimeout(PlatformOpenAI, AccountTypeAPIKey, nil)
	require.NoError(t, err)
	require.Nil(t, normalized)

	normalized, err = normalizeFirstOutputFailoverTimeout(PlatformOpenAI, AccountTypeAPIKey, &zero)
	require.NoError(t, err)
	require.Nil(t, normalized)

	seconds := 15
	normalized, err = normalizeFirstOutputFailoverTimeout(PlatformOpenAI, AccountTypeAPIKey, &seconds)
	require.NoError(t, err)
	require.NotSame(t, &seconds, normalized)
	require.Equal(t, 15, *normalized)
}

func TestNormalizeFirstOutputFailoverTimeoutRejectsUnsupportedValues(t *testing.T) {
	negative := -1
	_, err := normalizeFirstOutputFailoverTimeout(PlatformOpenAI, AccountTypeAPIKey, &negative)
	require.Error(t, err)

	seconds := 15
	_, err = normalizeFirstOutputFailoverTimeout(PlatformOpenAI, AccountTypeOAuth, &seconds)
	require.Error(t, err)

	_, err = normalizeFirstOutputFailoverTimeout(PlatformAnthropic, AccountTypeAPIKey, &seconds)
	require.Error(t, err)
}

func TestNormalizeFirstOutputFailoverCooldown(t *testing.T) {
	timeoutSeconds := 15
	zero := 0

	normalized, err := normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, &timeoutSeconds, nil,
	)
	require.NoError(t, err)
	require.Nil(t, normalized)

	normalized, err = normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, &timeoutSeconds, &zero,
	)
	require.NoError(t, err)
	require.Nil(t, normalized)

	minutes := 10
	normalized, err = normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, &timeoutSeconds, &minutes,
	)
	require.NoError(t, err)
	require.NotSame(t, &minutes, normalized)
	require.Equal(t, 10, *normalized)
}

func TestNormalizeFirstOutputFailoverCooldownRejectsInvalidConfiguration(t *testing.T) {
	timeoutSeconds := 15
	minutes := 10
	negative := -1
	tooLong := FirstOutputFailoverCooldownMaxMinutes + 1

	_, err := normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, &timeoutSeconds, &negative,
	)
	require.Error(t, err)

	_, err = normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, &timeoutSeconds, &tooLong,
	)
	require.Error(t, err)

	_, err = normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeOAuth, &timeoutSeconds, &minutes,
	)
	require.Error(t, err)

	_, err = normalizeFirstOutputFailoverCooldown(
		PlatformOpenAI, AccountTypeAPIKey, nil, &minutes,
	)
	require.Error(t, err)
}
