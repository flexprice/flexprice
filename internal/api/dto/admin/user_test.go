package admin

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseValidAddUserRequest returns an AddUserRequest that passes Validate,
// so tests can vary a single field in isolation.
func baseValidAddUserRequest() AddUserRequest {
	return AddUserRequest{TenantID: "tenant_1", Email: "teammate@acme.com", Password: "chosen-by-operator"}
}

func TestAddUserRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		modify  func(r *AddUserRequest)
		wantErr bool
	}{
		{name: "valid request without a role", modify: func(*AddUserRequest) {}},
		{name: "super_admin role", modify: func(r *AddUserRequest) { r.Role = types.RoleSuperAdmin }},
		{name: "all_writer role", modify: func(r *AddUserRequest) { r.Role = types.RoleAllWriter }},
		{name: "all_reader role", modify: func(r *AddUserRequest) { r.Role = types.RoleAllReader }},
		{name: "missing tenant id", modify: func(r *AddUserRequest) { r.TenantID = "" }, wantErr: true},
		{name: "blank tenant id", modify: func(r *AddUserRequest) { r.TenantID = "   " }, wantErr: true},
		{name: "missing email", modify: func(r *AddUserRequest) { r.Email = "" }, wantErr: true},
		{name: "malformed email", modify: func(r *AddUserRequest) { r.Email = "teammate-at-acme" }, wantErr: true},
		{name: "password of 7 characters", modify: func(r *AddUserRequest) { r.Password = "1234567" }, wantErr: true},
		{name: "unknown role", modify: func(r *AddUserRequest) { r.Role = "owner" }, wantErr: true},
		{name: "role only service accounts can hold", modify: func(r *AddUserRequest) { r.Role = types.RoleEventIngestor }, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseValidAddUserRequest()
			tc.modify(&req)

			err := req.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsValidation(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestAddUserRequest_ValidateNormalises(t *testing.T) {
	req := AddUserRequest{TenantID: " tenant_1 ", Email: " Teammate@ACME.com ", Password: "chosen-by-operator"}

	require.NoError(t, req.Validate())
	assert.Equal(t, "tenant_1", req.TenantID)
	assert.Equal(t, "teammate@acme.com", req.Email)
}

func TestRemoveUserRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     RemoveUserRequest
		wantErr bool
	}{
		{name: "valid request", req: RemoveUserRequest{TenantID: "tenant_1", Email: "teammate@acme.com"}},
		{name: "missing tenant id", req: RemoveUserRequest{Email: "teammate@acme.com"}, wantErr: true},
		{name: "missing email", req: RemoveUserRequest{TenantID: "tenant_1"}, wantErr: true},
		{name: "malformed email", req: RemoveUserRequest{TenantID: "tenant_1", Email: "teammate-at-acme"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsValidation(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRemoveUserRequest_ValidateNormalises(t *testing.T) {
	req := RemoveUserRequest{TenantID: " tenant_1 ", Email: " TeamMate@ACME.com "}

	require.NoError(t, req.Validate())
	assert.Equal(t, "tenant_1", req.TenantID)
	assert.Equal(t, "teammate@acme.com", req.Email)
}

func TestUserRequests_ValidateNil(t *testing.T) {
	var add *AddUserRequest
	err := add.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	var remove *RemoveUserRequest
	err = remove.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestNewUserResponse(t *testing.T) {
	resp := NewUserResponse(&user.User{
		ID:        "user_1",
		Email:     "teammate@acme.com",
		Roles:     []string{"all_writer"},
		BaseModel: types.BaseModel{TenantID: "tenant_1"},
	})

	assert.Equal(t, &UserResponse{UserID: "user_1", Email: "teammate@acme.com", TenantID: "tenant_1", Roles: []string{"all_writer"}}, resp)
	assert.Nil(t, NewUserResponse(nil))
}

func TestNewRemoveUserResponse(t *testing.T) {
	// The row is read before it is archived, so the response must say archived whatever status it was read with.
	resp := NewRemoveUserResponse(&user.User{
		ID:        "user_1",
		Email:     "teammate@acme.com",
		BaseModel: types.BaseModel{TenantID: "tenant_1", Status: types.StatusPublished},
	})

	assert.Equal(t, &RemoveUserResponse{UserID: "user_1", Email: "teammate@acme.com", TenantID: "tenant_1", Status: types.StatusArchived}, resp)
	assert.Nil(t, NewRemoveUserResponse(nil))
}
