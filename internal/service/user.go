package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
	CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.UserResponse, error)
	ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error)
}

type userService struct {
	userRepo    user.Repository
	tenantRepo  tenant.Repository
	rbacService *rbac.RBACService
}

func NewUserService(userRepo user.Repository, tenantRepo tenant.Repository, rbacService *rbac.RBACService) UserService {
	return &userService{
		userRepo:    userRepo,
		tenantRepo:  tenantRepo,
		rbacService: rbacService,
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

func (s *userService) CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.UserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate roles (required for service accounts)
	for _, role := range req.Roles {
		if !s.rbacService.ValidateRole(role) {
			return nil, ierr.NewError("invalid role").
				WithHint("Role '" + role + "' does not exist").
				WithReportableDetails(map[string]interface{}{
					"role": role,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Get tenant ID from context
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

	// Create service account with RBAC roles
	newUser := &user.User{
		ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
		Email: "",                           // Service accounts have no email
		Type:  types.UserTypeServiceAccount, // Always service_account
		Roles: req.Roles,                    // Required roles
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedBy: currentUserID,
			UpdatedBy: currentUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Validate user before creating
	if err := newUser.Validate(); err != nil {
		return nil, err
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	return dto.NewUserResponse(newUser, tenant), nil
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
