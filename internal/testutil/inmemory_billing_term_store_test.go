package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func termIDs(rows []*billingterm.Term) []string {
	return lo.Map(rows, func(t *billingterm.Term, _ int) string { return t.ID })
}

func TestInMemoryBillingTermStore_ListAndArchive(t *testing.T) {
	ctx := types.SetUserID(
		types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1"),
		"user_1",
	)
	store := NewInMemoryBillingTermStore()

	term := func(id, ownerID, environmentID string) *billingterm.Term {
		return &billingterm.Term{
			ID:            id,
			OwnerType:     types.BillingTermOwnerCommitment,
			OwnerID:       ownerID,
			EnvironmentID: environmentID,
			BaseModel:     types.BaseModel{TenantID: "tenant_1", Status: types.StatusPublished},
		}
	}

	_, err := store.CreateMany(ctx, []*billingterm.Term{
		term("bterm_b", "cmt_1", "env_1"),
		term("bterm_a", "cmt_1", "env_1"),
		term("bterm_c", "cmt_2", "env_1"),
		term("bterm_d", "cmt_1", "env_2"),
	})
	require.NoError(t, err)

	listed, err := store.List(ctx, types.BillingTermOwnerCommitment, "cmt_1")
	require.NoError(t, err)
	require.Equal(t, []string{"bterm_a", "bterm_b"}, termIDs(listed))

	archived, err := store.Archive(ctx, types.BillingTermOwnerCommitment, "cmt_1")
	require.NoError(t, err)
	require.Equal(t, []string{"bterm_a", "bterm_b"}, termIDs(archived))
	for _, row := range archived {
		require.Equal(t, types.StatusArchived, row.Status)
		require.Equal(t, "user_1", row.UpdatedBy)
	}

	listed, err = store.List(ctx, types.BillingTermOwnerCommitment, "cmt_1")
	require.NoError(t, err)
	require.Empty(t, listed)

	again, err := store.Archive(ctx, types.BillingTermOwnerCommitment, "cmt_1")
	require.NoError(t, err)
	require.Empty(t, again)

	other, err := store.List(ctx, types.BillingTermOwnerCommitment, "cmt_2")
	require.NoError(t, err)
	require.Equal(t, []string{"bterm_c"}, termIDs(other))

	elsewhere, err := store.List(types.SetEnvironmentID(ctx, "env_2"), types.BillingTermOwnerCommitment, "cmt_1")
	require.NoError(t, err)
	require.Equal(t, []string{"bterm_d"}, termIDs(elsewhere))

	_, err = store.GetLinkedCustomerID(ctx, "bterm_a")
	require.Error(t, err)
	require.True(t, ierr.IsNotFound(err))
}
