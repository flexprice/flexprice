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
	// Initialize context and repository
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
			// Reset repositories and service for each test
			s.userRepo = testutil.NewInMemoryUserStore()
			s.userService = &userService{
				userRepo:     s.userRepo,
				tenantRepo:   s.tenantRepo,
				rbacService:  nil,
				supabaseAuth: nil,
			}

			// Create a context with the test's user ID
			ctx := testutil.SetupContext()
			ctx = context.WithValue(ctx, types.CtxUserID, tc.contextUserID)

			// Execute setup function if provided
			if tc.setup != nil {
				tc.setup(ctx)
			}

			// Call the service method
			resp, err := s.userService.GetUserInfo(ctx)

			// Assert results
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

	// Path from module root; fallback when CWD is internal/service
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
