package auth

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAuth() *flexpriceAuth {
	return &flexpriceAuth{
		AuthConfig: config.AuthConfig{
			Secret: "test-secret-key-32-bytes-minimum!",
		},
	}
}

func TestGenerateDevToken(t *testing.T) {
	a := newTestAuth()
	ctx := context.Background()

	t.Run("includes environment_id when provided", func(t *testing.T) {
		token, expiresAt, err := a.GenerateDevToken("t_tenant1", "env_prod", "usr_dev", "", 1)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 5*time.Second)

		claims, err := a.ValidateToken(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, "t_tenant1", claims.TenantID)
		assert.Equal(t, "usr_dev", claims.UserID)
		assert.Equal(t, "env_prod", claims.EnvironmentID)
	})

	t.Run("omits environment_id claim when empty", func(t *testing.T) {
		token, _, err := a.GenerateDevToken("t_tenant1", "", types.DefaultUserID, "", 1)
		require.NoError(t, err)

		claims, err := a.ValidateToken(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, "t_tenant1", claims.TenantID)
		assert.Equal(t, types.DefaultUserID, claims.UserID)
		assert.Empty(t, claims.EnvironmentID)
	})

	t.Run("respects custom expiry hours", func(t *testing.T) {
		_, expiresAt, err := a.GenerateDevToken("t_tenant1", "", types.DefaultUserID, "", 8)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(8*time.Hour), expiresAt, 5*time.Second)
	})

	t.Run("returns error when tenantID is empty", func(t *testing.T) {
		_, _, err := a.GenerateDevToken("", "env_prod", "usr_dev", "", 1)
		assert.Error(t, err)
	})
}

func TestValidateToken_ExtractsEnvironmentID(t *testing.T) {
	a := newTestAuth()
	ctx := context.Background()

	t.Run("extracts environment_id from dev token", func(t *testing.T) {
		token, _, err := a.GenerateDevToken("t_tenant1", "env_staging", types.DefaultUserID, "", 1)
		require.NoError(t, err)

		claims, err := a.ValidateToken(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, "env_staging", claims.EnvironmentID)
	})

	t.Run("returns empty EnvironmentID for regular token without claim", func(t *testing.T) {
		token, err := a.generateToken("usr_abc", "t_tenant1")
		require.NoError(t, err)

		claims, err := a.ValidateToken(ctx, token)
		require.NoError(t, err)
		assert.Empty(t, claims.EnvironmentID)
	})
}
