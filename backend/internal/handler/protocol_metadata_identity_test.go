package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBindProtocolMetadataIdentityRequiresCompleteAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(30)

	tests := []struct {
		name     string
		apiKey   *service.APIKey
		tenantID int64
		want     bool
	}{
		{name: "complete", apiKey: &service.APIKey{ID: 20, GroupID: &groupID}, tenantID: 10, want: true},
		{name: "missing tenant", apiKey: &service.APIKey{ID: 20, GroupID: &groupID}},
		{name: "missing API key", apiKey: &service.APIKey{GroupID: &groupID}, tenantID: 10},
		{name: "missing group", apiKey: &service.APIKey{ID: 20}, tenantID: 10},
		{name: "missing API key object", tenantID: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

			bindProtocolMetadataIdentity(c, tt.apiKey, tt.tenantID)

			identity, ok := service.ProtocolMetadataIdentityFromContext(c.Request.Context())
			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, service.ProtocolMetadataIdentity{TenantID: 10, APIKeyID: 20, GroupID: 30}, identity)
			}
		})
	}
}
