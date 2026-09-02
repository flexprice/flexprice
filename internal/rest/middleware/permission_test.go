package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realRBACService loads the actual production roles.json config so these
// tests exercise the real role -> permission mapping, matching the live
// staging PoC (event_ingestor/event_reader hold no customer:write grant).
func realRBACService(t *testing.T) *rbac.RBACService {
	t.Helper()
	svc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: "../../config/rbac/roles.json"},
	})
	require.NoError(t, err)
	return svc
}

// newPermissionTestRouter seeds userType/roles into context (simulating
// AuthenticateMiddleware's setContextValues) and gates POST /customers
// behind RequirePermission(entity, action).
func newPermissionTestRouter(t *testing.T, rbacSvc *rbac.RBACService, userType string, roles []string, entity types.Entity, action types.Action) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pm := NewPermissionMiddleware(rbacSvc, newTestLogger(t))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if userType != "" {
			ctx = context.WithValue(ctx, types.CtxUserType, userType)
		}
		if roles != nil {
			ctx = context.WithValue(ctx, types.CtxRoles, roles)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.POST("/customers", pm.RequirePermission(entity, action), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"created": true})
	})

	return r
}

// superAdminRolesJSON gives all_writer real write permission on every entity, so
// a caller rejected under SuperAdminOnly is provably rejected by that option
// rather than by the underlying RBAC check.
const superAdminRolesJSON = `{
	"super_admin": {
		"name": "Super Admin",
		"permissions": { "*": ["*"] }
	},
	"all_writer": {
		"name": "All Writer",
		"permissions": { "*": ["read", "write"] }
	},
	"event_ingestor": {
		"name": "Event Ingestor",
		"permissions": { "event": ["write"] }
	}
}`

// newSuperAdminRouter gates PUT /users/:id/roles with the SuperAdminOnly option
// and runs it behind TenantStatusMiddleware, mirroring the real route.
func newSuperAdminRouter(t *testing.T, tenantStatus types.TenantInternalStatus, userType string, roles []string) *gin.Engine {
	t.Helper()
	log := newTestLogger(t)
	pm := NewPermissionMiddleware(newRBACServiceWithRoles(t, superAdminRolesJSON), log)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CtxTenantID, "tenant-rbac")
		ctx = context.WithValue(ctx, types.CtxUserID, "user-1")
		if userType != "" {
			ctx = context.WithValue(ctx, types.CtxUserType, userType)
		}
		if roles != nil {
			ctx = context.WithValue(ctx, types.CtxRoles, roles)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})

	r.Use(TenantStatusMiddleware(&mockTenantService{status: tenantStatus}, log))
	r.PUT("/users/:id/roles",
		pm.RequirePermission(types.EntityUser, types.ActionWrite, SuperAdminOnly()),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) },
	)

	return r
}

func TestRequirePermission_SuperAdminOnly(t *testing.T) {
	const wantAdminMsg = "This action requires a user account with Super Admin access"

	testCases := []struct {
		name         string
		userType     string
		roles        []string
		tenantStatus types.TenantInternalStatus
		wantCode     int
		wantErrMsg   string
	}{
		{
			// A dashboard session carries no user type at all, so this is the
			// ordinary success path and would break under a userType == "user" check.
			name:         "super_admin JWT user with no user type is allowed",
			userType:     "",
			roles:        []string{types.RoleSuperAdmin.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusOK,
		},
		{
			name:         "super_admin user is allowed",
			userType:     string(types.UserTypeUser),
			roles:        []string{types.RoleSuperAdmin.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusOK,
		},
		{
			// all_writer passes the entity/action check, so only SuperAdminOnly
			// can be what refuses it here.
			name:         "user with write access but not super_admin is denied",
			userType:     string(types.UserTypeUser),
			roles:        []string{types.RoleAllWriter.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   wantAdminMsg,
		},
		{
			// The key carries super_admin, yet administering people stays closed
			// to machine credentials.
			name:         "service account holding super_admin is denied",
			userType:     string(types.UserTypeServiceAccount),
			roles:        []string{types.RoleSuperAdmin.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   wantAdminMsg,
		},
		{
			name:         "service account without super_admin is denied",
			userType:     string(types.UserTypeServiceAccount),
			roles:        []string{types.RoleEventIngestor.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   wantAdminMsg,
		},
		{
			name:         "caller with no roles is denied",
			userType:     string(types.UserTypeUser),
			roles:        nil,
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   wantAdminMsg,
		},
		{
			name:         "caller with an empty role set is denied",
			userType:     string(types.UserTypeUser),
			roles:        []string{},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   wantAdminMsg,
		},
		{
			name:         "super_admin in a suspended tenant is denied",
			userType:     string(types.UserTypeUser),
			roles:        []string{types.RoleSuperAdmin.String()},
			tenantStatus: types.TenantInternalStatusSuspended,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "tenant account is suspended",
		},
		{
			// Suspension is the more fundamental problem and nothing the caller
			// can fix with a role change, so it must win over the admin guard.
			name:         "suspension is reported ahead of the super_admin requirement",
			userType:     string(types.UserTypeUser),
			roles:        []string{types.RoleAllWriter.String()},
			tenantStatus: types.TenantInternalStatusSuspended,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "tenant account is suspended",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := newSuperAdminRouter(t, tc.tenantStatus, tc.userType, tc.roles)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPut, "/users/user-2/roles", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.wantCode, w.Code)

			if tc.wantErrMsg != "" {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tc.wantErrMsg, body["message"])
			}
		})
	}
}

// Without the super_admin guard the same route admits any caller with entity write access,
// which is the escalation SuperAdminOnly exists to close.
func TestRequirePermission_PlainCheckAllowsWriter(t *testing.T) {
	router := newPermissionTestRouter(t, newRBACServiceWithRoles(t, superAdminRolesJSON),
		string(types.UserTypeUser), []string{types.RoleAllWriter.String()},
		types.EntityUser, types.ActionWrite)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/customers", nil))

	assert.Equal(t, http.StatusOK, w.Code, "all_writer passes a plain entity/action check")
}

// TestRequirePermission_DeniesServiceAccountWithoutRole reproduces the
// live-verified staging PoC: a service-account key scoped to
// event_ingestor/event_reader (neither of which grants customer:write)
// successfully called POST /v1/customers and got 201. That must now be 403.
func TestRequirePermission_DeniesServiceAccountWithoutRole(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPermissionTestRouter(t, rbacSvc, string(types.UserTypeServiceAccount),
		[]string{"event_ingestor", "event_reader"}, types.EntityCustomer, types.ActionWrite)

	req := httptest.NewRequest(http.MethodPost, "/customers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "service account without customer:write role must be denied")

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "insufficient permissions", body["message"])
}

// TestRequirePermission_AllowsServiceAccountWithRole confirms the allow path
// (previously never broken) still works after the fix. super_admin holds
// wildcard "*": ["*"] permissions in the real roles.json.
func TestRequirePermission_AllowsServiceAccountWithRole(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPermissionTestRouter(t, rbacSvc, string(types.UserTypeServiceAccount),
		[]string{"super_admin"}, types.EntityCustomer, types.ActionWrite)

	req := httptest.NewRequest(http.MethodPost, "/customers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "service account with the right role must be allowed through")
}

// newPortalSessionTestRouter mirrors the real route shape of
// GET /v1/customers/portal/:external_id, which is gated on customer:write
// because minting a portal session is an impersonation-style action.
func newPortalSessionTestRouter(t *testing.T, rbacSvc *rbac.RBACService, userType string, roles []string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pm := NewPermissionMiddleware(rbacSvc, newTestLogger(t))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if userType != "" {
			ctx = context.WithValue(ctx, types.CtxUserType, userType)
		}
		if roles != nil {
			ctx = context.WithValue(ctx, types.CtxRoles, roles)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.GET("/customers/portal/:external_id",
		pm.RequirePermission(types.EntityCustomer, types.ActionWrite),
		func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"token": "portal-jwt"})
		})

	return r
}

// TestPortalSession_DeniesReadOnlyPrincipal pins the authorization-bypass fix:
// GET /v1/customers/portal/:external_id previously carried no permission
// middleware, so any authenticated principal in the tenant could mint a portal
// session for another customer by supplying that customer's external_id. A
// read-only principal must be refused before any token is signed.
func TestPortalSession_DeniesReadOnlyPrincipal(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPortalSessionTestRouter(t, rbacSvc, string(types.UserTypeUser),
		[]string{types.RoleAllReader.String()})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/customers/portal/victim-ext", nil))

	assert.Equal(t, http.StatusForbidden, w.Code, "a read-only principal must not mint a portal session")

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "insufficient permissions", body["message"])
	assert.NotContains(t, w.Body.String(), "portal-jwt", "no session token may be signed on the denied path")
}

// TestPortalSession_DeniesServiceAccountWithoutRole covers the API-key caller:
// an event-scoped key holds neither customer:write nor any wildcard grant.
func TestPortalSession_DeniesServiceAccountWithoutRole(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPortalSessionTestRouter(t, rbacSvc, string(types.UserTypeServiceAccount),
		[]string{"event_ingestor", "event_reader"})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/customers/portal/victim-ext", nil))

	assert.Equal(t, http.StatusForbidden, w.Code, "event-scoped service account must not mint a portal session")
}

// TestPortalSession_AllowsWriter confirms the legitimate path still works:
// a principal holding customer write access can still create a portal session.
func TestPortalSession_AllowsWriter(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPortalSessionTestRouter(t, rbacSvc, string(types.UserTypeUser),
		[]string{types.RoleAllWriter.String()})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/customers/portal/victim-ext", nil))

	assert.Equal(t, http.StatusOK, w.Code, "a writer principal must still be able to create a portal session")
	assert.Contains(t, w.Body.String(), "portal-jwt", "the session token must be returned on the allowed path")
}

// TestPortalSession_AllowsSuperAdmin confirms a super_admin principal (wildcard
// grant in the real roles.json) can also create a portal session.
func TestPortalSession_AllowsSuperAdmin(t *testing.T) {
	rbacSvc := realRBACService(t)
	router := newPortalSessionTestRouter(t, rbacSvc, string(types.UserTypeUser),
		[]string{types.RoleSuperAdmin.String()})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/customers/portal/victim-ext", nil))

	assert.Equal(t, http.StatusOK, w.Code, "a super_admin principal must be able to create a portal session")
	assert.Contains(t, w.Body.String(), "portal-jwt", "the session token must be returned on the allowed path")
}
