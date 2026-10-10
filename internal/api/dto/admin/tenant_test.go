package admin

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseValidCreateTenantRequest returns a CreateTenantRequest that passes Validate,
// so tests can vary a single field in isolation.
func baseValidCreateTenantRequest() CreateTenantRequest {
	return CreateTenantRequest{TenantName: "Acme", Email: "owner@acme.com", Password: "chosen-by-operator"}
}

func TestCreateTenantRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		modify  func(r *CreateTenantRequest)
		wantErr bool
	}{
		{name: "valid request", modify: func(*CreateTenantRequest) {}},
		{name: "password of exactly 8 characters", modify: func(r *CreateTenantRequest) { r.Password = "12345678" }},
		{name: "missing tenant name", modify: func(r *CreateTenantRequest) { r.TenantName = "" }, wantErr: true},
		{name: "blank tenant name", modify: func(r *CreateTenantRequest) { r.TenantName = "   " }, wantErr: true},
		{name: "missing email", modify: func(r *CreateTenantRequest) { r.Email = "" }, wantErr: true},
		{name: "malformed email", modify: func(r *CreateTenantRequest) { r.Email = "owner-at-acme" }, wantErr: true},
		{name: "password of 7 characters", modify: func(r *CreateTenantRequest) { r.Password = "1234567" }, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseValidCreateTenantRequest()
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

func TestCreateTenantRequest_ValidateNormalises(t *testing.T) {
	req := CreateTenantRequest{TenantName: "  Acme  ", Email: "  Owner@ACME.com ", Password: "chosen-by-operator"}

	require.NoError(t, req.Validate())
	assert.Equal(t, "Acme", req.TenantName)
	assert.Equal(t, "owner@acme.com", req.Email)
}

func TestCreateTenantRequest_ValidateNil(t *testing.T) {
	var req *CreateTenantRequest

	err := req.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestNewCreateTenantResponse(t *testing.T) {
	owner := &user.User{ID: "user_1", Email: "owner@acme.com"}
	envs := []*environment.Environment{
		{ID: "env_sandbox", Name: "Sandbox", Type: types.EnvironmentDevelopment, BaseModel: types.BaseModel{TenantID: "tenant_1"}},
		{ID: "env_prod", Name: "Production", Type: types.EnvironmentProduction, BaseModel: types.BaseModel{TenantID: "tenant_1"}},
	}

	resp := NewCreateTenantResponse(&dto.TenantResponse{ID: "tenant_1", Name: "Acme"}, owner, envs)

	require.NotNil(t, resp)
	assert.Equal(t, "tenant_1", resp.TenantID)
	assert.Equal(t, "Acme", resp.TenantName)
	assert.Equal(t, "user_1", resp.UserID)
	assert.Equal(t, "owner@acme.com", resp.Email)
	require.Len(t, resp.Environments, 2)
	assert.Equal(t, "env_sandbox", resp.Environments[0].ID)
	assert.Equal(t, types.EnvironmentProduction, resp.Environments[1].Type)

	assert.Nil(t, NewCreateTenantResponse(nil, owner, envs))
	assert.Nil(t, NewCreateTenantResponse(&dto.TenantResponse{ID: "tenant_1"}, nil, envs))
}
