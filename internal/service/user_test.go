package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/logger"
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

// failingUserRepo returns an error for Create to simulate DB failure.
type failingUserRepo struct {
	*testutil.InMemoryUserStore
	createErr error
}

func (r *failingUserRepo) Create(ctx context.Context, u *user.User) error {
	return r.createErr
}

// authRepoSpy records create/delete calls.
type authRepoSpy struct {
	createdAuth   *domainAuth.Auth
	deletedUserID string
	createCalls   int
	deleteCalls   int
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

func (r *authRepoSpy) CreateAuth(ctx context.Context, a *domainAuth.Auth) error {
	r.createdAuth = a
	r.createCalls++
	return nil
}

func (r *authRepoSpy) GetAuthByUserID(ctx context.Context, userID string) (*domainAuth.Auth, error) {
	if r.createdAuth != nil && r.createdAuth.UserID == userID {
		return r.createdAuth, nil
	}
	return nil, errors.New("not found")
}

func (r *authRepoSpy) UpdateAuth(ctx context.Context, a *domainAuth.Auth) error {
	return nil
}

func (r *authRepoSpy) DeleteAuth(ctx context.Context, userID string) error {
	r.deletedUserID = userID
	r.deleteCalls++
	return nil
}

func (s *UserServiceSuite) TestInviteUser_CompensatingDeleteOnUserCreateFailure() {
	ctx := testutil.SetupContext()
	// set tenant and actor
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "test-actor")

	// prepare tenant repo with default tenant
	tenantRepo := testutil.NewInMemoryTenantStore()
	_ = tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "Test Tenant"})

	// prepare settings service expected by InviteUser
	settingsSvc := &settingsService{
		ServiceParams: ServiceParams{
			SettingsRepo: testutil.NewInMemorySettingsStore(),
		},
	}

	// auth spy and failing user repo
	authSpy := &authRepoSpy{}
	userRepo := &failingUserRepo{
		InMemoryUserStore: testutil.NewInMemoryUserStore(),
		createErr:         errors.New("forced user create failure"),
	}

	svc := &userService{
		userRepo:        userRepo,
		tenantRepo:      tenantRepo,
		authRepo:        authSpy,
		cfg:             &config.Configuration{Auth: config.AuthConfig{Provider: types.AuthProviderFlexprice}},
		rbacService:     nil,
		supabaseAuth:    nil,
		settingsService: settingsSvc,
		logger:          logger.NewNoopLogger(),
	}

	// Call InviteUser which should attempt CreateAuth then fail on userRepo.Create,
	// and then call DeleteAuth as compensation.
	resp, pw, err := svc.InviteUser(ctx, &dto.CreateUserRequest{Type: types.UserTypeUser, Email: "rollback@example.com"}, "test-actor")

	s.Error(err)
	s.Nil(resp)
	s.Nil(pw)

	// Validate that CreateAuth was called once and DeleteAuth was called once
	s.Equal(1, authSpy.createCalls, "expected CreateAuth to be called once")
	s.Equal(1, authSpy.deleteCalls, "expected DeleteAuth to be called once")
	s.NotNil(authSpy.createdAuth, "created auth should be recorded")
	s.Equal(authSpy.createdAuth.UserID, authSpy.deletedUserID, "deleted user id should match created auth user id")
}
