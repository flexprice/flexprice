package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
}

type userService struct {
	userRepo   user.Repository
	tenantRepo tenant.Repository
}

func NewUserService(userRepo user.Repository, tenantRepo tenant.Repository) UserService {
	return &userService{
		userRepo:   userRepo,
		tenantRepo: tenantRepo,
	}
}

func (s *userService) GetUserInfo(ctx context.Context) (*dto.UserResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required")
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
