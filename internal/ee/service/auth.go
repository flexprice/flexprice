package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

		onboardingService := NewOnboardingService(s.ServiceParams)

		err = onboardingService.OnboardNewUserWithTenant(ctx, dto.OnboardNewUserWithTenantRequest{
			UserID:     authResponse.ID,
			Email:      req.Email,
			TenantName: req.TenantName,
			TenantID:   tenantID,
			Metadata:   req.Metadata,
		})

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

// refusePasswordLoginUnderSSO blocks password login for a tenant that has SSO
// enforced, so turning on single sign-on closes the password path rather than
// adding a second way in alongside it.
//
// Super admins are exempt: an identity provider outage would otherwise lock the
// tenant out of its own account entirely, with no recovery short of direct
// database access. They are already the group trusted to configure SAML.
//
// A failure to read the setting lets the login proceed. The alternative fails
// closed on every login for every tenant if the settings read breaks, which
// turns a settings problem into a total outage; enforcement is a policy control
// on an already-authenticated user, not the authentication itself.
func (s *authService) refusePasswordLoginUnderSSO(ctx context.Context, user *user.User) error {
	if s.Config == nil || !s.Config.Auth.SAML.Enabled {
		return nil
	}

	// Login runs before a session exists, so the tenant is taken from the user
	// row rather than the context, and SAML settings are tenant-level so no
	// environment is needed.
	tenantCtx := types.SetTenantID(ctx, user.TenantID)

	cfg, err := GetSetting[types.SAMLConfig](
		NewSettingsService(s.ServiceParams).(*settingsService), tenantCtx, types.SettingKeySAMLConfig)
	if err != nil {
		// Info rather than Error: the login proceeds, so this records a decision
		// the code recovered from rather than a failure. It is still worth
		// seeing — a tenant that has enforcement on is not getting it.
		s.Logger.Info(ctx, "could not read saml settings while checking sso enforcement; allowing password login",
			"error", err,
			"tenant_id", user.TenantID,
			"user_id", user.ID,
		)
		return nil
	}

	// Enforcement applies only once SSO can actually serve a login: a tenant
	// that set the flag while still awaiting approval would otherwise lock
	// itself out of a login SAML cannot yet perform.
	if !cfg.Enabled || !cfg.Active || !cfg.EnforceSSO {
		return nil
	}
	if lo.Contains(user.Roles, types.RoleSuperAdmin.String()) {
		return nil
	}

	s.Logger.Info(ctx, "password login refused because the tenant enforces single sign-on",
		"tenant_id", user.TenantID,
		"user_id", user.ID,
	)
	return ierr.NewError("password login is disabled for this organisation").
		WithHint("Your organisation requires single sign-on. Sign in through your identity provider.").
		Mark(ierr.ErrPermissionDenied)
}

// Login authenticates a user and returns an auth token
func (s *authService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.AuthResponse, error) {
	user, err := s.UserRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		// Only normalize the not-found case: an unknown email must return the
		// same "invalid credentials" response as a valid email with a wrong
		// password (both ErrPermissionDenied / 401), otherwise the distinct
		// not-found status enables account enumeration on this unauthenticated,
		// unthrottled endpoint. Other errors (e.g. a database failure) must
		// propagate as-is so they surface as server errors, not bad credentials.
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Invalid email or password").
				Mark(ierr.ErrPermissionDenied)
		}
		return nil, err
	}

	// Refused before the credentials are checked, so no token is ever minted for
	// a login the tenant does not allow. Doing it afterwards meant the provider
	// had already issued one — discarded here, but for a provider that keeps
	// server-side session state, established there.
	//
	// The cost is that a caller learns enforcement is on for an email that
	// exists, without knowing the password. That is a narrow signal: the address
	// has to be a real user's, and what it reveals — this organisation uses SSO —
	// is something its own login page tells anyone who visits it.
	if err := s.refusePasswordLoginUnderSSO(ctx, user); err != nil {
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
		// Same generic response as the unknown-email and identity-mismatch cases
		// so none of the three is distinguishable to an enumerating caller.
		return nil, ierr.WithError(err).
			WithHint("Invalid email or password").
			Mark(ierr.ErrPermissionDenied)
	}

	if authResponse.ID != user.ID {
		return nil, ierr.NewError("invalid credentials").
			WithHint("Invalid email or password").
			Mark(ierr.ErrPermissionDenied)
	}

	response := &dto.AuthResponse{
		Token:    authResponse.AuthToken,
		UserID:   authResponse.ID,
		TenantID: user.TenantID,
	}

	return response, nil
}
