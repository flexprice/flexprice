package auth

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt"
	"github.com/nedpals/supabase-go"
)

type supabaseAuth struct {
	AuthConfig     config.AuthConfig
	SupabaseClient *supabase.Client
}

func NewSupabaseAuth(cfg *config.Configuration) Provider {
	// Validate the configuration
	if cfg.Auth.Supabase.BaseURL == "" || cfg.Auth.Supabase.AnonKey == "" {
		panic("Supabase BaseURL and AnonKey are required")
	}

	// Initialize Supabase client
	client := supabase.CreateClient(cfg.Auth.Supabase.BaseURL, cfg.Auth.Supabase.AnonKey)

	return &supabaseAuth{
		AuthConfig:     cfg.Auth,
		SupabaseClient: client,
	}
}

func (s *supabaseAuth) GetProvider() types.AuthProvider {
	return types.AuthProviderSupabase
}

func (s *supabaseAuth) SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	// Use Supabase's SignUp method
	session, err := s.SupabaseClient.Auth.SignUp(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("supabase signup failed: %w", err)
	}

	// Return the provider token
	return &AuthResponse{
		ProviderToken: session.Email,
	}, nil
}

func (s *supabaseAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	// Use Supabase's SignIn method
	session, err := s.SupabaseClient.Auth.SignIn(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("supabase login failed: %w", err)
	}

	// Validate the session and extract the access token
	if session == nil || session.AccessToken == "" {
		return nil, fmt.Errorf("invalid response from Supabase: missing session or access token")
	}

	return &AuthResponse{
		ProviderToken: session.AccessToken,
	}, nil
}

func (s *supabaseAuth) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.AuthConfig.Secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("token parse error: %w", err)
	}

	claims, ok := parsedToken.Claims.(jwt.MapClaims)
	if !ok || !parsedToken.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	userID, userOk := claims["sub"].(string)
	if !userOk {
		return nil, fmt.Errorf("token missing user ID")
	}

	tenantID, tenantOk := claims["tenant_id"].(string)
	if !tenantOk {
		tenantID = types.DefaultTenantID
	}

	return &auth.Claims{
		UserID:   userID,
		TenantID: tenantID,
	}, nil
}
