package auth

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Auth struct {
	UserID    string             `json:"user_id"` // unique identifier for this table
	Provider  types.AuthProvider `json:"provider"`
	Token     string             `json:"token"` // ex HashedPassword, etc
	Status    types.Status       `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type Claims struct {
	UserID        string `json:"user_id"`
	TenantID      string `json:"tenant_id"`
	Email         string `json:"email,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`

	// EmailVerified reports whether the identity provider has confirmed the
	// user controls Email. Only the Supabase provider populates it — the
	// flexprice and SSO providers leave it false because their tokens carry no
	// equivalent claim, so it must never be read as a general-purpose gate.
	// Consult it only on paths that have already established the provider is
	// Supabase; see authService.SignUp.
	EmailVerified bool `json:"email_verified,omitempty"`
}

// DashboardClaims represents the claims in a customer dashboard JWT token
type SessionClaims struct {
	ExternalCustomerID string // Required: external ID from tenant system
	CustomerID         string // Required: internal Flexprice customer ID
	TenantID           string
	EnvironmentID      string
}

func NewAuth(userID string, provider types.AuthProvider, token string) *Auth {
	return &Auth{
		UserID:    userID,
		Provider:  provider,
		Token:     token,
		Status:    types.StatusPublished,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// FromEnt converts an ent Auth to a domain Auth
func FromEnt(e *ent.Auth) *Auth {
	if e == nil {
		return nil
	}

	return &Auth{
		UserID:    e.UserID,
		Provider:  types.AuthProvider(e.Provider),
		Token:     e.Token,
		Status:    types.Status(e.Status),
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// FromEntList converts a list of ent Auths to domain Auths
func FromEntList(auths []*ent.Auth) []*Auth {
	if auths == nil {
		return nil
	}

	result := make([]*Auth, len(auths))
	for i, a := range auths {
		result[i] = FromEnt(a)
	}

	return result
}
