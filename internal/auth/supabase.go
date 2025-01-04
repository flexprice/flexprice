package auth

import (
	"context"
	"fmt"
	"log"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt"
	"github.com/nedpals/supabase-go"
)

type supabaseAuth struct {
	AuthConfig config.AuthConfig
	client     *supabase.Client
}

func NewSupabaseAuth(cfg *config.Configuration) Provider {
	supabaseUrl := cfg.Auth.Supabase.BaseURL
	adminApiKey := cfg.Auth.Supabase.ServiceKey

	client := supabase.CreateClient(supabaseUrl, adminApiKey)
	if client == nil {
		log.Fatalf("failed to create Supabase client")
	}

	return &supabaseAuth{
		AuthConfig: cfg.Auth,
		client:     client,
	}
}

func (s *supabaseAuth) GetProvider() types.AuthProvider {
	return types.AuthProviderSupabase
}

func (s *supabaseAuth) SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	_, err := s.client.Auth.SignUp(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign up: %w", err)
	}

	return s.Login(ctx, req, nil)
}

func (s *supabaseAuth) Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error) {
	user, err := s.client.Auth.SignIn(ctx, supabase.UserCredentials{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &AuthResponse{
		ProviderToken: user.AccessToken,
		AuthToken:     user.AccessToken,
		ID:            user.User.ID,
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

	// Get tenant_id from app_metadata
	var tenantID string
	if appMetadata, ok := claims["app_metadata"].(map[string]interface{}); ok {
		if tid, ok := appMetadata["tenant_id"].(string); ok {
			tenantID = tid
		}
	}

	// If no tenant_id found in app_metadata, use default
	if tenantID == "" {
		tenantID = types.DefaultTenantID
	}

	return &auth.Claims{
		UserID:   userID,
		TenantID: tenantID,
	}, nil
}

func (s *supabaseAuth) AssignUserToTenant(ctx context.Context, userID string, tenantID string) error {
	// Use Supabase Admin API to update user's app_metadata
	params := supabase.AdminUserParams{
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	}

	resp, err := s.client.Admin.UpdateUser(context.Background(), userID, params)
	if err != nil {
		return fmt.Errorf("failed to assign tenant to user: %w", err)
	}

	log, _ := logger.NewLogger(config.GetDefaultConfig())
	log.Debugw("assigned tenant to user",
		"user_id", userID,
		"tenant_id", tenantID,
		"response", resp,
	)

	return nil
}
