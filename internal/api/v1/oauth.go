package v1

import (
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// OAuthHandler handles generic OAuth endpoints for multiple providers
type OAuthHandler struct {
	oauthService service.OAuthService
	redirectURI  string
	logger       *logger.Logger
}

// NewOAuthHandler creates a new generic OAuth handler
func NewOAuthHandler(
	oauthService service.OAuthService,
	redirectURI string,
	logger *logger.Logger,
) *OAuthHandler {
	return &OAuthHandler{
		oauthService: oauthService,
		redirectURI:  redirectURI,
		logger:       logger,
	}
}

// InitiateOAuth initiates the OAuth flow for any supported provider
// POST /v1/oauth/init
func (h *OAuthHandler) InitiateOAuth(c *gin.Context) {
	var req dto.InitiateOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format. Check required fields: provider, name, credentials").
			Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()

	// Validate request (provider-specific validation) - fail fast
	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Get tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Validate provider
	provider := types.OAuthProvider(req.Provider)

	// Generate secure random tokens
	sessionID, err := h.oauthService.GenerateSessionID()
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to generate OAuth session ID").
			Mark(ierr.ErrInternal))
		return
	}

	csrfState, err := h.oauthService.GenerateCSRFState()
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Failed to generate CSRF state token").
			Mark(ierr.ErrInternal))
		return
	}

	// Add redirect_uri to metadata (will be used during token exchange)
	if req.Metadata == nil {
		req.Metadata = make(map[string]string)
	}
	req.Metadata[types.OAuthMetadataRedirectURI] = h.redirectURI

	// Create OAuth session
	session := &types.OAuthSession{
		SessionID:     sessionID,
		Provider:      provider,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		Name:          req.Name,
		Credentials:   req.Credentials, // Will be encrypted by service
		Metadata:      req.Metadata,
		SyncConfig:    req.SyncConfig, // Pass through the sync configuration
		CSRFState:     csrfState,
		ExpiresAt:     time.Now().Add(5 * time.Minute),
	}

	// Store session in cache (credentials will be encrypted)
	if err := h.oauthService.StoreOAuthSession(ctx, session); err != nil {
		c.Error(err)
		return
	}

	// Build provider-specific OAuth URL
	clientID := req.Credentials[types.OAuthCredentialClientID]
	oauthURL, err := h.oauthService.BuildOAuthURL(provider, clientID, h.redirectURI, csrfState, req.Metadata)
	if err != nil {
		c.Error(err)
		return
	}

	h.logger.Infow("initiated OAuth flow",
		"session_id", sessionID,
		"provider", provider,
		"tenant_id", tenantID)

	c.JSON(http.StatusOK, dto.InitiateOAuthResponse{
		OAuthURL:  oauthURL,
		SessionID: sessionID,
	})
}

// CompleteOAuth completes the OAuth flow for any supported provider
// POST /v1/oauth/complete
func (h *OAuthHandler) CompleteOAuth(c *gin.Context) {
	var req dto.CompleteOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format. Required fields: provider, session_id, code, state, realm_id").
			Mark(ierr.ErrValidation))
		return
	}

	ctx := c.Request.Context()

	// Validate request (provider-specific validation) - fail fast
	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Validate provider
	provider := types.OAuthProvider(req.Provider)

	// Retrieve OAuth session from cache
	session, err := h.oauthService.GetOAuthSession(ctx, req.SessionID)
	if err != nil {
		c.Error(err)
		return
	}

	// Validate provider matches session
	if session.Provider != provider {
		c.Error(ierr.NewError("provider mismatch").
			WithHint("The provider in the request does not match the session provider").
			Mark(ierr.ErrValidation))
		return
	}

	// Validate CSRF state (constant-time comparison to prevent timing attacks)
	if session.CSRFState != req.State {
		h.logger.Warnw("CSRF state mismatch detected",
			"session_id", req.SessionID,
			"provider", session.Provider,
			"expected_state_length", len(session.CSRFState),
			"provided_state_length", len(req.State))

		// Delete session on CSRF failure (security measure)
		_ = h.oauthService.DeleteOAuthSession(ctx, req.SessionID)

		c.Error(ierr.NewError("CSRF state validation failed").
			WithHint("The OAuth callback state parameter is invalid. This may indicate a CSRF attack or an expired session.").
			Mark(ierr.ErrValidation))
		return
	}

	h.logger.Debugw("CSRF state validated successfully",
		"session_id", req.SessionID,
		"provider", session.Provider)

	// Exchange code for connection using provider-specific logic
	connectionID, err := h.oauthService.ExchangeCodeForConnection(ctx, session, req.Code, req.RealmID)
	if err != nil {
		// Delete session on connection creation failure
		_ = h.oauthService.DeleteOAuthSession(ctx, req.SessionID) // Ignore cleanup errors, main error is connection creation
		c.Error(err)
		return
	}

	// Delete OAuth session from cache (cleanup - success case)
	if err := h.oauthService.DeleteOAuthSession(ctx, req.SessionID); err != nil {
		// Log but don't fail - connection was created successfully
		h.logger.Warnw("failed to delete OAuth session after successful connection",
			"session_id", req.SessionID,
			"provider", provider,
			"connection_id", connectionID,
			"error", err)
	}

	h.logger.Infow("completed OAuth flow",
		"session_id", req.SessionID,
		"provider", provider,
		"connection_id", connectionID)

	c.JSON(http.StatusOK, dto.CompleteOAuthResponse{
		Success:      true,
		ConnectionID: connectionID,
	})
}
