package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type UserServiceSuite struct {
	suite.Suite
	ctx             context.Context
	userService     *userService
	userRepo        *testutil.InMemoryUserStore
	tenantRepo      *testutil.InMemoryTenantStore
	secretRepo      *testutil.InMemorySecretStore
	environmentRepo *testutil.InMemoryEnvironmentStore
	db              postgres.IClient
}

func TestUserService(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

func (s *UserServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.userRepo = testutil.NewInMemoryUserStore()
	s.tenantRepo = testutil.NewInMemoryTenantStore()
	s.secretRepo = testutil.NewInMemorySecretStore()
	s.environmentRepo = testutil.NewInMemoryEnvironmentStore()
	s.db = testutil.NewMockPostgresClient(logger.NewNoopLogger())
	s.userService = &userService{
		userRepo:        s.userRepo,
		tenantRepo:      s.tenantRepo,
		secretRepo:      s.secretRepo,
		environmentRepo: s.environmentRepo,
		db:              s.db,
		rbacService:     nil,
		supabaseAuth:    nil,
		settingsService: nil,
	}

	err := s.tenantRepo.Create(s.ctx, &tenant.Tenant{
		ID:   types.DefaultTenantID,
		Name: "Test Tenant",
	})
	s.NoError(err)
}

func (s *UserServiceSuite) TestGetUserInfo() {
	testCases := []struct {
		name          string
		setup         func(ctx context.Context)
		contextUserID string
		expectedError bool
		expectedID    string
	}{
		{
			name: "user_found",
			setup: func(ctx context.Context) {
				_ = s.userRepo.Create(ctx, &user.User{
					ID:        "user-1",
					Email:     "test@example.com",
					BaseModel: types.GetDefaultBaseModel(ctx),
				})
			},
			contextUserID: "user-1",
			expectedError: false,
			expectedID:    "user-1",
		},
		{
			name:          "user_not_found",
			setup:         nil,
			contextUserID: "nonexistent-id",
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.userRepo = testutil.NewInMemoryUserStore()
			s.userService = &userService{
				userRepo:     s.userRepo,
				tenantRepo:   s.tenantRepo,
				rbacService:  nil,
				supabaseAuth: nil,
			}

			ctx := testutil.SetupContext()
			ctx = context.WithValue(ctx, types.CtxUserID, tc.contextUserID)

			if tc.setup != nil {
				tc.setup(ctx)
			}

			resp, err := s.userService.GetUserInfo(ctx)

			if tc.expectedError {
				s.Error(err)
				s.Nil(resp)
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.Equal(tc.expectedID, resp.ID)
			}
		})
	}
}

func (s *UserServiceSuite) TestCreateUser_TableDriven() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "test-actor")

	rbacSvc, _ := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "internal/config/rbac/roles.json"},
	})
	if rbacSvc == nil {
		rbacSvc, _ = rbac.NewRBACService(&config.Configuration{
			RBAC: config.RBACConfig{RolesConfigPath: "../internal/config/rbac/roles.json"},
		})
	}

	tests := []struct {
		name        string
		req         dto.CreateUserRequest
		setup       func() *userService
		wantErr     bool
		errContains string
	}{
		{
			name: "type_user_without_supabase_returns_error",
			req:  dto.CreateUserRequest{Type: types.UserTypeUser, Email: "u@example.com"},
			setup: func() *userService {
				return &userService{
					userRepo:        s.userRepo,
					tenantRepo:      s.tenantRepo,
					rbacService:     nil,
					supabaseAuth:    nil,
					settingsService: nil,
				}
			},
			wantErr:     true,
			errContains: "settings service not configured",
		},
		{
			name: "type_service_account_without_rbac_returns_error",
			req:  dto.CreateUserRequest{Type: types.UserTypeServiceAccount, Roles: []string{"event_ingestor"}},
			setup: func() *userService {
				return &userService{
					userRepo:        s.userRepo,
					tenantRepo:      s.tenantRepo,
					rbacService:     nil,
					supabaseAuth:    nil,
					settingsService: nil,
				}
			},
			wantErr:     true,
			errContains: "RBAC not configured",
		},
		{
			name: "invalid_user_type_returns_error",
			req:  dto.CreateUserRequest{Type: types.UserType("invalid"), Email: "u@example.com"},
			setup: func() *userService {
				return &userService{
					userRepo:        s.userRepo,
					tenantRepo:      s.tenantRepo,
					rbacService:     nil,
					supabaseAuth:    nil,
					settingsService: nil,
				}
			},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	if rbacSvc != nil {
		tests = append(tests, struct {
			name        string
			req         dto.CreateUserRequest
			setup       func() *userService
			wantErr     bool
			errContains string
		}{
			name: "type_service_account_success",
			req:  dto.CreateUserRequest{Type: types.UserTypeServiceAccount, Roles: []string{"event_ingestor"}},
			setup: func() *userService {
				return &userService{
					userRepo:        s.userRepo,
					tenantRepo:      s.tenantRepo,
					rbacService:     rbacSvc,
					supabaseAuth:    nil,
					settingsService: nil,
				}
			},
			wantErr:     false,
			errContains: "",
		})
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.userRepo = testutil.NewInMemoryUserStore()
			s.tenantRepo = testutil.NewInMemoryTenantStore()
			_ = s.tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "Test Tenant"})
			svc := tt.setup()

			resp, err := svc.CreateUser(ctx, &tt.req)

			if tt.wantErr {
				s.Error(err)
				s.Nil(resp)
				if tt.errContains != "" {
					s.Contains(err.Error(), tt.errContains)
				}
			} else {
				s.NoError(err)
				s.NotNil(resp)
				s.NotNil(resp.UserResponse)
				s.Equal(tt.req.Type, resp.UserResponse.Type)
			}
		})
	}
}

func (s *UserServiceSuite) TestUpdateUser_MetadataMerge() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "user-1")

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = types.DefaultTenantID
	baseModel.CreatedBy = "seed-user"
	baseModel.UpdatedBy = "seed-user"

	err := s.userRepo.Create(ctx, &user.User{
		ID:        "user-1",
		Email:     "test@example.com",
		Type:      types.UserTypeUser,
		Roles:     []string{},
		Metadata:  map[string]string{"region": "us", "plan": "basic"},
		BaseModel: baseModel,
	})
	s.NoError(err)

	resp, err := s.userService.UpdateUser(ctx, &dto.UpdateUserRequest{
		Metadata: map[string]string{"plan": "pro", "team": "growth"},
	})

	s.NoError(err)
	s.NotNil(resp)
	s.NotNil(resp.UserResponse)
	s.Equal("user-1", resp.ID)
	s.Equal("us", resp.Metadata["region"])
	s.Equal("pro", resp.Metadata["plan"])
	s.Equal("growth", resp.Metadata["team"])
}

func (s *UserServiceSuite) TestUpdateServiceAccount() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = types.DefaultTenantID

	seedSA := func() {
		s.userRepo = testutil.NewInMemoryUserStore()
		s.secretRepo = testutil.NewInMemorySecretStore()
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "sa-1",
			Name:      "old name",
			Type:      types.UserTypeServiceAccount,
			BaseModel: baseModel,
		})
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "user-1",
			Email:     "u@example.com",
			Type:      types.UserTypeUser,
			BaseModel: baseModel,
		})
		s.userService = &userService{
			userRepo:   s.userRepo,
			tenantRepo: s.tenantRepo,
			secretRepo: s.secretRepo,
			db:         s.db,
		}
	}

	s.Run("success_name_updated", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "sa-1", &dto.UpdateServiceAccountRequest{Name: "new name"})
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("new name", resp.Name)
	})

	s.Run("no_op_when_name_unchanged", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "sa-1", &dto.UpdateServiceAccountRequest{Name: "old name"})
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("old name", resp.Name)
	})

	s.Run("empty_id_returns_validation_error", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "", &dto.UpdateServiceAccountRequest{Name: "x"})
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "service account ID is required")
	})

	s.Run("empty_name_returns_validation_error", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "sa-1", &dto.UpdateServiceAccountRequest{Name: ""})
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("non_service_account_id_returns_not_found", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "user-1", &dto.UpdateServiceAccountRequest{Name: "x"})
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "service account not found")
	})

	s.Run("unknown_id_returns_not_found", func() {
		seedSA()
		resp, err := s.userService.UpdateServiceAccount(ctx, "sa-unknown", &dto.UpdateServiceAccountRequest{Name: "x"})
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("archived_service_account_cannot_be_updated", func() {
		seedSA()
		_ = s.userService.DeleteUser(ctx, "sa-1")
		resp, err := s.userService.UpdateServiceAccount(ctx, "sa-1", &dto.UpdateServiceAccountRequest{Name: "new name"})
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "archived")
	})
}

func (s *UserServiceSuite) TestDeleteUser() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = types.DefaultTenantID

	seedStore := func() {
		s.userRepo = testutil.NewInMemoryUserStore()
		s.secretRepo = testutil.NewInMemorySecretStore()
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "sa-1",
			Type:      types.UserTypeServiceAccount,
			BaseModel: baseModel,
		})
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "user-1",
			Email:     "u@example.com",
			Type:      types.UserTypeUser,
			BaseModel: baseModel,
		})
		s.userService = &userService{
			userRepo:   s.userRepo,
			tenantRepo: s.tenantRepo,
			secretRepo: s.secretRepo,
			db:         s.db,
		}
	}

	s.Run("success_service_account_archived", func() {
		seedStore()
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.NoError(err)
	})

	s.Run("second_delete_returns_not_found", func() {
		seedStore()
		_ = s.userService.DeleteUser(ctx, "sa-1")
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.Error(err)
		s.Contains(err.Error(), "not found")
	})

	s.Run("empty_id_returns_validation_error", func() {
		seedStore()
		err := s.userService.DeleteUser(ctx, "")
		s.Error(err)
		s.Contains(err.Error(), "service account ID is required")
	})

	s.Run("non_service_account_returns_validation_error", func() {
		seedStore()
		err := s.userService.DeleteUser(ctx, "user-1")
		s.Error(err)
		s.Contains(err.Error(), "only service accounts can be deleted")
	})

	s.Run("unknown_id_returns_not_found", func() {
		seedStore()
		err := s.userService.DeleteUser(ctx, "sa-unknown")
		s.Error(err)
	})

	s.Run("published_api_keys_across_envs_are_revoked", func() {
		seedStore()
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:            "key-sandbox",
			UserID:        "sa-1",
			UserType:      string(types.UserTypeServiceAccount),
			EnvironmentID: "env-sandbox",
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:            "key-prod",
			UserID:        "sa-1",
			UserType:      string(types.UserTypeServiceAccount),
			EnvironmentID: "env-prod",
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.NoError(err)

		archived, err := s.userRepo.GetByID(ctx, "sa-1")
		s.NoError(err)
		s.Equal(types.StatusArchived, archived.Status)

		keySandbox, err := s.secretRepo.Get(ctx, "key-sandbox")
		s.NoError(err)
		s.Equal(types.StatusDeleted, keySandbox.Status)

		keyProd, err := s.secretRepo.Get(ctx, "key-prod")
		s.NoError(err)
		s.Equal(types.StatusDeleted, keyProd.Status)
	})

	s.Run("expired_published_api_key_is_also_revoked", func() {
		seedStore()
		past := time.Now().Add(-24 * time.Hour)
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:            "key-expired",
			UserID:        "sa-1",
			UserType:      string(types.UserTypeServiceAccount),
			EnvironmentID: "env-sandbox",
			ExpiresAt:     &past,
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.NoError(err)

		key, err := s.secretRepo.Get(ctx, "key-expired")
		s.NoError(err)
		s.Equal(types.StatusDeleted, key.Status)
	})
}

func (s *UserServiceSuite) TestGetUser() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")
	// Request may be env-scoped; GetUser must still return keys across envs.
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env-sandbox")

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = types.DefaultTenantID

	s.userRepo = testutil.NewInMemoryUserStore()
	s.secretRepo = testutil.NewInMemorySecretStore()
	s.environmentRepo = testutil.NewInMemoryEnvironmentStore()
	_ = s.environmentRepo.Create(ctx, &environment.Environment{
		ID:   "env-sandbox",
		Name: "Sandbox",
		Type: types.EnvironmentDevelopment,
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	})
	_ = s.environmentRepo.Create(ctx, &environment.Environment{
		ID:   "env-prod",
		Name: "Production",
		Type: types.EnvironmentProduction,
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	})
	_ = s.userRepo.Create(ctx, &user.User{
		ID:        "sa-1",
		Name:      "Recon service",
		Type:      types.UserTypeServiceAccount,
		BaseModel: baseModel,
	})
	_ = s.userRepo.Create(ctx, &user.User{
		ID:        "user-1",
		Email:     "u@example.com",
		Type:      types.UserTypeUser,
		BaseModel: baseModel,
	})
	past := time.Now().Add(-24 * time.Hour)
	_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
		ID:            "key-sandbox",
		Name:          "sandbox-key",
		UserID:        "sa-1",
		UserType:      string(types.UserTypeServiceAccount),
		EnvironmentID: "env-sandbox",
		DisplayID:     "sk_****SB",
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	})
	_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
		ID:            "key-prod-expired",
		Name:          "prod-key",
		UserID:        "sa-1",
		UserType:      string(types.UserTypeServiceAccount),
		EnvironmentID: "env-prod",
		ExpiresAt:     &past,
		DisplayID:     "sk_****PR",
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	})
	s.userService = &userService{
		userRepo:        s.userRepo,
		tenantRepo:      s.tenantRepo,
		secretRepo:      s.secretRepo,
		environmentRepo: s.environmentRepo,
		db:              s.db,
	}

	s.Run("human_user_has_no_api_keys", func() {
		resp, err := s.userService.GetUser(ctx, "user-1")
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("user-1", resp.ID)
		s.Nil(resp.APIKeys)
	})

	s.Run("service_account_includes_published_keys_across_envs", func() {
		resp, err := s.userService.GetUser(ctx, "sa-1")
		s.NoError(err)
		s.NotNil(resp)
		s.Equal("sa-1", resp.ID)
		s.Require().NotNil(resp.APIKeys)
		s.Equal(2, len(resp.APIKeys))

		byID := map[string]*dto.SecretResponse{}
		for _, key := range resp.APIKeys {
			byID[key.ID] = key
		}
		s.Equal("Sandbox", byID["key-sandbox"].EnvironmentName)
		s.Equal("env-sandbox", byID["key-sandbox"].EnvironmentID)
		s.Equal("Production", byID["key-prod-expired"].EnvironmentName)
		s.Equal("env-prod", byID["key-prod-expired"].EnvironmentID)
	})

	s.Run("empty_id_returns_validation_error", func() {
		resp, err := s.userService.GetUser(ctx, "")
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("unknown_id_returns_not_found", func() {
		resp, err := s.userService.GetUser(ctx, "missing")
		s.Error(err)
		s.Nil(resp)
	})
}

// ---------------------------------------------------------------------------
// RBAC permission tests
// ---------------------------------------------------------------------------

type RBACPermissionSuite struct {
	suite.Suite
	rbacSvc *rbac.RBACService
}

func TestRBACPermissions(t *testing.T) {
	suite.Run(t, new(RBACPermissionSuite))
}

func (s *RBACPermissionSuite) SetupSuite() {
	svc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../../internal/config/rbac/roles.json"},
	})
	if err != nil || svc == nil {
		svc, err = rbac.NewRBACService(&config.Configuration{
			RBAC: config.RBACConfig{RolesConfigPath: "internal/config/rbac/roles.json"},
		})
	}
	s.Require().NotNil(svc, "RBAC service must load — check roles.json path")
	s.rbacSvc = svc
}

func (s *RBACPermissionSuite) TestSuperAdmin_CanDoEverything() {
	roles := []string{"super_admin"}
	checks := []struct{ entity, action string }{
		{"event", "read"},
		{"event", "write"},
		{"customer", "read"},
		{"customer", "write"},
		{"invoice", "read"},
		{"invoice", "write"},
		{"subscription", "read"},
		{"subscription", "write"},
		{"meter", "write"},
		{"anything", "delete"},
	}
	for _, c := range checks {
		s.True(s.rbacSvc.HasPermission(roles, c.entity, c.action),
			"super_admin should have %s:%s", c.entity, c.action)
	}
}

func (s *RBACPermissionSuite) TestEventIngestor_CanOnlyWriteEvents() {
	roles := []string{"event_ingestor"}

	// allowed
	s.True(s.rbacSvc.HasPermission(roles, "event", "write"), "event_ingestor can write events")

	// denied
	denied := []struct{ entity, action string }{
		{"event", "read"},
		{"customer", "read"},
		{"customer", "write"},
		{"invoice", "read"},
		{"subscription", "read"},
		{"meter", "write"},
	}
	for _, c := range denied {
		s.False(s.rbacSvc.HasPermission(roles, c.entity, c.action),
			"event_ingestor should NOT have %s:%s", c.entity, c.action)
	}
}

func (s *RBACPermissionSuite) TestEventReader_CanOnlyReadEvents() {
	roles := []string{"event_reader"}

	// allowed
	s.True(s.rbacSvc.HasPermission(roles, "event", "read"), "event_reader can read events")

	// denied
	denied := []struct{ entity, action string }{
		{"event", "write"},
		{"customer", "read"},
		{"customer", "write"},
		{"invoice", "write"},
		{"subscription", "write"},
	}
	for _, c := range denied {
		s.False(s.rbacSvc.HasPermission(roles, c.entity, c.action),
			"event_reader should NOT have %s:%s", c.entity, c.action)
	}
}

func (s *RBACPermissionSuite) TestMultipleRoles_UnionOfPermissions() {
	roles := []string{"event_ingestor", "event_reader"}

	s.True(s.rbacSvc.HasPermission(roles, "event", "write"), "union: can write events")
	s.True(s.rbacSvc.HasPermission(roles, "event", "read"), "union: can read events")
	s.False(s.rbacSvc.HasPermission(roles, "customer", "read"), "union: cannot read customers")
}

func (s *RBACPermissionSuite) TestUnknownRole_DeniedEverything() {
	roles := []string{"nonexistent_role"}
	s.False(s.rbacSvc.HasPermission(roles, "event", "read"))
	s.False(s.rbacSvc.HasPermission(roles, "customer", "write"))
}

func (s *RBACPermissionSuite) TestNoRoles_FullAccess() {
	// Empty roles = backward-compatible full access (see HasPermission implementation)
	s.True(s.rbacSvc.HasPermission([]string{}, "event", "read"))
	s.True(s.rbacSvc.HasPermission([]string{}, "customer", "write"))
}

func (s *RBACPermissionSuite) TestSuperAdmin_CombinedWithOtherRoles_StillFullAccess() {
	roles := []string{"event_reader", "super_admin"}
	s.True(s.rbacSvc.HasPermission(roles, "customer", "write"),
		"super_admin in role set grants full access regardless of other roles")
}

func (s *RBACPermissionSuite) TestValidateRole() {
	s.True(s.rbacSvc.ValidateRole("super_admin"))
	s.True(s.rbacSvc.ValidateRole("event_ingestor"))
	s.True(s.rbacSvc.ValidateRole("event_reader"))
	s.False(s.rbacSvc.ValidateRole("nonexistent"))
	s.False(s.rbacSvc.ValidateRole(""))
}
