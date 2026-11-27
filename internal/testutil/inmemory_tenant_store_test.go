package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInMemoryTenantStore_ListIDsPublishedOnly(t *testing.T) {
	store := NewInMemoryTenantStore()
	ctx := context.Background()
	for id, status := range map[string]types.Status{
		"t_b":        types.StatusPublished,
		"t_a":        types.StatusPublished,
		"t_archived": types.StatusArchived,
		"t_deleted":  types.StatusDeleted,
	} {
		require.NoError(t, store.Create(ctx, &tenant.Tenant{ID: id, Status: status}))
	}

	ids, err := store.ListIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"t_a", "t_b"}, ids)
}
