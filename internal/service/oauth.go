package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// OAuthSessionTTL is the lifetime of an OAuth session (5 minutes)
	// This matches typical OAuth authorization code expiry times
	OAuthSessionTTL = 5 * time.Minute
)

// OAuthService manages OAuth sessions during OAuth flows for multiple providers
// Sessions are stored in connections table as incomplete connections
type OAuthService interface {
	// StoreOAuthSession creates an incomplete connection with encrypted OAuth session data
	StoreOAuthSession(ctx context.Context, session *types.OAuthSession) error

	// GetOAuthSession retrieves and decrypts an OAuth session from connection
	GetOAuthSession(ctx context.Context, sessionID string) (*types.OAuthSession, error)

	// DeleteOAuthSession removes an incomplete OAuth connection (cleanup on error)
	DeleteOAuthSession(ctx context.Context, sessionID string) error

	// GenerateSessionID generates a cryptographically secure random session ID
	GenerateSessionID() (string, error)

	// GenerateCSRFState generates a cryptographically secure random CSRF state token
	GenerateCSRFState() (string, error)

	// BuildOAuthURL builds the provider-specific OAuth authorization URL
	BuildOAuthURL(provider types.OAuthProvider, clientID, redirectURI, state string, metadata map[string]string) (string, error)

	// ExchangeCodeForConnection exchanges the authorization code for tokens and updates the connection
	ExchangeCodeForConnection(ctx context.Context, session *types.OAuthSession, code, realmID string) (connectionID string, err error)
}

type oauthService struct {
	connectionRepo     connection.Repository
	encryptionService  security.EncryptionService
	connectionService  ConnectionService
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	connectionService ConnectionService,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) OAuthService {
	return &oauthService{
		connectionRepo:     connectionRepo,
		encryptionService:  encryptionService,
		connectionService:  connectionService,
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// StoreOAuthSession creates an incomplete connection with encrypted OAuth session data in encrypted_secret_data
func (s *oauthService) StoreOAuthSession(ctx context.Context, session *types.OAuthSession) error {
	// Validate session
	if err := session.Validate(); err != nil {
		return ierr.WithError(err).
			WithHint("OAuth session validation failed").
			Mark(ierr.ErrValidation)
	}

	// Check expiration
	if session.IsExpired() {
		return ierr.NewError("OAuth session has already expired").
			WithHint("Session must have a future expiration time").
			Mark(ierr.ErrValidation)
	}

	// CRITICAL: Encrypt all credentials before storing
	encryptedCredentials := make(map[string]string)

	// DEBUG: Log what credentials we received
	s.logger.Debugw("storing OAuth session credentials",
		"session_id", session.SessionID,
		"credentials_count", len(session.Credentials),
		"credentials_keys", func() []string {
			keys := make([]string, 0, len(session.Credentials))
			for k := range session.Credentials {
				keys = append(keys, k)
			}
			return keys
		}())

	for key, value := range session.Credentials {
		encrypted, err := s.encryptionService.Encrypt(value)
		if err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("Failed to encrypt credential '%s' for OAuth session", key)).
				Mark(ierr.ErrInternal)
		}
		encryptedCredentials[key] = encrypted
	}

	// Build OAuth session data to store in encrypted_secret_data
	oauthSessionData := map[string]interface{}{
		"session_id":     session.SessionID,
		"csrf_state":     session.CSRFState,
		"expires_at":     session.ExpiresAt.Format(time.RFC3339),
		"oauth_provider": string(session.Provider),
		"credentials":    encryptedCredentials,
	}

	// Add non-sensitive metadata
	for key, value := range session.Metadata {
		oauthSessionData[key] = value
	}

	// Add sync config if provided
	if session.SyncConfig != nil {
		oauthSessionData["sync_config"] = session.SyncConfig
	}

	// Encrypt the entire OAuth session data as JSON
	// This goes into encrypted_secret_data field
	sessionJSON, err := json.Marshal(oauthSessionData)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to serialize OAuth session data").
			Mark(ierr.ErrInternal)
	}

	encryptedSessionJSON, err := s.encryptionService.Encrypt(string(sessionJSON))
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to encrypt OAuth session data").
			Mark(ierr.ErrInternal)
	}

	// Check if a published QuickBooks connection already exists for this tenant/environment
	// GetByProvider automatically filters by ctx.tenant, ctx.env, provider, and status=published
	existingConn, err := s.connectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil && !ierr.IsNotFound(err) {
		// Real database error (not just "not found")
		return ierr.WithError(err).
			WithHint("Failed to check for existing connections").
			Mark(ierr.ErrDatabase)
	}

	// If connection exists, reject unless it's a pending OAuth session (has OAuthSessionData)
	if existingConn != nil {
		// Connection already exists
		return ierr.NewError("connection already exists").
			WithHintf("A published connection for QuickBooks already exists in this environment").
			WithReportableDetails(map[string]interface{}{
				"provider_type":          types.SecretProviderQuickBooks,
				"tenant_id":              session.TenantID,
				"environment_id":         session.EnvironmentID,
				"existing_connection_id": existingConn.ID,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	// Create incomplete connection
	// Generate proper connection ID (NOT session_id)
	incompleteConnection := &connection.Connection{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
		Name:         session.Name,
		ProviderType: types.SecretProviderQuickBooks, // Provider type for incomplete connection
		// Store encrypted OAuth session data in OAuthSessionData field (temporary)
		// This will be cleared and replaced with actual credentials after OAuth completion
		EncryptedSecretData: types.ConnectionMetadata{
			QuickBooks: &types.QuickBooksConnectionMetadata{
				OAuthSessionData: encryptedSessionJSON, // Temporary encrypted session data
			},
		},
		SyncConfig:    session.SyncConfig,
		EnvironmentID: session.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  session.TenantID,
			Status:    types.StatusPublished,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			CreatedBy: session.TenantID,
			UpdatedBy: session.TenantID,
		},
	}

	// Store in database
	if err := s.connectionRepo.Create(ctx, incompleteConnection); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to store OAuth session in database").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("stored OAuth session as incomplete connection",
		"session_id", session.SessionID,
		"connection_id", incompleteConnection.ID,
		"provider", session.Provider,
		"tenant_id", session.TenantID,
		"expires_at", session.ExpiresAt)

	return nil
}

// GetOAuthSession retrieves and decrypts an OAuth session from connection's encrypted_secret_data
func (s *oauthService) GetOAuthSession(ctx context.Context, sessionID string) (*types.OAuthSession, error) {
	if sessionID == "" {
		return nil, ierr.NewError("session_id is required").
			WithHint("Provide a valid session_id from the OAuth init response").
			Mark(ierr.ErrValidation)
	}

	// List all QuickBooks connections to find the one with matching session_id
	filter := &types.ConnectionFilter{
		ProviderType: types.SecretProviderQuickBooks,
	}

	connections, err := s.connectionRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve OAuth sessions").
			Mark(ierr.ErrDatabase)
	}

	// Find connection with matching session_id in encrypted_secret_data
	var conn *connection.Connection
	var oauthSessionData map[string]interface{}

	for _, c := range connections {
		if c.EncryptedSecretData.QuickBooks != nil && c.EncryptedSecretData.QuickBooks.OAuthSessionData != "" {
			// Decrypt the OAuth session data from OAuthSessionData field
			decryptedJSON, err := s.encryptionService.Decrypt(c.EncryptedSecretData.QuickBooks.OAuthSessionData)
			if err != nil {
				continue // Skip this connection if decryption fails
			}

			var sessionData map[string]interface{}
			if err := json.Unmarshal([]byte(decryptedJSON), &sessionData); err == nil {
				if storedSessionID, ok := sessionData["session_id"].(string); ok && storedSessionID == sessionID {
					conn = c
					oauthSessionData = sessionData
					break
				}
			}
		}
	}

	if conn == nil || oauthSessionData == nil {
		return nil, ierr.NewError("OAuth session not found or expired").
			WithHint("The OAuth session may have expired (5 minute timeout) or been deleted").
			Mark(ierr.ErrNotFound)
	}

	// Parse expires_at
	expiresAtStr, ok := oauthSessionData["expires_at"].(string)
	if !ok {
		return nil, ierr.NewError("OAuth session expiration time is missing").
			Mark(ierr.ErrInternal)
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to parse OAuth session expiration time").
			Mark(ierr.ErrInternal)
	}

	// Check if session has expired
	if time.Now().UTC().After(expiresAt) {
		// Auto-delete expired session
		_ = s.connectionRepo.Delete(ctx, conn)
		return nil, ierr.NewError("OAuth session has expired").
			WithHint("OAuth sessions expire after 5 minutes. Please restart the OAuth flow").
			Mark(ierr.ErrNotFound)
	}

	// Extract provider
	providerStr, ok := oauthSessionData["oauth_provider"].(string)
	if !ok {
		return nil, ierr.NewError("OAuth provider is missing from session").
			Mark(ierr.ErrInternal)
	}
	provider := types.OAuthProvider(providerStr)

	// Extract CSRF state
	csrfState, ok := oauthSessionData["csrf_state"].(string)
	if !ok {
		return nil, ierr.NewError("CSRF state is missing from session").
			Mark(ierr.ErrInternal)
	}

	// Decrypt credentials
	encryptedCreds, ok := oauthSessionData["credentials"].(map[string]interface{})
	if !ok {
		return nil, ierr.NewError("credentials are missing from session").
			Mark(ierr.ErrInternal)
	}

	decryptedCredentials := make(map[string]string)
	for key, value := range encryptedCreds {
		encryptedValue, ok := value.(string)
		if !ok {
			continue
		}

		decrypted, err := s.encryptionService.Decrypt(encryptedValue)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint(fmt.Sprintf("Failed to decrypt credential '%s' from OAuth session", key)).
				Mark(ierr.ErrInternal)
		}
		decryptedCredentials[key] = decrypted
	}

	// Extract non-sensitive metadata
	sessionMetadata := make(map[string]string)
	for key, value := range oauthSessionData {
		// Skip internal keys
		if key == "session_id" || key == "csrf_state" || key == "expires_at" || key == "credentials" || key == "sync_config" || key == "oauth_provider" {
			continue
		}
		if strValue, ok := value.(string); ok {
			sessionMetadata[key] = strValue
		}
	}

	// Extract sync config
	var syncConfig *types.SyncConfig
	if syncConfigValue, ok := oauthSessionData["sync_config"]; ok {
		if sc, ok := syncConfigValue.(*types.SyncConfig); ok {
			syncConfig = sc
		} else if scMap, ok := syncConfigValue.(map[string]interface{}); ok {
			syncConfig = &types.SyncConfig{}

			// Parse invoice sync config
			if invoiceMap, ok := scMap["invoice"].(map[string]interface{}); ok {
				inbound, _ := invoiceMap["inbound"].(bool)
				outbound, _ := invoiceMap["outbound"].(bool)
				syncConfig.Invoice = &types.EntitySyncConfig{
					Inbound:  inbound,
					Outbound: outbound,
				}
			}

			// Parse payment sync config
			if paymentMap, ok := scMap["payment"].(map[string]interface{}); ok {
				inbound, _ := paymentMap["inbound"].(bool)
				outbound, _ := paymentMap["outbound"].(bool)
				syncConfig.Payment = &types.EntitySyncConfig{
					Inbound:  inbound,
					Outbound: outbound,
				}
			}
		}
	}

	// Build session object
	session := &types.OAuthSession{
		SessionID:     sessionID,
		Provider:      provider,
		TenantID:      conn.TenantID,
		EnvironmentID: conn.EnvironmentID,
		Name:          conn.Name,
		Credentials:   decryptedCredentials,
		Metadata:      sessionMetadata,
		SyncConfig:    syncConfig,
		CSRFState:     csrfState,
		ExpiresAt:     expiresAt,
	}

	s.logger.Debugw("retrieved OAuth session from connection",
		"session_id", sessionID,
		"connection_id", conn.ID,
		"provider", session.Provider,
		"tenant_id", session.TenantID)

	return session, nil
}

// DeleteOAuthSession removes an incomplete OAuth connection (cleanup on error)
func (s *oauthService) DeleteOAuthSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil // Nothing to delete
	}

	// Find and delete the connection with this session_id
	filter := &types.ConnectionFilter{
		ProviderType: types.SecretProviderQuickBooks,
	}

	connections, err := s.connectionRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve OAuth session for deletion").
			Mark(ierr.ErrDatabase)
	}

	// Find connection with matching session_id
	for _, c := range connections {
		if c.EncryptedSecretData.QuickBooks != nil && c.EncryptedSecretData.QuickBooks.OAuthSessionData != "" {
			decryptedJSON, err := s.encryptionService.Decrypt(c.EncryptedSecretData.QuickBooks.OAuthSessionData)
			if err != nil {
				continue
			}

			var sessionData map[string]interface{}
			if err := json.Unmarshal([]byte(decryptedJSON), &sessionData); err == nil {
				if storedSessionID, ok := sessionData["session_id"].(string); ok && storedSessionID == sessionID {
					// Delete this connection
					if err := s.connectionRepo.Delete(ctx, c); err != nil {
						return ierr.WithError(err).
							WithHint("Failed to delete OAuth session").
							Mark(ierr.ErrDatabase)
					}

					s.logger.Debugw("deleted OAuth session connection",
						"session_id", sessionID,
						"connection_id", c.ID)

					return nil
				}
			}
		}
	}

	// Session not found, but that's okay
	return nil
}

// GenerateSessionID generates a cryptographically secure random session ID (32 bytes = 64 hex chars)
func (s *oauthService) GenerateSessionID() (string, error) {
	return generateSecureRandomHex(32)
}

// GenerateCSRFState generates a cryptographically secure random CSRF state token (32 bytes = 64 hex chars)
func (s *oauthService) GenerateCSRFState() (string, error) {
	return generateSecureRandomHex(32)
}

// generateSecureRandomHex generates a cryptographically secure random hex string
func generateSecureRandomHex(byteLength int) (string, error) {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to generate secure random token").
			Mark(ierr.ErrInternal)
	}
	return hex.EncodeToString(bytes), nil
}

// BuildOAuthURL builds the provider-specific OAuth authorization URL
func (s *oauthService) BuildOAuthURL(provider types.OAuthProvider, clientID, redirectURI, state string, metadata map[string]string) (string, error) {
	switch provider {
	case types.OAuthProviderQuickBooks:
		params := url.Values{}
		params.Set("client_id", clientID)
		params.Set("redirect_uri", redirectURI)
		params.Set("response_type", "code")
		params.Set("scope", "com.intuit.quickbooks.accounting")
		params.Set("state", state)
		return fmt.Sprintf("https://appcenter.intuit.com/connect/oauth2?%s", params.Encode()), nil

	// Add more providers here:
	// case types.OAuthProviderStripe:
	//     return buildStripeOAuthURL(clientID, redirectURI, state, metadata), nil

	default:
		return "", ierr.NewError(fmt.Sprintf("unsupported OAuth provider: %s", provider)).
			WithHint("Supported providers: quickbooks").
			Mark(ierr.ErrValidation)
	}
}

// ExchangeCodeForConnection exchanges the authorization code for tokens and updates the incomplete connection
func (s *oauthService) ExchangeCodeForConnection(
	ctx context.Context,
	session *types.OAuthSession,
	code, realmID string,
) (string, error) {
	switch session.Provider {
	case types.OAuthProviderQuickBooks:
		// Find the incomplete connection by session_id
		filter := &types.ConnectionFilter{
			ProviderType: types.SecretProviderQuickBooks,
		}

		connections, err := s.connectionRepo.List(ctx, filter)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("Failed to retrieve pending connection").
				Mark(ierr.ErrDatabase)
		}

		// Find connection with matching session_id
		var conn *connection.Connection
		for _, c := range connections {
			if c.EncryptedSecretData.QuickBooks != nil && c.EncryptedSecretData.QuickBooks.OAuthSessionData != "" {
				decryptedJSON, err := s.encryptionService.Decrypt(c.EncryptedSecretData.QuickBooks.OAuthSessionData)
				if err != nil {
					continue
				}

				var sessionData map[string]interface{}
				if err := json.Unmarshal([]byte(decryptedJSON), &sessionData); err == nil {
					if storedSessionID, ok := sessionData["session_id"].(string); ok && storedSessionID == session.SessionID {
						conn = c
						break
					}
				}
			}
		}

		if conn == nil {
			return "", ierr.NewError("OAuth session connection not found").
				WithHint("The OAuth session may have expired or been deleted").
				Mark(ierr.ErrNotFound)
		}

		// Build and encrypt QuickBooks connection metadata
		clientID := session.GetCredential(types.OAuthCredentialClientID)
		clientSecret := session.GetCredential(types.OAuthCredentialClientSecret)
		webhookToken := session.GetCredential(types.OAuthCredentialWebhookVerifierToken) // Extract from credentials, not metadata
		environment := session.GetMetadata(types.OAuthMetadataEnvironment)
		redirectURI := session.GetMetadata(types.OAuthMetadataRedirectURI)
		incomeAccountID := session.GetMetadata(types.OAuthMetadataIncomeAccountID)

		// Encrypt sensitive fields
		encryptedClientID, err := s.encryptionService.Encrypt(clientID)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("Failed to encrypt client ID").
				Mark(ierr.ErrInternal)
		}

		encryptedClientSecret, err := s.encryptionService.Encrypt(clientSecret)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("Failed to encrypt client secret").
				Mark(ierr.ErrInternal)
		}

		encryptedAuthCode, err := s.encryptionService.Encrypt(code)
		if err != nil {
			return "", ierr.WithError(err).
				WithHint("Failed to encrypt auth code").
				Mark(ierr.ErrInternal)
		}

		// Encrypt webhook_verifier_token if provided in credentials (following Chargebee pattern)
		var encryptedWebhookToken string
		if webhookToken != "" {
			encryptedWebhookToken, err = s.encryptionService.Encrypt(webhookToken)
			if err != nil {
				return "", ierr.WithError(err).
					WithHint("Failed to encrypt webhook verifier token").
					Mark(ierr.ErrInternal)
			}
		}

		// Update connection with encrypted credentials (replace OAuth session data)
		conn.EncryptedSecretData = types.ConnectionMetadata{
			QuickBooks: &types.QuickBooksConnectionMetadata{
				ClientID:             encryptedClientID,
				ClientSecret:         encryptedClientSecret,
				RealmID:              realmID,
				Environment:          environment,
				AuthCode:             encryptedAuthCode,
				RedirectURI:          redirectURI,
				IncomeAccountID:      incomeAccountID,
				WebhookVerifierToken: encryptedWebhookToken, // Include webhook token if provided
			},
		}

		// Update sync config if provided
		if session.SyncConfig != nil {
			conn.SyncConfig = session.SyncConfig
		}

		// Clear metadata - everything sensitive should be in encrypted_secret_data
		conn.Metadata = nil
		conn.UpdatedAt = time.Now().UTC()

		// Update in database
		if err := s.connectionRepo.Update(ctx, conn); err != nil {
			return "", ierr.WithError(err).
				WithHint("Failed to update connection with OAuth credentials").
				Mark(ierr.ErrDatabase)
		}

		s.logger.Infow("updated connection with OAuth credentials",
			"connection_id", conn.ID,
			"session_id", session.SessionID,
			"realm_id", realmID)

		// CRITICAL: Exchange auth_code for access_token and refresh_token
		// Use the QuickBooks integration client directly
		qbIntegration, err := s.integrationFactory.GetQuickBooksIntegration(ctx)
		if err != nil {
			// Cleanup: Delete the incomplete connection
			_ = s.connectionRepo.Delete(ctx, conn)
			return "", ierr.WithError(err).
				WithHint("Failed to get QuickBooks integration").
				Mark(ierr.ErrInternal)
		}

		// Exchange auth_code for tokens (this updates the connection in DB)
		if err := qbIntegration.Client.EnsureValidAccessToken(ctx); err != nil {
			// Cleanup: Delete the incomplete connection on token exchange failure
			_ = s.connectionRepo.Delete(ctx, conn)
			return "", ierr.WithError(err).
				WithHint("Failed to exchange authorization code for access tokens. The code may have expired.").
				Mark(ierr.ErrInternal)
		}

		s.logger.Infow("QuickBooks OAuth connection completed successfully",
			"connection_id", conn.ID,
			"realm_id", realmID)

		return conn.ID, nil

	// Add more providers here:
	// case types.OAuthProviderStripe:
	//     return s.exchangeStripeCode(ctx, session, code)

	default:
		return "", ierr.NewError(fmt.Sprintf("unsupported OAuth provider: %s", session.Provider)).
			WithHint("Supported providers: quickbooks").
			Mark(ierr.ErrValidation)
	}
}
