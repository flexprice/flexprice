package entityintegrationmapping

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyWith(t *testing.T) {
	ctx := context.Background()

	original := &EntityIntegrationMapping{
		ID:               "orig-id",
		EntityID:         "old-sub-123",
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     "paddle",
		ProviderEntityID: "paddle-sub-abc",
		Metadata:         map[string]interface{}{"synced_at": "2026-01-01"},
		EnvironmentID:    "env-1",
		BaseModel: types.BaseModel{
			TenantID: "tenant-1",
			Status:   types.StatusPublished,
		},
	}

	newSubID := "new-sub-456"
	copied := original.CopyWith(ctx, &EntityIntegrationMappingCloneOverrides{
		EntityID: &newSubID,
	})

	require.NotNil(t, copied)

	// New ID must be generated (not the original)
	assert.NotEqual(t, original.ID, copied.ID)
	assert.NotEmpty(t, copied.ID)

	// EntityID must be overridden
	assert.Equal(t, newSubID, copied.EntityID)

	// All other fields retained from original
	assert.Equal(t, original.EntityType, copied.EntityType)
	assert.Equal(t, original.ProviderType, copied.ProviderType)
	assert.Equal(t, original.ProviderEntityID, copied.ProviderEntityID)
	assert.Equal(t, original.Metadata["synced_at"], copied.Metadata["synced_at"])

	// Original must be unchanged
	assert.Equal(t, "orig-id", original.ID)
	assert.Equal(t, "old-sub-123", original.EntityID)
}

func TestCopyWithNilOverrides(t *testing.T) {
	ctx := context.Background()

	original := &EntityIntegrationMapping{
		ID:               "orig-id",
		EntityID:         "old-sub-123",
		EntityType:       types.IntegrationEntityTypeSubscription,
		ProviderType:     "paddle",
		ProviderEntityID: "paddle-sub-abc",
		EnvironmentID:    "env-1",
	}

	copied := original.CopyWith(ctx, nil)

	require.NotNil(t, copied)
	assert.NotEqual(t, original.ID, copied.ID)
	// EntityID kept when no override
	assert.Equal(t, original.EntityID, copied.EntityID)
}

func TestCopyWithNilReceiver(t *testing.T) {
	ctx := context.Background()
	var m *EntityIntegrationMapping
	result := m.CopyWith(ctx, nil)
	assert.Nil(t, result)
}
