package ent

import (
	"context"
	"errors"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestInvoiceLockAdmissionPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx, cancel := context.WithTimeout(billingTestContext("admission_tenant", "admission_env"), 10*time.Second)
	defer cancel()
	existing, late, created := newBillingTestInvoice(ctx), newBillingTestInvoice(ctx), newBillingTestInvoice(ctx)
	require.NoError(t, f.invs.Create(ctx, existing))
	require.NoError(t, f.invs.Create(ctx, late))
	rollback := errors.New("rollback lifecycle")

	err := f.client.WithTx(ctx, func(blocker context.Context) error {
		if _, err := f.invs.GetForUpdate(blocker, late.ID); err != nil {
			return err
		}
		return f.client.WithTx(ctx, func(tx context.Context) error {
			if _, err := f.invs.GetForUpdateNowait(tx, existing.ID); err != nil {
				return err
			}
			admitted, err := types.WithInvoiceLockAdmission(tx, []string{existing.ID})
			if err != nil {
				return err
			}
			_, err = f.invs.GetForUpdate(admitted, late.ID)
			require.True(t, ierr.IsVersionConflict(err), "late lock must fail before waiting on the other transaction: %v", err)
			require.True(t, ierr.IsVersionConflict(f.invs.Update(admitted, late)))
			require.True(t, ierr.IsVersionConflict(f.invs.UpdateDraftInvoicePeriod(admitted, late.ID, time.Now().UTC(), "SUBSCRIPTION_CYCLE", "late_key")))
			require.True(t, ierr.IsVersionConflict(f.invs.Delete(admitted, late.ID)))
			require.True(t, ierr.IsVersionConflict(f.invs.AddLineItems(admitted, late.ID, nil)))
			require.True(t, ierr.IsVersionConflict(f.invs.RemoveLineItems(admitted, late.ID, []string{"late_item"})))
			require.NoError(t, f.invs.Update(admitted, existing))
			require.NoError(t, f.invs.CreateWithLineItems(admitted, created))
			_, err = f.invs.GetForUpdate(admitted, created.ID)
			require.NoError(t, err, "this transaction may update its own inserted invoice")
			require.NoError(t, f.invs.Update(admitted, created))
			return rollback
		})
	})
	require.ErrorIs(t, err, rollback)
	_, err = f.invs.Get(ctx, created.ID)
	require.True(t, ierr.IsNotFound(err), "%+v", err)
}

func TestInvoiceReplacementLookupPostgres(t *testing.T) {
	f := newBillingPostgresFixture(t)
	ctx := billingTestContext("replacement_tenant", "replacement_env")
	original, replacement := newBillingTestInvoice(ctx), newBillingTestInvoice(ctx)
	original.InvoiceStatus = types.InvoiceStatusVoided
	require.NoError(t, f.invs.Create(ctx, original))
	require.NoError(t, f.invs.Create(ctx, replacement))
	original.RecalculatedInvoiceID = &replacement.ID
	require.NoError(t, f.invs.Update(ctx, original))
	got, err := f.invs.GetByRecalculatedInvoiceID(ctx, replacement.ID)
	require.NoError(t, err)
	require.Equal(t, original.ID, got.ID)
	for _, foreign := range []context.Context{billingTestContext("other", "replacement_env"), billingTestContext("replacement_tenant", "other")} {
		_, err := f.invs.GetByRecalculatedInvoiceID(foreign, replacement.ID)
		require.True(t, ierr.IsNotFound(err), "%v", err)
	}
	require.NoError(t, f.invs.Delete(ctx, original.ID))
	got, err = f.invs.GetByRecalculatedInvoiceID(ctx, replacement.ID)
	require.NoError(t, err, "archival must not reopen automatic overdue collection on a correction")
	require.Equal(t, original.ID, got.ID)
}
