package service

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type billingScheduleTestProvisioner struct {
	calls int
	err   error
}

func (p *billingScheduleTestProvisioner) Ensure(ctx context.Context) error {
	if ctx.Value(types.CtxDBTransaction) != nil {
		return errors.New("provisioning happened inside transaction")
	}
	p.calls++
	return p.err
}

func TestBillingSchedulePreparationScopeAndTransactions(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	provisioner := &billingScheduleTestProvisioner{}
	ready, err := prepareBillingSchedule(ctx, provisioner)
	require.NoError(t, err)
	tx := context.WithValue(ready, types.CtxDBTransaction, &ent.Tx{})
	_, err = prepareBillingSchedule(tx, provisioner)
	require.NoError(t, err)
	require.Equal(t, 1, provisioner.calls)
	_, err = prepareBillingSchedule(types.SetEnvironmentID(tx, "other"), provisioner)
	require.Error(t, err, "readiness cannot transfer to another environment in a transaction")
	_, err = prepareBillingSchedule(context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{}), provisioner)
	require.Error(t, err)
	require.Equal(t, 1, provisioner.calls)
	provisioner.err = errors.New("worker unavailable")
	_, err = prepareBillingSchedule(ctx, provisioner)
	require.ErrorContains(t, err, "worker unavailable")
}
