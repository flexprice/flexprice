package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/flexprice/flexprice/internal/api/admin/v1"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEnvironmentService struct {
	req  admindto.CreateEnvironmentRequest
	resp *admindto.EnvironmentResponse
	err  error
}

func (f *fakeEnvironmentService) CreateEnvironment(_ context.Context, req admindto.CreateEnvironmentRequest) (*admindto.EnvironmentResponse, error) {
	f.req = req
	return f.resp, f.err
}

type fakeTenantService struct {
	called bool
	req    admindto.CreateTenantRequest
	resp   *admindto.CreateTenantResponse
	err    error
}

func (f *fakeTenantService) CreateTenant(_ context.Context, req admindto.CreateTenantRequest) (*admindto.CreateTenantResponse, error) {
	f.called = true
	f.req = req
	return f.resp, f.err
}

func newTestServer(environments *fakeEnvironmentService) *Server {
	return newTestServerWith(environments, &fakeTenantService{})
}

func newTestServerWith(environments *fakeEnvironmentService, tenants *fakeTenantService) *Server {
	return NewRouter(Handlers{
		Health:      v1.NewHealthHandler(),
		Environment: v1.NewEnvironmentHandler(environments),
		Tenant:      v1.NewTenantHandler(tenants),
	}, nil, "test-secret")
}

func postTenant(server *Server, body, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(secretHeader, secret)
	}
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	server := newTestServer(&fakeEnvironmentService{})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	}
}

func TestCreateEnvironment(t *testing.T) {
	fake := &fakeEnvironmentService{
		resp: &admindto.EnvironmentResponse{
			ID:       "env_1",
			Name:     "Production",
			Type:     types.EnvironmentProduction,
			TenantID: "ten_1",
		},
	}
	server := newTestServer(fake)

	body := []byte(`{"name":"Production","type":"production","email":"owner@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(secretHeader, "test-secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "Production", fake.req.Name)
	require.Equal(t, "owner@example.com", fake.req.Email)

	var resp admindto.EnvironmentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "env_1", resp.ID)
	require.Equal(t, "ten_1", resp.TenantID)
	require.Equal(t, types.EnvironmentProduction, resp.Type)
}

func TestCreateEnvironmentValidation(t *testing.T) {
	fake := &fakeEnvironmentService{
		err: ierr.NewError("tenant_id or email is required").Mark(ierr.ErrValidation),
	}
	server := newTestServer(fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader([]byte(`{"name":"Production","type":"production"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(secretHeader, "test-secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateEnvironmentUnauthorized(t *testing.T) {
	server := newTestServer(&fakeEnvironmentService{})
	body := []byte(`{"name":"Production","type":"production","tenant_id":"ten_1"}`)

	for _, header := range []string{"", "wrong-secret"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if header != "" {
			req.Header.Set(secretHeader, header)
		}
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
}

func TestEmptySecretFailsClosed(t *testing.T) {
	server := NewRouter(Handlers{
		Health:      v1.NewHealthHandler(),
		Environment: v1.NewEnvironmentHandler(&fakeEnvironmentService{}),
	}, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader([]byte(`{"name":"Production","type":"production","tenant_id":"ten_1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(secretHeader, "anything")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPublicRouteNotMounted(t *testing.T) {
	server := newTestServer(&fakeEnvironmentService{})
	req := httptest.NewRequest(http.MethodGet, "/v1/customers", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateTenant(t *testing.T) {
	fake := &fakeTenantService{
		resp: &admindto.CreateTenantResponse{
			TenantID:   "tenant_1",
			TenantName: "Acme",
			UserID:     "user_1",
			Email:      "owner@acme.com",
			Environments: []*admindto.EnvironmentResponse{
				{ID: "env_1", Name: "Sandbox", Type: types.EnvironmentDevelopment, TenantID: "tenant_1"},
			},
		},
	}
	server := newTestServerWith(&fakeEnvironmentService{}, fake)

	rec := postTenant(server, `{"tenant_name":"Acme","email":"owner@acme.com","password":"chosen-by-operator","create_production":true}`, "test-secret")

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, admindto.CreateTenantRequest{
		TenantName:       "Acme",
		Email:            "owner@acme.com",
		Password:         "chosen-by-operator",
		CreateProduction: true,
	}, fake.req)

	var resp admindto.CreateTenantResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "tenant_1", resp.TenantID)
	assert.Equal(t, "user_1", resp.UserID)
	require.Len(t, resp.Environments, 1)
	assert.Equal(t, "env_1", resp.Environments[0].ID)
	assert.NotContains(t, rec.Body.String(), "chosen-by-operator")
}

func TestCreateTenantErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		secret     string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "missing secret",
			body:       `{"tenant_name":"Acme","email":"owner@acme.com","password":"chosen-by-operator"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong secret",
			body:       `{"tenant_name":"Acme","email":"owner@acme.com","password":"chosen-by-operator"}`,
			secret:     "wrong-secret",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed body",
			body:       `{"tenant_name":`,
			secret:     "test-secret",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "email already in use",
			body:       `{"tenant_name":"Acme","email":"owner@acme.com","password":"chosen-by-operator"}`,
			secret:     "test-secret",
			serviceErr: ierr.NewError("email already in use").Mark(ierr.ErrAlreadyExists),
			wantStatus: http.StatusConflict,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeTenantService{err: tt.serviceErr}
			server := newTestServerWith(&fakeEnvironmentService{}, fake)

			rec := postTenant(server, tt.body, tt.secret)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, fake.called)
		})
	}
}

type fakeUserService struct {
	called bool
	req    admindto.AddUserRequest
	resp   *admindto.UserResponse
	err    error
}

func (f *fakeUserService) AddUser(_ context.Context, req admindto.AddUserRequest) (*admindto.UserResponse, error) {
	f.called = true
	f.req = req
	return f.resp, f.err
}

func newUserTestServer(users *fakeUserService) *Server {
	return NewRouter(Handlers{
		Health:      v1.NewHealthHandler(),
		Environment: v1.NewEnvironmentHandler(&fakeEnvironmentService{}),
		Tenant:      v1.NewTenantHandler(&fakeTenantService{}),
		User:        v1.NewUserHandler(users),
	}, nil, "test-secret")
}

func postUser(server *Server, body, secret string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(secretHeader, secret)
	}
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)
	return rec
}

func TestAddUser(t *testing.T) {
	fake := &fakeUserService{
		resp: &admindto.UserResponse{
			UserID:   "user_1",
			Email:    "teammate@acme.com",
			TenantID: "tenant_1",
			Roles:    []string{"all_writer"},
		},
	}
	server := newUserTestServer(fake)

	rec := postUser(server, `{"tenant_id":"tenant_1","email":"teammate@acme.com","password":"chosen-by-operator","role":"all_writer"}`, "test-secret")

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, admindto.AddUserRequest{
		TenantID: "tenant_1",
		Email:    "teammate@acme.com",
		Password: "chosen-by-operator",
		Role:     "all_writer",
	}, fake.req)

	var resp admindto.UserResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "user_1", resp.UserID)
	assert.Equal(t, "tenant_1", resp.TenantID)
	assert.Equal(t, []string{"all_writer"}, resp.Roles)
	assert.NotContains(t, rec.Body.String(), "chosen-by-operator")
}

func TestAddUserErrors(t *testing.T) {
	body := `{"tenant_id":"tenant_1","email":"teammate@acme.com","password":"chosen-by-operator"}`
	tests := []struct {
		name       string
		body       string
		secret     string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{
			name:       "missing secret",
			body:       body,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed body",
			body:       `{"tenant_id":`,
			secret:     "test-secret",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown tenant",
			body:       body,
			secret:     "test-secret",
			serviceErr: ierr.NewError("tenant not found").Mark(ierr.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantCalled: true,
		},
		{
			name:       "email already in use",
			body:       body,
			secret:     "test-secret",
			serviceErr: ierr.NewError("email already in use").Mark(ierr.ErrAlreadyExists),
			wantStatus: http.StatusConflict,
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeUserService{err: tt.serviceErr}
			server := newUserTestServer(fake)

			rec := postUser(server, tt.body, tt.secret)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, fake.called)
		})
	}
}
