package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	domainMembership "github.com/flexprice/flexprice/internal/domain/tenantmembership"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func newMembershipTestService(t *testing.T) (*userService, *testutil.InMemoryUserStore, *testutil.InMemoryTenantMembershipStore, *testutil.InMemoryTenantStore) {
	t.Helper()
	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   "test-secret-key-32-bytes-minimum!",
		},
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	rbacSvc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	require.NoError(t, err)

	userRepo := testutil.NewInMemoryUserStore()
	membershipRepo := testutil.NewInMemoryTenantMembershipStore()
	tenantRepo := testutil.NewInMemoryTenantStore()
	svc := &userService{
		userRepo:       userRepo,
		membershipRepo: membershipRepo,
		tenantRepo:     tenantRepo,
		secretRepo:     testutil.NewInMemorySecretStore(),
		db:             testutil.NewMockPostgresClient(nil),
		cfg:            cfg,
		rbacService:    rbacSvc,
		settingsService: &settingsService{
			ServiceParams: ServiceParams{SettingsRepo: testutil.NewInMemorySettingsStore()},
		},
		logger: log,
	}
	return svc, userRepo, membershipRepo, tenantRepo
}

func seedMembershipUser(t *testing.T, userRepo *testutil.InMemoryUserStore, membershipRepo *testutil.InMemoryTenantMembershipStore, id, email, tenantID string, roles []string) *user.User {
	t.Helper()
	ctx := types.SetTenantID(context.Background(), tenantID)
	u := &user.User{
		ID:        id,
		Email:     email,
		Type:      types.UserTypeUser,
		Roles:     roles,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	u.TenantID = tenantID
	u.Status = types.StatusPublished
	require.NoError(t, userRepo.Create(ctx, u))
	require.NoError(t, membershipRepo.Create(ctx, domainMembership.New(id, tenantID, roles)))
	return u
}

func TestInviteExistingUserJoinsTenant(t *testing.T) {
	svc, userRepo, membershipRepo, tenantRepo := newMembershipTestService(t)
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: types.DefaultTenantID, Name: "Home"}))
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: "tenant_other", Name: "Other"}))

	homeRoles := []string{types.RoleSuperAdmin.String()}
	existing := seedMembershipUser(t, userRepo, membershipRepo, "user_existing", "shared@example.com", "tenant_other", homeRoles)
	seedMembershipUser(t, userRepo, membershipRepo, "actor", "actor@example.com", types.DefaultTenantID, []string{types.RoleSuperAdmin.String()})

	ctx := types.SetTenantID(context.Background(), types.DefaultTenantID)
	ctx = types.SetUserID(ctx, "actor")
	ctx = context.WithValue(ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})

	joined, password, err := svc.InviteUser(ctx, &dto.CreateUserRequest{
		Type:  types.UserTypeUser,
		Email: existing.Email,
		Roles: []string{types.RoleAllReader.String()},
	}, "actor")
	require.NoError(t, err)
	require.Nil(t, password)
	require.Equal(t, existing.ID, joined.ID)
	require.Equal(t, types.DefaultTenantID, joined.TenantID)
	require.Equal(t, []string{types.RoleAllReader.String()}, joined.Roles)

	mem, err := membershipRepo.GetByUserAndTenant(ctx, existing.ID, types.DefaultTenantID)
	require.NoError(t, err)
	require.Equal(t, []string{types.RoleAllReader.String()}, mem.Roles)

	home, err := membershipRepo.GetByUserAndTenant(ctx, existing.ID, "tenant_other")
	require.NoError(t, err)
	require.Equal(t, homeRoles, home.Roles)
}

func TestLeaveTenantKeepsIdentityWhenOtherMembershipRemains(t *testing.T) {
	svc, userRepo, membershipRepo, tenantRepo := newMembershipTestService(t)
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: types.DefaultTenantID, Name: "Home"}))
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: "tenant_other", Name: "Other"}))

	roles := []string{types.RoleSuperAdmin.String()}
	u := seedMembershipUser(t, userRepo, membershipRepo, "user_multi", "multi@example.com", types.DefaultTenantID, roles)
	require.NoError(t, membershipRepo.Create(context.Background(), domainMembership.New(u.ID, "tenant_other", []string{types.RoleAllReader.String()})))
	seedMembershipUser(t, userRepo, membershipRepo, "user_other", "other@example.com", types.DefaultTenantID, roles)

	ctx := types.SetTenantID(context.Background(), types.DefaultTenantID)
	ctx = types.SetUserID(ctx, u.ID)
	ctx = context.WithValue(ctx, types.CtxRoles, roles)

	require.NoError(t, svc.LeaveTenant(ctx))

	_, err := membershipRepo.GetByUserAndTenant(ctx, u.ID, types.DefaultTenantID)
	require.Error(t, err)

	still, err := userRepo.GetByIDGlobal(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, types.StatusPublished, still.Status)

	otherMem, err := membershipRepo.GetByUserAndTenant(ctx, u.ID, "tenant_other")
	require.NoError(t, err)
	require.Equal(t, []string{types.RoleAllReader.String()}, otherMem.Roles)
}

func TestListMyTenantsAndSwitchTenant(t *testing.T) {
	svc, userRepo, membershipRepo, tenantRepo := newMembershipTestService(t)
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: types.DefaultTenantID, Name: "Home"}))
	require.NoError(t, tenantRepo.Create(context.Background(), &tenant.Tenant{ID: "tenant_other", Name: "Other"}))

	roles := []string{types.RoleSuperAdmin.String()}
	u := seedMembershipUser(t, userRepo, membershipRepo, "user_switch", "switch@example.com", types.DefaultTenantID, roles)
	require.NoError(t, membershipRepo.Create(context.Background(), domainMembership.New(u.ID, "tenant_other", []string{types.RoleAllReader.String()})))

	ctx := types.SetTenantID(context.Background(), types.DefaultTenantID)
	ctx = types.SetUserID(ctx, u.ID)

	listed, err := svc.ListMyTenants(ctx)
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	resp, err := svc.SwitchTenant(ctx, &dto.SwitchTenantRequest{TenantID: "tenant_other"})
	require.NoError(t, err)
	require.Equal(t, u.ID, resp.UserID)
	require.Equal(t, "tenant_other", resp.TenantID)
	require.NotEmpty(t, resp.Token)
}
