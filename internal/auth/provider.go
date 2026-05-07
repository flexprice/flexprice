package auth

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/types"
)

// AuthRequest we create this by first checking the email in the DB and if found we
// set the user and tenant id and then with this request we try to validate the saved
// provider token with the user provided input and get the auth token
type AuthRequest struct {
	UserID   string
	TenantID string
	Email    string
	Password string
	Token    string
}

type AuthResponse struct {
	// ProviderToken is the fixed identifier or code provided by the provider
	// for example, in Supabase, it's the user ID and for Flexprice, it's the hashed password
	ProviderToken string
	// AuthToken is the token used to authenticate with the application or the generated
	// jwt token for the user
	AuthToken string
	// ID is the ID of the user
	ID string
}

// UserInviteRequest is used to create/invite a user in the configured auth provider.
// This flow provisions a user identity and sets the initial password (no auth token issuance).
type UserInviteRequest struct {
	Email string
}

type UserInviteResponse struct {
	// ID is the provider user ID (or generated user ID for Flexprice).
	ID string
	// Password is the generated initial password for the user (returned so it can be shown once).
	Password string
	// AuthRecord is optional provider-specific auth material that must be persisted server-side.
	// For Flexprice this contains the bcrypt hash; for Supabase it will be nil.
	AuthRecord *auth.Auth
}

type Provider interface {

	// User Management
	GetProvider() types.AuthProvider
	UserInvite(ctx context.Context, req UserInviteRequest) (*UserInviteResponse, error)
	SignUp(ctx context.Context, req AuthRequest) (*AuthResponse, error)
	Login(ctx context.Context, req AuthRequest, userAuthInfo *auth.Auth) (*AuthResponse, error)
	ValidateToken(ctx context.Context, token string) (*auth.Claims, error)
	AssignUserToTenant(ctx context.Context, userID string, tenantID string) error

	// Customer Dashboard Token Management
	GenerateSessionToken(customerID, externalCustomerID, tenantID, environmentID string, timeoutHours int) (string, time.Time, error)
	ValidateSessionToken(ctx context.Context, token string) (*auth.SessionClaims, error)
}

func NewProvider(cfg *config.Configuration) Provider {
	switch cfg.Auth.Provider {
	case types.AuthProviderSupabase:
		return NewSupabaseAuth(cfg)
	default:
		return NewFlexpriceAuth(cfg)
	}
}
