package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildUserRequestHistoryCTEScopesBothSources(t *testing.T) {
	cte, args := buildUserRequestHistoryCTE(service.UserRequestHistoryFilter{
		UserID:        42,
		IncludeErrors: true,
	})

	require.Equal(t, []any{int64(42)}, args)
	require.Contains(t, cte, "ul.user_id = $1")
	require.Contains(t, cte, "ul.actual_cost > 0")
	require.Contains(t, cte, "e.user_id = $1 OR e.deleted_key_owner_user_id = $1")
	require.Contains(t, cte, "COALESCE(e.is_count_tokens, false) = false")
	require.NotContains(t, cte, "AND FALSE")
}

func TestBuildUserRequestHistoryCTEAppliesSharedAndSourceSpecificFilters(t *testing.T) {
	requestType := int16(service.RequestTypeStream)
	billingType := int8(service.BillingTypeSubscription)
	statusCode := 502
	start := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	cte, args := buildUserRequestHistoryCTE(service.UserRequestHistoryFilter{
		UserID:        42,
		APIKeyID:      7,
		GroupID:       8,
		Model:         "gpt-5.4",
		RequestType:   &requestType,
		BillingType:   &billingType,
		BillingMode:   string(service.BillingModeToken),
		Category:      "upstream",
		StatusCode:    &statusCode,
		StartTime:     &start,
		EndTime:       &end,
		IncludeErrors: true,
	})

	require.Contains(t, cte, "ul.api_key_id = $2")
	require.Contains(t, cte, "e.api_key_id = $2")
	require.Contains(t, cte, "ul.group_id = $3")
	require.Contains(t, cte, "e.group_id = $3")
	require.Contains(t, cte, "COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = $4")
	require.Contains(t, cte, "ul.billing_type")
	require.Contains(t, cte, "ul.billing_mode")
	require.GreaterOrEqual(t, strings.Count(cte, "FALSE"), 3)
	require.Contains(t, cte, "COALESCE(e.upstream_status_code, e.status_code, 0) = $")
	require.Contains(t, args, "upstream")
	require.Contains(t, args, statusCode)
}

func TestBuildUserRequestHistoryCTECanDisableErrors(t *testing.T) {
	cte, _ := buildUserRequestHistoryCTE(service.UserRequestHistoryFilter{UserID: 42})
	require.Contains(t, cte, "COALESCE(e.is_count_tokens, false) = false AND FALSE")
}

func TestUserRequestHistoryOrderByIsWhitelistedAndStable(t *testing.T) {
	require.Equal(t,
		"status_code ASC, created_at ASC, record_type ASC, id ASC",
		userRequestHistoryOrderBy(pagination.PaginationParams{SortBy: "status_code", SortOrder: "asc"}),
	)
	require.Equal(t,
		"created_at DESC, record_type DESC, id DESC",
		userRequestHistoryOrderBy(pagination.PaginationParams{SortBy: "drop table", SortOrder: "desc"}),
	)
}
