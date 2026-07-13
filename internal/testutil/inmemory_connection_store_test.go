package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestInMemoryConnectionStore_Get_DoesNotAliasMutableFields covers Finding E:
// Get() must return a connection whose Metadata map and SyncConfig pointer are
// independent copies, not aliases into the store's persisted state. Mutating
// the returned connection must not affect what a subsequent Get() returns.
func TestInMemoryConnectionStore_Get_DoesNotAliasMutableFields(t *testing.T) {
	store := NewInMemoryConnectionStore()
	ctx := context.Background()

	original := &connection.Connection{
		ID:           "conn_1",
		Name:         "test-connection",
		ProviderType: types.SecretProviderGCS,
		Metadata: map[string]interface{}{
			"key1": "value1",
		},
		SyncConfig: &types.SyncConfig{
			Plan: &types.EntitySyncConfig{Inbound: false, Outbound: false},
			Storage: &types.StorageExportConfig{
				Bucket: "original-bucket",
				Region: "us-west-2",
			},
		},
		BaseModel: types.BaseModel{
			Status: types.StatusPublished,
		},
	}

	require.NoError(t, store.Create(ctx, original))

	first, err := store.Get(ctx, "conn_1")
	require.NoError(t, err)
	require.NotNil(t, first.Metadata)
	require.NotNil(t, first.SyncConfig)
	require.NotNil(t, first.SyncConfig.Plan)
	require.NotNil(t, first.SyncConfig.Storage)

	// Mutate the first result's map and nested pointer fields.
	first.Metadata["key1"] = "mutated"
	first.Metadata["key2"] = "new-key"
	first.SyncConfig.Plan.Outbound = true
	first.SyncConfig.Storage.Bucket = "mutated-bucket"

	second, err := store.Get(ctx, "conn_1")
	require.NoError(t, err)

	// The second fetch must be unaffected by mutating the first fetch's result.
	require.Equal(t, "value1", second.Metadata["key1"], "Metadata mutation on a Get() result leaked into the store")
	_, hasKey2 := second.Metadata["key2"]
	require.False(t, hasKey2, "Metadata key added to a Get() result leaked into the store")
	require.False(t, second.SyncConfig.Plan.Outbound, "SyncConfig.Plan mutation leaked into the store")
	require.Equal(t, "original-bucket", second.SyncConfig.Storage.Bucket, "SyncConfig.Storage mutation leaked into the store")

	// Also verify the original input's own map/pointer weren't captured by
	// reference at Create() time (copyConnection is called there too).
	original.Metadata["key1"] = "mutated-again"
	third, err := store.Get(ctx, "conn_1")
	require.NoError(t, err)
	require.Equal(t, "value1", third.Metadata["key1"], "Metadata on the input connection leaked into the store via Create()")
}
