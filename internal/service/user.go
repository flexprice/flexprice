package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
	"github.com/samber/lo"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
	CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error)
	ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error)
}

type userService struct {
	userRepo        user.Repository
	tenantRepo      tenant.Repository
	authRepo        domainAuth.Repository
	cfg             *config.Configuration
	rbacService     *rbac.RBACService
	supabaseAuth    *supabase.Client
	settingsService SettingsService
	logger          *logger.Logger
}

func NewUserService(
	userRepo user.Repository,
	tenantRepo tenant.Repository,
	authRepo domainAuth.Repository,
	cfg *config.Configuration,
	rbacService *rbac.RBACService,
	supabaseAuth *supabase.Client,
	settingsService SettingsService,
	logger *logger.Logger,
) UserService {
	return &userService{
		userRepo:        userRepo,
		tenantRepo:      tenantRepo,
		authRepo:        authRepo,
		cfg:             cfg,
		rbacService:     rbacService,
		supabaseAuth:    supabaseAuth,
		settingsService: settingsService,
		logger:          logger,
	}
}

func (s *userService) GetUserInfo(ctx context.Context) (*dto.UserResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	tenant, err := s.tenantRepo.GetByID(ctx, user.TenantID)
	if err != nil {
		return nil, err
	}

	return dto.NewUserResponse(user, tenant), nil
}

func (s *userService) CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required in context").
			Mark(ierr.ErrValidation)
	}

	// Verify tenant exists
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Get current user ID for audit fields
	currentUserID := types.GetUserID(ctx)
	if currentUserID == "" {
		currentUserID = "system"
	}

	var newUser *user.User
	var password *string

	switch req.Type {
	case types.UserTypeUser:
		// invite user to the tenant
		newUser, password, err = s.InviteUser(ctx, req, currentUserID)
		if err != nil {
			return nil, err
		}

	case types.UserTypeServiceAccount:
		if s.rbacService == nil {
			return nil, ierr.NewError("RBAC not configured").
				WithHint("Service accounts require RBAC for role validation; provide a non-nil RBAC service.").
				Mark(ierr.ErrValidation)
		}
		for _, role := range req.Roles {
			if !s.rbacService.ValidateRole(role) {
				return nil, ierr.NewError("invalid role").
					WithHint("Role '" + role + "' does not exist").
					WithReportableDetails(map[string]interface{}{"role": role}).
					Mark(ierr.ErrValidation)
			}
		}
		newUser = &user.User{
			ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
			Email: "",
			Type:  types.UserTypeServiceAccount,
			Roles: req.Roles,
		}
		newUser.BaseModel = types.GetDefaultBaseModel(ctx)
		newUser.BaseModel.CreatedBy = currentUserID
		newUser.BaseModel.UpdatedBy = currentUserID

		if err := newUser.Validate(); err != nil {
			return nil, err
		}
		if err := s.userRepo.Create(ctx, newUser); err != nil {
			return nil, err
		}
	default:
		return nil, ierr.NewError("invalid user type").WithHint("Type must be 'user' or 'service_account'").Mark(ierr.ErrValidation)
	}

	return &dto.CreateUserResponse{
		UserResponse: dto.NewUserResponse(newUser, tenant),
		Password:     *password,
	}, nil
}

func (s *userService) ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error) {
	// Get tenant ID from context
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant_id not found in context").
			WithHint("Authentication context is missing tenant information").
			Mark(ierr.ErrValidation)
	}

	// Get users by filter from repository (tenantID comes from context in repo)
	users, total, err := s.userRepo.ListByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get tenant for response construction
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Convert to DTOs
	userResponses := make([]*dto.UserResponse, len(users))
	for i, u := range users {
		userResponses[i] = dto.NewUserResponse(u, tenant)
	}

	return &dto.ListUsersResponse{
		Items:      userResponses,
		Pagination: types.NewPaginationResponse(int(total), filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// InviteUser invites a user to the tenant
func (s *userService) InviteUser(ctx context.Context, req *dto.CreateUserRequest, currentUserID string) (*user.User, *string, error) {

	var userID string

	// Check if user by email already exists
	existingUser, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, nil, err
	}
	if existingUser != nil {
		return nil, nil, ierr.NewError("email already in use").
			WithHint("A user with this email already exists in this tenant").
			WithReportableDetails(map[string]interface{}{"email": req.Email}).
			Mark(ierr.ErrAlreadyExists)
	}
	// Enforce per-tenant user limit from add_user_config (GetSetting returns default when not set)
	svc, ok := s.settingsService.(*settingsService)
	if !ok || svc == nil {
		return nil, nil, ierr.NewError("settings service not configured").
			WithHint("User creation requires settings service for add_user_config.").
			Mark(ierr.ErrValidation)
	}
	addUserConfig, err := GetSetting[types.TenantConfig](svc, ctx, types.SettingKeyTenantConfig)
	if err != nil {
		return nil, nil, err
	}
	// ListByFilter uses tenant from context and repo filters by StatusPublished
	_, totalActiveUsers, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(1),
			Offset: lo.ToPtr(0),
			Status: lo.ToPtr(types.StatusPublished),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if totalActiveUsers >= int64(addUserConfig.MaxUsers) {
		return nil, nil, ierr.NewError("user limit reached: you cannot add any more users").
			WithHintf("Maximum %d user(s) allowed for this tenant. Limit reached.", addUserConfig.MaxUsers).
			WithReportableDetails(map[string]interface{}{"max_users": addUserConfig.MaxUsers, "current_active_users": totalActiveUsers}).
			Mark(ierr.ErrValidation)
	}

	if s.cfg == nil {
		return nil, nil, ierr.NewError("auth configuration missing").
			WithHint("User creation requires auth provider configuration").
			Mark(ierr.ErrValidation)
	}

	provider := authProvider.NewProvider(s.cfg)
	inviteResp, err := provider.UserInvite(ctx, authProvider.UserInviteRequest{
		Email: req.Email,
	})
	if err != nil {
		return nil, nil, err
	}
	userID = inviteResp.ID
	password := inviteResp.Password

	// Persist provider-specific auth material for later login (e.g. Flexprice bcrypt hash).
	if inviteResp.AuthRecord != nil {
		if s.authRepo == nil {
			return nil, nil, ierr.NewError("auth repository not configured").
				WithHint("Auth provider returned an auth record but auth repository is nil").
				Mark(ierr.ErrValidation)
		}
		if err := s.authRepo.CreateAuth(ctx, inviteResp.AuthRecord); err != nil {
			return nil, nil, err
		}
	}

	newUser := &user.User{
		ID:    userID,
		Email: req.Email,
		Type:  types.UserTypeUser,
		Roles: []string{},
	}
	newUser.BaseModel = types.GetDefaultBaseModel(ctx)
	newUser.BaseModel.CreatedBy = currentUserID
	newUser.BaseModel.UpdatedBy = currentUserID

	if err := newUser.Validate(); err != nil {
		return nil, nil, err
	}
	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, nil, err
	}
	return newUser, &password, nil
}
