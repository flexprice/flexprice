package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/cache"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSecretCache_DeleteUnderTenantCtxRemovesGlobalEntry(t *testing.T) {
	repo := &secretRepository{cache: cache.NewInMemoryCache()}
	sec := &domainSecret.Secret{ID: "secret_1", Value: "hashed-value-1"}

	repo.SetCache(context.Background(), sec)
	require.NotNil(t, repo.GetCache(context.Background(), sec.Value))

	tenantCtx := types.SetTenantID(context.Background(), "tenant_1")
	tenantCtx = types.SetEnvironmentID(tenantCtx, "env_1")
	repo.DeleteCache(tenantCtx, sec.Value)

	require.Nil(t, repo.GetCache(context.Background(), sec.Value))
}
