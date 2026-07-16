package auth

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/nedpals/supabase-go"
	"github.com/sethvargo/go-password/password"
)

type supabaseAuth struct {
	AuthConfig config.AuthConfig
	client     *supabase.Client
	logger     *logger.Logger
}

func NewSupabaseAuth(cfg *config.Configuration) Provider {
	supabaseUrl := cfg.Auth.Supabase.BaseURL
	adminApiKey := cfg.Auth.Supabase.ServiceKey

	client := supabase.CreateClient(supabaseUrl, adminApiKey)
	if client == nil {
		log.Fatalf("failed to create Supabase client")
	}

	logger, _ := logger.NewLogger(cfg)

	return &supabaseAuth{
		AuthConfig: cfg.Auth,
		client:     client,
		logger:     logger,
	}
}

func (s *supabaseAuth) GetProvider() types.AuthProvider {
	return types.AuthProviderSupabase
}

// SignUp is not used directly for Supabase as users sign up through the Supabase UI
// This method is kept for compatibility with the Provider interface
func (s *supabaseAuth) SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	// For Supabase, we don't directly sign up users through this method
	// Instead, we validate the token and get user info
	// For Supabase, we validate the token and extract user info
	if req.Token == "" {
		return nil, ierr.NewError("token is required").
			Mark(ierr.ErrPermissionDenied)
	}

	// Validate the token and extract user ID
	claims, err := s.ValidateToken(ctx, req.Token)
	if err != nil {
		return nil, ierr.NewError("invalid token").
			Mark(ierr.ErrPermissionDenied)
	}

	if claims.Email != req.Email {
		return nil, ierr.NewError("email mismatch").
			Mark(ierr.ErrPermissionDenied)
	}

	// Create auth response with the token
	authResponse := &AuthResponse{
		ProviderToken: claims.UserID,
		AuthToken:     req.Token,
		ID:            claims.UserID,
	}

	return authResponse, nil
}

// Login validates the token and returns user info
func (s *supabaseAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	user, err := s.client.Auth.SignIn(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get user").
			Mark(ierr.ErrPermissionDenied)
	}
	return &AuthResponse{
		ProviderToken: user.User.ID,
		AuthToken:     user.AccessToken,
		ID:            user.User.ID,
	}, nil
}

func (s *supabaseAuth) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ierr.NewError("unexpected signing method").
				WithHint("Unexpected signing method").
				WithReportableDetails(map[string]interface{}{
					"signing_method": token.Method.Alg(),
				}).
				Mark(ierr.ErrPermissionDenied)
		}
		return []byte(s.AuthConfig.Secret), nil
	})

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Token parse error").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, ierr.NewError("invalid token claims").
			WithHint("Invalid token claims").
			Mark(ierr.ErrPermissionDenied)
	}

	userID, userOk := claims["sub"].(string)
	if !userOk {
		return nil, ierr.NewError("token missing user ID").
			WithHint("Token missing user ID").
			Mark(ierr.ErrPermissionDenied)
	}

	// Get tenant_id from app_metadata
	var tenantID string
	if appMetadata, ok := claims["app_metadata"].(map[string]interface{}); ok {
		if tid, ok := appMetadata["tenant_id"].(string); ok {
			tenantID = tid
		}
	}

	email, ok := claims["email"].(string)
	if !ok {
		return nil, ierr.NewError("token missing email").
			WithHint("Token missing email").
			Mark(ierr.ErrPermissionDenied)
	}

	return &auth.Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
	}, nil
}

func (s *supabaseAuth) AssignUserToTenant(ctx context.Context, userID string, tenantID string) error {
	// Use Supabase Admin API to update user's app_metadata
	params := supabase.AdminUserParams{
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	}

	resp, err := s.client.Admin.UpdateUser(ctx, userID, params)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to assign tenant to user").
			Mark(ierr.ErrSystem)
	}

	s.logger.Debug(ctx, "assigned tenant to user",
		"user_id", userID,
		"tenant_id", tenantID,
		"response", resp,
	)

	return nil
}

// GenerateDevToken creates a short-lived JWT that matches the Supabase claim schema so it
// passes supabaseAuth.ValidateToken: { sub, email, app_metadata.tenant_id }.
// environmentID is accepted for interface compatibility but not embedded — pass X-Environment-ID header instead.
func (s *supabaseAuth) GenerateDevToken(tenantID, _, userID, email string, expiryHours int) (string, time.Time, error) {
	if tenantID == "" {
		return "", time.Time{}, ierr.NewError("tenantID is required").
			WithHint("Provide a tenant ID to generate a dev token").
			Mark(ierr.ErrValidation)
	}
	if email == "" {
		return "", time.Time{}, ierr.NewError("email is required for Supabase dev tokens").
			WithHint("Pass -user-email flag or set USER_EMAIL env var").
			Mark(ierr.ErrValidation)
	}

	expiresAt := time.Now().Add(time.Duration(expiryHours) * time.Hour)

	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"app_metadata": map[string]interface{}{
			"tenant_id": tenantID,
		},
		"exp": expiresAt.Unix(),
		"iat": time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.AuthConfig.Secret))
	if err != nil {
		return "", time.Time{}, ierr.WithError(err).
			WithHint("Failed to sign Supabase dev token").
			Mark(ierr.ErrSystem)
	}
	return signed, expiresAt, nil
}

// GenerateSessionToken generates a session token
// Note: For Supabase, dashboard tokens use the same mechanism as Flexprice auth
func (s *supabaseAuth) GenerateSessionToken(customerID, externalCustomerID, tenantID, environmentID string, timeoutHours int) (string, time.Time, error) {
	// Validate required parameters
	// Not Implemented yet
	return "", time.Time{}, nil
}

// ValidateSessionToken validates a session token
func (s *supabaseAuth) ValidateSessionToken(ctx context.Context, token string) (*auth.SessionClaims, error) {
	// Not Implemented yet
	return nil, nil
}

// UserInvite provisions a user in the configured auth provider and returns the newly created user ID and password.
func (s *supabaseAuth) UserInvite(ctx context.Context, req UserInviteRequest) (*UserInviteResponse, error) {
	if req.Email == "" {
		return nil, ierr.NewError("email is required").
			WithHint("Provide a valid email to invite/create a user").
			Mark(ierr.ErrValidation)
	}
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant id is required").
			WithHint("Provide tenant id in request context to associate the user in app_metadata").
			Mark(ierr.ErrValidation)
	}

	// Generate an initial password (no auth token issuance here).
	createdPassword, err := password.Generate(16, 4, 2, false, false)
	if err != nil {
		return nil, err
	}

	// Create in Supabase first. We intentionally do NOT return any auth token here.
	supabaseUser, err := s.client.Admin.CreateUser(ctx, supabase.AdminUserParams{
		Email:        req.Email,
		Password:     &createdPassword,
		EmailConfirm: true,
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	})
	if err != nil {
		// supabase-go returns a raw *supabase.ErrorResponse with no ierr classification,
		// which would otherwise surface as an opaque HTTP 500 with an empty body. Wrap it
		// so the caller gets a meaningful status/message, and detect the common
		// "email already registered" conflict. Supabase auth users are global (not
		// tenant-scoped), so an email absent from the Flexprice DB can still exist here.
		if supaErr, ok := err.(*supabase.ErrorResponse); ok {
			details := map[string]interface{}{
				"email":          req.Email,
				"supabase_code":  supaErr.Code,
				"supabase_error": supaErr.Message,
			}
			if supaErr.Code == http.StatusConflict ||
				supaErr.Code == http.StatusUnprocessableEntity ||
				strings.Contains(strings.ToLower(supaErr.Message), "already") {
				return nil, ierr.WithError(supaErr).
					WithHint("A user with this email already exists in the authentication provider").
					WithReportableDetails(details).
					Mark(ierr.ErrAlreadyExists)
			}
			return nil, ierr.WithError(supaErr).
				WithHintf("Authentication provider rejected user creation (code %d)", supaErr.Code).
				WithReportableDetails(details).
				Mark(ierr.ErrSystem)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create user in the authentication provider").
			WithReportableDetails(map[string]interface{}{"email": req.Email}).
			Mark(ierr.ErrSystem)
	}

	return &UserInviteResponse{ID: supabaseUser.ID, Password: createdPassword, AuthRecord: nil}, nil
}

// GenerateCheckoutToken creates a short-lived JWT for frontend payment checkout flows.
// Signed with the shared auth secret so the checkout page can decode it regardless of auth provider.
func (s *supabaseAuth) GenerateCheckoutToken(extraClaims map[string]interface{}) (string, error) {
	expiresAt := time.Now().Add(checkoutTokenTTL)

	claims := jwt.MapClaims{
		"exp": expiresAt.Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range extraClaims {
		claims[k] = v
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.AuthConfig.Secret))
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to sign checkout token").
			Mark(ierr.ErrSystem)
	}
	return signed, nil
}
