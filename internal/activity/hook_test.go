package activity

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	entgo "entgo.io/ent"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type recordingSink struct {
	mu      sync.Mutex
	batches []Batch
}

func (s *recordingSink) Publish(b Batch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, b)
}

func (s *recordingSink) entries() []Entry {
	var out []Entry
	for _, b := range s.batches {
		out = append(out, b.Entries...)
	}
	return out
}

var testClient = ent.NewClient()

// mutate runs the hook around a write that returns result.
func mutate(t *testing.T, sink Sink, ctx context.Context, m entgo.Mutation, result entgo.Value) {
	t.Helper()
	next := entgo.MutateFunc(func(context.Context, entgo.Mutation) (entgo.Value, error) { return result, nil })
	_, err := Hook(Registrations(), sink)(next).Mutate(ctx, m)
	require.NoError(t, err)
}

func userCtx() context.Context {
	ctx := types.SetTenantID(context.Background(), "t_ctx")
	return types.SetActor(ctx, types.Actor{Type: types.ActorTypeUser, ID: "user_1"}, types.SourceDashboard)
}

func invoiceUpdate(id string) entgo.Mutation {
	return testClient.Invoice.UpdateOneID(id).SetPaymentStatus(types.PaymentStatusSucceeded).Mutation()
}

func invoiceRow(id, tenant string) *ent.Invoice {
	return &ent.Invoice{ID: id, TenantID: tenant, EnvironmentID: "env_1", CustomerID: "cust_1"}
}

func TestHook_DeclaredActionWins(t *testing.T) {
	sink := &recordingSink{}
	ctx := WithAction(userCtx(), "inv_1", InvoiceVoided)
	mutate(t, sink, ctx, invoiceUpdate("inv_1"), invoiceRow("inv_1", "t_row"))
	require.Equal(t, InvoiceVoided, sink.entries()[0].Action)
}

func TestHook_DeclaredActionOtherIDIgnored(t *testing.T) {
	sink := &recordingSink{}
	ctx := WithAction(userCtx(), "inv_A", InvoiceVoided)
	mutate(t, sink, ctx, invoiceUpdate("inv_B"), invoiceRow("inv_B", "t_row"))
	require.Equal(t, Action("invoice.updated"), sink.entries()[0].Action)
}

func TestHook_CountOnlyWriteNotCaptured(t *testing.T) {
	sink := &recordingSink{}
	mutate(t, sink, userCtx(), testClient.Invoice.Update().SetPaymentStatus(types.PaymentStatusSucceeded).Mutation(), 3)
	require.Empty(t, sink.entries())
}

func TestHook_SnapshotPanicRecovered(t *testing.T) {
	reg, err := NewRegistry(Registration{
		EntityType: types.SystemEntityTypeInvoice, EntName: "Invoice",
		Snapshot: snapshotOf(func(*ent.Invoice) *invoice.Invoice { panic("boom") }),
	})
	require.NoError(t, err)
	sink := &recordingSink{}
	next := entgo.MutateFunc(func(context.Context, entgo.Mutation) (entgo.Value, error) { return invoiceRow("inv_1", "t"), nil })
	v, err := Hook(reg, sink)(next).Mutate(userCtx(), invoiceUpdate("inv_1"))
	require.NoError(t, err)
	require.NotNil(t, v)
	require.Empty(t, sink.entries())
}

func TestHook_TenantFromRow(t *testing.T) {
	sink := &recordingSink{}
	mutate(t, sink, userCtx(), invoiceUpdate("inv_1"), invoiceRow("inv_1", "t_row"))
	require.Equal(t, "t_row", sink.batches[0].TenantID)
	require.Equal(t, "env_1", sink.batches[0].EnvironmentID)

	sink = &recordingSink{}
	mutate(t, sink, userCtx(), invoiceUpdate("inv_1"), &ent.Invoice{ID: "inv_1"})
	require.Equal(t, "t_ctx", sink.batches[0].TenantID, "ctx is the fallback")

	sink = &recordingSink{}
	mutate(t, sink, types.SetActor(context.Background(), types.Actor{}, types.SourceAPI), invoiceUpdate("inv_1"), &ent.Invoice{ID: "inv_1"})
	require.Empty(t, sink.entries(), "no tenant anywhere skips the entry")
}

func TestHook_UnknownActor(t *testing.T) {
	sink := &recordingSink{}
	mutate(t, sink, context.Background(), invoiceUpdate("inv_1"), invoiceRow("inv_1", "t"))
	require.Equal(t, types.Actor{Type: types.ActorTypeSystem, ID: "unknown"}, sink.entries()[0].Actor)
}

func TestHook_RedactAndDrop(t *testing.T) {
	sink := &recordingSink{}
	pay := &ent.Payment{ID: "pay_1", TenantID: "t", EnvironmentID: "e", GatewayMetadata: map[string]string{"card": "4242"}}
	mutate(t, sink, userCtx(), testClient.Payment.Update().SetPaymentStatus("SUCCEEDED").Mutation(), []*ent.Payment{pay})
	var state map[string]any
	require.NoError(t, json.Unmarshal(sink.entries()[0].State, &state))
	require.Equal(t, "[redacted]", state["gateway_metadata"])

	sink = &recordingSink{}
	mutate(t, sink, userCtx(), invoiceUpdate("inv_1"), invoiceRow("inv_1", "t"))
	state = map[string]any{}
	require.NoError(t, json.Unmarshal(sink.entries()[0].State, &state))
	require.NotContains(t, state, "version")
	require.Equal(t, "cust_1", state["customer_id"])
}
