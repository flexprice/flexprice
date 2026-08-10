package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-32-bytes-minimum!"

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
	r.Use(AuthenticateMiddleware(cfg, nil, log))
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

	t.Run("falls back to X-Environment-ID header when claim absent", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(types.HeaderEnvironment, "env_from_header")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "env_from_header")
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

	t.Run("no environment_id in claim or header results in empty env", func(t *testing.T) {
		token := makeJWT(t, "t_tenant1", "usr_dev", "", 1)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"environment_id":""`)
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
		}, nil, newTestLogger(t)))
		r.GET("/capture", func(c *gin.Context) {
			capturedCtx = c.Request.Context()
			c.Status(http.StatusOK)
		})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/capture", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		assert.False(t, types.IsServiceAccount(capturedCtx))
	})
}
