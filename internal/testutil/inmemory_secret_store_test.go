package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInMemorySecretStoreRevokeByUserID(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "t1"), "env1")
	store := NewInMemorySecretStore()

	add := func(id, tenant, env, user string) {
		s := &secret.Secret{ID: id, UserID: user, EnvironmentID: env}
		s.TenantID = tenant
		s.Status = types.StatusPublished
		require.NoError(t, store.InMemoryStore.Create(ctx, id, s))
	}
	for i := 0; i < 60; i++ {
		add(fmt.Sprintf("a%d", i), "t1", "env1", "u1")
	}
	add("b1", "t1", "env2", "u1")
	add("other-user", "t1", "env1", "u2")
	add("other-tenant", "t2", "env1", "u1")

	require.NoError(t, store.RevokeByUserID(ctx, "u1"))

	for id, want := range map[string]types.Status{
		"a0": types.StatusDeleted, "a59": types.StatusDeleted, "b1": types.StatusDeleted,
		"other-user": types.StatusPublished, "other-tenant": types.StatusPublished,
	} {
		got, err := store.InMemoryStore.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, got.Status, id)
	}
}
