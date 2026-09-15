package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAssociationEnvironmentID = "env_addon_assoc_store_test"

func associationStoreTestContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = types.SetEnvironmentID(ctx, testAssociationEnvironmentID)
	return ctx
}

func newAssociation(ctx context.Context, id, addonID string) *addonassociation.AddonAssociation {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	return &addonassociation.AddonAssociation{
		ID:            id,
		EnvironmentID: testAssociationEnvironmentID,
		EntityID:      "sub_1",
		EntityType:    types.AddonAssociationEntityTypeSubscription,
		AddonID:       addonID,
		StartDate:     &start,
		AddonStatus:   types.AddonStatusActive,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

func TestAddonAssociationStore_CreateBulk(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	require.NoError(t, store.CreateBulk(ctx, []*addonassociation.AddonAssociation{
		newAssociation(ctx, "assoc_1", "addon_a"),
		newAssociation(ctx, "assoc_2", "addon_b"),
	}))

	for _, id := range []string{"assoc_1", "assoc_2"} {
		stored, err := store.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, types.AddonStatusActive, stored.AddonStatus)
	}

	require.NoError(t, store.CreateBulk(ctx, nil))
}

func TestAddonAssociationStore_CancelBulk(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	require.NoError(t, store.CreateBulk(ctx, []*addonassociation.AddonAssociation{
		newAssociation(ctx, "assoc_1", "addon_a"),
		newAssociation(ctx, "assoc_2", "addon_b"),
		newAssociation(ctx, "assoc_3", "addon_c"),
	}))

	effectiveAt := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.CancelBulk(ctx, []string{"assoc_1", "assoc_2"}, effectiveAt, "batch"))

	for _, id := range []string{"assoc_1", "assoc_2"} {
		stored, err := store.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, types.AddonStatusCancelled, stored.AddonStatus)
		assert.Equal(t, "batch", stored.CancellationReason)
		require.NotNil(t, stored.EndDate)
		assert.True(t, stored.EndDate.Equal(effectiveAt))
		require.NotNil(t, stored.CancelledAt)
		assert.True(t, stored.CancelledAt.Equal(effectiveAt))
		assert.Equal(t, types.StatusPublished, stored.Status)
	}

	untouched, err := store.GetByID(ctx, "assoc_3")
	require.NoError(t, err)
	assert.Equal(t, types.AddonStatusActive, untouched.AddonStatus)

	require.NoError(t, store.CancelBulk(ctx, nil, effectiveAt, "batch"))
}

func TestAddonAssociationStore_ActivateBulk(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	pending := newAssociation(ctx, "assoc_1", "addon_a")
	pending.AddonStatus = types.AddonStatusPending
	require.NoError(t, store.Create(ctx, pending))

	require.NoError(t, store.ActivateBulk(ctx, []string{"assoc_1"}))

	stored, err := store.GetByID(ctx, "assoc_1")
	require.NoError(t, err)
	assert.Equal(t, types.AddonStatusActive, stored.AddonStatus)
	assert.Nil(t, stored.EndDate, "activation must not stamp a lifecycle end")

	require.NoError(t, store.ActivateBulk(ctx, nil))
}

// A never-activated association is archived, so it drops out of published reads without
// claiming the customer ever held the addon.
func TestAddonAssociationStore_DeleteBulk(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	pending := newAssociation(ctx, "assoc_1", "addon_a")
	pending.AddonStatus = types.AddonStatusPending
	require.NoError(t, store.Create(ctx, pending))

	require.NoError(t, store.DeleteBulk(ctx, []string{"assoc_1"}))

	stored, err := store.GetByID(ctx, "assoc_1")
	require.NoError(t, err)
	assert.Equal(t, types.StatusArchived, stored.Status)
	assert.Equal(t, types.AddonStatusPending, stored.AddonStatus, "archiving is not a cancellation")
	assert.Nil(t, stored.EndDate)

	require.NoError(t, store.DeleteBulk(ctx, nil))
}

// Unknown ids are skipped rather than erroring, matching a predicate update.
func TestAddonAssociationStore_BulkTransitionsSkipUnknownIDs(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	require.NoError(t, store.Create(ctx, newAssociation(ctx, "assoc_1", "addon_a")))

	effectiveAt := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.CancelBulk(ctx, []string{"assoc_1", "assoc_missing"}, effectiveAt, ""))
	require.NoError(t, store.ActivateBulk(ctx, []string{"assoc_missing"}))
	require.NoError(t, store.DeleteBulk(ctx, []string{"assoc_missing"}))

	stored, err := store.GetByID(ctx, "assoc_1")
	require.NoError(t, err)
	assert.Equal(t, types.AddonStatusCancelled, stored.AddonStatus)
	assert.Empty(t, stored.CancellationReason, "an empty reason leaves the field alone")
}

func TestAddonAssociationStore_GetByIDs(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	require.NoError(t, store.CreateBulk(ctx, []*addonassociation.AddonAssociation{
		newAssociation(ctx, "assoc_1", "addon_a"),
		newAssociation(ctx, "assoc_2", "addon_b"),
		newAssociation(ctx, "assoc_3", "addon_c"),
	}))

	found, err := store.GetByIDs(ctx, []string{"assoc_1", "assoc_3"})
	require.NoError(t, err)
	ids := lo.Map(found, func(a *addonassociation.AddonAssociation, _ int) string { return a.ID })
	assert.ElementsMatch(t, []string{"assoc_1", "assoc_3"}, ids)

	partial, err := store.GetByIDs(ctx, []string{"assoc_1", "assoc_missing"})
	require.NoError(t, err)
	require.Len(t, partial, 1)
	assert.Equal(t, "assoc_1", partial[0].ID)

	empty, err := store.GetByIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestAddonAssociationStore_AssociationIDsFilter(t *testing.T) {
	ctx := associationStoreTestContext()
	store := NewInMemoryAddonAssociationStore()

	require.NoError(t, store.CreateBulk(ctx, []*addonassociation.AddonAssociation{
		newAssociation(ctx, "assoc_1", "addon_a"),
		newAssociation(ctx, "assoc_2", "addon_b"),
	}))

	filter := types.NewNoLimitAddonAssociationFilter()
	filter.AssociationIDs = []string{"assoc_2"}

	found, err := store.List(ctx, filter)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "assoc_2", found[0].ID)

	count, err := store.Count(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestAddonAssociationFilter_RejectsEmptyAssociationID(t *testing.T) {
	filter := types.NewAddonAssociationFilter()
	filter.AssociationIDs = []string{""}
	assert.Error(t, filter.Validate())

	filter.AssociationIDs = []string{"assoc_1"}
	assert.NoError(t, filter.Validate())
}
