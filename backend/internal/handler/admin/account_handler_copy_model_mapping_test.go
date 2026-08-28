//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type copyModelMappingAdminService struct {
	*stubAdminService
	lastInput *service.CopyAccountModelMappingInput
	result    *service.CopyAccountModelMappingResult
	err       error
}

func (s *copyModelMappingAdminService) CopyAccountModelMapping(ctx context.Context, input *service.CopyAccountModelMappingInput) (*service.CopyAccountModelMappingResult, error) {
	copied := *input
	copied.TargetAccountIDs = append([]int64(nil), input.TargetAccountIDs...)
	s.lastInput = &copied
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func setupCopyModelMappingRouter(svc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/model-mapping/copy", handler.CopyModelMapping)
	return router
}

func TestCopyModelMappingHandlerCallsService(t *testing.T) {
	svc := &copyModelMappingAdminService{
		stubAdminService: newStubAdminService(),
		result: &service.CopyAccountModelMappingResult{
			SourceAccountID:  100,
			TargetAccountIDs: []int64{200, 300},
			Platform:         service.PlatformAntigravity,
			MappingCount:     2,
			Success:          2,
			Failed:           0,
			SuccessIDs:       []int64{200, 300},
			FailedIDs:        []int64{},
			Results: []service.CopyAccountModelMappingAccountResult{
				{AccountID: 200, Success: true},
				{AccountID: 300, Success: true},
			},
		},
	}
	router := setupCopyModelMappingRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"source_account_id":  100,
		"target_account_ids": []int64{200, 300},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-mapping/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, svc.lastInput)
	require.Equal(t, int64(100), svc.lastInput.SourceAccountID)
	require.Equal(t, []int64{200, 300}, svc.lastInput.TargetAccountIDs)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			SourceAccountID  int64   `json:"source_account_id"`
			TargetAccountIDs []int64 `json:"target_account_ids"`
			Platform         string  `json:"platform"`
			MappingCount     int     `json:"mapping_count"`
			Success          int     `json:"success"`
			Failed           int     `json:"failed"`
			SuccessIDs       []int64 `json:"success_ids"`
			FailedIDs        []int64 `json:"failed_ids"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, int64(100), resp.Data.SourceAccountID)
	require.Equal(t, []int64{200, 300}, resp.Data.TargetAccountIDs)
	require.Equal(t, service.PlatformAntigravity, resp.Data.Platform)
	require.Equal(t, 2, resp.Data.MappingCount)
	require.Equal(t, 2, resp.Data.Success)
	require.Equal(t, 0, resp.Data.Failed)
}

func TestCopyModelMappingHandlerRejectsEmptyTargetList(t *testing.T) {
	svc := &copyModelMappingAdminService{stubAdminService: newStubAdminService()}
	router := setupCopyModelMappingRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"source_account_id":  100,
		"target_account_ids": []int64{},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-mapping/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Nil(t, svc.lastInput)
}

func TestCopyModelMappingHandlerMapsServiceValidationErrors(t *testing.T) {
	svc := &copyModelMappingAdminService{
		stubAdminService: newStubAdminService(),
		err:              infraerrors.BadRequest("EMPTY_MODEL_MAPPING", "source account model_mapping is empty"),
	}
	router := setupCopyModelMappingRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"source_account_id":  100,
		"target_account_ids": []int64{200},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-mapping/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCopyModelMappingHandlerMapsServiceNotFound(t *testing.T) {
	svc := &copyModelMappingAdminService{
		stubAdminService: newStubAdminService(),
		err:              infraerrors.NotFound("ACCOUNT_NOT_FOUND", "target account not found"),
	}
	router := setupCopyModelMappingRouter(svc)

	body, _ := json.Marshal(map[string]any{
		"source_account_id":  100,
		"target_account_ids": []int64{200},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/model-mapping/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}
