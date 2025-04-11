package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/types"
)

type AuthService interface {
	SignUp(ctx context.Context, req *dto.SignUpRequest) (*dto.AuthResponse, error)
	Login(ctx context.Context, req *dto.LoginRequest) (*dto.AuthResponse, error)
}

type authService struct {
	ServiceParams
	pubSub       pubsub.PubSub
	authProvider authProvider.Provider
}

func NewAuthService(
	params ServiceParams,
	pubSub pubsub.PubSub,
) AuthService {
	return &authService{
		ServiceParams: params,
		pubSub:        pubSub,
		authProvider:  authProvider.NewProvider(params.Config),
	}
}

// SignUp creates a new user and returns an auth token
func (s *authService) SignUp(ctx context.Context, req *dto.SignUpRequest) (*dto.AuthResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Check if user already exists in our system
	existingUser, err := s.UserRepo.GetByEmail(ctx, req.Email)
	if existingUser != nil {
		// TODO: Check if the user is already onboarded to a tenant
		// if not, return an error
		// if yes, return the auth response with the user info
		return nil, ierr.NewError("user already exists").
			WithHint("An account with this email already exists").
			WithReportableDetails(map[string]interface{}{
				"email": req.Email,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	// Generate a tenant ID
	tenantID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT)

	authResponse, err := s.authProvider.SignUp(ctx, authProvider.AuthRequest{
		Email:    req.Email,
		Password: req.Password,
		Token:    req.Token,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sign up with authentication provider").
			Mark(ierr.ErrSystem)
	}

	response := &dto.AuthResponse{
		Token:    authResponse.AuthToken,
		UserID:   authResponse.ID,
		TenantID: tenantID,
	}

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Create auth record
		if s.authProvider.GetProvider() == types.AuthProviderFlexprice {
			auth := auth.NewAuth(authResponse.ID, s.authProvider.GetProvider(), authResponse.ProviderToken)
			err = s.AuthRepo.CreateAuth(ctx, auth)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to create authentication record").
					Mark(ierr.ErrDatabase)
			}
		}

		onboardingService := NewOnboardingService(s.ServiceParams, s.pubSub)

		err = onboardingService.OnboardNewUserWithTenant(
			ctx,
			authResponse.ID,
			req.Email,
			req.TenantName,
			tenantID,
		)

		if err != nil {
			return err
		}

		// Assign tenant to user in auth provider
		if err := s.authProvider.AssignUserToTenant(ctx, authResponse.ID, tenantID); err != nil {
			return ierr.WithError(err).
				WithHint("Unable to assign tenant to user in auth provider").
				Mark(ierr.ErrSystem)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// Login authenticates a user and returns an auth token
func (s *authService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.AuthResponse, error) {
	user, err := s.UserRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, err
	}

	var auth *auth.Auth
	if s.authProvider.GetProvider() == types.AuthProviderFlexprice {
		auth, err = s.AuthRepo.GetAuthByUserID(ctx, user.ID)
		if err != nil {
			return nil, err
		}
	}

	authResponse, err := s.authProvider.Login(ctx, authProvider.AuthRequest{
		UserID:   user.ID,
		TenantID: user.TenantID,
		Email:    user.Email,
		Password: req.Password,
	}, auth)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to authenticate").
			Mark(ierr.ErrPermissionDenied)
	}

	if authResponse.ID != user.ID {
		return nil, ierr.NewError("user not found").
			WithHint("User not found").
			WithReportableDetails(map[string]interface{}{
				"user_id": user.ID,
			}).
			Mark(ierr.ErrPermissionDenied)
	}

	response := &dto.AuthResponse{
		Token:    authResponse.AuthToken,
		UserID:   authResponse.ID,
		TenantID: user.TenantID,
	}

	return response, nil
}
