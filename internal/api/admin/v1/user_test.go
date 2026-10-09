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

type stubUserService struct {
	addCalled bool
	addReq    admin.AddUserRequest
	addResp   *admin.UserResponse
	addErr    error

	removeCalled bool
	removeReq    admin.RemoveUserRequest
	removeResp   *admin.RemoveUserResponse
	removeErr    error
}

func (s *stubUserService) AddUser(_ context.Context, req admin.AddUserRequest) (*admin.UserResponse, error) {
	s.addCalled = true
	s.addReq = req
	return s.addResp, s.addErr
}

func (s *stubUserService) RemoveUser(_ context.Context, req admin.RemoveUserRequest) (*admin.RemoveUserResponse, error) {
	s.removeCalled = true
	s.removeReq = req
	return s.removeResp, s.removeErr
}

const (
	addUserBody    = `{"tenant_id":"tenant_1","email":"teammate@acme.com","password":"chosen-by-operator","role":"all_writer"}`
	removeUserBody = `{"tenant_id":"tenant_1","email":"teammate@acme.com"}`
)

func TestAddUser_Success(t *testing.T) {
	stub := &stubUserService{
		addResp: &admin.UserResponse{UserID: "user_1", Email: "teammate@acme.com", TenantID: "tenant_1", Roles: []string{"all_writer"}},
	}
	router := setupAdminRouter(http.MethodPost, "/v1/users", NewUserHandler(stub).AddUser)

	w := doAdminRequest(router, http.MethodPost, "/v1/users", addUserBody)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, admin.AddUserRequest{
		TenantID: "tenant_1",
		Email:    "teammate@acme.com",
		Password: "chosen-by-operator",
		Role:     types.RoleAllWriter,
	}, stub.addReq)
	assert.JSONEq(t, `{"user_id":"user_1","email":"teammate@acme.com","tenant_id":"tenant_1","roles":["all_writer"]}`, w.Body.String())
}

func TestAddUser_Errors(t *testing.T) {
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
			body:       addUserBody,
			serviceErr: ierr.NewError("tenant not found").Mark(ierr.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantCalled: true,
		},
		{
			name:       "email already in use",
			body:       addUserBody,
			serviceErr: ierr.NewError("email already in use").Mark(ierr.ErrAlreadyExists),
			wantStatus: http.StatusConflict,
			wantCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubUserService{addErr: tc.serviceErr}
			router := setupAdminRouter(http.MethodPost, "/v1/users", NewUserHandler(stub).AddUser)

			w := doAdminRequest(router, http.MethodPost, "/v1/users", tc.body)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCalled, stub.addCalled)
		})
	}
}

func TestRemoveUser_Success(t *testing.T) {
	stub := &stubUserService{
		removeResp: &admin.RemoveUserResponse{UserID: "user_1", Email: "teammate@acme.com", TenantID: "tenant_1", Status: types.StatusArchived},
	}
	router := setupAdminRouter(http.MethodPost, "/v1/users/remove", NewUserHandler(stub).RemoveUser)

	w := doAdminRequest(router, http.MethodPost, "/v1/users/remove", removeUserBody)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, admin.RemoveUserRequest{TenantID: "tenant_1", Email: "teammate@acme.com"}, stub.removeReq)
	assert.JSONEq(t, `{"user_id":"user_1","email":"teammate@acme.com","tenant_id":"tenant_1","status":"archived"}`, w.Body.String())
}

func TestRemoveUser_Errors(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
		wantCalled bool
	}{
		{name: "malformed body", body: `{"tenant_id":`, wantStatus: http.StatusBadRequest},
		{
			name:       "user not in this tenant",
			body:       removeUserBody,
			serviceErr: ierr.NewError("user not found in this tenant").Mark(ierr.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantCalled: true,
		},
		{
			name:       "last user of the tenant",
			body:       removeUserBody,
			serviceErr: ierr.NewError("cannot remove the last user in the tenant").Mark(ierr.ErrValidation),
			wantStatus: http.StatusBadRequest,
			wantCalled: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubUserService{removeErr: tc.serviceErr}
			router := setupAdminRouter(http.MethodPost, "/v1/users/remove", NewUserHandler(stub).RemoveUser)

			w := doAdminRequest(router, http.MethodPost, "/v1/users/remove", tc.body)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantCalled, stub.removeCalled)
		})
	}
}
