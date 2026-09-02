package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cockroachErrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
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
		db:              testutil.NewMockPostgresClient(nil),
		rbacService:     nil,
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
				userRepo:    s.userRepo,
				tenantRepo:  s.tenantRepo,
				rbacService: nil,
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
	// The caller can only grant access it holds, so these cases act as a super_admin.
	ctx = context.WithValue(ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})

	rbacSvc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	s.Require().NoError(err)

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
					settingsService: nil,
				}
			},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	{
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
					settingsService: nil,
				}
			},
			wantErr:     false,
			errContains: "",
		})

		// reader is a person's access level, so a service account must not hold it.
		tests = append(tests, struct {
			name        string
			req         dto.CreateUserRequest
			setup       func() *userService
			wantErr     bool
			errContains string
		}{
			name: "type_service_account_with_user_role_rejected",
			req:  dto.CreateUserRequest{Type: types.UserTypeServiceAccount, Roles: []string{types.RoleAllReader.String()}},
			setup: func() *userService {
				return &userService{
					userRepo:        s.userRepo,
					tenantRepo:      s.tenantRepo,
					rbacService:     rbacSvc,
					settingsService: nil,
				}
			},
			wantErr:     true,
			errContains: "not assignable to this user type",
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

// Creating a principal is a way to hand out access, so it must not become a
// route around the caller's own limits: a reader minting a write-scoped service
// account would be a straightforward privilege escalation.
func (s *UserServiceSuite) TestCreateUser_CannotGrantBeyondCallerAccess() {
	rbacSvc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	s.Require().NoError(err)

	testCases := []struct {
		name        string
		callerRoles []string
		saRoles     []string
		wantErr     bool
	}{
		{
			name:        "super_admin can create a write-scoped service account",
			callerRoles: []string{types.RoleSuperAdmin.String()},
			saRoles:     []string{types.RoleEventIngestor.String()},
		},
		{
			name:        "writer can create a write-scoped service account",
			callerRoles: []string{types.RoleAllWriter.String()},
			saRoles:     []string{types.RoleEventIngestor.String()},
		},
		{
			name:        "reader cannot create a write-scoped service account",
			callerRoles: []string{types.RoleAllReader.String()},
			saRoles:     []string{types.RoleEventIngestor.String()},
			wantErr:     true,
		},
		{
			name:        "writer cannot create a super_admin service account",
			callerRoles: []string{types.RoleAllWriter.String()},
			saRoles:     []string{types.RoleSuperAdmin.String()},
			wantErr:     true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			ctx := testutil.SetupContext()
			ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
			ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")
			ctx = context.WithValue(ctx, types.CtxRoles, tc.callerRoles)

			userRepo := testutil.NewInMemoryUserStore()
			tenantRepo := testutil.NewInMemoryTenantStore()
			_ = tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "Test Tenant"})

			svc := &userService{userRepo: userRepo, tenantRepo: tenantRepo, rbacService: rbacSvc}

			resp, err := svc.CreateUser(ctx, &dto.CreateUserRequest{
				Type:  types.UserTypeServiceAccount,
				Name:  "sa",
				Roles: tc.saRoles,
			})

			if tc.wantErr {
				s.Error(err)
				s.Nil(resp)
				s.Contains(err.Error(), "exceeds your own access")
			} else {
				s.NoError(err)
				s.Require().NotNil(resp)
				s.Equal(tc.saRoles, resp.Roles)
			}
		})
	}
}

// An invited user must land on a role rather than on no roles at all: with the
// empty-role-set fallback gone, a user created without roles would be refused
// every request.
func (s *UserServiceSuite) TestInviteUser_RoleAssignment() {
	rbacSvc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	s.Require().NoError(err)

	testCases := []struct {
		name        string
		reqRoles    []string
		wantRoles   []string
		wantErr     bool
		errContains string
	}{
		{
			name:      "no roles requested defaults to super_admin",
			reqRoles:  nil,
			wantRoles: []string{types.RoleSuperAdmin.String()},
		},
		{
			name:      "empty roles requested defaults to super_admin",
			reqRoles:  []string{},
			wantRoles: []string{types.RoleSuperAdmin.String()},
		},
		{
			name:      "requested roles are honoured",
			reqRoles:  []string{types.RoleAllWriter.String()},
			wantRoles: []string{types.RoleAllWriter.String()},
		},
		{
			name:        "undefined role is rejected",
			reqRoles:    []string{"nonexistent"},
			wantErr:     true,
			errContains: "invalid role",
		},
		{
			name:        "super_admin cannot be combined with another role",
			reqRoles:    []string{types.RoleSuperAdmin.String(), types.RoleAllReader.String()},
			wantErr:     true,
			errContains: "super admin role need not be combined",
		},
		{
			name:        "a service account scope cannot be given to a person",
			reqRoles:    []string{types.RoleEventIngestor.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
	}

	// Admitting someone to the tenant is restricted to super_admins, so a
	// writer is refused even when the invitee would only be a reader.
	s.Run("only a super_admin can invite", func() {
		for _, callerRoles := range [][]string{
			{types.RoleAllWriter.String()},
			{types.RoleAllReader.String()},
			{},
		} {
			ctx := testutil.SetupContext()
			ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
			ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")
			ctx = context.WithValue(ctx, types.CtxRoles, callerRoles)

			tenantRepo := testutil.NewInMemoryTenantStore()
			_ = tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "Test Tenant"})
			authRepo := testutil.NewInMemoryAuthRepository()

			svc := &userService{
				userRepo:    testutil.NewInMemoryUserStore(),
				tenantRepo:  tenantRepo,
				authRepo:    authRepo,
				rbacService: rbacSvc,
			}

			created, _, err := svc.InviteUser(ctx, &dto.CreateUserRequest{
				Type:  types.UserTypeUser,
				Email: "invitee@example.com",
				Roles: []string{types.RoleAllReader.String()},
			}, "actor-1")

			s.Error(err, "caller %v must not be able to invite", callerRoles)
			s.Nil(created)
			s.Contains(err.Error(), "only super_admin can invite users")
			s.Zero(authRepo.Count(), "a refused invite must not leave auth material behind")
		}
	})

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			ctx := testutil.SetupContext()
			ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
			ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")
			// The caller can only grant access it holds, so the inviter is a super_admin.
			ctx = context.WithValue(ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})

			userRepo := testutil.NewInMemoryUserStore()
			tenantRepo := testutil.NewInMemoryTenantStore()
			_ = tenantRepo.Create(ctx, &tenant.Tenant{ID: types.DefaultTenantID, Name: "Test Tenant"})

			authRepo := testutil.NewInMemoryAuthRepository()
			svc := &userService{
				userRepo:    userRepo,
				tenantRepo:  tenantRepo,
				authRepo:    authRepo,
				rbacService: rbacSvc,
				cfg: &config.Configuration{
					Auth: config.AuthConfig{
						Provider: types.AuthProviderFlexprice,
						Secret:   "test-secret-key-32-bytes-minimum!",
					},
				},
				settingsService: &settingsService{
					ServiceParams: ServiceParams{SettingsRepo: testutil.NewInMemorySettingsStore()},
				},
			}

			created, password, err := svc.InviteUser(ctx, &dto.CreateUserRequest{
				Type:  types.UserTypeUser,
				Email: "invitee@example.com",
				Roles: tc.reqRoles,
			}, "actor-1")

			if tc.wantErr {
				s.Error(err)
				s.Nil(created)
				s.Contains(err.Error(), tc.errContains)

				// A rejected role must fail before anything is provisioned,
				// otherwise an auth record is left behind holding the invitee's
				// identity with no user attached to it.
				s.Zero(authRepo.Count(), "a rejected role must not leave auth material behind")
				return
			}

			s.NoError(err)
			s.Require().NotNil(created)
			s.Equal(tc.wantRoles, created.Roles)
			s.Equal(types.UserTypeUser, created.Type)
			s.NotNil(password)

			// The role must be on the persisted record, not just the returned struct.
			stored, err := userRepo.GetByID(ctx, created.ID)
			s.NoError(err)
			s.Equal(tc.wantRoles, stored.Roles)
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

func (s *UserServiceSuite) TestCreateSupportChatToken() {
	const (
		appID  = "pylon-app-id"
		secret = "deadbeefdeadbeefdeadbeefdeadbeef"
		email  = "support@example.com"
	)

	ctx := testutil.SetupContext()
	_ = s.userRepo.Create(ctx, &user.User{
		ID:        types.DefaultUserID,
		Email:     email,
		Name:      "Support User",
		Type:      types.UserTypeUser,
		BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.userService.cfg = &config.Configuration{
		ChatSupport: config.ChatSupportConfig{
			AppID:          appID,
			IdentitySecret: secret,
		},
	}

	resp, err := s.userService.CreateSupportChatToken(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Empty(resp.ExpiresAt)

	secretBytes, err := hex.DecodeString(secret)
	s.Require().NoError(err)
	mac := hmac.New(sha256.New, secretBytes)
	_, err = mac.Write([]byte(email))
	s.Require().NoError(err)
	s.Equal(hex.EncodeToString(mac.Sum(nil)), resp.Token)
}

func (s *UserServiceSuite) TestCreateSupportChatToken_InvalidHexSecret() {
	ctx := testutil.SetupContext()
	_ = s.userRepo.Create(ctx, &user.User{
		ID:        types.DefaultUserID,
		Email:     "support@example.com",
		Type:      types.UserTypeUser,
		BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.userService.cfg = &config.Configuration{
		ChatSupport: config.ChatSupportConfig{
			AppID:          "pylon-app-id",
			IdentitySecret: "not-a-hex-string",
		},
	}

	resp, err := s.userService.CreateSupportChatToken(ctx)
	s.Require().Error(err)
	s.Nil(resp)
}

func (s *UserServiceSuite) TestRemoveUser() {
	ctx := testutil.SetupContext()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "actor-1")
	ctx = context.WithValue(ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})

	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = types.DefaultTenantID

	seedStore := func() {
		s.userRepo = testutil.NewInMemoryUserStore()
		s.secretRepo = testutil.NewInMemorySecretStore()
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "actor-1",
			Email:     "actor@example.com",
			Type:      types.UserTypeUser,
			BaseModel: baseModel,
		})
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "user-1",
			Email:     "u@example.com",
			Type:      types.UserTypeUser,
			BaseModel: baseModel,
		})
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "sa-1",
			Type:      types.UserTypeServiceAccount,
			BaseModel: baseModel,
		})
		s.userService = &userService{db: testutil.NewMockPostgresClient(testLogger(s.T())), userRepo: s.userRepo, tenantRepo: s.tenantRepo, secretRepo: s.secretRepo, cfg: &config.Configuration{}, logger: testLogger(s.T())}
	}

	s.Run("success_user_removed_api_key_retained", func() {
		seedStore()
		_ = s.secretRepo.Create(ctx, &domainSecret.Secret{
			ID:       "key-1",
			UserID:   "user-1",
			UserType: string(types.UserTypeUser),
			BaseModel: types.BaseModel{
				TenantID: types.DefaultTenantID,
				Status:   types.StatusPublished,
			},
		})
		err := s.userService.RemoveUser(ctx, "user-1")
		s.NoError(err)

		removedUser, err := s.userRepo.GetByID(ctx, "user-1")
		s.NoError(err)
		s.Equal(types.StatusArchived, removedUser.Status)

		key, err := s.secretRepo.Get(ctx, "key-1")
		s.NoError(err, "the user's API key must remain active after removal")
		s.Equal(types.StatusPublished, key.Status)
	})

	s.Run("empty_id_returns_validation_error", func() {
		seedStore()
		err := s.userService.RemoveUser(ctx, "")
		s.Error(err)
		s.Contains(err.Error(), "user ID is required")
	})

	s.Run("unknown_id_returns_not_found", func() {
		seedStore()
		err := s.userService.RemoveUser(ctx, "user-unknown")
		s.Error(err)
	})

	s.Run("cross_tenant_target_returns_not_found", func() {
		seedStore()
		otherTenantModel := baseModel
		otherTenantModel.TenantID = "other-tenant"
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "other-tenant-user",
			Email:     "other@example.com",
			Type:      types.UserTypeUser,
			BaseModel: otherTenantModel,
		})

		err := s.userService.RemoveUser(ctx, "other-tenant-user")
		s.Error(err)
		s.Contains(err.Error(), "user not found")

		otherCtx := context.WithValue(ctx, types.CtxTenantID, "other-tenant")
		untouched, getErr := s.userRepo.GetByID(otherCtx, "other-tenant-user")
		s.NoError(getErr)
		s.Equal(types.StatusPublished, untouched.Status)
	})

	s.Run("service_account_returns_validation_error", func() {
		seedStore()
		err := s.userService.RemoveUser(ctx, "sa-1")
		s.Error(err)
		s.Contains(err.Error(), "only human users can be removed")
	})

	s.Run("self_removal_returns_permission_error", func() {
		seedStore()
		err := s.userService.RemoveUser(ctx, "actor-1")
		s.Error(err)
		s.Contains(err.Error(), "cannot remove yourself")

		untouched, getErr := s.userRepo.GetByID(ctx, "actor-1")
		s.NoError(getErr)
		s.Equal(types.StatusPublished, untouched.Status)
	})

	s.Run("non_super_admin_returns_permission_error", func() {
		seedStore()
		readerCtx := context.WithValue(ctx, types.CtxRoles, []string{types.RoleAllReader.String()})

		err := s.userService.RemoveUser(readerCtx, "user-1")
		s.Error(err)
		s.Contains(err.Error(), "only super_admin can remove users")

		untouched, getErr := s.userRepo.GetByID(ctx, "user-1")
		s.NoError(getErr)
		s.Equal(types.StatusPublished, untouched.Status)
	})

	s.Run("last_human_user_returns_validation_error", func() {
		s.userRepo = testutil.NewInMemoryUserStore()
		s.secretRepo = testutil.NewInMemorySecretStore()
		_ = s.userRepo.Create(ctx, &user.User{
			ID:        "user-1",
			Email:     "u@example.com",
			Type:      types.UserTypeUser,
			BaseModel: baseModel,
		})
		s.userService = &userService{db: testutil.NewMockPostgresClient(testLogger(s.T())), userRepo: s.userRepo, tenantRepo: s.tenantRepo, secretRepo: s.secretRepo, cfg: &config.Configuration{}, logger: testLogger(s.T())}

		err := s.userService.RemoveUser(ctx, "user-1")
		s.Error(err)
		s.Contains(err.Error(), "cannot remove the last user")

		untouched, getErr := s.userRepo.GetByID(ctx, "user-1")
		s.NoError(getErr)
		s.Equal(types.StatusPublished, untouched.Status)
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

// A writer that could not read would be unable to fetch the very records it is
// allowed to modify, so write implies read.
func (s *RBACPermissionSuite) TestWriter_CanReadAndWrite() {
	roles := []string{types.RoleAllWriter.String()}
	for _, entity := range []string{"customer", "invoice", "event"} {
		s.True(s.rbacSvc.HasPermission(roles, entity, "read"), "writer should read %s", entity)
		s.True(s.rbacSvc.HasPermission(roles, entity, "write"), "writer should write %s", entity)
	}
}

// Reader stays read-only; widening writer must not have widened reader with it.
func (s *RBACPermissionSuite) TestReader_CanOnlyRead() {
	roles := []string{types.RoleAllReader.String()}
	for _, entity := range []string{"customer", "invoice", "event"} {
		s.True(s.rbacSvc.HasPermission(roles, entity, "read"), "reader should read %s", entity)
		s.False(s.rbacSvc.HasPermission(roles, entity, "write"), "reader should NOT write %s", entity)
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

// A caller carrying no roles holds no grant, so every check must deny. This
// closes the former fail-open path, where an empty role set was read as full
// access and any principal whose roles were never populated passed every check.
func (s *RBACPermissionSuite) TestNoRoles_DeniedEverything() {
	s.False(s.rbacSvc.HasPermission([]string{}, "event", "read"))
	s.False(s.rbacSvc.HasPermission([]string{}, "customer", "write"))
	s.False(s.rbacSvc.HasPermission(nil, "customer", "read"))
}

func (s *RBACPermissionSuite) TestSuperAdmin_CombinedWithOtherRoles_StillFullAccess() {
	roles := []string{"event_reader", "super_admin"}
	s.True(s.rbacSvc.HasPermission(roles, "customer", "write"),
		"super_admin in role set grants full access regardless of other roles")
}

// A caller must not be able to hand out access it does not itself hold,
// otherwise any writer could mint a super_admin service account and escalate.
func (s *RBACPermissionSuite) TestCanGrantRoles() {
	testCases := []struct {
		name        string
		callerRoles []string
		requested   []string
		wantErr     bool
	}{
		{
			name:        "super_admin can grant anything",
			callerRoles: []string{types.RoleSuperAdmin.String()},
			requested:   []string{types.RoleSuperAdmin.String()},
		},
		{
			name:        "super_admin can grant a narrow scope",
			callerRoles: []string{types.RoleSuperAdmin.String()},
			requested:   []string{types.RoleEventIngestor.String()},
		},
		{
			name:        "writer can grant a write scope it fully covers",
			callerRoles: []string{types.RoleAllWriter.String()},
			requested:   []string{types.RoleEventIngestor.String()},
		},
		{
			name:        "reader can grant a read scope it fully covers",
			callerRoles: []string{types.RoleAllReader.String()},
			requested:   []string{types.RoleEventReader.String()},
		},

		{
			name:        "reader cannot grant a write scope",
			callerRoles: []string{types.RoleAllReader.String()},
			requested:   []string{types.RoleEventIngestor.String()},
			wantErr:     true,
		},
		{
			name:        "reader cannot grant writer",
			callerRoles: []string{types.RoleAllReader.String()},
			requested:   []string{types.RoleAllWriter.String()},
			wantErr:     true,
		},
		{
			name:        "writer can grant a read scope, since writer includes read",
			callerRoles: []string{types.RoleAllWriter.String()},
			requested:   []string{types.RoleEventReader.String()},
		},
		{
			name:        "writer cannot grant super_admin",
			callerRoles: []string{types.RoleAllWriter.String()},
			requested:   []string{types.RoleSuperAdmin.String()},
			wantErr:     true,
		},
		{
			name:        "reader cannot grant super_admin",
			callerRoles: []string{types.RoleAllReader.String()},
			requested:   []string{types.RoleSuperAdmin.String()},
			wantErr:     true,
		},
		{
			name:        "caller with no roles can grant nothing",
			callerRoles: []string{},
			requested:   []string{types.RoleEventReader.String()},
			wantErr:     true,
		},
		{
			name:        "one ungrantable role rejects the whole set",
			callerRoles: []string{types.RoleAllReader.String()},
			requested:   []string{types.RoleEventReader.String(), types.RoleEventIngestor.String()},
			wantErr:     true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.rbacSvc.CanGrantRoles(tc.callerRoles, tc.requested)
			if tc.wantErr {
				s.Error(err)
				s.Contains(err.Error(), "exceeds your own access")
			} else {
				s.NoError(err)
			}
		})
	}
}

func (s *RBACPermissionSuite) TestValidateRoles() {
	testCases := []struct {
		name        string
		userType    types.UserType
		roles       []string
		wantErr     bool
		errContains string
	}{
		{name: "empty_role_set_is_allowed", userType: types.UserTypeUser, roles: []string{}},

		// A person holds an access level over the tenant.
		{name: "user_may_hold_super_admin", userType: types.UserTypeUser, roles: []string{types.RoleSuperAdmin.String()}},
		{name: "user_may_hold_reader", userType: types.UserTypeUser, roles: []string{types.RoleAllReader.String()}},
		{name: "user_may_hold_writer", userType: types.UserTypeUser, roles: []string{types.RoleAllWriter.String()}},
		{name: "user_may_hold_reader_and_writer", userType: types.UserTypeUser, roles: []string{types.RoleAllReader.String(), types.RoleAllWriter.String()}},

		// A service account holds full access or a narrow machine scope.
		{name: "service_account_may_hold_super_admin", userType: types.UserTypeServiceAccount, roles: []string{types.RoleSuperAdmin.String()}},
		{name: "service_account_may_hold_event_scopes", userType: types.UserTypeServiceAccount, roles: []string{types.RoleEventIngestor.String(), types.RoleEventReader.String()}},

		// The two sets are disjoint apart from super_admin.
		{
			name:        "user_may_not_hold_event_ingestor",
			userType:    types.UserTypeUser,
			roles:       []string{types.RoleEventIngestor.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
		{
			name:        "user_may_not_hold_event_reader",
			userType:    types.UserTypeUser,
			roles:       []string{types.RoleEventReader.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
		{
			name:        "service_account_may_not_hold_reader",
			userType:    types.UserTypeServiceAccount,
			roles:       []string{types.RoleAllReader.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
		{
			name:        "service_account_may_not_hold_writer",
			userType:    types.UserTypeServiceAccount,
			roles:       []string{types.RoleAllWriter.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
		{
			name:        "one_disallowed_role_rejects_the_whole_set",
			userType:    types.UserTypeServiceAccount,
			roles:       []string{types.RoleEventReader.String(), types.RoleAllWriter.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},
		{
			name:        "unknown_user_type_may_hold_nothing",
			userType:    types.UserType("robot"),
			roles:       []string{types.RoleAllReader.String()},
			wantErr:     true,
			errContains: "not assignable to this user type",
		},

		{
			name:        "undefined_role_rejected",
			userType:    types.UserTypeUser,
			roles:       []string{"nonexistent"},
			wantErr:     true,
			errContains: "invalid role",
		},
		{
			name:        "empty_role_name_rejected",
			userType:    types.UserTypeUser,
			roles:       []string{""},
			wantErr:     true,
			errContains: "invalid role",
		},
		{
			name:        "one_undefined_role_rejects_the_whole_set",
			userType:    types.UserTypeUser,
			roles:       []string{types.RoleAllReader.String(), "nonexistent"},
			wantErr:     true,
			errContains: "invalid role",
		},
		{
			name:        "super_admin_cannot_be_combined",
			userType:    types.UserTypeUser,
			roles:       []string{types.RoleSuperAdmin.String(), types.RoleAllReader.String()},
			wantErr:     true,
			errContains: "super admin role need not be combined",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			err := s.rbacSvc.ValidateRoles(tc.userType, tc.roles)
			if tc.wantErr {
				s.Error(err)
				s.Contains(err.Error(), tc.errContains)
			} else {
				s.NoError(err)
			}
		})
	}
}

const (
	adminUserID  = "user_admin"
	targetUserID = "user_target"
	envProdID    = "env_prod"
	envStagingID = "env_staging"

	// Matches the encoding used by ErrorBuilder.WithReportableDetails.
	jsonDetailsPrefix = "__json__:"
)

type UserRolesSuite struct {
	suite.Suite
	ctx         context.Context
	userService *userService
	userRepo    *testutil.InMemoryUserStore
	tenantRepo  *testutil.InMemoryTenantStore
	secretRepo  *testutil.InMemorySecretStore
	envRepo     *testutil.InMemoryEnvironmentStore
}

func TestUserRoles(t *testing.T) {
	suite.Run(t, new(UserRolesSuite))
}

func (s *UserRolesSuite) SetupTest() {
	s.userRepo = testutil.NewInMemoryUserStore()
	s.tenantRepo = testutil.NewInMemoryTenantStore()
	s.secretRepo = testutil.NewInMemorySecretStore()
	s.envRepo = testutil.NewInMemoryEnvironmentStore()

	rbacSvc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	s.Require().NoError(err, "RBAC service must load, check roles.json path")

	log, err := logger.NewLogger(&config.Configuration{})
	s.Require().NoError(err)

	s.userService = &userService{
		userRepo:        s.userRepo,
		tenantRepo:      s.tenantRepo,
		secretRepo:      s.secretRepo,
		environmentRepo: s.envRepo,
		db:              testutil.NewMockPostgresClient(log),
		rbacService:     rbacSvc,
		logger:          log,
	}

	// The caller is a super_admin acting on a different user, which is the only
	// shape the endpoint accepts; individual cases override what they need.
	s.ctx = testutil.SetupContext()
	s.ctx = context.WithValue(s.ctx, types.CtxUserID, adminUserID)
	s.ctx = context.WithValue(s.ctx, types.CtxRoles, []string{types.RoleSuperAdmin.String()})

	s.Require().NoError(s.tenantRepo.Create(s.ctx, &tenant.Tenant{
		ID:   types.DefaultTenantID,
		Name: "Test Tenant",
	}))

	for id, name := range map[string]string{envProdID: "Production", envStagingID: "Staging"} {
		s.Require().NoError(s.envRepo.Create(s.ctx, environmentFixture(id, name)))
	}

	s.seedUser(adminUserID, types.UserTypeUser, []string{types.RoleSuperAdmin.String()}, types.StatusPublished)
	s.seedUser(targetUserID, types.UserTypeUser, []string{types.RoleAllReader.String()}, types.StatusPublished)
}

func environmentFixture(id, name string) *domainEnvironment.Environment {
	return &domainEnvironment.Environment{
		ID:   id,
		Name: name,
		Type: types.EnvironmentDevelopment,
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	}
}

func (s *UserRolesSuite) seedUser(id string, userType types.UserType, roles []string, status types.Status) {
	email := id + "@example.com"
	if userType == types.UserTypeServiceAccount {
		email = ""
	}
	s.Require().NoError(s.userRepo.Create(s.ctx, &user.User{
		ID:    id,
		Email: email,
		Type:  userType,
		Roles: roles,
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   status,
		},
	}))
}

// seedAPIKey creates an active private key owned by userID in the given
// environment. expiresAt nil means the key never expires.
func (s *UserRolesSuite) seedAPIKey(id, name, userID, envID string, expiresAt *time.Time) {
	s.Require().NoError(s.secretRepo.Create(s.ctx, &domainSecret.Secret{
		ID:            id,
		Name:          name,
		Type:          types.SecretTypePrivateKey,
		Provider:      types.SecretProviderFlexPrice,
		Value:         "hashed_" + id,
		EnvironmentID: envID,
		UserID:        userID,
		ExpiresAt:     expiresAt,
		BaseModel: types.BaseModel{
			TenantID: types.DefaultTenantID,
			Status:   types.StatusPublished,
		},
	}))
}

func (s *UserRolesSuite) update(id string, roles ...string) (*dto.UpdateUserRolesResponse, error) {
	return s.userService.UpdateUserRoles(s.ctx, id, &dto.UpdateUserRolesRequest{Roles: roles})
}

func (s *UserRolesSuite) TestSuperAdminUpdatesAnotherUser() {
	resp, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal([]string{types.RoleAllWriter.String()}, resp.Roles)

	// The write must be visible through the repository, not just echoed back.
	stored, err := s.userRepo.GetByID(s.ctx, targetUserID)
	s.NoError(err)
	s.Equal([]string{types.RoleAllWriter.String()}, stored.Roles)
}

func (s *UserRolesSuite) TestPromotionToSuperAdmin() {
	resp, err := s.update(targetUserID, types.RoleSuperAdmin.String())

	s.NoError(err)
	s.Equal([]string{types.RoleSuperAdmin.String()}, resp.Roles)
}

func (s *UserRolesSuite) TestCallerWithoutSuperAdminIsDenied() {
	s.ctx = context.WithValue(s.ctx, types.CtxRoles, []string{types.RoleAllWriter.String()})

	_, err := s.update(targetUserID, types.RoleAllReader.String())

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
	s.assertRolesUnchanged(targetUserID, types.RoleAllReader.String())
}

func (s *UserRolesSuite) TestCallerWithNoRolesIsDenied() {
	s.ctx = context.WithValue(s.ctx, types.CtxRoles, []string{})

	_, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
}

// A super_admin must not be able to demote themselves; this is what guarantees
// a tenant always retains at least one super_admin.
func (s *UserRolesSuite) TestCallerCannotUpdateOwnRoles() {
	_, err := s.update(adminUserID, types.RoleAllReader.String())

	s.Error(err)
	s.True(ierr.IsPermissionDenied(err), "expected permission denied, got %v", err)
	s.assertRolesUnchanged(adminUserID, types.RoleSuperAdmin.String())
}

func (s *UserRolesSuite) TestServiceAccountRolesCannotBeChanged() {
	const svcAccountID = "user_service_account"
	s.seedUser(svcAccountID, types.UserTypeServiceAccount, []string{types.RoleEventIngestor.String()}, types.StatusPublished)

	_, err := s.update(svcAccountID, types.RoleEventReader.String())

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected validation error, got %v", err)
	s.assertRolesUnchanged(svcAccountID, types.RoleEventIngestor.String())
}

func (s *UserRolesSuite) TestArchivedUserIsRejected() {
	const archivedID = "user_archived"
	s.seedUser(archivedID, types.UserTypeUser, []string{types.RoleAllReader.String()}, types.StatusArchived)

	_, err := s.update(archivedID, types.RoleAllWriter.String())

	s.Error(err)
	s.True(ierr.IsValidation(err), "expected validation error, got %v", err)
}

func (s *UserRolesSuite) TestUnknownUserIsRejected() {
	_, err := s.update("user_does_not_exist", types.RoleAllWriter.String())

	s.Error(err)
	s.True(ierr.IsNotFound(err), "expected not found, got %v", err)
}

func (s *UserRolesSuite) TestRoleValidation() {
	testCases := []struct {
		name  string
		id    string
		roles []string
	}{
		{
			name:  "empty user id",
			id:    "",
			roles: []string{types.RoleAllWriter.String()},
		},
		{
			name:  "no roles supplied",
			id:    targetUserID,
			roles: []string{},
		},
		{
			name:  "role does not exist",
			id:    targetUserID,
			roles: []string{"not_a_real_role"},
		},
		{
			name:  "service account scope cannot be given to a person",
			id:    targetUserID,
			roles: []string{types.RoleEventIngestor.String()},
		},
		{
			name:  "super_admin cannot be combined with another role",
			id:    targetUserID,
			roles: []string{types.RoleSuperAdmin.String(), types.RoleAllReader.String()},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_, err := s.update(tc.id, tc.roles...)

			s.Error(err)
			s.True(ierr.IsValidation(err), "expected validation error, got %v", err)
			s.assertRolesUnchanged(targetUserID, types.RoleAllReader.String())
		})
	}
}

func (s *UserRolesSuite) TestActiveAPIKeyBlocksUpdate() {
	s.seedAPIKey("sec_1", "billing-key", targetUserID, envProdID, nil)

	_, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected validation error, got %v", err)
	s.assertRolesUnchanged(targetUserID, types.RoleAllReader.String())

	details := s.errorDetails(err)
	s.EqualValues(1, details["active_api_key_count"])

	keysByEnv := s.activeAPIKeys(details)
	s.Require().Contains(keysByEnv, envProdID)
	s.Equal("Production", keysByEnv[envProdID].EnvName)
	s.Equal([]dto.ActiveAPIKey{{ID: "sec_1", KeyName: "billing-key"}}, keysByEnv[envProdID].APIKeys)
}

// Roles are tenant-wide while secrets are environment-scoped, so a key living
// outside the caller's current environment must still block the change.
func (s *UserRolesSuite) TestKeysAreFoundAcrossEnvironments() {
	s.seedAPIKey("sec_prod", "prod-key", targetUserID, envProdID, nil)
	s.seedAPIKey("sec_stg_a", "ci-key", targetUserID, envStagingID, nil)
	s.seedAPIKey("sec_stg_b", "test-key", targetUserID, envStagingID, nil)

	_, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.Require().Error(err)

	details := s.errorDetails(err)
	s.EqualValues(3, details["active_api_key_count"])

	keysByEnv := s.activeAPIKeys(details)
	s.Len(keysByEnv, 2)
	s.Equal("Production", keysByEnv[envProdID].EnvName)
	s.Len(keysByEnv[envProdID].APIKeys, 1)
	s.Equal("Staging", keysByEnv[envStagingID].EnvName)
	s.Len(keysByEnv[envStagingID].APIKeys, 2)
}

func (s *UserRolesSuite) TestKeysThatDoNotBlockUpdate() {
	// Another user's key is irrelevant to this user's role change.
	s.seedAPIKey("sec_other_user", "other-key", adminUserID, envProdID, nil)
	// An expired key can no longer authenticate, so it cannot carry stale roles.
	s.seedAPIKey("sec_expired", "expired-key", targetUserID, envProdID, lo.ToPtr(time.Now().UTC().Add(-time.Hour)))

	_, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.NoError(err)
	s.assertRolesUnchanged(targetUserID, types.RoleAllWriter.String())
}

func (s *UserRolesSuite) TestDeletedKeyDoesNotBlockUpdate() {
	s.seedAPIKey("sec_1", "billing-key", targetUserID, envProdID, nil)
	s.Require().NoError(s.secretRepo.Delete(s.ctx, "sec_1"))

	_, err := s.update(targetUserID, types.RoleAllWriter.String())

	s.NoError(err)
	s.assertRolesUnchanged(targetUserID, types.RoleAllWriter.String())
}

func (s *UserRolesSuite) assertRolesUnchanged(id string, want ...string) {
	stored, err := s.userRepo.GetByID(s.ctx, id)
	s.Require().NoError(err)
	s.Equal(want, stored.Roles)
}

// errorDetails extracts an ierr error's reportable details the same way the HTTP
// error handler does (see middleware/errhandler.go), so assertions see exactly
// the JSON payload a client receives rather than the in-process Go values.
func (s *UserRolesSuite) errorDetails(err error) map[string]any {
	for _, sdp := range cockroachErrors.GetAllSafeDetails(err) {
		for _, payload := range sdp.SafeDetails {
			if !strings.HasPrefix(payload, jsonDetailsPrefix) {
				continue
			}
			var details map[string]any
			if json.Unmarshal([]byte(payload[len(jsonDetailsPrefix):]), &details) == nil {
				return details
			}
		}
	}
	s.Failf("no reportable details", "error carried no JSON details: %v", err)
	return nil
}

// activeAPIKeys re-decodes the details payload into the typed shape the docs
// promise, which also asserts the wire format has not drifted.
func (s *UserRolesSuite) activeAPIKeys(details map[string]any) map[string]dto.ActiveEnvironmentAPIKeys {
	raw, err := json.Marshal(details["active_api_keys"])
	s.Require().NoError(err)

	var byEnv map[string]dto.ActiveEnvironmentAPIKeys
	s.Require().NoError(json.Unmarshal(raw, &byEnv))
	return byEnv
}
