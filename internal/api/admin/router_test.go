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

func newTestServer(environments *fakeEnvironmentService) *Server {
	return NewRouter(Handlers{
		Health:      v1.NewHealthHandler(),
		Environment: v1.NewEnvironmentHandler(environments),
	}, nil, "test-secret")
}

func TestHealth(t *testing.T) {
	server := newTestServer(&fakeEnvironmentService{})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()
		server.engine.ServeHTTP(rec, req)

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
	server.engine.ServeHTTP(rec, req)

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
	server.engine.ServeHTTP(rec, req)

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
		server.engine.ServeHTTP(rec, req)
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
	server.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPublicRouteNotMounted(t *testing.T) {
	server := newTestServer(&fakeEnvironmentService{})
	req := httptest.NewRequest(http.MethodGet, "/v1/customers", nil)
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
