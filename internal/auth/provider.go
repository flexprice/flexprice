package auth

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/types"
)

type AuthRequest struct {
	UserID   string
	TenantID string
	Email    string
	Password string
}

type AuthResponse struct {
	ProviderToken string
	AuthToken     string
	ID            string
}

type Provider interface {

	// User Management
	GetProvider() types.AuthProvider
	SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error)
	Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error)
	ValidateToken(ctx context.Context, token string) (*auth.Claims, error)
	AssignUserToTenant(ctx context.Context, userID string, tenantID string) error
}

func NewProvider(cfg *config.Configuration) Provider {
	switch cfg.Auth.Provider {
	case types.AuthProviderSupabase:
		return NewSupabaseAuth(cfg)
	default:
		return NewFlexpriceAuth(cfg)
	}
}
