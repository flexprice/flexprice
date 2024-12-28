package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
)

type AuthService interface {
	SignUp(ctx context.Context, req *dto.SignUpRequest) (*dto.AuthResponse, error)
	Login(ctx context.Context, req *dto.LoginRequest) (*dto.AuthResponse, error)
}

type authService struct {
	userRepo     user.Repository
	authProvider authProvider.Provider
	authRepo     auth.Repository
}

func NewAuthService(cfg *config.Configuration, userRepo user.Repository, authRepo auth.Repository) AuthService {
	return &authService{
		userRepo:     userRepo,
		authProvider: authProvider.NewProvider(cfg),
		authRepo:     authRepo,
	}
}

// SignUp creates a new user and returns an auth token
// TODO: make it transactional
func (s *authService) SignUp(ctx context.Context, req *dto.SignUpRequest) (*dto.AuthResponse, error) {
	tenantID := types.DefaultTenantID

	user := user.NewUser(req.Email, tenantID)
	err := s.userRepo.Create(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("unable to create user: %w", err)
	}

	authResponse, err := s.authProvider.SignUp(ctx, authProvider.AuthRequest{
		UserID:   user.ID,
		TenantID: tenantID,
		Email:    user.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to sign up: %w", err)
	}

	auth := auth.NewAuth(user.ID, types.AuthProviderFlexprice, authResponse.ProviderToken)
	err = s.authRepo.CreateAuth(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("unable to create auth: %w", err)
	}

	response := &dto.AuthResponse{
		Token: authResponse.AuthToken,
	}

	return response, nil
}

// Login authenticates a user and returns an auth token
func (s *authService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.AuthResponse, error) {
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("unable to get user: %w", err)
	}

	auth, err := s.authRepo.GetAuthByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch user authentication channel: %w", err)
	}

	authResponse, err := s.authProvider.Login(ctx, authProvider.AuthRequest{
		UserID:   user.ID,
		TenantID: user.TenantID,
		Email:    user.Email,
		Password: req.Password,
	}, auth)
	if err != nil {
		return nil, fmt.Errorf("unable to login: %w", err)
	}

	response := &dto.AuthResponse{
		Token: authResponse.AuthToken,
	}

	return response, nil
}
