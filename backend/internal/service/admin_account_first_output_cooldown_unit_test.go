//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAccountClearingFirstOutputBudgetAlsoClearsCooldown(t *testing.T) {
	accountID := int64(5338)
	timeoutSeconds := 12
	cooldownMinutes := 10
	repo := &updateAccountCredsRepoStub{account: &Account{
		ID:                                 accountID,
		Platform:                           PlatformOpenAI,
		Type:                               AccountTypeAPIKey,
		Status:                             StatusActive,
		FirstOutputFailoverTimeoutSeconds:  &timeoutSeconds,
		FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	clear := 0

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		FirstOutputFailoverTimeoutSeconds: &clear,
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Nil(t, updated.FirstOutputFailoverTimeoutSeconds)
	require.Nil(t, updated.FirstOutputFailoverCooldownMinutes)
}

func TestUpdateAccountCanEnableFirstOutputCooldownForExistingBudget(t *testing.T) {
	accountID := int64(5338)
	timeoutSeconds := 12
	repo := &updateAccountCredsRepoStub{account: &Account{
		ID:                                accountID,
		Platform:                          PlatformOpenAI,
		Type:                              AccountTypeAPIKey,
		Status:                            StatusActive,
		FirstOutputFailoverTimeoutSeconds: &timeoutSeconds,
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	cooldownMinutes := 10

	updated, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, &timeoutSeconds, updated.FirstOutputFailoverTimeoutSeconds)
	require.NotSame(t, &cooldownMinutes, updated.FirstOutputFailoverCooldownMinutes)
	require.Equal(t, 10, *updated.FirstOutputFailoverCooldownMinutes)
}

func TestUpdateAccountRejectsFirstOutputCooldownWithoutBudget(t *testing.T) {
	accountID := int64(5338)
	repo := &updateAccountCredsRepoStub{account: &Account{
		ID:       accountID,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	cooldownMinutes := 10

	_, err := svc.UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		FirstOutputFailoverCooldownMinutes: &cooldownMinutes,
	})

	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
}
