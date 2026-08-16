package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainEnvironment "github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
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

// newTestUserRepo seeds the active "usr_dev" user that makeJWT mints tokens for,
// in each tenant these tests issue tokens for, so the middleware's role lookup
// resolves and the request reaches the behaviour under test. Tests concerned
// with the lookup itself seed their own store instead.
func newTestUserRepo() *testutil.InMemoryUserStore {
	devUser := func(tenantID, email string) *user.User {
		return &user.User{
			ID:    "usr_dev",
			Email: email,
			Type:  types.UserTypeUser,
			Roles: []string{types.RoleSuperAdmin.String()},
			BaseModel: types.BaseModel{
				TenantID: tenantID,
				Status:   types.StatusPublished,
			},
		}
	}
	return newUserRepoWith(
		devUser("t_tenant1", "usr_dev@example.com"),
		devUser("t_tenant_without_envs", "usr_dev+no_envs@example.com"),
	)
}

func newUserRepoWith(users ...*user.User) *testutil.InMemoryUserStore {
	store := testutil.NewInMemoryUserStore()
	for _, u := range users {
		ctx := context.WithValue(context.Background(), types.CtxTenantID, u.TenantID)
		_ = store.Create(ctx, u)
	}
	return store
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
	r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), newTestUserRepo(), log))
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
		}, nil, newTestEnvironmentRepo(), newTestUserRepo(), newTestLogger(t)))
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

// The dashboard bootstraps under /v1/users, /v1/environments, and /v1/tenants
// before it can send X-Environment-ID. Its login JWT carries no environment
// claim, so requiring a header to reach those routes would be circular.
//
// The exemption is by group prefix and does not check the method, so writes
// under these groups are exempt too: a JWT with no X-Environment-ID can create
// users, clone environments, and update tenant billing. Those handlers run with
// no environment in context, so any repository call filtering on
// environment_id matches nothing or writes an empty value. Every route outside
// the three groups still requires a selection.
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
		r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), newTestUserRepo(), newTestLogger(t)))
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
		// Guards the prefix boundary: a sibling group whose name merely starts
		// with an exempt one must not be swept in.
		r.GET("/v1/usersettings", handler)
		return r
	}

	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "", 1))
		newRouter().ServeHTTP(w, req)
		return w
	}

	t.Run("bootstrap reads succeed without a header", func(t *testing.T) {
		for _, path := range []string{
			"/v1/users/me",
			"/v1/environments",
			"/v1/environments/env_dev",
			"/v1/tenants/t_tenant1",
			"/v1/tenants/billing",
		} {
			w := do(http.MethodGet, path)
			require.Equal(t, http.StatusOK, w.Code, path)
			assert.Contains(t, w.Body.String(), `"environment_id":""`, path)
		}
	})

	// Prefix matching ignores the method, so these writes are served with no
	// environment resolved. Asserted explicitly rather than left implicit: it is
	// the cost of matching by group, and a future change that reinstates a method
	// check should fail here and be read as intentional.
	t.Run("writes under the same groups are served without a header", func(t *testing.T) {
		for _, tc := range []struct {
			method, path string
		}{
			{http.MethodPut, "/v1/users/me"},
			{http.MethodPost, "/v1/users"},
			{http.MethodPost, "/v1/environments"},
			{http.MethodPost, "/v1/environments/env_dev/clone"},
			{http.MethodPut, "/v1/tenants/update"},
		} {
			w := do(tc.method, tc.path)
			require.Equal(t, http.StatusOK, w.Code, tc.method+" "+tc.path)
			assert.Contains(t, w.Body.String(), `"environment_id":""`, tc.method+" "+tc.path)
		}
	})

	// The exemption stops at the three groups; everything else is still refused.
	t.Run("routes outside the exempt groups still require a header", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(http.MethodGet, "/v1/customers").Code)
		// /v1/usersettings shares a prefix with /v1/users but is a different group.
		assert.Equal(t, http.StatusForbidden, do(http.MethodGet, "/v1/usersettings").Code)
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
	}, newTestUserRepo(), newTestLogger(t)))
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "", 1))
	req.Header.Set(types.HeaderEnvironment, "env_dev")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// A JWT carries no roles claim, so the user record is the only thing that can
// establish what a dashboard session may do — and the only thing that can
// withdraw it, since a token stays valid after the account behind it is closed.
func TestAuthenticateMiddleware_JWTRolesComeFromUserRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   testSecret,
			APIKey:   config.APIKeyConfig{Header: "x-api-key"},
		},
	}

	activeUser := func(roles []string) *user.User {
		return &user.User{
			ID:    "usr_dev",
			Email: "usr_dev@example.com",
			Type:  types.UserTypeUser,
			Roles: roles,
			BaseModel: types.BaseModel{
				TenantID: "t_tenant1",
				Status:   types.StatusPublished,
			},
		}
	}

	withStatus := func(status types.Status) *user.User {
		u := activeUser([]string{types.RoleSuperAdmin.String()})
		u.Status = status
		return u
	}

	testCases := []struct {
		name      string
		userRepo  *testutil.InMemoryUserStore
		wantCode  int
		wantRoles []string
	}{
		{
			name:      "roles on the record are placed in the request context",
			userRepo:  newUserRepoWith(activeUser([]string{types.RoleAllReader.String()})),
			wantCode:  http.StatusOK,
			wantRoles: []string{types.RoleAllReader.String()},
		},
		{
			name:      "multiple roles are carried through intact",
			userRepo:  newUserRepoWith(activeUser([]string{types.RoleAllReader.String(), types.RoleAllWriter.String()})),
			wantCode:  http.StatusOK,
			wantRoles: []string{types.RoleAllReader.String(), types.RoleAllWriter.String()},
		},
		// An empty role set is no longer read as full access, so it must reach
		// RequirePermission as-is rather than being substituted for anything.
		{
			name:      "an empty role set is carried through, not widened",
			userRepo:  newUserRepoWith(activeUser([]string{})),
			wantCode:  http.StatusOK,
			wantRoles: []string{},
		},
		{
			name:     "a valid token for a user that no longer exists is refused",
			userRepo: testutil.NewInMemoryUserStore(),
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "an archived user is refused",
			userRepo: newUserRepoWith(withStatus(types.StatusArchived)),
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "a deleted user is refused",
			userRepo: newUserRepoWith(withStatus(types.StatusDeleted)),
			wantCode: http.StatusUnauthorized,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedRoles []string

			r := gin.New()
			r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), tc.userRepo, newTestLogger(t)))
			r.GET("/test", func(c *gin.Context) {
				capturedRoles = types.GetRoles(c.Request.Context())
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "env_dev", 1))
			r.ServeHTTP(w, req)

			require.Equal(t, tc.wantCode, w.Code)
			if tc.wantCode == http.StatusOK {
				assert.Equal(t, tc.wantRoles, capturedRoles)
			} else {
				// The refusal must not disclose whether the account exists.
				assert.JSONEq(t, `{"error":"Unauthorized"}`, w.Body.String())
			}
		})
	}
}

// Config-map API keys have no database record to carry roles, so the middleware
// grants them super_admin explicitly. Without it they would hold no roles at
// all and be refused every check.
func TestAuthenticateMiddleware_ConfigAPIKeyIsSuperAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const apiKey = "test-config-key"
	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   testSecret,
			APIKey: config.APIKeyConfig{
				Header: "x-api-key",
				Keys: map[string]config.APIKeyDetails{
					auth.HashAPIKey(apiKey): {
						TenantID: "t_tenant1",
						UserID:   "usr_dev",
						Name:     "config key",
						IsActive: true,
					},
				},
			},
		},
	}

	var capturedRoles []string
	r := gin.New()
	r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), newTestUserRepo(), newTestLogger(t)))
	r.GET("/test", func(c *gin.Context) {
		capturedRoles = types.GetRoles(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set(types.HeaderEnvironment, "env_dev")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{types.RoleSuperAdmin.String()}, capturedRoles)
}
