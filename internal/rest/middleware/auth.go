package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// validateAPIKey validates the API key against the config first, then the database.
func validateAPIKey(ctx context.Context, cfg *config.Configuration, secretService service.SecretService, apiKey string) (tenantID, userID, environmentID, userType string, roles []string, valid bool) {
	if apiKey == "" {
		return "", "", "", "", nil, false
	}

	// First check in config
	tenantID, userID, valid = auth.ValidateAPIKey(cfg, apiKey)
	if valid {
		return tenantID, userID, "", "", []string{}, true // Empty roles = full access for config keys
	}

	// If not found in config, check in database
	if secretService != nil {
		secret, err := secretService.VerifyAPIKey(ctx, apiKey)
		if err == nil && secret != nil {
			// Return roles from the secret for RBAC permission checks
			return secret.TenantID, secret.UserID, secret.EnvironmentID, secret.UserType, secret.Roles, true
		}
	}

	return "", "", "", "", nil, false
}

// errEnvironmentUnresolved signals that no environment could be established for
// the caller. Callers must abort the request rather than continue with an empty
// environment ID: every repository filters on environment_id, so an empty value
// silently matches nothing instead of failing.
var errEnvironmentUnresolved = errors.New("environment could not be resolved")

// resolveEnvironmentID determines which environment a request operates on.
//
// Credentials bound to an environment (database-backed API keys) always win —
// such a key must stay pinned and cannot be redirected by a header. Callers that
// carry no bound environment (dashboard JWTs, config-map API keys) select one
// per request via the X-Environment-ID header, which is how the UI switches
// environments without re-minting a token.
//
// A caller-supplied environment is only honoured after confirming it belongs to
// the caller's tenant; environmentRepo.Get filters on tenant_id, so an ID from
// another tenant resolves to not-found and the request is refused.
func resolveEnvironmentID(ctx context.Context, c *gin.Context, environmentRepo domainEnvironment.Repository, boundEnvironmentID, tenantID string) (string, error) {
	if boundEnvironmentID != "" {
		return boundEnvironmentID, nil
	}

	if environmentRepo == nil || tenantID == "" {
		return "", errEnvironmentUnresolved
	}

	if requested := c.GetHeader(types.HeaderEnvironment); requested != "" {
		env, err := environmentRepo.Get(ctx, requested)
		if err != nil {
			// Absence means the environment does not exist or belongs to another
			// tenant, which is a refusal. Anything else is an infrastructure
			// failure and must surface as such rather than as an access denial.
			if ierr.IsNotFound(err) {
				return "", errEnvironmentUnresolved
			}
			return "", err
		}
		if env == nil {
			return "", errEnvironmentUnresolved
		}
		return env.ID, nil
	}

	return defaultEnvironmentID(ctx, environmentRepo)
}

// defaultEnvironmentID picks the environment used when a caller supplies no
// header. Tenants are onboarded with a single development environment, so that
// is both the common case and the safe default: a request that lands here by
// mistake touches the sandbox rather than production data.
//
// The development environment is looked up by type rather than by scanning a
// page of results: it is created at onboarding and is therefore the tenant's
// oldest, so a tenant with more environments than one page holds would not find
// it in the newest N.
func defaultEnvironmentID(ctx context.Context, environmentRepo domainEnvironment.Repository) (string, error) {
	if env, err := environmentRepo.GetDefaultByType(ctx, types.EnvironmentDevelopment); err != nil {
		if !ierr.IsNotFound(err) {
			return "", err
		}
	} else if env != nil {
		return env.ID, nil
	}

	// No development environment: fall back to the newest environment the tenant
	// has. List orders by created_at DESC, so the first entry is the newest.
	environments, err := environmentRepo.List(ctx, types.Filter{Limit: 1})
	if err != nil {
		return "", err
	}
	if len(environments) == 0 {
		return "", errEnvironmentUnresolved
	}

	return environments[0].ID, nil
}

// setContextValues sets the tenant ID, user ID, environment ID, roles, and caller type in the context.
// It reports whether an environment was established for the request.
func setContextValues(c *gin.Context, environmentRepo domainEnvironment.Repository, tenantID, userID, environmentID, userType string, roles []string) error {
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	// Set roles for RBAC permission checks
	if roles != nil {
		ctx = context.WithValue(ctx, types.CtxRoles, roles)
	}

	if userType != "" {
		ctx = context.WithValue(ctx, types.CtxUserType, userType)
	}

	// Environment resolution needs the tenant already in context: the repository
	// scopes its lookups to the tenant taken from ctx.
	resolvedEnvironmentID, err := resolveEnvironmentID(ctx, c, environmentRepo, environmentID, tenantID)
	if err != nil {
		return err
	}

	ctx = context.WithValue(ctx, types.CtxEnvironmentID, resolvedEnvironmentID)
	c.Request = c.Request.WithContext(ctx)
	return nil
}

// abortUnresolvedEnvironment ends a request whose environment could not be
// established.
//
// A refusal and an infrastructure failure are answered differently. Only
// errEnvironmentUnresolved — no such environment, or one belonging to another
// tenant — is an access decision and yields 403; that response deliberately does
// not distinguish the two so it cannot be used to probe for foreign environment
// IDs. Anything else means the lookup itself failed, which is a 500: reporting a
// database outage as an access denial hides the fault and tells the caller not
// to retry something that is in fact retryable.
func abortUnresolvedEnvironment(c *gin.Context, log *logger.Logger, err error, tenantID, userID string) {
	if !errors.Is(err, errEnvironmentUnresolved) {
		log.Error(c.Request.Context(), "environment resolution failed",
			"error", err,
			"tenant_id", tenantID,
			"user_id", userID,
			"requested_environment_id", c.GetHeader(types.HeaderEnvironment),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal server error",
			"message": "Failed to resolve environment",
		})
		c.Abort()
		return
	}

	log.Info(c.Request.Context(), "environment could not be resolved for request",
		"tenant_id", tenantID,
		"user_id", userID,
		"requested_environment_id", c.GetHeader(types.HeaderEnvironment),
	)
	c.JSON(http.StatusForbidden, gin.H{
		"error":   "Access denied",
		"message": "Invalid or inaccessible environment",
	})
	c.Abort()
}

// GuestAuthenticateMiddleware is a middleware that allows requests without authentication
// For now it sets a default tenant ID and user ID in the request context
func GuestAuthenticateMiddleware(c *gin.Context) {
	c.Next()
}

// APIKeyAuthMiddleware is a middleware that only allows requests with valid API keys
func APIKeyAuthMiddleware(cfg *config.Configuration, secretService service.SecretService, environmentRepo domainEnvironment.Repository, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader(cfg.Auth.APIKey.Header)
		tenantID, userID, environmentID, userType, roles, valid := validateAPIKey(c.Request.Context(), cfg, secretService, apiKey)
		if !valid {
			logger.Debug(c.Request.Context(), "invalid api key")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		if err := setContextValues(c, environmentRepo, tenantID, userID, environmentID, userType, roles); err != nil {
			abortUnresolvedEnvironment(c, logger, err, tenantID, userID)
			return
		}
		c.Next()
	}
}

// AuthenticateMiddleware is a middleware that authenticates requests based on either:
// 1. JWT token in the Authorization header as a Bearer token
// 2. API key in the x-api-key header (or configured header name)
func AuthenticateMiddleware(cfg *config.Configuration, secretService service.SecretService, environmentRepo domainEnvironment.Repository, logger *logger.Logger) gin.HandlerFunc {
	authProvider := auth.NewProvider(cfg)

	return func(c *gin.Context) {
		// First check for API key
		apiKey := c.GetHeader(cfg.Auth.APIKey.Header)
		tenantID, userID, environmentID, userType, roles, valid := validateAPIKey(c.Request.Context(), cfg, secretService, apiKey)
		if valid {
			if err := setContextValues(c, environmentRepo, tenantID, userID, environmentID, userType, roles); err != nil {
				abortUnresolvedEnvironment(c, logger, err, tenantID, userID)
				return
			}
			c.Next()
			return
		}

		// If no API key, check for JWT token
		authHeader := c.GetHeader(types.HeaderAuthorization)
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Check if the authorization header is in the correct format
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := authProvider.ValidateToken(c.Request.Context(), tokenString)
		if err != nil {
			logger.Error(c.Request.Context(), "failed to validate token", "error", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if claims == nil || claims.UserID == "" || claims.TenantID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		// JWT users have empty roles = full access
		if err := setContextValues(c, environmentRepo, claims.TenantID, claims.UserID, claims.EnvironmentID, "", []string{}); err != nil {
			abortUnresolvedEnvironment(c, logger, err, claims.TenantID, claims.UserID)
			return
		}
		c.Next()
	}
}

// SessionTokenAuthMiddleware validates session JWT tokens
// It extracts the dashboard-specific claims and sets them in the context
func SessionTokenAuthMiddleware(cfg *config.Configuration, logger *logger.Logger) gin.HandlerFunc {
	authProvider := auth.NewFlexpriceAuth(cfg)
	return func(c *gin.Context) {
		authHeader := c.GetHeader(types.HeaderSessionToken)
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Session token required"})
			c.Abort()
			return
		}

		claims, err := authProvider.ValidateSessionToken(c.Request.Context(), authHeader)
		if err != nil {
			logger.Error(c.Request.Context(), "failed to validate session token", "error", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired session token"})
			c.Abort()
			return
		}

		// Set session-specific context values
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CtxTenantID, claims.TenantID)
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, claims.EnvironmentID)
		ctx = context.WithValue(ctx, types.CtxCustomerID, claims.CustomerID)
		ctx = context.WithValue(ctx, types.CtxExternalCustomerID, claims.ExternalCustomerID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
