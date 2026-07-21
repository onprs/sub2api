package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type userRequestHistoryRepoStub struct {
	OpsRepository
	filter UserRequestHistoryFilter
	params pagination.PaginationParams
	items  []UserRequestRecord
	total  int64
}

func (s *userRequestHistoryRepoStub) ListUserRequestHistory(
	_ context.Context,
	params pagination.PaginationParams,
	filter UserRequestHistoryFilter,
) ([]UserRequestRecord, int64, error) {
	s.params = params
	s.filter = filter
	return s.items, s.total, nil
}

func TestListUserRequestHistoryForcesAuthenticatedUserScope(t *testing.T) {
	repo := &userRequestHistoryRepoStub{
		items: []UserRequestRecord{{RecordType: UserRequestRecordSuccess, ID: 1}},
		total: 1,
	}
	svc := &OpsService{opsRepo: repo}
	params := pagination.PaginationParams{Page: 2, PageSize: 20, SortBy: "created_at", SortOrder: "desc"}

	items, total, err := svc.ListUserRequestHistory(context.Background(), 42, params, UserRequestHistoryFilter{
		UserID:        999,
		IncludeErrors: true,
		Category:      "upstream",
	})

	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, repo.items, items)
	require.Equal(t, int64(42), repo.filter.UserID)
	require.True(t, repo.filter.IncludeErrors)
	require.Equal(t, "upstream", repo.filter.Category)
	require.Equal(t, params, repo.params)
}

func TestListUserRequestHistoryRejectsInvalidUser(t *testing.T) {
	svc := &OpsService{opsRepo: &userRequestHistoryRepoStub{}}
	_, _, err := svc.ListUserRequestHistory(context.Background(), 0, pagination.PaginationParams{}, UserRequestHistoryFilter{})
	require.Error(t, err)
}

func TestIsUserRequestCategory(t *testing.T) {
	for _, value := range []string{"", "success", "upstream", "cyber", "other"} {
		require.True(t, IsUserRequestCategory(value), value)
	}
	require.False(t, IsUserRequestCategory("unknown-category"))
}
