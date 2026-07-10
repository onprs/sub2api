//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountRepoStubForCopyModelMapping struct {
	mockAccountRepoForGemini
	accounts    map[int64]*Account
	updateErrBy map[int64]error
	updates     []*Account
}

func (r *accountRepoStubForCopyModelMapping) GetByID(_ context.Context, id int64) (*Account, error) {
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

func (r *accountRepoStubForCopyModelMapping) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := r.accounts[id]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *accountRepoStubForCopyModelMapping) Update(_ context.Context, account *Account) error {
	if err, ok := r.updateErrBy[account.ID]; ok {
		return err
	}
	copied := *account
	copied.Credentials = cloneCredentialsForCopyModelMappingTest(account.Credentials)
	r.accounts[account.ID] = &copied
	r.updates = append(r.updates, &copied)
	return nil
}

func cloneCredentialsForCopyModelMappingTest(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestAdminServiceCopyModelMappingCopiesAndPreservesTargetCredentials(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {
				ID:       100,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"claude-opus-4-6":     "claude-opus-4-6-thinking",
						"claude-sonnet-4-6":   "claude-sonnet-4-6",
						"gemini-3.1-pro-high": "gemini-pro-agent",
					},
					"access_token": "source-access-token",
				},
			},
			200: {
				ID:       200,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"old-model": "old-upstream",
					},
					"access_token":  "target-access-token",
					"refresh_token": "target-refresh-token",
					"project_id":    "target-project",
					"base_url":      "https://target.example.com",
					"oauth_type":    "antigravity",
					"other_key":     "keep-me",
				},
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID: 100,
		TargetAccountIDs: []int64{
			200,
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(100), result.SourceAccountID)
	require.Equal(t, PlatformAntigravity, result.Platform)
	require.Equal(t, 3, result.MappingCount)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, []int64{200}, result.SuccessIDs)
	require.Len(t, repo.updates, 1)

	target := repo.accounts[200]
	require.Equal(t, "target-access-token", target.Credentials["access_token"])
	require.Equal(t, "target-refresh-token", target.Credentials["refresh_token"])
	require.Equal(t, "target-project", target.Credentials["project_id"])
	require.Equal(t, "https://target.example.com", target.Credentials["base_url"])
	require.Equal(t, "antigravity", target.Credentials["oauth_type"])
	require.Equal(t, "keep-me", target.Credentials["other_key"])
	require.Equal(t, map[string]any{
		"claude-opus-4-6":     "claude-opus-4-6-thinking",
		"claude-sonnet-4-6":   "claude-sonnet-4-6",
		"gemini-3.1-pro-high": "gemini-pro-agent",
	}, target.Credentials["model_mapping"])
	require.NotContains(t, target.Credentials["model_mapping"], "old-model")
}

func TestAdminServiceCopyModelMappingRejectsEmptySourceMapping(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {ID: 100, Platform: PlatformAntigravity, Credentials: map[string]any{}},
			200: {ID: 200, Platform: PlatformAntigravity, Credentials: map[string]any{}},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID:  100,
		TargetAccountIDs: []int64{200},
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "source account model_mapping is empty")
	require.Empty(t, repo.updates)
}

func TestAdminServiceCopyModelMappingRejectsCrossPlatformTarget(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {
				ID:       100,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-3-flash": "gemini-3-flash"},
				},
			},
			200: {ID: 200, Platform: PlatformOpenAI, Credentials: map[string]any{}},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID:  100,
		TargetAccountIDs: []int64{200},
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "target account platform does not match source account")
	require.Empty(t, repo.updates)
}

func TestAdminServiceCopyModelMappingRejectsSourceAsTarget(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {
				ID:       100,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-3-flash": "gemini-3-flash"},
				},
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID:  100,
		TargetAccountIDs: []int64{100},
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "source account cannot be a copy target")
	require.Empty(t, repo.updates)
}

func TestAdminServiceCopyModelMappingRejectsMissingTargetBeforeWriting(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {
				ID:       100,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-3-flash": "gemini-3-flash"},
				},
			},
			200: {ID: 200, Platform: PlatformAntigravity, Credentials: map[string]any{}},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID:  100,
		TargetAccountIDs: []int64{200, 300},
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "target account not found")
	require.Empty(t, repo.updates)
}

func TestAdminServiceCopyModelMappingReportsPartialUpdateFailure(t *testing.T) {
	repo := &accountRepoStubForCopyModelMapping{
		accounts: map[int64]*Account{
			100: {
				ID:       100,
				Platform: PlatformAntigravity,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-3-flash": "gemini-3-flash"},
				},
			},
			200: {ID: 200, Platform: PlatformAntigravity, Credentials: map[string]any{}},
			300: {ID: 300, Platform: PlatformAntigravity, Credentials: map[string]any{}},
		},
		updateErrBy: map[int64]error{
			300: errors.New("write failed"),
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.CopyAccountModelMapping(context.Background(), &CopyAccountModelMappingInput{
		SourceAccountID:  100,
		TargetAccountIDs: []int64{200, 300},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []int64{200}, result.SuccessIDs)
	require.Equal(t, []int64{300}, result.FailedIDs)
	require.Len(t, result.Results, 2)
	require.Len(t, repo.updates, 1)
}
