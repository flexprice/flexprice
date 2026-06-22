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

// callerCtx controls what the simulated auth middleware seeds into the request context.
type callerCtx struct {
	userType string   // "user", "service_account", or "" (not set)
	roles    []string // only meaningful for service accounts
}

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
//  1. Seeds tenantID, userType, and roles into the request context (simulating AuthenticateMiddleware)
//  2. Runs TenantStatusMiddleware
//  3. Has a read route GET /test (no permission check) and a write route POST /test
//     (gated by RequirePermission, which enforces suspension + service-account RBAC)
func newTestRouter(t *testing.T, tenantID string, caller callerCtx, svc *mockTenantService, rbacSvc *rbac.RBACService) *gin.Engine {
	t.Helper()

	log := newTestLogger(t)
	permMW := NewPermissionMiddleware(rbacSvc, log)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate AuthenticateMiddleware seeding identity into context
	r.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		if tenantID != "" {
			ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
		}
		if caller.userType != "" {
			ctx = context.WithValue(ctx, types.CtxUserType, caller.userType)
		}
		if caller.roles != nil {
			ctx = context.WithValue(ctx, types.CtxRoles, caller.roles)
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

	// Write route — gated by RequirePermission (suspension + service-account RBAC)
	r.POST("/test", permMW.RequirePermission("entity", types.ActionWrite), statusHandler)

	return r
}

// newPermissiveRBACService returns an RBACService with no roles — empty roles always grant full access.
func newPermissiveRBACService(t *testing.T) *rbac.RBACService {
	t.Helper()
	return newRBACServiceFromJSON(t, `{}`)
}

// newRBACServiceWithRoles returns an RBACService with a single "writer" role
// that has write access to "entity", used to test service-account RBAC.
func newRBACServiceWithRoles(t *testing.T) *rbac.RBACService {
	t.Helper()
	return newRBACServiceFromJSON(t, `{
		"writer": {
			"name": "Writer",
			"description": "Can write entity",
			"permissions": { "entity": ["write", "read"] }
		}
	}`)
}

func newRBACServiceFromJSON(t *testing.T, rolesJSON string) *rbac.RBACService {
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

func TestTenantStatusMiddleware(t *testing.T) {
	activeSvc := func(status types.TenantInternalStatus) *mockTenantService {
		return &mockTenantService{status: status}
	}
	userCaller := callerCtx{userType: "user"}
	anonCaller := callerCtx{} // no user type set (e.g. config API key)

	testCases := []struct {
		name               string
		skip               string
		method             string
		tenantID           string
		caller             callerCtx
		svc                *mockTenantService
		rbacSvc            *rbac.RBACService
		wantCode           int
		wantInternalStatus string
		wantErrMsg         string
	}{
		// ── Tenant status stamping ──────────────────────────────────────────────
		{
			name:               "active status stamped onto context",
			method:             http.MethodGet,
			tenantID:           "t1",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusActive),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:               "trialing status stamped onto context",
			method:             http.MethodGet,
			tenantID:           "t2",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusTrialing),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusTrialing),
		},
		// ── Suspension enforcement ──────────────────────────────────────────────
		{
			name:               "suspended tenant: read passes through",
			method:             http.MethodGet,
			tenantID:           "t3",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusSuspended),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusSuspended),
		},
		{
			name:       "suspended tenant: write blocked",
			method:     http.MethodPost,
			tenantID:   "t3",
			caller:     userCaller,
			svc:        activeSvc(types.TenantInternalStatusSuspended),
			wantCode:   http.StatusForbidden,
			wantErrMsg: "tenant account is suspended",
		},
		{
			name:               "active tenant: write passes through",
			method:             http.MethodPost,
			tenantID:           "t4",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusActive),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:               "trialing tenant: write passes through",
			method:             http.MethodPost,
			tenantID:           "t5",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusTrialing),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusTrialing),
		},
		// ── Service error ───────────────────────────────────────────────────────
		{
			name:       "service error returns 500",
			method:     http.MethodGet,
			tenantID:   "t6",
			caller:     userCaller,
			svc:        &mockTenantService{err: errors.New("db unavailable")},
			wantCode:   http.StatusInternalServerError,
			wantErrMsg: "failed to verify tenant access",
		},
		// ── Service account RBAC ────────────────────────────────────────────────
		{
			name:               "service account with required role: write allowed",
			method:             http.MethodPost,
			tenantID:           "t7",
			caller:             callerCtx{userType: string(types.UserTypeServiceAccount), roles: []string{"writer"}},
			svc:                activeSvc(types.TenantInternalStatusActive),
			rbacSvc:            nil, // set below via table default override
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:       "service account without required role: write blocked",
			method:     http.MethodPost,
			tenantID:   "t8",
			caller:     callerCtx{userType: string(types.UserTypeServiceAccount), roles: []string{"reader_only"}},
			svc:        activeSvc(types.TenantInternalStatusActive),
			wantCode:   http.StatusForbidden,
			wantErrMsg: "Forbidden",
		},
		// ── User account bypasses RBAC ──────────────────────────────────────────
		{
			name:               "user account: write allowed regardless of roles",
			method:             http.MethodPost,
			tenantID:           "t9",
			caller:             userCaller,
			svc:                activeSvc(types.TenantInternalStatusActive),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:               "no user type set (config key): write allowed",
			method:             http.MethodPost,
			tenantID:           "t10",
			caller:             anonCaller,
			svc:                activeSvc(types.TenantInternalStatusActive),
			wantCode:           http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
	}

	rbacWithRoles := newRBACServiceWithRoles(t)
	permissive := newPermissiveRBACService(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rbacSvc := permissive
			if tc.rbacSvc != nil {
				rbacSvc = tc.rbacSvc
			}
			// Service account RBAC cases need the roles-aware RBAC service
			if tc.caller.userType == string(types.UserTypeServiceAccount) {
				rbacSvc = rbacWithRoles
			}

			router := newTestRouter(t, tc.tenantID, tc.caller, tc.svc, rbacSvc)

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
				var body map[string]interface{}
				assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body["error"], tc.wantErrMsg)
			}
		})
	}
}
