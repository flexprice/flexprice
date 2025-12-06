package dto

import (
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InitiateOAuthRequest represents a generic request to initiate OAuth for any provider
type InitiateOAuthRequest struct {
	Provider    types.OAuthProvider `json:"provider" binding:"required"`    // e.g., "quickbooks"
	Name        string              `json:"name" binding:"required"`        // Connection name
	Credentials map[string]string   `json:"credentials" binding:"required"` // Provider-specific credentials (all required for the provider)
	Metadata    map[string]string   `json:"metadata" binding:"required"`    // Provider-specific metadata (all required for the provider)
	SyncConfig  *types.SyncConfig   `json:"sync_config"`                    // Optional sync configuration for the connection
}

// Validate validates the OAuth init request with provider-specific rules
func (r *InitiateOAuthRequest) Validate() error {
	switch r.Provider {
	case types.OAuthProviderQuickBooks:
		// Validate required credentials
		if r.Credentials[types.OAuthCredentialClientID] == "" {
			return errors.NewError("client_id is required for QuickBooks OAuth").
				WithHint("Provide your QuickBooks app client_id in the credentials field").
				Mark(errors.ErrValidation)
		}
		if r.Credentials[types.OAuthCredentialClientSecret] == "" {
			return errors.NewError("client_secret is required for QuickBooks OAuth").
				WithHint("Provide your QuickBooks app client_secret in the credentials field").
				Mark(errors.ErrValidation)
		}

		// Validate required metadata
		if len(r.Metadata) == 0 {
			return errors.NewError("metadata is required for QuickBooks OAuth").
				WithHint("Provide environment, and optionally income_account_id in the metadata field").
				Mark(errors.ErrValidation)
		}

		environment := r.Metadata[types.OAuthMetadataEnvironment]
		if environment == "" {
			return errors.NewError("environment is required for QuickBooks OAuth").
				WithHint("Set metadata.environment to either 'sandbox' or 'production'").
				Mark(errors.ErrValidation)
		}
		if environment != types.OAuthEnvironmentSandbox && environment != types.OAuthEnvironmentProduction {
			return errors.NewError("invalid environment value").
				WithHintf("Environment must be 'sandbox' or 'production', got '%s'", environment).
				Mark(errors.ErrValidation)
		}

		// income_account_id is optional, defaults to "79" if not provided

		return nil

	default:
		return errors.NewError("unsupported OAuth provider").
			WithHintf("Provider '%s' is not supported. Supported providers: quickbooks", r.Provider).
			Mark(errors.ErrValidation)
	}
}

// InitiateOAuthResponse represents the response from initiating OAuth
type InitiateOAuthResponse struct {
	OAuthURL  string `json:"oauth_url"`
	SessionID string `json:"session_id"`
}

// CompleteOAuthRequest represents a generic request to complete OAuth for any provider
type CompleteOAuthRequest struct {
	Provider  types.OAuthProvider `json:"provider" binding:"required"`   // e.g., "quickbooks"
	SessionID string              `json:"session_id" binding:"required"` // Session ID from initiate OAuth
	Code      string              `json:"code" binding:"required"`       // OAuth authorization code from the provider
	State     string              `json:"state" binding:"required"`      // CSRF state token
	RealmID   string              `json:"realm_id"`                      // QuickBooks realm ID (required for QuickBooks, validated in Validate())
}

// Validate validates the OAuth complete request with provider-specific rules
func (r *CompleteOAuthRequest) Validate() error {
	// Validate all required fields are non-empty
	if r.SessionID == "" {
		return errors.NewError("session_id is required").
			WithHint("Session ID is required to complete OAuth flow").
			Mark(errors.ErrValidation)
	}
	if r.Code == "" {
		return errors.NewError("code is required").
			WithHint("Authorization code is required to complete OAuth flow").
			Mark(errors.ErrValidation)
	}
	if r.State == "" {
		return errors.NewError("state is required").
			WithHint("CSRF state token is required to complete OAuth flow").
			Mark(errors.ErrValidation)
	}

	// Provider-specific validation
	switch r.Provider {
	case types.OAuthProviderQuickBooks:
		if r.RealmID == "" {
			return errors.NewError("realm_id is required for QuickBooks OAuth").
				WithHint("QuickBooks realm_id must be provided to complete the OAuth flow").
				Mark(errors.ErrValidation)
		}
		return nil

	default:
		return errors.NewError("unsupported OAuth provider").
			WithHintf("Provider '%s' is not supported. Supported providers: quickbooks", r.Provider).
			Mark(errors.ErrValidation)
	}
}

// CompleteOAuthResponse represents the response from completing OAuth
type CompleteOAuthResponse struct {
	Success      bool   `json:"success"`
	ConnectionID string `json:"connection_id"`
}
