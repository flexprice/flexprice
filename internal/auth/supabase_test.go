package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/golang-jwt/jwt/v4"
	"github.com/nedpals/supabase-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSupabaseAuth() *supabaseAuth {
	return &supabaseAuth{
		AuthConfig: config.AuthConfig{
			Secret: "test-secret-key-32-bytes-minimum!",
		},
	}
}

func TestSupabaseDevTokenEnvironmentID(t *testing.T) {
	a := newTestSupabaseAuth()

	t.Run("extracts top-level environment_id", func(t *testing.T) {
		token, _, err := a.GenerateDevToken("tenant_123", "env_123", "user_123", "dev@example.com", 1)
		require.NoError(t, err)

		claims, err := a.ValidateToken(context.Background(), token)
		require.NoError(t, err)
		assert.Equal(t, "env_123", claims.EnvironmentID)
	})

	t.Run("allows an omitted environment_id", func(t *testing.T) {
		token, _, err := a.GenerateDevToken("tenant_123", "", "user_123", "dev@example.com", 1)
		require.NoError(t, err)

		claims, err := a.ValidateToken(context.Background(), token)
		require.NoError(t, err)
		assert.Empty(t, claims.EnvironmentID)
	})

	t.Run("ignores a non-string environment_id", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":            "user_123",
			"email":          "dev@example.com",
			"environment_id": 123,
			"app_metadata": map[string]interface{}{
				"tenant_id": "tenant_123",
			},
		})
		signed, err := token.SignedString([]byte(a.AuthConfig.Secret))
		require.NoError(t, err)

		claims, err := a.ValidateToken(context.Background(), signed)
		require.NoError(t, err)
		assert.Empty(t, claims.EnvironmentID)
	})
}

func TestSupabaseRemoveUser(t *testing.T) {
	testLogger, err := logger.NewLogger(&config.Configuration{})
	require.NoError(t, err)

	newAuth := func(handler http.HandlerFunc) (*supabaseAuth, *httptest.Server) {
		srv := httptest.NewServer(handler)
		authConfig := config.AuthConfig{Secret: "test-secret-key-32-bytes-minimum!"}
		authConfig.Supabase.ServiceKey = "service-key"
		return &supabaseAuth{
			AuthConfig: authConfig,
			client:     supabase.CreateClient(srv.URL, authConfig.Supabase.ServiceKey),
			logger:     testLogger,
		}, srv
	}

	t.Run("fetches then deletes the user via the admin API", func(t *testing.T) {
		var gotMethods []string
		var gotPaths []string
		var gotAuth []string
		a, srv := newAuth(func(w http.ResponseWriter, r *http.Request) {
			gotMethods = append(gotMethods, r.Method)
			gotPaths = append(gotPaths, r.URL.Path)
			gotAuth = append(gotAuth, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusOK)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "user_123"})
		})
		defer srv.Close()

		err := a.RemoveUser(context.Background(), "user_123")
		require.NoError(t, err)

		require.Equal(t, []string{http.MethodGet, http.MethodDelete}, gotMethods)
		assert.Equal(t, "/auth/v1/admin/users/user_123", gotPaths[0])
		assert.Equal(t, "/auth/v1/admin/users/user_123", gotPaths[1])
		assert.Equal(t, "Bearer service-key", gotAuth[1])
	})

	t.Run("succeeds without deleting when the user is already gone from Supabase", func(t *testing.T) {
		var sawDelete bool
		a, srv := newAuth(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				sawDelete = true
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 404, "msg": "not found"})
		})
		defer srv.Close()

		err := a.RemoveUser(context.Background(), "user_123")
		require.NoError(t, err)
		assert.False(t, sawDelete, "delete must not be attempted once the lookup shows the user is already gone")
	})

	t.Run("fails without deleting when the lookup errors for a reason other than not-found", func(t *testing.T) {
		var sawDelete bool
		a, srv := newAuth(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				sawDelete = true
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 500, "msg": "boom"})
		})
		defer srv.Close()

		err := a.RemoveUser(context.Background(), "user_123")
		require.Error(t, err)
		assert.False(t, sawDelete, "delete must not be attempted when the user lookup fails")
	})

	t.Run("propagates a failed delete", func(t *testing.T) {
		a, srv := newAuth(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 500, "msg": "boom"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "user_123"})
		})
		defer srv.Close()

		err := a.RemoveUser(context.Background(), "user_123")
		require.Error(t, err)
	})
}
