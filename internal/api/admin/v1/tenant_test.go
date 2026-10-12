package v1

import (
	"context"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTenantService struct {
	called bool
	req    admin.CreateTenantRequest
	resp   *admin.CreateTenantResponse
	err    error

	listCalled bool
	listReq    admin.ListTenantsRequest
	listResp   *admin.ListTenantsResponse
	listErr    error

	getID   string
	getResp *admin.TenantDetailResponse
	getErr  error
}

func (s *stubTenantService) CreateTenant(_ context.Context, req admin.CreateTenantRequest) (*admin.CreateTenantResponse, error) {
	s.called = true
	s.req = req
	return s.resp, s.err
}

func (s *stubTenantService) ListTenants(_ context.Context, req admin.ListTenantsRequest) (*admin.ListTenantsResponse, error) {
	s.listCalled = true
	s.listReq = req
	return s.listResp, s.listErr
}

func (s *stubTenantService) GetTenant(_ context.Context, tenantID string) (*admin.TenantDetailResponse, error) {
	s.getID = tenantID
	return s.getResp, s.getErr
}

const createTenantBody = `{"tenant_name":"Acme","email":"owner@acme.com","password":"chosen-by-operator","create_production":true}`

func TestCreateTenant_Success(t *testing.T) {
	stub := &stubTenantService{
		resp: &admin.CreateTenantResponse{TenantID: "tenant_1", TenantName: "Acme", UserID: "user_1", Email: "owner@acme.com"},
	}
	router := setupAdminRouter(http.MethodPost, "/v1/tenants", NewTenantHandler(stub).CreateTenant)

	w := doAdminRequest(router, http.MethodPost, "/v1/tenants", createTenantBody)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, admin.CreateTenantRequest{
		TenantName:       "Acme",
		Email:            "owner@acme.com",
		Password:         "chosen-by-operator",
		CreateProduction: true,
	}, stub.req)
	assert.JSONEq(t, `{"tenant_id":"tenant_1","tenant_name":"Acme","user_id":"user_1","email":"owner@acme.com","environments":null}`, w.Body.String())
}

func TestCreateTenant_Errors(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{name: "malformed body", body: `{"tenant_name":`, wantStatus: http.StatusBadRequest},
		{
			name:       "invalid input",
			body:       createTenantBody,
			serviceErr: ierr.NewError("invalid email").Mark(ierr.ErrValidation),
			wantStatus: http.StatusBadRequest,
			wantCalled: true,
		},
		{
			name:       "email already in use",
			body:       createTenantBody,
			serviceErr: ierr.NewError("email already in use").Mark(ierr.ErrAlreadyExists),
			wantStatus: http.StatusConflict,
			wantCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubTenantService{err: tc.serviceErr}
			router := setupAdminRouter(http.MethodPost, "/v1/tenants", NewTenantHandler(stub).CreateTenant)

			w := doAdminRequest(router, http.MethodPost, "/v1/tenants", tc.body)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCalled, stub.called)
		})
	}
}

func TestListTenants_Success(t *testing.T) {
	stub := &stubTenantService{listResp: &admin.ListTenantsResponse{Items: []*admin.TenantResponse{}}}
	router := setupAdminRouter(http.MethodGet, "/v1/tenants", NewTenantHandler(stub).ListTenants)

	w := doAdminRequest(router, http.MethodGet, "/v1/tenants?search=Sarvam+AI&internal_status=active&limit=10&offset=20", "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, admin.ListTenantsRequest{
		Search:         "Sarvam AI",
		InternalStatus: types.TenantInternalStatusActive,
		Limit:          10,
		Offset:         20,
	}, stub.listReq)
}

func TestListTenants_Errors(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{name: "limit that is not a number", query: "?limit=ten", wantStatus: http.StatusBadRequest},
		{name: "offset that is not a number", query: "?offset=1.5", wantStatus: http.StatusBadRequest},
		{
			name:       "value the service rejects",
			query:      "?limit=500",
			serviceErr: ierr.NewError("invalid limit").Mark(ierr.ErrValidation),
			wantStatus: http.StatusBadRequest,
			wantCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubTenantService{listErr: tc.serviceErr}
			router := setupAdminRouter(http.MethodGet, "/v1/tenants", NewTenantHandler(stub).ListTenants)

			w := doAdminRequest(router, http.MethodGet, "/v1/tenants"+tc.query, "")

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCalled, stub.listCalled)
		})
	}
}

const tenantDetailRoute = "/v1/tenants/:tenant_id"

func TestGetTenant_Success(t *testing.T) {
	stub := &stubTenantService{getResp: &admin.TenantDetailResponse{TenantResponse: admin.TenantResponse{ID: "tenant_1"}}}
	router := setupAdminRouter(http.MethodGet, tenantDetailRoute, NewTenantHandler(stub).GetTenant)

	w := doAdminRequest(router, http.MethodGet, "/v1/tenants/tenant_1", "")

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "tenant_1", stub.getID)
}

func TestGetTenant_NotFound(t *testing.T) {
	stub := &stubTenantService{getErr: ierr.NewError("tenant not found").WithHint("Tenant not found").Mark(ierr.ErrNotFound)}
	router := setupAdminRouter(http.MethodGet, tenantDetailRoute, NewTenantHandler(stub).GetTenant)

	w := doAdminRequest(router, http.MethodGet, "/v1/tenants/tenant_missing", "")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "tenant_missing", stub.getID)
}
