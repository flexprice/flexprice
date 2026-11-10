package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInMemoryUserStore_EmailReuseAfterDelete(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "t1")
	store := NewInMemoryUserStore()

	first := &user.User{ID: "u1", TenantID: "t1", Email: "Alice@Example.com", Status: types.StatusPublished}
	require.NoError(t, store.Create(ctx, first))
	require.Equal(t, "alice@example.com", first.Email)

	require.NoError(t, store.Delete(ctx, "u1"))
	_, err := store.GetByEmail(ctx, "alice@example.com")
	require.Error(t, err)

	second := &user.User{ID: "u2", TenantID: "t1", Email: "ALICE@example.com", Status: types.StatusPublished}
	require.NoError(t, store.Create(ctx, second))
	got, err := store.GetByEmail(ctx, "Alice@example.com")
	require.NoError(t, err)
	require.Equal(t, "u2", got.ID)
}
