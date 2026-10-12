package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
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

func TestListTenantsRequest_Validate(t *testing.T) {
	cases := []struct {
		name      string
		req       ListTenantsRequest
		wantLimit int
		wantErr   bool
	}{
		{name: "no values gives a page of 50", req: ListTenantsRequest{}, wantLimit: 50},
		{name: "limit of 100", req: ListTenantsRequest{Limit: 100}, wantLimit: 100},
		{name: "known internal status", req: ListTenantsRequest{InternalStatus: types.TenantInternalStatusSuspended}, wantLimit: 50},
		{name: "limit above 100", req: ListTenantsRequest{Limit: 101}, wantErr: true},
		{name: "negative limit", req: ListTenantsRequest{Limit: -1}, wantErr: true},
		{name: "negative offset", req: ListTenantsRequest{Offset: -1}, wantErr: true},
		{name: "unknown internal status", req: ListTenantsRequest{InternalStatus: "deleted"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req

			err := req.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, ierr.IsValidation(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLimit, req.Limit)
		})
	}
}

func TestListTenantsRequest_ValidateTrimsSearch(t *testing.T) {
	req := ListTenantsRequest{Search: "  Sarvam "}

	require.NoError(t, req.Validate())
	assert.Equal(t, "Sarvam", req.Search)
}

func TestListTenantsRequest_ValidateNil(t *testing.T) {
	var req *ListTenantsRequest

	err := req.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestNewListTenantsResponse(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	page := []*tenant.Tenant{{
		ID:             "tenant_1",
		Name:           "Acme Inc",
		Status:         types.StatusPublished,
		InternalStatus: types.TenantInternalStatusActive,
		CreatedAt:      time.Date(2026, 4, 1, 15, 30, 0, 0, ist),
	}}

	body, err := json.Marshal(NewListTenantsResponse(page, 132, 50, 0))

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"items": [{"id":"tenant_1","name":"Acme Inc","status":"published","internal_status":"active","created_at":"2026-04-01T10:00:00Z"}],
		"pagination": {"total":132,"limit":50,"offset":0}
	}`, string(body))
	assert.Nil(t, NewTenantResponse(nil))
}

func TestNewListTenantsResponse_EmptyPage(t *testing.T) {
	body, err := json.Marshal(NewListTenantsResponse(nil, 4, 50, 10))

	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"pagination":{"total":4,"limit":50,"offset":10}}`, string(body))
}

func TestNewTenantDetailResponse(t *testing.T) {
	ist := time.FixedZone("IST", 5*60*60+30*60)
	acme := &tenant.Tenant{
		ID:             "tenant_1",
		Name:           "Acme Inc",
		Status:         types.StatusPublished,
		InternalStatus: types.TenantInternalStatusActive,
		BillingDetails: tenant.TenantBillingDetails{
			Email:     "billing@acme.com",
			HelpEmail: "help@acme.com",
			Phone:     "+1 555 0100",
			Address: tenant.TenantAddress{
				Line1:      "1 Market St",
				City:       "San Francisco",
				State:      "CA",
				PostalCode: "94105",
				Country:    "US",
			},
		},
		Metadata:  types.Metadata{"plan": "enterprise"},
		CreatedAt: time.Date(2026, 4, 1, 15, 30, 0, 0, ist),
		UpdatedAt: time.Date(2026, 9, 20, 13, 45, 0, 0, ist),
	}

	body, err := json.Marshal(NewTenantDetailResponse(acme))

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id": "tenant_1",
		"name": "Acme Inc",
		"status": "published",
		"internal_status": "active",
		"billing_details": {
			"email": "billing@acme.com",
			"help_email": "help@acme.com",
			"phone": "+1 555 0100",
			"address": {
				"address_line1": "1 Market St",
				"address_line2": "",
				"address_city": "San Francisco",
				"address_state": "CA",
				"address_postal_code": "94105",
				"address_country": "US"
			}
		},
		"metadata": {"plan": "enterprise"},
		"created_at": "2026-04-01T10:00:00Z",
		"updated_at": "2026-09-20T08:15:00Z"
	}`, string(body))
}

func TestNewTenantDetailResponse_NoBillingDetailsOrMetadata(t *testing.T) {
	bare := &tenant.Tenant{
		ID:             "tenant_1",
		Name:           "Acme Inc",
		Status:         types.StatusPublished,
		InternalStatus: types.TenantInternalStatusTrialing,
		CreatedAt:      time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
	}

	body, err := json.Marshal(NewTenantDetailResponse(bare))

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id": "tenant_1",
		"name": "Acme Inc",
		"status": "published",
		"internal_status": "trialing",
		"created_at": "2026-04-01T10:00:00Z",
		"updated_at": "2026-04-01T10:00:00Z"
	}`, string(body))
	assert.Nil(t, NewTenantDetailResponse(nil))
}
