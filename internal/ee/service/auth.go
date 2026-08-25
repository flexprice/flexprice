package service

import (
	"context"
	"strings"

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

	// Require the provider to have confirmed the email before an account and
	// tenant are created for it.
	//
	// Without this the check lives entirely in the Supabase project's "Confirm
	// email" setting, which the backend cannot observe: a project configured
	// without confirmation issues a session straight away, and that session is
	// indistinguishable here from a confirmed one. Enforcing it in code keeps
	// account creation correct independently of how the project is configured.
	//
	// Scoped to Supabase because only its tokens carry the claim; the flexprice
	// and SSO providers would always read false and be rejected outright.
	if s.authProvider.GetProvider() == types.AuthProviderSupabase {
		if err := s.refuseUnverifiedEmail(ctx, req.Token, req.Email); err != nil {
			return nil, err
		}
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

// refuseUnverifiedEmail blocks account creation when Supabase has not recorded
// a confirmed email for the signing-in user.
//
// The token is re-validated here rather than trusting anything the caller sent
// alongside it: the signup handler accepts the token from the request body or
// an Authorization header, so the identity on it is only trustworthy once the
// signature has been checked.
//
// The email on the token must also match the email being registered, so a token
// issued for one address cannot create an account for another.
//
// Confirmation itself is read from the provider rather than from the token. See
// supabaseAuth.EmailConfirmed for why the token's own claim cannot be trusted.
func (s *authService) refuseUnverifiedEmail(ctx context.Context, token, email string) error {
	if token == "" {
		return ierr.NewError("token is required").
			WithHint("A verified sign-in is required to create an account").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, err := s.authProvider.ValidateToken(ctx, token)
	if err != nil {
		return ierr.WithError(err).
			WithHint("A verified sign-in is required to create an account").
			Mark(ierr.ErrPermissionDenied)
	}

	if !strings.EqualFold(strings.TrimSpace(claims.Email), strings.TrimSpace(email)) {
		s.Logger.Info(ctx, "signup refused because the token email does not match the registered email",
			"token_email", claims.Email,
			"request_email", email,
		)
		return ierr.NewError("token email does not match the registered email").
			WithHint("A verified sign-in is required to create an account").
			Mark(ierr.ErrPermissionDenied)
	}

	checker, ok := s.authProvider.(authProvider.EmailConfirmChecker)
	if !ok {
		// SignUp only calls this for the Supabase provider, which implements the
		// interface. Reaching here means the wiring changed, and treating an
		// unknown provider as verified would silently drop the guard.
		return ierr.NewError("auth provider cannot report email confirmation").
			WithHint("A verified sign-in is required to create an account").
			Mark(ierr.ErrSystem)
	}

	confirmed, err := checker.EmailConfirmed(ctx, claims.UserID)
	if err != nil {
		return err
	}
	if !confirmed {
		s.Logger.Info(ctx, "signup refused because the email is not confirmed",
			"email", email,
		)
		return ierr.NewError("email is not verified").
			WithHint("Confirm your email address before creating an account, then sign in again.").
			Mark(ierr.ErrPermissionDenied)
	}

	return nil
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
