package activity

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCollapseByEntity(t *testing.T) {
	reg := Registrations()
	entry := func(action Action, state string) Entry {
		return Entry{EntityType: types.SystemEntityTypeInvoice, EntityID: "inv_1", Action: action, State: []byte(state), TenantID: "t", EnvironmentID: "e"}
	}
	tests := []struct {
		name       string
		in         []Entry
		wantAction Action
		wantState  string
	}{
		{"created then updated", []Entry{entry("invoice.created", "1"), entry("invoice.updated", "2")}, "invoice.created", "2"},
		{"updated then declared", []Entry{entry("invoice.updated", "1"), entry(InvoiceFinalized, "2")}, InvoiceFinalized, "2"},
		{"created then deleted", []Entry{entry("invoice.created", "1"), entry("invoice.deleted", "2")}, "invoice.deleted", "2"},
		{"declared priority beats later declared", []Entry{entry(InvoiceVoided, "1"), entry(InvoiceRefunded, "2")}, InvoiceVoided, "2"},
		{"created then declared finalized", []Entry{entry("invoice.created", "1"), entry(InvoiceFinalized, "2")}, "invoice.created", "2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collapseByEntity(reg, tt.in)
			require.Len(t, got, 1)
			require.Equal(t, tt.wantAction, got[0].Action)
			require.Equal(t, tt.wantState, string(got[0].State))
		})
	}
}

func txCtx() context.Context {
	return types.WithTxStore(types.WithPostCommitHooks(userCtx()))
}

func TestCollector_RollbackDrops(t *testing.T) {
	sink := &recordingSink{}
	ctx := txCtx()
	mutate(t, sink, ctx, invoiceUpdate("inv_1"), invoiceRow("inv_1", "t"))
	types.DiscardPostCommitHooks(ctx)
	require.Empty(t, sink.batches)
}

func TestCollector_NestedTxSingleFlush(t *testing.T) {
	sink := &recordingSink{}
	ctx := txCtx()
	mutate(t, sink, ctx, invoiceUpdate("inv_1"), invoiceRow("inv_1", "t"))
	mutate(t, sink, ctx, invoiceUpdate("inv_2"), invoiceRow("inv_2", "t"))
	require.Empty(t, sink.batches, "nothing before commit")
	types.RunPostCommitHooks(ctx)
	require.Len(t, sink.batches, 1)
	require.Len(t, sink.batches[0].Entries, 2)
}

func TestCollector_OutsideTxImmediate(t *testing.T) {
	sink := &recordingSink{}
	mutate(t, sink, userCtx(), invoiceUpdate("inv_1"), invoiceRow("inv_1", "t"))
	require.Len(t, sink.batches, 1)
	require.False(t, sink.batches[0].CommittedAt.IsZero())
}
