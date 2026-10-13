package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopRevenueFacts satisfies interfaces.RevenueService without touching a DB.
type noopRevenueFacts struct{}

func (noopRevenueFacts) RollupSubscription(context.Context, string) error { return nil }
func (noopRevenueFacts) RollupDirty(context.Context, types.RollupDirtyRequest) (types.RollupDirtyResult, error) {
	return types.RollupDirtyResult{}, nil
}
func (noopRevenueFacts) FinalizeSubscriptionPeriod(context.Context, string) error { return nil }
func (noopRevenueFacts) RevertInvoiceFacts(context.Context, string) error         { return nil }
func (noopRevenueFacts) ReconcileBookedInvoices(context.Context, time.Time) (int, int, int, error) {
	return 0, 0, 0, nil
}
func (noopRevenueFacts) GetRevenueAnalytics(context.Context, *dto.RevenueAnalyticsRequest) (*dto.RevenueAnalyticsResponse, error) {
	return nil, nil
}

// TestAsyncRevenueFactsUpdate_DropsCallerTransaction: three of the four hook
// call sites fire from inside an open transaction. Carrying that transaction
// into a goroutine that outlives the request puts two goroutines on one
// Postgres connection and keeps using it after the commit — the wire protocol
// desynchronizes and the poisoned connection goes back to the pool, so the
// next unrelated request fails instead. The detached work must run on its own
// connection, while keeping the tenant scope it needs.
func TestAsyncRevenueFactsUpdate_DropsCallerTransaction(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = types.SetEnvironmentID(ctx, "env_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	require.NotNil(t, ctx.Value(types.CtxDBTransaction), "the caller is inside a transaction")

	seen := make(chan context.Context, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}

	asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
		func(asyncCtx context.Context, _ interfaces.RevenueService) error {
			seen <- asyncCtx
			return nil
		})

	select {
	case got := <-seen:
		assert.Nil(t, got.Value(types.CtxDBTransaction),
			"detached work must not reuse the request's transaction")
		assert.Equal(t, "tenant_hook", types.GetTenantID(got), "tenant scope survives")
		assert.Equal(t, "env_hook", types.GetEnvironmentID(got), "environment scope survives")
	case <-time.After(5 * time.Second):
		t.Fatal("the detached update never ran")
	}
}

// TestAsyncRevenueFactsUpdate_WaitsForCommit: the update reads the invoice the
// caller's transaction is still writing. Fired immediately it would read from
// another connection, see nothing, and leave the flip to the drift sweep — so
// inside a transaction it queues and runs only once the commit publishes the
// write. A rollback never runs it at all.
func TestAsyncRevenueFactsUpdate_WaitsForCommit(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = types.SetEnvironmentID(ctx, "env_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	ctx = types.WithPostCommitHooks(ctx)

	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}
	fire := func() {
		asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
			func(context.Context, interfaces.RevenueService) error {
				ran <- struct{}{}
				return nil
			})
	}

	fire()
	select {
	case <-ran:
		t.Fatal("the update ran while the transaction was still open")
	case <-time.After(150 * time.Millisecond):
	}

	types.RunPostCommitHooks(ctx)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the update never ran after the commit")
	}

	// Once the transaction has finished, registration is closed: a late
	// caller must run inline rather than queue onto a list nobody drains.
	fire()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("a registration after the commit was silently dropped")
	}
}

// TestAsyncRevenueFactsUpdate_DroppedOnRollback: queued work belongs to the
// transaction that produced it. A rollback means the invoice it would read was
// never written, so the work is discarded rather than run.
func TestAsyncRevenueFactsUpdate_DroppedOnRollback(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ctx = context.WithValue(ctx, types.CtxDBTransaction, &ent.Tx{})
	ctx = types.WithPostCommitHooks(ctx)

	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}
	fire := func() {
		asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
			func(context.Context, interfaces.RevenueService) error {
				ran <- struct{}{}
				return nil
			})
	}

	fire()
	types.DiscardPostCommitHooks(ctx)
	select {
	case <-ran:
		t.Fatal("work queued by a rolled-back transaction must not run")
	case <-time.After(150 * time.Millisecond):
	}

	// Registration is closed now, so a later caller runs inline instead of
	// queueing into a list that will never be drained.
	fire()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("a registration after the rollback was silently dropped")
	}
}

// TestAsyncRevenueFactsUpdate_RunsImmediatelyOutsideTransaction: the
// payment-processor path calls this with no transaction open, and must not
// wait for a commit that will never come.
func TestAsyncRevenueFactsUpdate_RunsImmediatelyOutsideTransaction(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_hook")
	ran := make(chan struct{}, 1)
	params := ServiceParams{Logger: logger.NewNoopLogger(), RevenueFacts: noopRevenueFacts{}}

	asyncRevenueFactsUpdate(ctx, params, "test", "inv_hook",
		func(context.Context, interfaces.RevenueService) error {
			ran <- struct{}{}
			return nil
		})

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the update never ran")
	}
}
