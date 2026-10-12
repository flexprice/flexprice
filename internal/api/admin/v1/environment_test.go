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

type stubEnvironmentService struct {
	listTenantID string
	listResp     *admin.ListEnvironmentsResponse
	listErr      error
}

func (s *stubEnvironmentService) CreateEnvironment(context.Context, admin.CreateEnvironmentRequest) (*admin.EnvironmentResponse, error) {
	return nil, nil
}

func (s *stubEnvironmentService) ListEnvironments(_ context.Context, tenantID string) (*admin.ListEnvironmentsResponse, error) {
	s.listTenantID = tenantID
	return s.listResp, s.listErr
}

const tenantEnvironmentsRoute = "/v1/tenants/:tenant_id/environments"

func TestListEnvironments_Success(t *testing.T) {
	stub := &stubEnvironmentService{listResp: &admin.ListEnvironmentsResponse{Items: []*admin.EnvironmentResponse{}}}
	router := setupAdminRouter(http.MethodGet, tenantEnvironmentsRoute, NewEnvironmentHandler(stub).ListEnvironments)

	w := doAdminRequest(router, http.MethodGet, "/v1/tenants/tenant_1/environments", "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "tenant_1", stub.listTenantID)
}

func TestListEnvironments_UnknownTenant(t *testing.T) {
	stub := &stubEnvironmentService{listErr: ierr.NewError("tenant not found").WithHint("Tenant not found").Mark(ierr.ErrNotFound)}
	router := setupAdminRouter(http.MethodGet, tenantEnvironmentsRoute, NewEnvironmentHandler(stub).ListEnvironments)

	w := doAdminRequest(router, http.MethodGet, "/v1/tenants/tenant_missing/environments", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "tenant_missing", stub.listTenantID)
}
