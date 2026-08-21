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

func TestSupabaseEmailVerifiedClaim(t *testing.T) {
	a := newTestSupabaseAuth()

	// Both locations are accepted because Supabase has moved this flag between
	// them across versions; an upgrade in either direction must not silently
	// turn every account into an unverified one.
	sign := func(t *testing.T, extra map[string]interface{}) string {
		t.Helper()
		claims := jwt.MapClaims{
			"sub":   "user_123",
			"email": "dev@example.com",
			"app_metadata": map[string]interface{}{
				"tenant_id": "tenant_123",
			},
		}
		for k, v := range extra {
			claims[k] = v
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
			SignedString([]byte(a.AuthConfig.Secret))
		require.NoError(t, err)
		return signed
	}

	t.Run("reads a top-level email_verified", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, map[string]interface{}{
			"email_verified": true,
		}))
		require.NoError(t, err)
		assert.True(t, claims.EmailVerified)
	})

	// user_metadata is writable by the user through the client SDK, so a copy
	// of the flag there proves nothing about the provider's confirmation state
	// and must never satisfy the signup guard on its own.
	t.Run("ignores email_verified nested in user_metadata", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, map[string]interface{}{
			"user_metadata": map[string]interface{}{"email_verified": true},
		}))
		require.NoError(t, err)
		assert.False(t, claims.EmailVerified)
	})

	t.Run("ignores user_metadata even when the top-level claim is false", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, map[string]interface{}{
			"email_verified": false,
			"user_metadata":  map[string]interface{}{"email_verified": true},
		}))
		require.NoError(t, err)
		assert.False(t, claims.EmailVerified)
	})

	t.Run("reports false when the claim is absent", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, nil))
		require.NoError(t, err)
		assert.False(t, claims.EmailVerified)
	})

	t.Run("reports false when the claim is explicitly false", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, map[string]interface{}{
			"email_verified": false,
			"user_metadata":  map[string]interface{}{"email_verified": false},
		}))
		require.NoError(t, err)
		assert.False(t, claims.EmailVerified)
	})

	t.Run("ignores a non-boolean claim rather than treating it as verified", func(t *testing.T) {
		claims, err := a.ValidateToken(context.Background(), sign(t, map[string]interface{}{
			"email_verified": "true",
			"user_metadata":  map[string]interface{}{"email_verified": "true"},
		}))
		require.NoError(t, err)
		assert.False(t, claims.EmailVerified)
	})
}
