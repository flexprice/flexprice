package v1

import (
	"context"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSettingsService struct {
	called bool
	req    admin.UpdateTenantConfigRequest
	resp   *admin.TenantConfigResponse
	err    error
}

func (s *stubSettingsService) UpdateTenantConfig(_ context.Context, req admin.UpdateTenantConfigRequest) (*admin.TenantConfigResponse, error) {
	s.called = true
	s.req = req
	return s.resp, s.err
}

const tenantConfigPath = "/v1/settings/tenant_config"

func TestUpdateTenantConfig_Success(t *testing.T) {
	stub := &stubSettingsService{
		resp: &admin.TenantConfigResponse{TenantID: "tenant_1", Production: 2, Development: 5, MaxUsers: 20},
	}
	router := setupAdminRouter(http.MethodPut, tenantConfigPath, NewSettingsHandler(stub).UpdateTenantConfig)

	w := doAdminRequest(router, http.MethodPut, tenantConfigPath, `{"tenant_id":"tenant_1","value":{"max_users":20}}`)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "tenant_1", stub.req.TenantID)
	require.NotNil(t, stub.req.Value.MaxUsers)
	assert.Equal(t, 20, *stub.req.Value.MaxUsers)
	assert.Nil(t, stub.req.Value.Production, "a limit left out must reach the service as not sent")
	assert.Nil(t, stub.req.Value.Development, "a limit left out must reach the service as not sent")
	assert.JSONEq(t, `{"tenant_id":"tenant_1","production":2,"development":5,"max_users":20}`, w.Body.String())
}

func TestUpdateTenantConfig_Errors(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{name: "malformed body", body: `{"tenant_id":`, wantStatus: http.StatusBadRequest},
		{
			name:       "unknown tenant",
			body:       `{"tenant_id":"tenant_1","value":{"development":5}}`,
			serviceErr: ierr.NewError("tenant not found").Mark(ierr.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubSettingsService{err: tc.serviceErr}
			router := setupAdminRouter(http.MethodPut, tenantConfigPath, NewSettingsHandler(stub).UpdateTenantConfig)

			w := doAdminRequest(router, http.MethodPut, tenantConfigPath, tc.body)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCalled, stub.called)
		})
	}
}
