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

func TestSecretCache_SetUnderTenantCtxReadableByAuth(t *testing.T) {
	repo := &secretRepository{cache: cache.NewInMemoryCache()}
	sec := &domainSecret.Secret{ID: "secret_2", Value: "hashed-value-2"}

	tenantCtx := types.SetTenantID(context.Background(), "tenant_1")
	tenantCtx = types.SetEnvironmentID(tenantCtx, "env_1")
	repo.SetCache(tenantCtx, sec)

	require.NotNil(t, repo.GetCache(context.Background(), sec.Value))

	repo.DeleteCache(context.Background(), sec.Value)
	require.Nil(t, repo.GetCache(tenantCtx, sec.Value))
}

// Revoked keys must stay cached until commit, and stay cached on rollback.
func TestSecretRepository_InvalidateCacheAfterCommit(t *testing.T) {
	tests := []struct {
		name            string
		inTx            bool
		finish          func(context.Context)
		wantCachedAfter bool
	}{
		{name: "no transaction deletes immediately"},
		{name: "commit deletes after commit", inTx: true, finish: types.RunPostCommitHooks},
		{name: "rollback keeps the key cached", inTx: true, finish: types.DiscardPostCommitHooks, wantCachedAfter: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := entitlementCacheTestContext()
			if tt.inTx {
				ctx = types.WithPostCommitHooks(ctx)
			}
			repo := &secretRepository{cache: cache.NewInMemoryCache()}
			key := &domainSecret.Secret{ID: "key-1", Value: "hashed-value"}
			repo.SetCache(ctx, key)

			repo.invalidateCache(ctx, []*domainSecret.Secret{key})

			require.Equal(t, tt.inTx, repo.GetCache(ctx, key.Value) != nil, "before the transaction ends")
			if tt.finish != nil {
				tt.finish(ctx)
			}
			require.Equal(t, tt.wantCachedAfter, repo.GetCache(ctx, key.Value) != nil, "after the transaction ends")
		})
	}
}
