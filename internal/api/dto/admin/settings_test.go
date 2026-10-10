package admin

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTenantConfigRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     UpdateTenantConfigRequest
		wantErr bool
	}{
		{name: "only production", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{Production: lo.ToPtr(2)}}},
		{name: "only development", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{Development: lo.ToPtr(5)}}},
		{name: "only max_users", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{MaxUsers: lo.ToPtr(20)}}},
		{name: "production of zero", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{Production: lo.ToPtr(0)}}},
		{name: "max_users of 1", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{MaxUsers: lo.ToPtr(1)}}},
		{name: "missing tenant id", req: UpdateTenantConfigRequest{Value: TenantConfigValue{Development: lo.ToPtr(5)}}, wantErr: true},
		{name: "no limits sent", req: UpdateTenantConfigRequest{TenantID: "tenant_1"}, wantErr: true},
		{name: "negative production", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{Production: lo.ToPtr(-1)}}, wantErr: true},
		{name: "negative development", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{Development: lo.ToPtr(-1)}}, wantErr: true},
		{name: "max_users of 0", req: UpdateTenantConfigRequest{TenantID: "tenant_1", Value: TenantConfigValue{MaxUsers: lo.ToPtr(0)}}, wantErr: true},
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

func TestUpdateTenantConfigRequest_ValidateTrimsTenantID(t *testing.T) {
	req := UpdateTenantConfigRequest{TenantID: " tenant_1 ", Value: TenantConfigValue{Development: lo.ToPtr(5)}}

	require.NoError(t, req.Validate())
	assert.Equal(t, "tenant_1", req.TenantID)
}

func TestUpdateTenantConfigRequest_ValidateNil(t *testing.T) {
	var req *UpdateTenantConfigRequest

	err := req.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestUpdateTenantConfigRequest_ToSettingValue(t *testing.T) {
	cases := []struct {
		name  string
		value TenantConfigValue
		want  map[string]interface{}
	}{
		{name: "only the limit sent", value: TenantConfigValue{Development: lo.ToPtr(5)}, want: map[string]interface{}{"development": 5}},
		{name: "an explicit zero is kept", value: TenantConfigValue{Production: lo.ToPtr(0)}, want: map[string]interface{}{"production": 0}},
		{
			name:  "all three limits",
			value: TenantConfigValue{Production: lo.ToPtr(1), Development: lo.ToPtr(2), MaxUsers: lo.ToPtr(3)},
			want:  map[string]interface{}{"production": 1, "development": 2, "max_users": 3},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := UpdateTenantConfigRequest{TenantID: "tenant_1", Value: tc.value}
			assert.Equal(t, tc.want, req.ToSettingValue())
		})
	}

	var nilReq *UpdateTenantConfigRequest
	assert.Empty(t, nilReq.ToSettingValue())
}

func TestNewTenantConfigResponse(t *testing.T) {
	resp := NewTenantConfigResponse("tenant_1", types.TenantConfig{Production: 1, Development: 2, MaxUsers: 3})

	assert.Equal(t, &TenantConfigResponse{TenantID: "tenant_1", Production: 1, Development: 2, MaxUsers: 3}, resp)
}
