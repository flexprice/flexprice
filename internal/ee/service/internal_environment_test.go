package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestInternalCreateEnvironment(t *testing.T) {
	const (
		tenantID = "ten_1"
		email    = "owner@example.com"
	)

	tests := []struct {
		name    string
		req     dto.InternalCreateEnvironmentRequest
		setup   func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore)
		wantErr bool
	}{
		{
			name: "by tenant id",
			req:  dto.InternalCreateEnvironmentRequest{Name: "Production", Type: "production", TenantID: tenantID},
			setup: func(_ *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
			},
		},
		{
			name: "by email",
			req:  dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development", Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
		},
		{
			name: "tenant id and email agree",
			req:  dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development", TenantID: tenantID, Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
		},
		{
			name: "tenant id and email disagree",
			req:  dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development", TenantID: "ten_other", Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
			wantErr: true,
		},
		{
			name:    "missing tenant",
			req:     dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development"},
			wantErr: true,
		},
		{
			name:    "unknown tenant",
			req:     dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development", TenantID: "missing"},
			wantErr: true,
		},
		{
			name:    "unknown email",
			req:     dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "development", Email: "missing@example.com"},
			wantErr: true,
		},
		{
			name: "invalid type",
			req:  dto.InternalCreateEnvironmentRequest{Name: "Sandbox", Type: "sandbox", TenantID: tenantID},
			setup: func(_ *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
			},
			wantErr: true,
		},
		{
			name:    "missing name",
			req:     dto.InternalCreateEnvironmentRequest{Type: "development", TenantID: tenantID},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := testutil.NewInMemoryUserStore()
			tenants := testutil.NewInMemoryTenantStore()
			environments := testutil.NewInMemoryEnvironmentStore()
			if tt.setup != nil {
				tt.setup(users, tenants)
			}

			svc := NewInternalEnvironmentService(ServiceParams{
				UserRepo:        users,
				TenantRepo:      tenants,
				EnvironmentRepo: environments,
			})
			resp, err := svc.CreateEnvironment(context.Background(), tt.req)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.req.Name, resp.Name)
			require.Equal(t, tt.req.Type, resp.Type)
			require.Equal(t, tenantID, resp.TenantID)
			require.NotEmpty(t, resp.ID)

			stored, err := environments.Get(context.Background(), resp.ID)
			require.NoError(t, err)
			require.Equal(t, tenantID, stored.TenantID)
			if tt.req.Email != "" {
				require.NotEmpty(t, stored.CreatedBy)
			}
		})
	}
}
