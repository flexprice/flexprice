package billingterms

import (
	"context"
	"errors"
	"fmt"
	"testing"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func assignedVoidSource(t *testing.T) (context.Context, Service, *memoryExecution, *domain.ScheduleItem, InvoiceVoidRequest) {
	t.Helper()
	ctx, core, db := newCore(t)
	state, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}})
	require.NoError(t, err)
	item := state.Items()[0]
	claimMemory(db, item, types.InvoiceStatusFinalized)
	item = db.items[item.ID]
	return ctx, core, db, item, InvoiceVoidRequest{InvoiceID: *item.InvoiceID, Sources: []InvoiceSource{{ItemID: item.ID, InvoiceLineItemID: *item.InvoiceLineItemID}}}
}

func voidMemoryInvoice(db *memoryExecution, invoiceID string) {
	for id, binding := range db.bindings {
		if binding.InvoiceID == invoiceID {
			binding.InvoiceStatus = types.InvoiceStatusVoided
			db.bindings[id] = binding
		}
	}
}

func TestInvoiceVoidRequiresCallerTransaction(t *testing.T) {
	ctx, core, db, _, req := assignedVoidSource(t)
	called := false
	err := core.WithInvoiceVoid(ctx, req, func(context.Context) error { called = true; return nil })
	require.Error(t, err)
	require.False(t, called)
	err = db.WithTx(ctx, func(tx context.Context) error { return core.WithInvoiceVoid(tx, req, nil) })
	require.Error(t, err)
}

func TestInvoiceVoidPreservesHistoryAndSkipsRepeatedCallback(t *testing.T) {
	ctx, core, db, item, req := assignedVoidSource(t)
	db.owner.CanEdit = false
	db.owner.CanCollect = false
	db.owner.LifecycleKey = "terminated"
	for id, rule := range db.rules {
		db.rules[id] = domain.NewRuleBuilder(rule).WithStatus(types.StatusArchived).Build()
	}
	db.items[item.ID] = domain.NewScheduleItemBuilder(item).WithStatus(types.StatusArchived).Build()
	called := 0
	for range 2 {
		err := db.WithTx(ctx, func(tx context.Context) error {
			return core.WithInvoiceVoid(tx, req, func(context.Context) error { called++; voidMemoryInvoice(db, req.InvoiceID); return nil })
		})
		require.NoError(t, err)
	}
	require.Equal(t, 1, called)
	got := db.items[item.ID]
	require.Equal(t, types.BillingScheduleCanceled, got.ScheduleStatus)
	require.Equal(t, types.StatusArchived, got.Status)
	require.Equal(t, types.BillingScheduleInvoiceVoided, *got.CancellationReason)
	require.True(t, got.Amount.Equal(item.Amount))
	require.Equal(t, item.BillOn, got.BillOn)
	require.Equal(t, item.BillingTermID, got.BillingTermID)
	require.Equal(t, item.InvoiceID, got.InvoiceID)
	require.Equal(t, item.InvoiceLineItemID, got.InvoiceLineItemID)
}

func TestInvoiceVoidRejectsIncompleteOrConflictingSources(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*InvoiceVoidRequest, *memoryExecution, *domain.ScheduleItem)
	}{
		{"missing forward source", func(r *InvoiceVoidRequest, _ *memoryExecution, _ *domain.ScheduleItem) { r.Sources = nil }},
		{"unknown source", func(r *InvoiceVoidRequest, _ *memoryExecution, _ *domain.ScheduleItem) {
			r.Sources[0].ItemID = "unknown"
		}},
		{"wrong line", func(r *InvoiceVoidRequest, _ *memoryExecution, _ *domain.ScheduleItem) {
			r.Sources[0].InvoiceLineItemID = "other_line"
		}},
		{"duplicate source", func(r *InvoiceVoidRequest, _ *memoryExecution, _ *domain.ScheduleItem) {
			r.Sources = append(r.Sources, r.Sources[0])
		}},
		{"wrong invoice", func(r *InvoiceVoidRequest, _ *memoryExecution, _ *domain.ScheduleItem) { r.InvoiceID = "other_invoice" }},
		{"foreign environment", func(_ *InvoiceVoidRequest, db *memoryExecution, i *domain.ScheduleItem) {
			copy := domain.NewScheduleItemBuilder(i).Build()
			copy.EnvironmentID = "foreign"
			db.items[i.ID] = copy
		}},
		{"incomplete binding audit", func(_ *InvoiceVoidRequest, db *memoryExecution, _ *domain.ScheduleItem) { db.missingBinding = true }},
		{"wrong binding audit", func(_ *InvoiceVoidRequest, db *memoryExecution, _ *domain.ScheduleItem) { db.wrongBinding = true }},
		{"live invoice canceled source", func(_ *InvoiceVoidRequest, db *memoryExecution, i *domain.ScheduleItem) {
			db.items[i.ID] = domain.NewScheduleItemBuilder(i).WithStatus(types.StatusArchived).WithScheduleStatus(types.BillingScheduleCanceled).WithCancellationReason(ptr(types.BillingScheduleInvoiceVoided)).Build()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, core, db, item, req := assignedVoidSource(t)
			test.change(&req, db, item)
			called := false
			err := db.WithTx(ctx, func(tx context.Context) error {
				return core.WithInvoiceVoid(tx, req, func(context.Context) error { called = true; return nil })
			})
			require.Error(t, err)
			require.False(t, called)
		})
	}
}

func TestInvoiceVoidCallbackMustPersistVoidAndPropagateFailure(t *testing.T) {
	ctx, core, db, item, req := assignedVoidSource(t)
	err := db.WithTx(ctx, func(tx context.Context) error {
		return core.WithInvoiceVoid(tx, req, func(context.Context) error { return nil })
	})
	require.Error(t, err)
	require.Equal(t, types.BillingScheduleInvoiced, db.items[item.ID].ScheduleStatus)
	failure := errors.New("void failed")
	err = db.WithTx(ctx, func(tx context.Context) error {
		return core.WithInvoiceVoid(tx, req, func(context.Context) error { return failure })
	})
	require.ErrorIs(t, err, failure)
	require.Equal(t, types.BillingScheduleInvoiced, db.items[item.ID].ScheduleStatus)
	voidMemoryInvoice(db, req.InvoiceID)
	called := false
	err = db.WithTx(ctx, func(tx context.Context) error {
		return core.WithInvoiceVoid(tx, req, func(context.Context) error { called = true; return nil })
	})
	require.NoError(t, err, "a completed invoice void can finish source bookkeeping without replaying the void")
	require.False(t, called)
	require.Equal(t, types.BillingScheduleCanceled, db.items[item.ID].ScheduleStatus)
}

type groupedVoidExecution struct {
	*memoryExecution
	locks []domain.OwnerRef
}

func (m *groupedVoidExecution) LockOwner(ctx context.Context, owner domain.OwnerRef) error {
	if !m.InTransaction(ctx) {
		return errors.New("missing transaction")
	}
	m.locks = append(m.locks, owner)
	return nil
}

func (m *groupedVoidExecution) BindingStates(ctx context.Context, items []*domain.ScheduleItem) ([]domain.BindingState, error) {
	if len(items) > MaxItems {
		return nil, errors.New("unbounded invoice binding read")
	}
	return m.memoryExecution.BindingStates(ctx, items)
}

func TestInvoiceVoidGroupedSourcesUseBoundedReadsAndSortedOwnerLocks(t *testing.T) {
	ctx, _, db := newCore(t)
	store := &groupedVoidExecution{memoryExecution: db}
	resolver, err := NewOwnerResolver(&memoryOwner{db: db})
	require.NoError(t, err)
	core := NewService(&memoryRules{db}, &memoryItems{db}, store, resolver)
	req := InvoiceVoidRequest{InvoiceID: "grouped_invoice"}
	for index := range 601 {
		id, lineID := fmt.Sprintf("item_%04d", index), fmt.Sprintf("line_%04d", index)
		owner := domain.OwnerRef{Type: "future_fee", ID: "owner_b"}
		if index%2 == 1 {
			owner.ID = "owner_a"
		}
		item := &domain.ScheduleItem{ID: id, OwnerType: owner.Type, OwnerID: owner.ID, Amount: decimal.NewFromInt(1),
			ScheduleStatus: types.BillingScheduleInvoiced, InvoiceID: &req.InvoiceID, InvoiceLineItemID: &lineID,
			EnvironmentID: db.owner.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx)}
		item.CreatedAt = db.stamp()
		db.items[id] = item
		db.bindings[id] = domain.BindingState{ItemID: id, InvoiceID: req.InvoiceID, InvoiceLineItemID: lineID, TenantID: db.owner.TenantID, EnvironmentID: db.owner.EnvironmentID, InvoiceStatus: types.InvoiceStatusFinalized}
		req.Sources = append(req.Sources, InvoiceSource{ItemID: id, InvoiceLineItemID: lineID})
	}
	err = store.WithTx(ctx, func(tx context.Context) error {
		return core.WithInvoiceVoid(tx, req, func(context.Context) error { voidMemoryInvoice(db, req.InvoiceID); return nil })
	})
	require.NoError(t, err)
	require.Equal(t, []domain.OwnerRef{{Type: "future_fee", ID: "owner_a"}, {Type: "future_fee", ID: "owner_b"}}, store.locks)
	for _, item := range db.items {
		require.Equal(t, types.BillingScheduleCanceled, item.ScheduleStatus)
	}
}
