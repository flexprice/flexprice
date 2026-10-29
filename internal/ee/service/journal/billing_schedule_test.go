package journal

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestScheduledPrincipalRequiresPerformanceObligationAllocation(t *testing.T) {
	ctx := testutil.SetupContext()
	for _, test := range []struct {
		name   string
		kind   types.InvoiceType
		mixed  bool
		voided bool
	}{
		{name: "separate invoice", kind: types.InvoiceTypeOneOff},
		{name: "mixed subscription invoice", kind: types.InvoiceTypeSubscription, mixed: true},
		{name: "voided subscription invoice", kind: types.InvoiceTypeSubscription, mixed: true, voided: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			line := &invoice.InvoiceLineItem{ID: "scheduled_line", BillingScheduleItemID: lo.ToPtr("schedule_item"),
				Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)}
			inv := &invoice.Invoice{ID: "scheduled_invoice", CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID),
				InvoiceType: test.kind, InvoiceStatus: types.InvoiceStatusFinalized, Currency: "usd",
				Subtotal: line.Amount, Total: line.Amount, FinalizedAt: lo.ToPtr(finalizedAt), LineItems: []*invoice.InvoiceLineItem{line}, BaseModel: types.GetDefaultBaseModel(ctx)}
			if test.mixed {
				inv.LineItems = append([]*invoice.InvoiceLineItem{{ID: "ordinary_line", SubscriptionLineItemID: lo.ToPtr("sli_1"), Amount: decimal.NewFromInt(40), BaseModel: types.GetDefaultBaseModel(ctx)}}, inv.LineItems...)
				inv.Subtotal = inv.Subtotal.Add(decimal.NewFromInt(40))
				inv.Total = inv.Subtotal
			}
			if test.voided {
				inv.InvoiceStatus, inv.VoidedAt = types.InvoiceStatusVoided, lo.ToPtr(voidedAt)
			}
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
			for range 2 {
				err := svc.PostInvoice(ctx, inv)
				require.True(t, ierr.IsInvalidOperation(err), "%v", err)
				require.ErrorContains(t, err, "performance obligation allocation")
			}
			entries, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
	for _, voided := range []bool{false, true} {
		name := "credit"
		if voided {
			name = "voided credit"
		}
		t.Run(name, func(t *testing.T) {
			inv := &invoice.Invoice{ID: "scheduled_invoice", CustomerID: "cust_1", Currency: "usd", BaseModel: types.GetDefaultBaseModel(ctx),
				LineItems: []*invoice.InvoiceLineItem{
					{ID: "ordinary_line", SubscriptionLineItemID: lo.ToPtr("sli_1"), Amount: decimal.NewFromInt(40), BaseModel: types.GetDefaultBaseModel(ctx)},
					{ID: "scheduled_line", BillingScheduleItemID: lo.ToPtr("schedule_item"), Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)},
				}}
			cn := &creditnote.CreditNote{ID: "scheduled_credit", InvoiceID: inv.ID, CustomerID: inv.CustomerID, SubscriptionID: lo.ToPtr(testSubscriptionID),
				CreditNoteStatus: types.CreditNoteStatusFinalized, CreditNoteType: types.CreditNoteTypeAdjustment, Currency: "usd",
				TotalAmount: decimal.NewFromInt(140), FinalizedAt: lo.ToPtr(finalizedAt), BaseModel: types.GetDefaultBaseModel(ctx),
				LineItems: []*creditnote.CreditNoteLineItem{
					{ID: "ordinary_credit", InvoiceLineItemID: "ordinary_line", Amount: decimal.NewFromInt(40), BaseModel: types.GetDefaultBaseModel(ctx)},
					{ID: "scheduled_credit_line", InvoiceLineItemID: "scheduled_line", Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)},
				}}
			if voided {
				cn.CreditNoteStatus, cn.VoidedAt = types.CreditNoteStatusVoided, lo.ToPtr(voidedAt)
			}
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
			for range 2 {
				err := svc.PostCreditNote(ctx, cn)
				require.True(t, ierr.IsInvalidOperation(err), "%v", err)
				require.ErrorContains(t, err, "performance obligation allocation")
			}
			entries, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
