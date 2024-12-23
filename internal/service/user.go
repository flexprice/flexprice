package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
}

type userService struct {
	userRepo user.Repository
}

func NewUserService(userRepo user.Repository) UserService {
	return &userService{
		userRepo: userRepo,
	}
}

func (s *userService) GetUserInfo(ctx context.Context) (*dto.UserResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return dto.NewUserResponse(user), nil
}
