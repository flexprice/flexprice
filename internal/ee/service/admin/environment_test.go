package admin

import (
	"context"
	"fmt"
	"testing"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateEnvironment(t *testing.T) {
	const (
		tenantID = "ten_1"
		email    = "owner@example.com"
	)

	tests := []struct {
		name    string
		req     admindto.CreateEnvironmentRequest
		setup   func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore)
		wantErr bool
	}{
		{
			name: "by tenant id",
			req:  admindto.CreateEnvironmentRequest{Name: "Production", Type: types.EnvironmentProduction, TenantID: tenantID},
			setup: func(_ *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
			},
		},
		{
			name: "by email",
			req:  admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment, Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
		},
		{
			name: "tenant id and email agree",
			req:  admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment, TenantID: tenantID, Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
		},
		{
			name: "tenant id and email disagree",
			req:  admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment, TenantID: "ten_other", Email: email},
			setup: func(users *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
				require.NoError(t, users.Create(context.Background(), user.NewUser(email, tenantID)))
			},
			wantErr: true,
		},
		{
			name:    "missing tenant",
			req:     admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment},
			wantErr: true,
		},
		{
			name:    "unknown tenant",
			req:     admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment, TenantID: "missing"},
			wantErr: true,
		},
		{
			name:    "unknown email",
			req:     admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: types.EnvironmentDevelopment, Email: "missing@example.com"},
			wantErr: true,
		},
		{
			name: "invalid type",
			req:  admindto.CreateEnvironmentRequest{Name: "Sandbox", Type: "sandbox", TenantID: tenantID},
			setup: func(_ *testutil.InMemoryUserStore, tenants *testutil.InMemoryTenantStore) {
				require.NoError(t, tenants.Create(context.Background(), &tenant.Tenant{ID: tenantID, Name: "Acme"}))
			},
			wantErr: true,
		},
		{
			name:    "missing name",
			req:     admindto.CreateEnvironmentRequest{Type: types.EnvironmentDevelopment, TenantID: tenantID},
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

			svc := NewEnvironmentService(service.ServiceParams{
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

// tenantScopedEnvironmentStore lists like the Postgres repository: it needs a tenant on the context
// and returns only that tenant's environments, which the in-memory store does not check.
type tenantScopedEnvironmentStore struct {
	*testutil.InMemoryEnvironmentStore
}

func (s tenantScopedEnvironmentStore) List(ctx context.Context, filter types.Filter) ([]*environment.Environment, error) {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID not found in context").Mark(ierr.ErrValidation)
	}
	envs, err := s.InMemoryEnvironmentStore.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return lo.Filter(envs, func(e *environment.Environment, _ int) bool { return e.TenantID == tenantID }), nil
}

func TestListEnvironments(t *testing.T) {
	newEnv := func(id, tenantID string, envType types.EnvironmentType) *environment.Environment {
		return &environment.Environment{
			ID:        id,
			Name:      envType.DisplayTitle(),
			Type:      envType,
			BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished},
		}
	}

	tests := []struct {
		name    string
		envs    []*environment.Environment
		wantIDs []string
	}{
		{
			name: "only the tenant's environments",
			envs: []*environment.Environment{
				newEnv("env_sandbox", "ten_1", types.EnvironmentDevelopment),
				newEnv("env_prod", "ten_1", types.EnvironmentProduction),
				newEnv("env_other", "ten_2", types.EnvironmentDevelopment),
			},
			wantIDs: []string{"env_sandbox", "env_prod"},
		},
		{
			name:    "a tenant with no environments",
			envs:    []*environment.Environment{newEnv("env_other", "ten_2", types.EnvironmentDevelopment)},
			wantIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tenants := testutil.NewInMemoryTenantStore()
			require.NoError(t, tenants.Create(ctx, &tenant.Tenant{ID: "ten_1", Name: "Acme"}))
			environments := testutil.NewInMemoryEnvironmentStore()
			for _, env := range tt.envs {
				require.NoError(t, environments.Create(ctx, env))
			}
			svc := NewEnvironmentService(service.ServiceParams{
				TenantRepo:      tenants,
				EnvironmentRepo: tenantScopedEnvironmentStore{InMemoryEnvironmentStore: environments},
			})

			resp, err := svc.ListEnvironments(ctx, "ten_1")

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Items, "an empty list must marshal as [], not null")
			ids := lo.Map(resp.Items, func(item *admindto.EnvironmentResponse, _ int) string { return item.ID })
			assert.ElementsMatch(t, tt.wantIDs, ids)
		})
	}
}

func TestListEnvironmentsUnknownTenant(t *testing.T) {
	environments := testutil.NewInMemoryEnvironmentStore()
	svc := NewEnvironmentService(service.ServiceParams{
		TenantRepo:      notFoundTenantStore{InMemoryTenantStore: testutil.NewInMemoryTenantStore()},
		EnvironmentRepo: tenantScopedEnvironmentStore{InMemoryEnvironmentStore: environments},
	})

	resp, err := svc.ListEnvironments(context.Background(), "ten_missing")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Nil(t, resp)
}

// pagedEnvironmentStore pages a fixed, ordered list of environments by limit and offset, as Postgres does.
type pagedEnvironmentStore struct {
	*testutil.InMemoryEnvironmentStore
	envs []*environment.Environment
}

func (s pagedEnvironmentStore) List(_ context.Context, filter types.Filter) ([]*environment.Environment, error) {
	start := min(filter.Offset, len(s.envs))
	end := min(start+filter.Limit, len(s.envs))
	return s.envs[start:end], nil
}

func TestListEnvironmentsReadsEveryPage(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "exactly one full page", count: environmentPageSize},
		{name: "one more than a page", count: environmentPageSize + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			tenants := testutil.NewInMemoryTenantStore()
			require.NoError(t, tenants.Create(ctx, &tenant.Tenant{ID: "ten_1", Name: "Acme"}))
			envs := make([]*environment.Environment, tt.count)
			for i := range envs {
				envs[i] = &environment.Environment{ID: fmt.Sprintf("env_%d", i), BaseModel: types.BaseModel{TenantID: "ten_1"}}
			}
			svc := NewEnvironmentService(service.ServiceParams{
				TenantRepo:      tenants,
				EnvironmentRepo: pagedEnvironmentStore{InMemoryEnvironmentStore: testutil.NewInMemoryEnvironmentStore(), envs: envs},
			})

			resp, err := svc.ListEnvironments(ctx, "ten_1")

			require.NoError(t, err)
			require.Equal(t, tt.count, len(resp.Items))
			ids := lo.Map(resp.Items, func(item *admindto.EnvironmentResponse, _ int) string { return item.ID })
			wantIDs := lo.Map(envs, func(e *environment.Environment, _ int) string { return e.ID })
			assert.Equal(t, wantIDs, ids, "pages must be joined in the order they were read")
		})
	}
}

// failingListEnvironmentStore fails every listing, like a database error while reading a tenant's environments.
type failingListEnvironmentStore struct {
	*testutil.InMemoryEnvironmentStore
}

func (failingListEnvironmentStore) List(context.Context, types.Filter) ([]*environment.Environment, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func TestListEnvironmentsReadError(t *testing.T) {
	ctx := context.Background()
	tenants := testutil.NewInMemoryTenantStore()
	require.NoError(t, tenants.Create(ctx, &tenant.Tenant{ID: "ten_1", Name: "Acme"}))
	svc := NewEnvironmentService(service.ServiceParams{
		TenantRepo:      tenants,
		EnvironmentRepo: failingListEnvironmentStore{InMemoryEnvironmentStore: testutil.NewInMemoryEnvironmentStore()},
	})

	resp, err := svc.ListEnvironments(ctx, "ten_1")

	require.Error(t, err)
	assert.True(t, ierr.IsDatabase(err))
	assert.Nil(t, resp)
}
