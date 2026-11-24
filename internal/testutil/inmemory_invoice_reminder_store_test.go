package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/types"
)

func TestInMemoryInvoiceReminderStore_KeepsItsOwnCopies(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_a"), "env_a")
	store := NewInMemoryInvoiceReminderStore(NewInMemoryInvoiceStore())
	approved := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	approvedAt := approved
	r := collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{ID: "irem_1", InvoiceID: "inv_1", BaseModel: types.GetDefaultBaseModel(ctx)}).
		WithStatus(types.ReminderStatusApproved, "", &approvedAt).
		WithMetadata(map[string]interface{}{"original_body": "kept"}).
		WithText(types.ReminderToneFriendly, "s", "b").Build()
	require.NoError(t, store.Create(ctx, r))

	*r.ApprovedAt = approved.Add(time.Hour)
	r.Metadata["original_body"] = "changed by the caller"
	got := store.All(ctx)
	require.Len(t, got, 1)
	require.True(t, got[0].ApprovedAt.Equal(approved), "the stored approval time is not shared with the caller")
	require.Equal(t, "kept", got[0].Metadata["original_body"])

	*got[0].ApprovedAt = approved.Add(2 * time.Hour)
	require.True(t, store.All(ctx)[0].ApprovedAt.Equal(approved), "a read copy does not write through either")
}

func TestInMemoryInvoiceReminderStore_GetRowsWithoutIDsReturnsNothing(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_a"), "env_a")
	store := NewInMemoryInvoiceReminderStore(NewInMemoryInvoiceStore())
	rows, err := store.GetRows(ctx, time.Now(), nil)
	require.NoError(t, err)
	require.Empty(t, rows, "like production, no ids means no rows, not every invoice")
}
