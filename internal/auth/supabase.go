package auth

import (
	"context"
	"log"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/nedpals/supabase-go"
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

	s.logger.Debugw("assigned tenant to user",
		"user_id", userID,
		"tenant_id", tenantID,
		"response", resp,
	)

	return nil
}
