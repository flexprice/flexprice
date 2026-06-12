package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type UserServiceSuite struct {
	suite.Suite
	ctx         context.Context
	userService *userService
	userRepo    *testutil.InMemoryUserStore
	tenantRepo  *testutil.InMemoryTenantStore
}

func TestUserService(t *testing.T) {
	suite.Run(t, new(UserServiceSuite))
}

func (s *UserServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.userRepo = testutil.NewInMemoryUserStore()
	s.tenantRepo = testutil.NewInMemoryTenantStore()
	s.userService = &userService{
		userRepo:        s.userRepo,
		tenantRepo:      s.tenantRepo,
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
		s.userService = &userService{userRepo: s.userRepo, tenantRepo: s.tenantRepo}
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
		s.userService = &userService{userRepo: s.userRepo, tenantRepo: s.tenantRepo}
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
}
