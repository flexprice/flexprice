package internalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	v1 "github.com/flexprice/flexprice/internal/api/internalapi/v1"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

type fakeInternalEnvironmentService struct {
	req  dto.InternalCreateEnvironmentRequest
	resp *dto.InternalEnvironmentResponse
	err  error
}

func (f *fakeInternalEnvironmentService) CreateEnvironment(_ context.Context, req dto.InternalCreateEnvironmentRequest) (*dto.InternalEnvironmentResponse, error) {
	f.req = req
	return f.resp, f.err
}

func newTestServer(environments *fakeInternalEnvironmentService) *Server {
	return NewRouter(Handlers{
		Health:      v1.NewHealthHandler(),
		Environment: v1.NewEnvironmentHandler(environments),
	}, nil)
}

func TestHealth(t *testing.T) {
	server := newTestServer(&fakeInternalEnvironmentService{})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/health", nil)
		rec := httptest.NewRecorder()
		server.engine.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	}
}

func TestCreateEnvironment(t *testing.T) {
	fake := &fakeInternalEnvironmentService{
		resp: &dto.InternalEnvironmentResponse{
			ID:       "env_1",
			Name:     "Production",
			Type:     "production",
			TenantID: "ten_1",
		},
	}
	server := newTestServer(fake)

	body := []byte(`{"name":"Production","type":"production","email":"owner@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "Production", fake.req.Name)
	require.Equal(t, "owner@example.com", fake.req.Email)

	var resp dto.InternalEnvironmentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "env_1", resp.ID)
	require.Equal(t, "ten_1", resp.TenantID)
}

func TestCreateEnvironmentValidation(t *testing.T) {
	fake := &fakeInternalEnvironmentService{
		err: ierr.NewError("tenant_id or email is required").Mark(ierr.ErrValidation),
	}
	server := newTestServer(fake)

	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewReader([]byte(`{"name":"Production","type":"production"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPublicRouteNotMounted(t *testing.T) {
	server := newTestServer(&fakeInternalEnvironmentService{})
	req := httptest.NewRequest(http.MethodGet, "/v1/customers", nil)
	rec := httptest.NewRecorder()
	server.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
