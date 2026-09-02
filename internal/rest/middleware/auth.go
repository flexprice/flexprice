package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
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
		// Config keys are operator-provisioned and carry no database record to
		// hold roles, so they are granted full access explicitly.
		return tenantID, userID, "", "", []string{types.RoleSuperAdmin.String()}, true
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

	requested := c.GetHeader(types.HeaderEnvironment)
	if requested == "" {
		// No environment to operate on. Discovery routes are the exception:
		// they are how a caller learns which environments exist, so requiring a
		// selection to reach them would be circular.
		if isEnvironmentDiscoveryRoute(c) {
			return "", nil
		}
		return "", errEnvironmentUnresolved
	}

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

// environmentDiscoveryRoutes are the routes a caller can reach before it has
// selected an environment: identity, environment discovery, and tenant. These
// are how an unbound caller -- a dashboard JWT, which carries no environment
// claim -- learns an ID to send in X-Environment-ID on every later request, so
// requiring a selection to reach them would be circular.
//
// The exemption is per route and method rather than per group. A handler
// reached this way runs with no environment in context, so any route that
// mutates a specific environment must not be listed: without a selection there
// is nothing for the middleware to authorise the target against, and the
// handler would act on a path parameter no one checked. Environment writes
// (PUT /:id, POST /:id/clone) are therefore absent and must carry a header.
var environmentDiscoveryRoutes = map[string]map[string]struct{}{
	"/v1/users/me": {
		http.MethodGet: {},
		http.MethodPut: {},
	},
	"/v1/users": {
		http.MethodPost: {},
	},
	// User admin is tenant-scoped, not environment-scoped: it acts on
	// users/service-accounts/roles, never on an environment, so an unbound
	// dashboard JWT must reach it during bootstrap like the other user routes.
	"/v1/users/:id": {
		http.MethodPut:    {},
		http.MethodDelete: {},
	},
	"/v1/users/:id/roles": {
		http.MethodPut: {},
	},
	"/v1/users/search": {
		http.MethodPost: {},
	},
	"/v1/environments": {
		http.MethodGet:  {},
		http.MethodPost: {},
	},
	"/v1/environments/:id": {
		http.MethodGet: {},
	},
	"/v1/tenants/update": {
		http.MethodPut: {},
	},
	"/v1/tenants/:id": {
		http.MethodGet: {},
	},
	"/v1/tenants/billing": {
		http.MethodGet: {},
	},
}

// isEnvironmentDiscoveryRoute reports whether a route can be served without an
// environment selected.
func isEnvironmentDiscoveryRoute(c *gin.Context) bool {
	// FullPath is the matched route template, so this cannot be spoofed by a
	// crafted URL the router did not match to one of these handlers. An
	// unmatched request yields "", which the map below does not contain.
	path := c.FullPath()
	if path == "" {
		return false
	}

	methods, ok := environmentDiscoveryRoutes[path]
	if !ok {
		return false
	}

	_, ok = methods[c.Request.Method]
	return ok
}

// setContextValues sets the tenant ID, user ID, environment ID, roles, and
// caller type in the context. It returns errEnvironmentUnresolved when no
// environment could be established for the caller, and the underlying error
// when the lookup itself failed; callers pass the error to
// abortEnvironmentResolution, which distinguishes the two.
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

	// Only discovery routes resolve to an empty environment, and they do not read
	// it. Leaving the key unset rather than storing "" keeps GetEnvironmentID's
	// zero value meaning "no environment" for anything that does read it.
	if resolvedEnvironmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, resolvedEnvironmentID)
	}
	c.Request = c.Request.WithContext(ctx)
	return nil
}

// abortEnvironmentResolution ends a request whose environment could not be
// established, answering according to why.
//
// Only errEnvironmentUnresolved — no such environment, or one belonging to
// another tenant — is an access decision and yields 403; that response
// deliberately does not distinguish the two so it cannot be used to probe for
// foreign environment IDs. Anything else means the lookup itself failed, which
// is a 500: reporting a database outage as an access denial hides the fault and
// tells the caller not to retry something that is in fact retryable.
func abortEnvironmentResolution(c *gin.Context, log *logger.Logger, err error, tenantID, userID string) {
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
			abortEnvironmentResolution(c, logger, err, tenantID, userID)
			return
		}
		c.Next()
	}
}

// AuthenticateMiddleware is a middleware that authenticates requests based on either:
// 1. JWT token in the Authorization header as a Bearer token
// 2. API key in the x-api-key header (or configured header name)
func AuthenticateMiddleware(cfg *config.Configuration, secretService service.SecretService, environmentRepo domainEnvironment.Repository, userRepo domainUser.Repository, logger *logger.Logger) gin.HandlerFunc {
	authProvider := auth.NewProvider(cfg)

	// SSO tokens are validated separately from password-login tokens because the
	// provider handling password login cannot necessarily validate them: a
	// deployment using Supabase validates against the Supabase claim schema,
	// while a SAML login mints Flexprice claims for a user that has no Supabase
	// account, so every request after a successful SSO login came back 401.
	//
	// This validator is strictly stricter than the ordinary one — it additionally
	// requires SAML to be enabled on the deployment and the token to carry the
	// SSO marker — so routing a token to it can never be an advantage.
	ssoValidator := auth.NewSSOTokenValidator(cfg)

	return func(c *gin.Context) {
		// First check for API key
		apiKey := c.GetHeader(cfg.Auth.APIKey.Header)
		tenantID, userID, environmentID, userType, roles, valid := validateAPIKey(c.Request.Context(), cfg, secretService, apiKey)
		if valid {
			if err := setContextValues(c, environmentRepo, tenantID, userID, environmentID, userType, roles); err != nil {
				abortEnvironmentResolution(c, logger, err, tenantID, userID)
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

		// Which validator to run is decided by an unverified claim, which is safe
		// because it decides only that — being sent to the SSO validator is not an
		// advantage, since that validator verifies the same signature and then
		// requires the marker again along with SAML being enabled. A caller
		// setting the marker on a token the ordinary provider would have accepted
		// only causes their own token to be refused.
		validate := authProvider.ValidateToken
		if auth.IsSSOToken(tokenString) {
			validate = ssoValidator.Validate
		}

		claims, err := validate(c.Request.Context(), tokenString)
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

		// A JWT carries no roles claim, so the user record is the only source
		// of the session's permissions. The lookup is scoped to the tenant the
		// token names, since the repository takes it from the context.
		tenantCtx := types.SetTenantID(c.Request.Context(), claims.TenantID)
		user, err := userRepo.GetByID(tenantCtx, claims.UserID)
		if err != nil {
			if ierr.IsNotFound(err) {
				logger.Info(c.Request.Context(), "rejecting token for a user that no longer exists",
					"user_id", claims.UserID,
					"tenant_id", claims.TenantID,
				)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
				c.Abort()
				return
			}
			logger.Error(c.Request.Context(), "failed to load user roles", "error", err, "user_id", claims.UserID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
			c.Abort()
			return
		}

		// A token outlives the account it was issued for, so an archived user
		// must be refused here rather than on the strength of the token alone.
		// The response does not distinguish this from a missing user: telling an
		// unauthenticated caller which accounts exist is an enumeration oracle.
		if user.Status != types.StatusPublished {
			logger.Info(c.Request.Context(), "rejecting token for an inactive user",
				"user_id", claims.UserID,
				"tenant_id", claims.TenantID,
				"status", user.Status,
			)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		if err := setContextValues(c, environmentRepo, claims.TenantID, claims.UserID, claims.EnvironmentID, "", user.Roles); err != nil {
			abortEnvironmentResolution(c, logger, err, claims.TenantID, claims.UserID)
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
