package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-32-bytes-minimum!"

// fakeEnvironmentRepo mirrors the tenant scoping of the real repository: Get,
// List and GetDefaultByType only ever return environments owned by the tenant in
// context. The err fields inject an infrastructure failure so that the
// distinction between "not found" and "the database is down" can be asserted.
type fakeEnvironmentRepo struct {
	environments []*domainEnvironment.Environment
	getErr       error
	listErr      error
	defaultErr   error
}

func (f *fakeEnvironmentRepo) Get(ctx context.Context, id string) (*domainEnvironment.Environment, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	tenantID := types.GetTenantID(ctx)
	for _, env := range f.environments {
		if env.ID == id && env.TenantID == tenantID {
			return env, nil
		}
	}
	return nil, ierr.NewError("environment not found").Mark(ierr.ErrNotFound)
}

func (f *fakeEnvironmentRepo) List(ctx context.Context, filter types.Filter) ([]*domainEnvironment.Environment, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	tenantID := types.GetTenantID(ctx)
	var result []*domainEnvironment.Environment
	for _, env := range f.environments {
		if env.TenantID == tenantID {
			result = append(result, env)
		}
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// GetDefaultByType returns the tenant's first published environment of that
// type, matching the repository's oldest-first ordering over this fixed slice.
func (f *fakeEnvironmentRepo) GetDefaultByType(ctx context.Context, envType types.EnvironmentType) (*domainEnvironment.Environment, error) {
	if f.defaultErr != nil {
		return nil, f.defaultErr
	}
	tenantID := types.GetTenantID(ctx)
	for _, env := range f.environments {
		if env.TenantID == tenantID && env.Type == envType {
			return env, nil
		}
	}
	return nil, ierr.NewError("environment not found").Mark(ierr.ErrNotFound)
}

func (f *fakeEnvironmentRepo) Create(ctx context.Context, env *domainEnvironment.Environment) error {
	return nil
}

func (f *fakeEnvironmentRepo) Update(ctx context.Context, env *domainEnvironment.Environment) error {
	return nil
}

func (f *fakeEnvironmentRepo) CountByType(ctx context.Context, envType types.EnvironmentType) (int, error) {
	return 0, nil
}

func testEnvironment(id, tenantID string, envType types.EnvironmentType) *domainEnvironment.Environment {
	return &domainEnvironment.Environment{
		ID:        id,
		Name:      id,
		Type:      envType,
		BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished},
	}
}

// newTestEnvironmentRepo seeds one tenant with a development and a production
// environment, plus an environment owned by a different tenant used to assert
// cross-tenant requests are refused.
func newTestEnvironmentRepo() *fakeEnvironmentRepo {
	return &fakeEnvironmentRepo{
		environments: []*domainEnvironment.Environment{
			testEnvironment("env_prod", "t_tenant1", types.EnvironmentProduction),
			testEnvironment("env_dev", "t_tenant1", types.EnvironmentDevelopment),
			testEnvironment("env_other_tenant", "t_tenant2", types.EnvironmentDevelopment),
		},
	}
}

func makeJWT(t *testing.T, tenantID, userID, environmentID string, expiryHours int) string {
	t.Helper()
	claims := jwt.MapClaims{
		"tenant_id": tenantID,
		"user_id":   userID,
		"exp":       time.Now().Add(time.Duration(expiryHours) * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
	}
	if environmentID != "" {
		claims["environment_id"] = environmentID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testSecret))
	require.NoError(t, err)
	return signed
}

func newAuthTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: "flexprice",
			Secret:   testSecret,
			APIKey:   config.APIKeyConfig{Header: "x-api-key"},
		},
	}
	log := newTestLogger(t)

	r := gin.New()
	r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), log))
	r.GET("/test", func(c *gin.Context) {
		ctx := c.Request.Context()
		c.JSON(http.StatusOK, gin.H{
			"tenant_id":      types.GetTenantID(ctx),
			"user_id":        types.GetUserID(ctx),
			"environment_id": types.GetEnvironmentID(ctx),
		})
	})
	return r
}

func TestAuthenticateMiddleware_EnvironmentIDFromJWT(t *testing.T) {
	router := newAuthTestRouter(t)

	t.Run("uses environment_id from JWT claim when present", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "env_prod", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "env_prod")
	})

	// The dashboard switches environments by sending this header alongside a JWT
	// that carries no environment claim, so a header naming an environment the
	// caller's tenant owns must still be honoured.
	t.Run("uses X-Environment-ID header when claim absent and header is owned by tenant", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(types.HeaderEnvironment, "env_prod")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"environment_id":"env_prod"`)
	})

	t.Run("rejects X-Environment-ID header naming another tenant's environment", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(types.HeaderEnvironment, "env_other_tenant")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.NotContains(t, w.Body.String(), "env_other_tenant")
	})

	t.Run("rejects X-Environment-ID header naming an unknown environment", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(types.HeaderEnvironment, "env_does_not_exist")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("JWT claim takes priority over header", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "env_from_jwt", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(types.HeaderEnvironment, "env_from_header")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "env_from_jwt")
		assert.NotContains(t, w.Body.String(), "env_from_header")
	})

	// No claim and no header means nothing selects an environment. Rather than
	// guessing one, the request is refused: a wrong guess would silently operate
	// on the wrong environment's data.
	t.Run("no environment_id in claim or header is refused", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("denies the request when the tenant has no environments", func(t *testing.T) {
		token := makeJWT(t, "t_tenant_without_envs", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("JWT user is not a service account", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		var capturedCtx context.Context
		r := gin.New()
		r.Use(AuthenticateMiddleware(&config.Configuration{
			Auth: config.AuthConfig{
				Provider: "flexprice",
				Secret:   testSecret,
				APIKey:   config.APIKeyConfig{Header: "x-api-key"},
			},
		}, nil, newTestEnvironmentRepo(), newTestLogger(t)))
		r.GET("/capture", func(c *gin.Context) {
			capturedCtx = c.Request.Context()
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/capture", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		// An ordinary route needs a selected environment to get past the
		// middleware; this test is about caller type, not resolution.
		req.Header.Set(types.HeaderEnvironment, "env_dev")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.False(t, types.IsServiceAccount(capturedCtx))
	})
}

// newEnvironmentResolutionContext builds the gin context resolveEnvironmentID
// expects: the tenant is already in the request context by the time the
// environment is resolved.
func newEnvironmentResolutionContext(tenantID, headerValue string) *gin.Context {
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	if headerValue != "" {
		req.Header.Set(types.HeaderEnvironment, headerValue)
	}
	ctx := context.WithValue(req.Context(), types.CtxTenantID, tenantID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req.WithContext(ctx)
	return c
}

func TestResolveEnvironmentID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A database-backed API key is issued against one environment. Honouring a
	// header here would let a caller redirect a scoped key at another
	// environment, so the bound value must win even when it is contradicted.
	t.Run("credential-bound environment wins over the header", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant1", "env_dev")

		resolved, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "env_prod", "t_tenant1")

		require.NoError(t, err)
		assert.Equal(t, "env_prod", resolved)
	})

	// A bound environment is trusted from the credential itself and is not
	// re-validated, so it resolves even if the repository cannot see it.
	t.Run("credential-bound environment is used without a repository lookup", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant1", "")

		resolved, err := resolveEnvironmentID(c.Request.Context(), c, &fakeEnvironmentRepo{}, "env_bound", "t_tenant1")

		require.NoError(t, err)
		assert.Equal(t, "env_bound", resolved)
	})

	t.Run("header is honoured when it names an environment the tenant owns", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant1", "env_prod")

		resolved, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "", "t_tenant1")

		require.NoError(t, err)
		assert.Equal(t, "env_prod", resolved)
	})

	t.Run("header naming another tenant's environment is refused", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant1", "env_other_tenant")

		_, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "", "t_tenant1")

		assert.ErrorIs(t, err, errEnvironmentUnresolved)
	})

	t.Run("no header on an ordinary route is refused rather than defaulted", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant1", "")

		_, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "", "t_tenant1")

		assert.ErrorIs(t, err, errEnvironmentUnresolved)
	})

	t.Run("fails when the tenant owns no environments", func(t *testing.T) {
		c := newEnvironmentResolutionContext("t_tenant_without_envs", "")

		_, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "", "t_tenant_without_envs")

		assert.ErrorIs(t, err, errEnvironmentUnresolved)
	})

	// Guards the guard: without a tenant there is nothing to scope the lookup
	// to, so an unbound caller must not resolve an environment at all.
	t.Run("fails when there is no tenant to scope the lookup", func(t *testing.T) {
		c := newEnvironmentResolutionContext("", "env_prod")

		_, err := resolveEnvironmentID(c.Request.Context(), c, newTestEnvironmentRepo(), "", "")

		assert.ErrorIs(t, err, errEnvironmentUnresolved)
	})

}

// A failed lookup must not be reported as an access decision: 403 tells the
// caller their credentials are wrong and not to retry, while a database outage
// is retryable and needs to surface as a fault.
func TestResolveEnvironmentIDPropagatesRepositoryFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbErr := ierr.NewError("connection refused").Mark(ierr.ErrDatabase)

	t.Run("header lookup failure is propagated, not turned into a refusal", func(t *testing.T) {
		repo := &fakeEnvironmentRepo{getErr: dbErr}
		c := newEnvironmentResolutionContext("t_tenant1", "env_prod")

		_, err := resolveEnvironmentID(c.Request.Context(), c, repo, "", "t_tenant1")

		require.Error(t, err)
		assert.NotErrorIs(t, err, errEnvironmentUnresolved)
	})

}

// The dashboard bootstraps via a small set of GETs before it can send
// X-Environment-ID. Its login JWT carries no environment claim, so requiring a
// header to reach those routes would be circular. Writes under the same groups
// still require a selection.
func TestAuthenticateMiddleware_EnvironmentDiscoveryWithoutHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   testSecret,
			APIKey:   config.APIKeyConfig{Header: "x-api-key"},
		},
	}

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), newTestLogger(t)))
		handler := func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"environment_id": types.GetEnvironmentID(c.Request.Context()),
			})
		}
		r.GET("/v1/users/me", handler)
		r.PUT("/v1/users/me", handler)
		r.POST("/v1/users", handler)
		r.GET("/v1/environments", handler)
		r.GET("/v1/environments/:id", handler)
		r.POST("/v1/environments", handler)
		r.POST("/v1/environments/:id/clone", handler)
		r.GET("/v1/tenants/:id", handler)
		r.GET("/v1/tenants/billing", handler)
		r.PUT("/v1/tenants/update", handler)
		r.GET("/v1/customers", handler)
		return r
	}

	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "", 1))
		newRouter().ServeHTTP(w, req)
		return w
	}

	t.Run("bootstrap GETs succeed without a header", func(t *testing.T) {
		for _, path := range []string{
			"/v1/users/me",
			"/v1/environments",
			"/v1/environments/env_dev",
			"/v1/tenants/t_tenant1",
		} {
			w := do(http.MethodGet, path)
			require.Equal(t, http.StatusOK, w.Code, path)
			assert.Contains(t, w.Body.String(), `"environment_id":""`, path)
		}
	})

	t.Run("writes and non-bootstrap reads under the same groups still require a header", func(t *testing.T) {
		for _, tc := range []struct {
			method, path string
		}{
			{http.MethodPut, "/v1/users/me"},
			{http.MethodPost, "/v1/users"},
			{http.MethodPost, "/v1/environments"},
			{http.MethodPost, "/v1/environments/env_dev/clone"},
			{http.MethodGet, "/v1/tenants/billing"},
			{http.MethodPut, "/v1/tenants/update"},
			{http.MethodGet, "/v1/customers"},
		} {
			assert.Equal(t, http.StatusForbidden, do(tc.method, tc.path).Code, tc.method+" "+tc.path)
		}
	})
}

// The middleware must translate the two error classes into different statuses.
func TestAuthenticateMiddleware_EnvironmentLookupFailureIsNotForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   testSecret,
			APIKey:   config.APIKeyConfig{Header: "x-api-key"},
		},
	}

	r := gin.New()
	r.Use(AuthenticateMiddleware(cfg, nil, &fakeEnvironmentRepo{
		getErr: ierr.NewError("connection refused").Mark(ierr.ErrDatabase),
	}, newTestLogger(t)))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "", 1))
	req.Header.Set(types.HeaderEnvironment, "env_dev")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
