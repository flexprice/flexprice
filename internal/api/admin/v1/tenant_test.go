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

type stubTenantService struct {
	called bool
	req    admin.CreateTenantRequest
	resp   *admin.CreateTenantResponse
	err    error
}

func (s *stubTenantService) CreateTenant(_ context.Context, req admin.CreateTenantRequest) (*admin.CreateTenantResponse, error) {
	s.called = true
	s.req = req
	return s.resp, s.err
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
