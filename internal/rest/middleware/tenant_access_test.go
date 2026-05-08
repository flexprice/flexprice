package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// mockTenantRepo is a minimal in-memory implementation of tenant.Repository for tests.
type mockTenantRepo struct {
	tenant *domainTenant.Tenant
	err    error
}

func (m *mockTenantRepo) GetByID(_ context.Context, _ string) (*domainTenant.Tenant, error) {
	return m.tenant, m.err
}
func (m *mockTenantRepo) Create(_ context.Context, _ *domainTenant.Tenant) error { return nil }
func (m *mockTenantRepo) List(_ context.Context) ([]*domainTenant.Tenant, error) { return nil, nil }
func (m *mockTenantRepo) Update(_ context.Context, _ *domainTenant.Tenant) error { return nil }

// newTestRouter builds a minimal Gin router that:
//  1. Seeds tenantID into the request context (simulating AuthenticateMiddleware)
//  2. Runs TenantAccessMiddleware
//  3. Has a /test handler that writes the internal_status from ctx into the response
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

func newTestRouter(t *testing.T, tenantID string, repo *mockTenantRepo) *gin.Engine {
	log := newTestLogger(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Simulate AuthenticateMiddleware seeding tenantID
	r.Use(func(c *gin.Context) {
		if tenantID != "" {
			ctx := context.WithValue(c.Request.Context(), types.CtxTenantID, tenantID)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	})

	r.Use(TenantAccessMiddleware(repo, log))

	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"internal_status": string(types.GetTenantInternalStatus(c.Request.Context())),
		})
	})

	return r
}

func TestTenantAccessMiddleware(t *testing.T) {
	testCases := []struct {
		name               string
		tenantID           string
		repoTenant         *domainTenant.Tenant
		repoErr            error
		wantStatus         int
		wantInternalStatus string // only checked on 200
	}{
		{
			name:     "suspended tenant is blocked",
			tenantID: "tenant-1",
			repoTenant: &domainTenant.Tenant{
				ID:             "tenant-1",
				InternalStatus: types.TenantInternalStatusSuspended,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:     "active tenant passes through and stamps ctx",
			tenantID: "tenant-2",
			repoTenant: &domainTenant.Tenant{
				ID:             "tenant-2",
				InternalStatus: types.TenantInternalStatusActive,
			},
			wantStatus:         http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusActive),
		},
		{
			name:     "trialing tenant passes through and stamps ctx",
			tenantID: "tenant-3",
			repoTenant: &domainTenant.Tenant{
				ID:             "tenant-3",
				InternalStatus: types.TenantInternalStatusTrialing,
			},
			wantStatus:         http.StatusOK,
			wantInternalStatus: string(types.TenantInternalStatusTrialing),
		},
		{
			name:       "empty tenantID skips check and passes through",
			tenantID:   "",
			repoTenant: nil,
			wantStatus: http.StatusOK,
		},
		{
			name:       "repo error fails open and passes through",
			tenantID:   "tenant-4",
			repoErr:    errors.New("db unavailable"),
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockTenantRepo{tenant: tc.repoTenant, err: tc.repoErr}
			router := newTestRouter(t, tc.tenantID, repo)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tc.wantStatus, w.Code)

			if tc.wantStatus == http.StatusOK && tc.wantInternalStatus != "" {
				var body map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &body)
				assert.NoError(t, err)
				assert.Equal(t, tc.wantInternalStatus, body["internal_status"])
			}

			if tc.wantStatus == http.StatusUnauthorized {
				var body map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &body)
				assert.NoError(t, err)
				assert.Equal(t, "tenant account is suspended", body["error"])
			}
		})
	}
}
