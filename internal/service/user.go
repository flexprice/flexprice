package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
)

type UserService interface {
	CreateUser(ctx context.Context, user *user.User) error
	GetUserByEmail(ctx context.Context, email string) (*user.User, error)
	GetUserInfo(ctx context.Context) (*user.User, error)
}

type userService struct {
	userRepo user.Repository
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func NewUserService(userRepo user.Repository) UserService {
	return &userService{
		userRepo: userRepo,
	}
}

func (s *userService) CreateUser(ctx context.Context, user *user.User) error {
	if user.Email == "" {
		return fmt.Errorf("email is required")
	}
	// Validate email format
	if !emailRegex.MatchString(user.Email) {
		return errors.New("invalid email format")
	}

	// Check for duplicate email
	existingUser, _ := s.userRepo.GetByEmail(ctx, user.Email)
	if existingUser != nil {
		return errors.New("user already exists")
	}
	return s.userRepo.Create(ctx, user)
}

func (s *userService) GetUserByEmail(ctx context.Context, email string) (*user.User, error) {
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	return s.userRepo.GetByEmail(ctx, email)
}

func (s *userService) GetUserInfo(ctx context.Context) (*user.User, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}

	return s.userRepo.GetByID(ctx, userID)
}
