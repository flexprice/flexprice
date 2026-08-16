package middleware

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// PermissionMiddleware handles RBAC permission checks
type PermissionMiddleware struct {
	rbacService *rbac.RBACService
	logger      *logger.Logger
}

// NewPermissionMiddleware creates a new permission middleware instance
func NewPermissionMiddleware(rbacService *rbac.RBACService, logger *logger.Logger) *PermissionMiddleware {
	return &PermissionMiddleware{
		rbacService: rbacService,
		logger:      logger,
	}
}

// PermissionOption declares an extra rule a route requires on top of its
// entity/action pair. Options are resolved once when the route is registered.
// Add an option here rather than a new middleware when a route needs a stricter
// policy, for example a future route limited to a specific role.
type PermissionOption func(*permissionRules)

type permissionRules struct {
	superAdminOnly bool
}

// SuperAdminOnly restricts a route to a person holding super_admin. Use it for
// routes that administer people, such as changing another user's roles, where a
// service account must be refused no matter which roles its key was minted with.
//
// The caller kind is tested with IsServiceAccount rather than by requiring
// "user": a JWT session carries no user type at all (see setContextValues in
// auth.go), so an equality check would reject every dashboard user.
func SuperAdminOnly() PermissionOption {
	return func(r *permissionRules) { r.superAdminOnly = true }
}

// RequirePermission gates a route on the caller's roles for the given entity and
// action, plus any options the route opts into. Checks run in order of how
// fundamental they are, so the caller is told about the problem it must fix
// first: a suspended tenant, then the route's own rules, then plain RBAC.
func (pm *PermissionMiddleware) RequirePermission(entity types.Entity, action types.Action, opts ...PermissionOption) gin.HandlerFunc {
	var rules permissionRules
	for _, opt := range opts {
		opt(&rules)
	}

	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Suspended tenants are blocked from all write operations. Checked first
		// so a suspended tenant hears about the suspension rather than a narrower
		// role problem it cannot act on until the account is restored.
		if action == types.ActionWrite && types.GetTenantInternalStatus(ctx) == types.TenantInternalStatusSuspended {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "tenant account is suspended",
			})
			return
		}

		if rules.superAdminOnly && (types.IsServiceAccount(ctx) || !lo.Contains(types.GetRoles(ctx), types.RoleSuperAdmin.String())) {
			pm.logger.Info(ctx, "access denied: administrative action requires a super_admin user",
				"user_id", types.GetUserID(ctx),
				"tenant_id", types.GetTenantID(ctx),
				"environment_id", types.GetEnvironmentID(ctx),
				"roles", types.GetRoles(ctx),
				"is_service_account", types.IsServiceAccount(ctx),
				"entity", entity,
				"action", action,
				"path", c.Request.URL.Path,
			)
			// Names both requirements, so it reads correctly whichever half fails:
			// a service account fails on "user account" even when its key carries
			// super_admin, and a plain user fails on "Super Admin access".
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "This action requires a user account with Super Admin access",
			})
			return
		}

		roles := types.GetRoles(ctx)
		if !pm.rbacService.HasPermission(roles, string(entity), string(action)) {
			// A caller carrying no roles at all is refused every check, which
			// looks identical to holding the wrong role. Call it out separately:
			// it usually means the principal's roles column is null or empty
			// rather than that the role set is misconfigured.
			if len(roles) == 0 {
				pm.logger.Info(ctx, "access denied: caller has no roles assigned",
					"user_id", types.GetUserID(ctx),
					"tenant_id", types.GetTenantID(ctx),
					"path", c.Request.URL.Path,
				)
			}

			pm.logger.Info(ctx, "access denied due to insufficient RBAC roles",
				"user_id", types.GetUserID(ctx),
				"tenant_id", types.GetTenantID(ctx),
				"environment_id", types.GetEnvironmentID(ctx),
				"roles", roles,
				"entity", entity,
				"action", action,
				"path", c.Request.URL.Path,
			)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"message": "insufficient permissions",
			})
			return
		}

		c.Next()
	}
}
