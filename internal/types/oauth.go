package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// OAuth credential field names (used in Credentials map)
const (
	// Common OAuth credentials
	OAuthCredentialClientID     = "client_id"
	OAuthCredentialClientSecret = "client_secret"

	// OAuth tokens (managed by backend after auth code exchange)
	OAuthCredentialAccessToken  = "access_token"
	OAuthCredentialRefreshToken = "refresh_token"
	OAuthCredentialAuthCode     = "auth_code" // Temporary during OAuth flow

	// Webhook security
	OAuthCredentialWebhookVerifierToken = "webhook_verifier_token"

	// QuickBooks-specific credentials
	// (Currently QuickBooks uses client_id and client_secret, which are common)
)

// OAuth metadata field names (used in Metadata map)
const (
	// QuickBooks metadata
	OAuthMetadataEnvironment     = "environment"       // "sandbox" or "production"
	OAuthMetadataIncomeAccountID = "income_account_id" // Optional, defaults to "79"
	OAuthMetadataRedirectURI     = "redirect_uri"      // Set by backend
	OAuthMetadataRealmID         = "realm_id"          // QuickBooks company ID

	// Future providers can add their metadata constants here
	// Stripe metadata example:
	// OAuthMetadataStripeAccountType = "account_type" // "standard" or "express"
)

// OAuth environment values for QuickBooks
const (
	OAuthEnvironmentSandbox    = "sandbox"
	OAuthEnvironmentProduction = "production"
)

// OAuthProvider represents the type of OAuth provider
type OAuthProvider string

const (
	OAuthProviderQuickBooks OAuthProvider = "quickbooks"
)

// OAuthSession represents a temporary OAuth session stored in cache during the OAuth flow.
// This is a generic session that supports multiple OAuth providers.
// Sessions auto-expire after 5 minutes for security.
type OAuthSession struct {
	// Session identification
	SessionID     string        `json:"session_id"`     // Random 32-byte hex string (cache key)
	Provider      OAuthProvider `json:"provider"`       // OAuth provider (quickbooks, stripe, etc.)
	TenantID      string        `json:"tenant_id"`      // Current tenant ID
	EnvironmentID string        `json:"environment_id"` // Current environment ID

	// Connection configuration (generic fields)
	Name string `json:"name"` // User-provided connection name

	// Provider-specific credentials (encrypted before caching)
	Credentials map[string]string `json:"credentials"` // e.g., client_id, client_secret, api_key

	// Provider-specific metadata (not encrypted, safe to store)
	Metadata map[string]string `json:"metadata"` // e.g., environment (sandbox/production), realm_id, income_account_id

	// Sync configuration (optional)
	SyncConfig *SyncConfig `json:"sync_config"` // Optional sync configuration for the connection

	// CSRF protection
	CSRFState string `json:"csrf_state"` // Random 32-byte hex string for state validation

	// Session lifecycle
	ExpiresAt time.Time `json:"expires_at"` // Auto-cleanup timestamp (5 minutes)
}

// IsExpired checks if the OAuth session has expired
func (s *OAuthSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Validate validates the OAuth session fields
func (s *OAuthSession) Validate() error {
	if s.SessionID == "" {
		return ierr.NewError("session_id is required").Mark(ierr.ErrValidation)
	}
	if s.Provider == "" {
		return ierr.NewError("provider is required").Mark(ierr.ErrValidation)
	}

	// Validate provider is supported
	switch s.Provider {
	case OAuthProviderQuickBooks:
		// Valid provider - validate QuickBooks-specific requirements
		if len(s.Credentials) == 0 {
			return ierr.NewError("credentials are required").Mark(ierr.ErrValidation)
		}
		if s.GetCredential(OAuthCredentialClientID) == "" {
			return ierr.NewError("client_id is required in credentials").Mark(ierr.ErrValidation)
		}
		if s.GetCredential(OAuthCredentialClientSecret) == "" {
			return ierr.NewError("client_secret is required in credentials").Mark(ierr.ErrValidation)
		}
		if len(s.Metadata) == 0 {
			return ierr.NewError("metadata is required").Mark(ierr.ErrValidation)
		}
		if s.GetMetadata(OAuthMetadataEnvironment) == "" {
			return ierr.NewError("environment is required in metadata").Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unsupported OAuth provider").
			WithHintf("Provider '%s' is not supported. Supported providers: quickbooks", s.Provider).
			Mark(ierr.ErrValidation)
	}

	if s.TenantID == "" {
		return ierr.NewError("tenant_id is required").Mark(ierr.ErrValidation)
	}
	if s.CSRFState == "" {
		return ierr.NewError("csrf_state is required").Mark(ierr.ErrValidation)
	}
	if s.Name == "" {
		return ierr.NewError("name is required").Mark(ierr.ErrValidation)
	}

	return nil
}

// GetCredential retrieves a credential value by key
func (s *OAuthSession) GetCredential(key string) string {
	if s.Credentials == nil {
		return ""
	}
	return s.Credentials[key]
}

// GetMetadata retrieves a metadata value by key
func (s *OAuthSession) GetMetadata(key string) string {
	if s.Metadata == nil {
		return ""
	}
	return s.Metadata[key]
}
