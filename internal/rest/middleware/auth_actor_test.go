package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetContextValuesStoresActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	actor := types.Actor{Type: types.ActorTypeAPIKey, ID: "sec_1", Label: "k", UserID: "user_1"}
	if err := setContextValues(c, nil, "tenant_1", "user_1", "env_1", "user", nil, actor, types.SourceAPI); err != nil {
		t.Fatal(err)
	}
	ctx := c.Request.Context()
	if got := types.GetActor(ctx); got != actor {
		t.Fatalf("actor not stored: %+v", got)
	}
	if types.GetSource(ctx) != types.SourceAPI {
		t.Fatal("source not stored")
	}
	if types.GetUserID(ctx) != "user_1" {
		t.Fatal("legacy user id not derived")
	}
}

func TestAuthenticateMiddlewareSetsActorPerCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const apiKey = "test-config-key"
	cfg := &config.Configuration{
		Auth: config.AuthConfig{
			Provider: types.AuthProviderFlexprice,
			Secret:   testSecret,
			APIKey: config.APIKeyConfig{
				Header: "x-api-key",
				Keys: map[string]config.APIKeyDetails{
					auth.HashAPIKey(apiKey): {TenantID: "t_tenant1", UserID: "usr_dev", Name: "config key", IsActive: true},
				},
			},
		},
	}
	userRepo := newUserRepoWith(&user.User{
		ID:        "usr_dev",
		Email:     "dev@example.com",
		Type:      types.UserTypeUser,
		BaseModel: types.BaseModel{TenantID: "t_tenant1", Status: types.StatusPublished},
	})

	testCases := []struct {
		name       string
		setHeaders func(req *http.Request)
		wantActor  types.Actor
		wantSource types.Source
	}{
		{
			name: "dashboard JWT is a user actor",
			setHeaders: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+makeJWT(t, "t_tenant1", "usr_dev", "env_dev", 1))
			},
			wantActor:  types.Actor{Type: types.ActorTypeUser, ID: "usr_dev", Label: "dev@example.com"},
			wantSource: types.SourceDashboard,
		},
		{
			name: "config API key is the operator key actor",
			setHeaders: func(req *http.Request) {
				req.Header.Set("x-api-key", apiKey)
				req.Header.Set(types.HeaderEnvironment, "env_dev")
			},
			wantActor:  types.Actor{Type: types.ActorTypeAPIKey, ID: "config", Label: "Operator key", UserID: "usr_dev"},
			wantSource: types.SourceAPI,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gotActor types.Actor
			var gotSource types.Source
			r := gin.New()
			r.Use(AuthenticateMiddleware(cfg, nil, newTestEnvironmentRepo(), userRepo, newTestLogger(t)))
			r.GET("/test", func(c *gin.Context) {
				gotActor = types.GetActor(c.Request.Context())
				gotSource = types.GetSource(c.Request.Context())
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			tc.setHeaders(req)
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tc.wantActor, gotActor)
			assert.Equal(t, tc.wantSource, gotSource)
		})
	}
}
