package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type UserServiceSuite struct {
	suite.Suite
	ctx         context.Context
	userService *userService
	userRepo    *testutil.InMemoryUserStore
	tenantRepo  *testutil.InMemoryTenantStore
	secretRepo  *testutil.InMemorySecretStore
}

func TestUserService(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

func (s *UserServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.userRepo = testutil.NewInMemoryUserStore()
	s.tenantRepo = testutil.NewInMemoryTenantStore()
	s.secretRepo = testutil.NewInMemorySecretStore()
	s.userService = &userService{
		userRepo:        s.userRepo,
		tenantRepo:      s.tenantRepo,
		secretRepo:      s.secretRepo,
		rbacService:     nil,
		supabaseAuth:    nil,
		settingsService: nil,
	}

	s.tenantRepo.Create(s.ctx, &tenant.Tenant{
		ID:   types.DefaultTenantID,
		Name: "Test Tenant",
	})
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
		s.userService = &userService{userRepo: s.userRepo, tenantRepo: s.tenantRepo, secretRepo: s.secretRepo}
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
		s.userService = &userService{userRepo: s.userRepo, tenantRepo: s.tenantRepo, secretRepo: s.secretRepo}
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

	s.Run("active_api_key_blocks_archive", func() {
		seedStore()
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:       "key-1",
			UserID:   "sa-1",
			UserType: string(types.UserTypeServiceAccount),
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.Error(err)
		s.Contains(err.Error(), "active API keys")
	})

	s.Run("expired_api_key_allows_archive", func() {
		seedStore()
		past := time.Now().Add(-24 * time.Hour)
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:        "key-2",
			UserID:    "sa-1",
			UserType:  string(types.UserTypeServiceAccount),
			ExpiresAt: &past,
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		err := s.userService.DeleteUser(ctx, "sa-1")
		s.NoError(err)
	})
}

func (s *UserServiceSuite) TestListUsersByFilter_UserIDs() {
	ctx := testutil.SetupContext()

	seedStore := func() {
		s.userRepo = testutil.NewInMemoryUserStore()
		s.userService = &userService{userRepo: s.userRepo, tenantRepo: s.tenantRepo, secretRepo: s.secretRepo}
		for _, u := range []struct {
			id    string
			email string
			typ   types.UserType
		}{
			{"user-a", "a@example.com", types.UserTypeUser},
			{"user-b", "b@example.com", types.UserTypeUser},
			{"sa-c", "", types.UserTypeServiceAccount},
		} {
			s.NoError(s.userRepo.Create(ctx, &user.User{
				ID:        u.id,
				Email:     u.email,
				Type:      u.typ,
				BaseModel: types.GetDefaultBaseModel(ctx),
			}))
		}
	}

	testCases := []struct {
		name        string
		filter      *types.UserFilter
		expectedIDs []string
	}{
		{
			name:        "no_user_ids_returns_all_users",
			filter:      &types.UserFilter{QueryFilter: types.NewNoLimitQueryFilter()},
			expectedIDs: []string{"user-a", "user-b", "sa-c"},
		},
		{
			name: "user_ids_returns_only_matching_users",
			filter: &types.UserFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				UserIDs:     []string{"user-b"},
			},
			expectedIDs: []string{"user-b"},
		},
		{
			name: "user_ids_combined_with_type_filter",
			filter: &types.UserFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				UserIDs:     []string{"user-a", "sa-c"},
				Type:        lo.ToPtr(types.UserTypeServiceAccount),
			},
			expectedIDs: []string{"sa-c"},
		},
		{
			name: "unknown_user_id_returns_empty",
			filter: &types.UserFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				UserIDs:     []string{"user-missing"},
			},
			expectedIDs: []string{},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			seedStore()

			resp, err := s.userService.ListUsersByFilter(ctx, tc.filter)
			s.NoError(err)

			gotIDs := lo.Map(resp.Items, func(u *dto.UserResponse, _ int) string { return u.ID })
			s.ElementsMatch(tc.expectedIDs, gotIDs)
			s.Equal(len(tc.expectedIDs), resp.Pagination.Total)
		})
	}
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
