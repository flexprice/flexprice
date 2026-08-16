package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	dto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// mockTenantService implements service.TenantService for tests.
// Only GetTenantInternalStatus is exercised; all other methods are stubs.
type mockTenantService struct {
	status types.TenantInternalStatus
	err    error
}

func (m *mockTenantService) GetTenantInternalStatus(_ context.Context, _ string) (types.TenantInternalStatus, error) {
	return m.status, m.err
}
func (m *mockTenantService) CreateTenant(_ context.Context, _ dto.CreateTenantRequest) (*dto.TenantResponse, error) {
	return nil, nil
}
func (m *mockTenantService) GetTenantByID(_ context.Context, _ string) (*dto.TenantResponse, error) {
	return nil, nil
}
func (m *mockTenantService) AssignTenantToUser(_ context.Context, _ dto.AssignTenantRequest) error {
	return nil
}
func (m *mockTenantService) GetAllTenants(_ context.Context) ([]*dto.TenantResponse, error) {
	return nil, nil
}
func (m *mockTenantService) UpdateTenant(_ context.Context, _ string, _ dto.UpdateTenantRequest) (*dto.TenantResponse, error) {
	return nil, nil
}
func (m *mockTenantService) GetBillingUsage(_ context.Context) (*dto.TenantBillingUsage, error) {
	return nil, nil
}
func (m *mockTenantService) CreateTenantAsBillingCustomer(_ context.Context, _ *domainTenant.Tenant) error {
	return nil
}

var _ service.TenantService = (*mockTenantService)(nil)

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: "info"},
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

// newTestRouter builds a minimal Gin router that:
//  1. Seeds tenantID into the request context (simulating AuthenticateMiddleware)
//  2. Runs TenantStatusMiddleware
//  3. Has a read route GET /test (no permission check) and a write route POST /test
//     (gated by RequirePermission, which also enforces tenant suspension)
func newTestRouter(t *testing.T, tenantID string, svc *mockTenantService) *gin.Engine {
	t.Helper()

	log := newTestLogger(t)
	permMW := NewPermissionMiddleware(newPermissiveRBACService(t), log)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate AuthenticateMiddleware seeding tenantID and the caller's roles.
	// The roles grant everything so that a refusal here can only come from the
	// tenant's suspended status, which is what these tests are about.
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.CtxRoles, []string{types.RoleSuperAdmin.String()})
		if tenantID != "" {
			ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})

	r.Use(TenantStatusMiddleware(svc, log))

	statusHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"internal_status": string(types.GetTenantInternalStatus(c.Request.Context())),
		})
	}

	// Read route — no permission gate
	r.GET("/test", statusHandler)

	// Write route — gated by RequirePermission, which checks tenant suspension
	r.POST("/test", permMW.RequirePermission("entity", "write"), statusHandler)

	return r
}

// newPermissiveRBACService returns a real *rbac.RBACService whose only role
// grants every entity and action, so tests pairing it with a super_admin caller
// focus purely on tenant suspension rather than RBAC rules.
func newPermissiveRBACService(t *testing.T) *rbac.RBACService {
	t.Helper()
	return newRBACServiceWithRoles(t, `{
		"super_admin": {
			"name": "Super Admin",
			"permissions": { "*": ["*"] }
		}
	}`)
}

// newRBACServiceWithRoles creates a real *rbac.RBACService from a raw JSON roles definition.
func newRBACServiceWithRoles(t *testing.T, rolesJSON string) *rbac.RBACService {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "roles-*.json")
	if err != nil {
		t.Fatalf("create temp roles file: %v", err)
	}
	if _, err := f.WriteString(rolesJSON); err != nil {
		t.Fatalf("write temp roles file: %v", err)
	}
	f.Close()

	svc, err := rbac.NewRBACService(&config.Configuration{
		RBAC: config.RBACConfig{RolesConfigPath: f.Name()},
	})
	if err != nil {
		t.Fatalf("create RBACService: %v", err)
	}
	return svc
}

// newRBACRouter builds a router that seeds the given userType and roles into context,
// runs TenantStatusMiddleware, then gates POST /test behind RequirePermission.
func newRBACRouter(t *testing.T, rbacSvc *rbac.RBACService, tenantStatus types.TenantInternalStatus, userType string, roles []string) *gin.Engine {
	t.Helper()
	log := newTestLogger(t)
	permMW := NewPermissionMiddleware(rbacSvc, log)
	svc := &mockTenantService{status: tenantStatus}

	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CtxTenantID, "tenant-rbac")
		if userType != "" {
			ctx = context.WithValue(ctx, types.CtxUserType, userType)
		}
		if roles != nil {
			ctx = context.WithValue(ctx, types.CtxRoles, roles)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})

	r.Use(TenantStatusMiddleware(svc, log))
	r.POST("/test", permMW.RequirePermission("event", "write"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return r
}

func TestTenantStatusMiddleware(t *testing.T) {
	testCases := []struct {
		name               string
		method             string
		tenantID           string
		status             types.TenantInternalStatus
		svcErr             error
		wantCode           int
		wantInternalStatus string
		wantErrMsg         string
	}{
		{
			name:               "stamps active status onto context",
			method:             http.MethodGet,
			tenantID:           "tenant-1",
			status:             types.TenantInternalStatusActive,
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:               "stamps trialing status onto context",
			method:             http.MethodGet,
			tenantID:           "tenant-2",
			status:             types.TenantInternalStatusTrialing,
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusTrialing),
		},
		{
			name:               "suspended tenant read passes through",
			method:             http.MethodGet,
			tenantID:           "tenant-3",
			status:             types.TenantInternalStatusSuspended,
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusSuspended),
		},
		{
			name:       "suspended tenant write is blocked by RequirePermission",
			method:     http.MethodPost,
			tenantID:   "tenant-3",
			status:     types.TenantInternalStatusSuspended,
			wantCode:   http.StatusForbidden,
			wantErrMsg: "tenant account is suspended",
		},
		{
			name:               "active tenant write passes through",
			method:             http.MethodPost,
			tenantID:           "tenant-4",
			status:             types.TenantInternalStatusActive,
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:               "trialing tenant write passes through",
			method:             http.MethodPost,
			tenantID:           "tenant-5",
			status:             types.TenantInternalStatusTrialing,
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusTrialing),
		},
		{
			name:       "service error returns 500",
			method:     http.MethodGet,
			tenantID:   "tenant-6",
			svcErr:     errors.New("db unavailable"),
			wantCode:   http.StatusInternalServerError,
			wantErrMsg: "failed to verify tenant access",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockTenantService{status: tc.status, err: tc.svcErr}
			router := newTestRouter(t, tc.tenantID, svc)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tc.method, "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.wantCode, w.Code)

			if tc.wantInternalStatus != "" {
				var body map[string]string
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tc.wantInternalStatus, body["internal_status"])
			}

			if tc.wantErrMsg != "" {
				var body map[string]string
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tc.wantErrMsg, body["message"])
			}
		})
	}
}

// rolesJSON with a wildcard super_admin role, an event_ingestor role that
// allows event writes, and a read-only reader role. reader is defined so that a
// denial for a reader is caused by the role lacking the grant rather than by the
// role being unknown.
const testRolesJSON = `{
	"super_admin": {
		"name": "Super Admin",
		"permissions": { "*": ["*"] }
	},
	"event_ingestor": {
		"name": "Event Ingestor",
		"permissions": { "event": ["write"] }
	},
	"reader": {
		"name": "Reader",
		"permissions": { "*": ["read"] }
	}
}`

// RBAC applies to every caller type, not just service accounts, so the cases
// below are organised by the roles a caller holds rather than by how it
// authenticated.
func TestRequirePermission_RBAC(t *testing.T) {
	testCases := []struct {
		name         string
		userType     string
		roles        []string
		tenantStatus types.TenantInternalStatus
		wantCode     int
		wantErrMsg   string
	}{
		{
			name:         "service account with required role is allowed",
			userType:     string(types.UserTypeServiceAccount),
			roles:        []string{"event_ingestor"},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusOK,
		},
		{
			name:         "service account without required role is denied",
			userType:     string(types.UserTypeServiceAccount),
			roles:        []string{"some_other_role"},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "insufficient permissions",
		},
		// Every caller type is now subject to RBAC. A caller carrying no roles
		// holds no grant and is refused, where it previously fell through to
		// full access.
		{
			name:         "caller with no roles at all is denied",
			userType:     "",
			roles:        nil,
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "insufficient permissions",
		},
		{
			name:         "caller with an empty role set is denied",
			userType:     "",
			roles:        []string{},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "insufficient permissions",
		},
		{
			name:         "user with the required role is allowed",
			userType:     string(types.UserTypeUser),
			roles:        []string{"event_ingestor"},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusOK,
		},
		{
			name:         "user without the required role is denied",
			userType:     string(types.UserTypeUser),
			roles:        []string{types.RoleAllReader.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "insufficient permissions",
		},
		{
			name:         "config API key carries super_admin and is allowed",
			userType:     "",
			roles:        []string{types.RoleSuperAdmin.String()},
			tenantStatus: types.TenantInternalStatusActive,
			wantCode:     http.StatusOK,
		},
		{
			name:         "suspended tenant blocks service account with valid role",
			userType:     string(types.UserTypeServiceAccount),
			roles:        []string{"event_ingestor"},
			tenantStatus: types.TenantInternalStatusSuspended,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "tenant account is suspended",
		},
		{
			name:         "suspended tenant blocks JWT user",
			userType:     "",
			roles:        nil,
			tenantStatus: types.TenantInternalStatusSuspended,
			wantCode:     http.StatusForbidden,
			wantErrMsg:   "tenant account is suspended",
		},
	}

	rbacSvc := newRBACServiceWithRoles(t, testRolesJSON)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := newRBACRouter(t, rbacSvc, tc.tenantStatus, tc.userType, tc.roles)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodPost, "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.wantCode, w.Code)

			if tc.wantErrMsg != "" {
				var body map[string]string
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tc.wantErrMsg, body["message"])
			}
		})
	}
}

func TestIsServiceAccount(t *testing.T) {
	testCases := []struct {
		name     string
		userType string
		want     bool
	}{
		{"service_account type returns true", string(types.UserTypeServiceAccount), true},
		{"user type returns false", string(types.UserTypeUser), false},
		{"empty string returns false", "", false},
		{"arbitrary string returns false", "admin", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.userType != "" {
				ctx = context.WithValue(ctx, types.CtxUserType, tc.userType)
			}
			assert.Equal(t, tc.want, types.IsServiceAccount(ctx))
		})
	}
}
