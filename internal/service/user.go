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
	GetByID(ctx context.Context, userID, tenantID string) (*user.User, error)
	ListServiceAccounts(ctx context.Context, tenantID string) ([]*user.User, error)
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

	// Validate roles if provided
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

	// Get tenant ID from request or context
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = types.GetTenantID(ctx)
	}
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	// Verify tenant exists
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Determine email based on user type
	email := req.Email
	userType := user.UserType(req.Type)

	// Service accounts have NO email
	if userType == user.UserTypeServiceAccount {
		email = "" // No email for service accounts
	}

	// Check if user already exists (only for regular users with emails)
	if userType == user.UserTypeUser && email != "" {
		existingUser, _ := s.userRepo.GetByEmail(ctx, email)
		if existingUser != nil {
			return nil, ierr.NewError("user already exists").
				WithHint("A user with this email already exists").
				WithReportableDetails(map[string]interface{}{
					"email": email,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}

	// Get current user ID for audit fields
	currentUserID := types.GetUserID(ctx)
	if currentUserID == "" {
		currentUserID = "system"
	}

	// Create user with RBAC fields
	newUser := &user.User{
		ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
		Email: email, // Empty for service accounts
		Type:  req.Type,
		Roles: req.Roles,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedBy: currentUserID,
			UpdatedBy: currentUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, err
	}

	return dto.NewUserResponse(newUser, tenant), nil
}

func (s *userService) GetByID(ctx context.Context, userID, tenantID string) (*user.User, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Verify tenant matches
	if user.TenantID != tenantID {
		return nil, ierr.NewError("user not found").
			WithHint("User does not belong to this tenant").
			Mark(ierr.ErrNotFound)
	}

	return user, nil
}

func (s *userService) ListServiceAccounts(ctx context.Context, tenantID string) ([]*user.User, error) {
	// Get all users for the tenant filtered by type=service_account
	users, err := s.userRepo.ListByType(ctx, tenantID, string(user.UserTypeServiceAccount))
	if err != nil {
		return nil, err
	}

	return users, nil
}
