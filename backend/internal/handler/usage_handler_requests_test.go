package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userRequestHistoryRepoCapture struct {
	service.OpsRepository
	params pagination.PaginationParams
	filter service.UserRequestHistoryFilter
}

type userRequestHistorySettingRepo struct {
	service.SettingRepository
}

func (userRequestHistorySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = "true"
	}
	return values, nil
}

func (s *userRequestHistoryRepoCapture) ListUserRequestHistory(
	_ context.Context,
	params pagination.PaginationParams,
	filter service.UserRequestHistoryFilter,
) ([]service.UserRequestRecord, int64, error) {
	s.params = params
	s.filter = filter
	return []service.UserRequestRecord{{
		RecordType: service.UserRequestRecordSuccess,
		ID:         1, RequestID: "client:req-1", StatusCode: 200, Category: "success",
	}}, 1, nil
}

func newUserRequestHistoryTestRouter(repo *userRequestHistoryRepoCapture, errorsEnabled bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageRepo := &userUsageRepoCapture{}
	usageSvc := service.NewUsageService(usageRepo, nil, nil, nil)
	opsSvc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	var settingSvc *service.SettingService
	if errorsEnabled {
		settingSvc = service.NewSettingService(userRequestHistorySettingRepo{}, nil)
	}
	h := NewUsageHandler(usageSvc, nil, opsSvc, settingSvc)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage/requests", h.ListRequests)
	return router
}

func TestUserRequestHistoryParsesUnifiedFilters(t *testing.T) {
	repo := &userRequestHistoryRepoCapture{}
	router := newUserRequestHistoryTestRouter(repo, true)

	req := httptest.NewRequest(http.MethodGet, "/usage/requests?page=2&page_size=25&group_id=7&model=gpt-5.4&request_type=stream&billing_type=1&billing_mode=token&category=upstream&status_code=502&sort_by=status_code&sort_order=asc", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(42), repo.filter.UserID)
	require.Equal(t, int64(7), repo.filter.GroupID)
	require.Equal(t, "gpt-5.4", repo.filter.Model)
	require.NotNil(t, repo.filter.RequestType)
	require.Equal(t, int16(service.RequestTypeStream), *repo.filter.RequestType)
	require.NotNil(t, repo.filter.BillingType)
	require.Equal(t, int8(1), *repo.filter.BillingType)
	require.Equal(t, "token", repo.filter.BillingMode)
	require.Equal(t, "upstream", repo.filter.Category)
	require.NotNil(t, repo.filter.StatusCode)
	require.Equal(t, 502, *repo.filter.StatusCode)
	require.True(t, repo.filter.IncludeErrors)
	require.Equal(t, 2, repo.params.Page)
	require.Equal(t, 25, repo.params.PageSize)
	require.Equal(t, "status_code", repo.params.SortBy)
	require.Equal(t, "asc", repo.params.SortOrder)
	require.Contains(t, rec.Body.String(), `"record_type":"success"`)
}

func TestUserRequestHistoryKeepsErrorsDisabledWhenSettingIsUnavailable(t *testing.T) {
	repo := &userRequestHistoryRepoCapture{}
	router := newUserRequestHistoryTestRouter(repo, false)

	req := httptest.NewRequest(http.MethodGet, "/usage/requests", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, repo.filter.IncludeErrors)
}

func TestUserRequestHistoryRejectsInvalidCategoryAndStatus(t *testing.T) {
	for _, path := range []string{
		"/usage/requests?category=not-valid",
		"/usage/requests?status_code=99",
		"/usage/requests?status_code=text",
	} {
		repo := &userRequestHistoryRepoCapture{}
		router := newUserRequestHistoryTestRouter(repo, true)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}
