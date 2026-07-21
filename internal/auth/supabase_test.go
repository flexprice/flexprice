package auth

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/golang-jwt/jwt/v4"
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
