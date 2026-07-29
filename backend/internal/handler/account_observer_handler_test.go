package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountObserverDeleteStub struct {
	deletedIDs []int64
	err        error
}

func (s *accountObserverDeleteStub) DeleteAccount(_ context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.err
}

func TestAccountObserverDeleteRequiresExplicitScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deleter := &accountObserverDeleteStub{}
	handler := &AccountObserverHandler{adminService: deleter}
	context, recorder := accountObserverDeleteContext(t, "42", service.AccountObserverReadScope)

	handler.DeleteAccount(context)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Empty(t, deleter.deletedIDs)
}

func TestAccountObserverDeleteUsesAdminCascade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deleter := &accountObserverDeleteStub{}
	handler := &AccountObserverHandler{adminService: deleter}
	context, _ := accountObserverDeleteContext(t, "42", service.AccountObserverReadDeleteScope)

	handler.DeleteAccount(context)

	require.Equal(t, http.StatusNoContent, context.Writer.Status())
	require.Equal(t, []int64{42}, deleter.deletedIDs)
}

func TestAccountObserverDeleteIsIdempotentWhenAccountIsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deleter := &accountObserverDeleteStub{err: service.ErrAccountNotFound}
	handler := &AccountObserverHandler{adminService: deleter}
	context, _ := accountObserverDeleteContext(t, "42", service.AccountObserverReadDeleteScope)

	handler.DeleteAccount(context)

	require.Equal(t, http.StatusNoContent, context.Writer.Status())
	require.Equal(t, []int64{42}, deleter.deletedIDs)
}

func TestAccountObserverDeleteRejectsInvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deleter := &accountObserverDeleteStub{err: errors.New("must not be called")}
	handler := &AccountObserverHandler{adminService: deleter}
	context, recorder := accountObserverDeleteContext(t, "invalid", service.AccountObserverReadDeleteScope)

	handler.DeleteAccount(context)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Empty(t, deleter.deletedIDs)
}

func accountObserverDeleteContext(
	t *testing.T,
	accountID string,
	scope string,
) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodDelete, "/", nil)
	context.Params = gin.Params{{Key: "id", Value: accountID}}
	context.Set("integration_scope", scope)
	return context, recorder
}
