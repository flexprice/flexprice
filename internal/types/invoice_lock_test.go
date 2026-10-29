package types

import (
	"context"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestInvoiceLockAdmission(t *testing.T) {
	transaction := new(int)
	ctx := context.WithValue(context.Background(), CtxTenantID, "tenant")
	ctx = SetEnvironmentID(ctx, "environment")
	ctx = context.WithValue(ctx, CtxDBTransaction, transaction)
	admitted, err := WithInvoiceLockAdmission(ctx, []string{"existing"})
	require.NoError(t, err)
	require.True(t, HasInvoiceLockAdmission(admitted))
	require.False(t, HasInvoiceLockAdmission(WithoutDBTransaction(admitted)))
	require.False(t, HasInvoiceLockAdmission(nil))
	require.NoError(t, CheckInvoiceLockAdmission(admitted, "existing"))
	require.True(t, ierr.IsVersionConflict(CheckInvoiceLockAdmission(admitted, "late")))
	require.NoError(t, AdmitCreatedInvoice(admitted, "created"))
	require.NoError(t, CheckInvoiceLockAdmission(admitted, "created"))
	_, err = WithInvoiceLockAdmission(admitted, []string{"late"})
	require.True(t, ierr.IsVersionConflict(err), "nested lifecycle operations cannot widen the existing lock set")

	for _, changed := range []context.Context{
		context.WithValue(admitted, CtxTenantID, "other"),
		SetEnvironmentID(admitted, "other"),
		context.WithValue(admitted, CtxDBTransaction, new(int)),
	} {
		require.True(t, ierr.IsInvalidOperation(CheckInvoiceLockAdmission(changed, "existing")))
		require.True(t, ierr.IsInvalidOperation(AdmitCreatedInvoice(changed, "created_elsewhere")))
	}
	require.NoError(t, CheckInvoiceLockAdmission(WithoutDBTransaction(admitted), "unrelated"))
	require.NoError(t, CheckInvoiceLockAdmission(ctx, "ordinary_invoice"))
	_, err = WithInvoiceLockAdmission(WithoutDBTransaction(ctx), nil)
	require.True(t, ierr.IsInvalidOperation(err))
}

func TestInvoiceLockAdmissionValidation(t *testing.T) {
	for _, ctx := range []context.Context{nil, context.Background(), SetEnvironmentID(context.Background(), "env")} {
		_, err := WithInvoiceLockAdmission(ctx, nil)
		require.Error(t, err)
	}
	ctx := context.WithValue(SetEnvironmentID(context.Background(), "env"), CtxTenantID, "tenant")
	ctx = context.WithValue(ctx, CtxDBTransaction, new(int))
	for _, ids := range [][]string{{""}, {" "}, make([]string, 501)} {
		_, err := WithInvoiceLockAdmission(ctx, ids)
		require.True(t, ierr.IsValidation(err))
	}
}
