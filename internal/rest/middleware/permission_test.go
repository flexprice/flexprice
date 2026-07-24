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
