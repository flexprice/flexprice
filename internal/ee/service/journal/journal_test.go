package journal

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/domain/payment"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestPost(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
	oct17 := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

	line := func(account types.LedgerAccountKey, direction types.JournalDirection) *domainJournal.Line {
		return domainJournal.NewLineBuilder(nil).WithAccount(account).WithDirection(direction).WithAmount(decimal.NewFromInt(100)).Build()
	}
	entry := func(eventType types.JournalEventType, sourceID string, debit, credit types.LedgerAccountKey) *domainJournal.Entry {
		return domainJournal.NewBuilder(nil).
			WithEventType(eventType).
			WithSource(types.RevRecEntityTypeInvoice, sourceID).
			WithBusinessDate(oct17).
			WithCustomerID("cust_1").
			WithCurrency("usd").
			WithLines([]*domainJournal.Line{line(debit, types.JournalDirectionDebit), line(credit, types.JournalDirectionCredit)}).
			Build()
	}
	invoice := func(sourceID string) *domainJournal.Entry {
		return entry(types.JournalEventInvoiceFinalized, sourceID, types.LedgerAccountReceivable, types.LedgerAccountDeferredRevenue)
	}
	recognition := func() *domainJournal.Entry {
		return entry(types.JournalEventRevenueEarned, "pob_1", types.LedgerAccountUnbilledReceivable, types.LedgerAccountRevenue)
	}

	tests := []struct {
		name     string
		entries  []*domainJournal.Entry
		wantKeys []string
	}{
		{name: "billing event posts with its key", entries: []*domainJournal.Entry{invoice("inv_1")}, wantKeys: []string{"invoice_finalized:inv_1"}},
		{name: "same billing event posts once", entries: []*domainJournal.Entry{invoice("inv_1"), invoice("inv_1")}, wantKeys: []string{"invoice_finalized:inv_1"}},
		{name: "recognition posts every time without a key", entries: []*domainJournal.Entry{recognition(), recognition()}, wantKeys: []string{"", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := testutil.NewInMemoryJournalStore()
			periods := accountingperiodsvc.NewAccountingPeriodService(testutil.NewInMemoryAccountingPeriodStore())
			svc := &journalService{journalRepo: store, periodService: periods}

			var ids []string
			for _, e := range tt.entries {
				id, err := svc.post(ctx, e)
				require.NoError(t, err)
				ids = append(ids, id)
			}
			require.Len(t, lo.Uniq(ids), len(tt.wantKeys), "a repeated billing event returns the posted entry's ID")

			october, err := periods.PeriodFor(ctx, oct17)
			require.NoError(t, err)
			saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Len(t, saved, len(tt.wantKeys))
			for i, e := range saved {
				require.Equal(t, tt.wantKeys[i], e.IdempotencyKey())
				require.Equal(t, october.ID(), e.AccountingPeriodID())
				require.NotEmpty(t, e.ID())
				require.False(t, e.CreatedAt().IsZero())
				for _, l := range e.Lines() {
					require.NotEmpty(t, l.ID())
				}
			}
		})
	}

	t.Run("unbalanced entry is rejected", func(t *testing.T) {
		svc := &journalService{
			journalRepo:   testutil.NewInMemoryJournalStore(),
			periodService: accountingperiodsvc.NewAccountingPeriodService(testutil.NewInMemoryAccountingPeriodStore()),
		}
		e := domainJournal.NewBuilder(invoice("inv_2")).
			WithLines([]*domainJournal.Line{line(types.LedgerAccountReceivable, types.JournalDirectionDebit)}).
			Build()
		_, err := svc.post(ctx, e)
		require.Error(t, err)
	})
}

func TestPostInvoice(t *testing.T) {
	ctx := testutil.SetupContext()
	newInvoice := func(id string, subscriptionID *string, lines ...*invoice.InvoiceLineItem) *invoice.Invoice {
		subtotal := decimal.Zero
		for _, l := range lines {
			subtotal = subtotal.Add(l.Amount)
		}
		return &invoice.Invoice{ID: id, CustomerID: "cust_1", SubscriptionID: subscriptionID, InvoiceStatus: types.InvoiceStatusFinalized,
			Currency: "usd", Subtotal: subtotal, Total: subtotal, FinalizedAt: lo.ToPtr(finalizedAt), LineItems: lines, BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	newLine := func(id string, sli *string, amount int64) *invoice.InvoiceLineItem {
		return &invoice.InvoiceLineItem{ID: id, SubscriptionLineItemID: sli, Amount: decimal.NewFromInt(amount), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	sub := lo.ToPtr(testSubscriptionID)

	tests := []struct {
		name      string
		invoice   func() *invoice.Invoice
		taxes     []*taxapplied.TaxApplied
		wantLines map[types.JournalEventType][]string
		wantErr   bool
	}{
		{
			name: "100 with 20 credits applied, then voided",
			invoice: func() *invoice.Invoice {
				inv := newInvoice("inv_1", sub, newLine("l_1", lo.ToPtr("sli_1"), 100))
				inv.TotalPrepaidCreditsApplied, inv.Total = decimal.NewFromInt(20), decimal.NewFromInt(80)
				inv.InvoiceStatus, inv.VoidedAt = types.InvoiceStatusVoided, lo.ToPtr(voidedAt)
				return inv
			},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventInvoiceFinalized: {"debit accounts_receivable 80", "debit customer_credit 20", "credit deferred_revenue 100 pob_1"},
				types.JournalEventInvoiceVoided:    {"credit accounts_receivable 100", "debit deferred_revenue 100 pob_1"},
			},
		},
		{
			name: "inclusive tax is not revenue",
			invoice: func() *invoice.Invoice {
				inv := newInvoice("inv_2", sub, newLine("l_2", lo.ToPtr("sli_1"), 118))
				inv.TotalTax = decimal.NewFromInt(18)
				return inv
			},
			taxes: []*taxapplied.TaxApplied{{ID: "ta_1", EntityType: types.TaxRateEntityTypeInvoice, EntityID: "inv_2",
				TaxBehavior: types.TaxBehaviorInclusive, TaxAmount: decimal.NewFromInt(18), BaseModel: types.GetDefaultBaseModel(ctx)}},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventInvoiceFinalized: {"debit accounts_receivable 118", "credit tax_payable 18", "credit deferred_revenue 100 pob_1"},
			},
		},
		{
			name:    "standalone one-off line is revenue",
			invoice: func() *invoice.Invoice { return newInvoice("inv_3", nil, newLine("l_3", nil, 50)) },
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventInvoiceFinalized: {"debit accounts_receivable 50", "credit revenue 50"},
			},
		},
		{
			name: "wallet top-up is customer credit",
			invoice: func() *invoice.Invoice {
				inv := newInvoice("inv_4", nil, newLine("l_4", nil, 500))
				inv.Metadata = types.Metadata{"wallet_transaction_id": "wtx_1"}
				return inv
			},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventInvoiceFinalized: {"debit accounts_receivable 500", "credit customer_credit 500"},
			},
		},
		{
			name: "draft posts nothing",
			invoice: func() *invoice.Invoice {
				inv := newInvoice("inv_5", sub, newLine("l_5", lo.ToPtr("sli_1"), 100))
				inv.FinalizedAt = nil
				return inv
			},
			wantLines: map[types.JournalEventType][]string{},
		},
		{
			name: "line item without a POB fails",
			invoice: func() *invoice.Invoice {
				return newInvoice("inv_6", sub, newLine("l_6", lo.ToPtr("sli_unsynced"), 100))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newTestService(t, ctx, testSources{taxes: tt.taxes})

			inv := tt.invoice()
			err := svc.PostInvoice(ctx, inv)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, svc.PostInvoice(ctx, inv), "a repeat posts nothing new")

			saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Len(t, saved, len(tt.wantLines))
			for _, e := range saved {
				require.ElementsMatch(t, tt.wantLines[e.EventType()], lineSummary(e))
				require.Equal(t, lo.Ternary(inv.SubscriptionID == nil, "", "contract_1"), e.ContractID())
				wantDate := lo.Ternary(e.EventType() == types.JournalEventInvoiceVoided, "2026-11-05", "2026-11-01")
				require.Equal(t, wantDate, e.BusinessDate().Format(time.DateOnly), "local date in the subscription or customer timezone")
			}
			requireReversal(t, saved, types.JournalEventInvoiceVoided, types.JournalEventInvoiceFinalized)
		})
	}
}

const testSubscriptionID = "subs_1"

var (
	finalizedAt = time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC) // Nov 1 in IST
	voidedAt    = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
)

func TestPostCreditNote(t *testing.T) {
	ctx := testutil.SetupContext()
	// l_pob is on sli_1 (pob_1); l_plain has no subscription line item.
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "cust_1", Currency: "usd", BaseModel: types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{
			{ID: "l_pob", SubscriptionLineItemID: lo.ToPtr("sli_1"), Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)},
			{ID: "l_plain", Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)},
		}}
	cnLine := func(id, invoiceLineID string, amount int64) *creditnote.CreditNoteLineItem {
		return &creditnote.CreditNoteLineItem{ID: id, InvoiceLineItemID: invoiceLineID, Amount: decimal.NewFromInt(amount), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	newCreditNote := func(id string, subscriptionID *string, total int64, lines ...*creditnote.CreditNoteLineItem) *creditnote.CreditNote {
		return &creditnote.CreditNote{ID: id, InvoiceID: inv.ID, CustomerID: "cust_1", SubscriptionID: subscriptionID,
			CreditNoteStatus: types.CreditNoteStatusFinalized, CreditNoteType: types.CreditNoteTypeAdjustment, Currency: "usd",
			TotalAmount: decimal.NewFromInt(total), LineItems: lines, FinalizedAt: lo.ToPtr(finalizedAt), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	reversalTax := func(creditNoteID string, amount int64) *taxapplied.TaxApplied {
		return &taxapplied.TaxApplied{ID: "ta_" + creditNoteID, EntityType: types.TaxRateEntityTypeCreditNote, EntityID: creditNoteID,
			TaxAmount: decimal.NewFromInt(amount), TaxTransactionType: types.TaxTransactionTypeReversal, BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	finalized := []string{"debit contra_revenue 100 pob_1", "debit tax_payable 18", "credit accounts_receivable 118"}
	sub := lo.ToPtr(testSubscriptionID)

	tests := []struct {
		name       string
		creditNote func() *creditnote.CreditNote
		taxes      []*taxapplied.TaxApplied
		wantLines  map[types.JournalEventType][]string
	}{
		{
			name:       "exclusive tax on a POB line",
			creditNote: func() *creditnote.CreditNote { return newCreditNote("cn_1", sub, 118, cnLine("cnl_1", "l_pob", 100)) },
			taxes:      []*taxapplied.TaxApplied{reversalTax("cn_1", 18)},
			wantLines:  map[types.JournalEventType][]string{types.JournalEventCreditNoteFinalized: finalized},
		},
		{
			name: "voided adjustment is reversed",
			creditNote: func() *creditnote.CreditNote {
				cn := newCreditNote("cn_2", sub, 118, cnLine("cnl_2", "l_pob", 100))
				cn.CreditNoteStatus, cn.VoidedAt = types.CreditNoteStatusVoided, lo.ToPtr(voidedAt)
				return cn
			},
			taxes: []*taxapplied.TaxApplied{reversalTax("cn_2", 18)},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventCreditNoteFinalized: finalized,
				types.JournalEventCreditNoteVoided:    {"credit contra_revenue 100 pob_1", "credit tax_payable 18", "debit accounts_receivable 118"},
			},
		},
		{
			name: "inclusive tax comes off every line pro rata",
			creditNote: func() *creditnote.CreditNote {
				return newCreditNote("cn_3", sub, 100, cnLine("cnl_3a", "l_pob", 60), cnLine("cnl_3b", "l_plain", 40))
			},
			taxes: []*taxapplied.TaxApplied{reversalTax("cn_3", 12)},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventCreditNoteFinalized: {"debit contra_revenue 52.8 pob_1", "debit contra_revenue 35.2", "debit tax_payable 12", "credit accounts_receivable 100"},
			},
		},
		{
			name:       "untaxed without a subscription",
			creditNote: func() *creditnote.CreditNote { return newCreditNote("cn_4", nil, 50, cnLine("cnl_4", "l_plain", 50)) },
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventCreditNoteFinalized: {"debit contra_revenue 50", "credit accounts_receivable 50"},
			},
		},
		{
			name: "draft posts nothing",
			creditNote: func() *creditnote.CreditNote {
				cn := newCreditNote("cn_5", nil, 50, cnLine("cnl_5", "l_plain", 50))
				cn.CreditNoteStatus, cn.FinalizedAt = types.CreditNoteStatusDraft, nil
				return cn
			},
			wantLines: map[types.JournalEventType][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newTestService(t, ctx, testSources{taxes: tt.taxes, invoices: []*invoice.Invoice{inv}})
			cn := tt.creditNote()
			require.NoError(t, svc.PostCreditNote(ctx, cn))
			require.NoError(t, svc.PostCreditNote(ctx, cn), "a repeat posts nothing new")

			saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Len(t, saved, len(tt.wantLines))
			for _, e := range saved {
				require.ElementsMatch(t, tt.wantLines[e.EventType()], lineSummary(e))
				require.Equal(t, lo.Ternary(cn.SubscriptionID == nil, "", "contract_1"), e.ContractID())
				wantDate := lo.Ternary(e.EventType() == types.JournalEventCreditNoteVoided, "2026-11-05", "2026-11-01")
				require.Equal(t, wantDate, e.BusinessDate().Format(time.DateOnly))
			}
			requireReversal(t, saved, types.JournalEventCreditNoteVoided, types.JournalEventCreditNoteFinalized)
		})
	}
}

func TestPostPayment(t *testing.T) {
	ctx := testutil.SetupContext()
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID), Currency: "usd", BaseModel: types.GetDefaultBaseModel(ctx)}
	newPayment := func(id string, method types.PaymentMethodType) *payment.Payment {
		return &payment.Payment{ID: id, DestinationType: types.PaymentDestinationTypeInvoice, DestinationID: inv.ID, PaymentMethodType: method,
			Amount: decimal.NewFromInt(80), Currency: "usd", PaymentStatus: types.PaymentStatusSucceeded, SucceededAt: lo.ToPtr(finalizedAt),
			BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	byCard := []string{"debit cash 80", "credit accounts_receivable 80"}

	tests := []struct {
		name      string
		payment   func() *payment.Payment
		wantLines map[types.JournalEventType][]string
	}{
		{
			name:      "card payment",
			payment:   func() *payment.Payment { return newPayment("pay_1", types.PaymentMethodTypeCard) },
			wantLines: map[types.JournalEventType][]string{types.JournalEventPaymentSucceeded: byCard},
		},
		{
			name:    "wallet credits payment",
			payment: func() *payment.Payment { return newPayment("pay_2", types.PaymentMethodTypeCredits) },
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventPaymentSucceeded: {"debit customer_credit 80", "credit accounts_receivable 80"},
			},
		},
		{
			name: "refunded payment is reversed",
			payment: func() *payment.Payment {
				p := newPayment("pay_3", types.PaymentMethodTypeCard)
				p.PaymentStatus, p.RefundedAt = types.PaymentStatusRefunded, lo.ToPtr(voidedAt)
				return p
			},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventPaymentSucceeded: byCard,
				types.JournalEventPaymentReversed:  {"credit cash 80", "debit accounts_receivable 80"},
			},
		},
		{
			name: "voided payment is reversed",
			payment: func() *payment.Payment {
				p := newPayment("pay_4", types.PaymentMethodTypeCard)
				p.PaymentStatus, p.VoidedAt = types.PaymentStatusVoided, lo.ToPtr(voidedAt)
				return p
			},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventPaymentSucceeded: byCard,
				types.JournalEventPaymentReversed:  {"credit cash 80", "debit accounts_receivable 80"},
			},
		},
		{
			name: "pending payment posts nothing",
			payment: func() *payment.Payment {
				p := newPayment("pay_5", types.PaymentMethodTypeCard)
				p.PaymentStatus, p.SucceededAt = types.PaymentStatusPending, nil
				return p
			},
			wantLines: map[types.JournalEventType][]string{},
		},
		{
			name: "customer destination posts nothing",
			payment: func() *payment.Payment {
				p := newPayment("pay_6", types.PaymentMethodTypeCard)
				p.DestinationType, p.DestinationID = types.PaymentDestinationTypeCustomer, "cust_1"
				return p
			},
			wantLines: map[types.JournalEventType][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
			p := tt.payment()
			require.NoError(t, svc.PostPayment(ctx, p))
			require.NoError(t, svc.PostPayment(ctx, p), "a repeat posts nothing new")

			saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			require.Len(t, saved, len(tt.wantLines))
			for _, e := range saved {
				require.ElementsMatch(t, tt.wantLines[e.EventType()], lineSummary(e))
				require.Equal(t, "contract_1", e.ContractID())
				wantDate := lo.Ternary(e.EventType() == types.JournalEventPaymentReversed, "2026-11-05", "2026-11-01")
				require.Equal(t, wantDate, e.BusinessDate().Format(time.DateOnly))
			}
			requireReversal(t, saved, types.JournalEventPaymentReversed, types.JournalEventPaymentSucceeded)
		})
	}
}

func TestPostRefund(t *testing.T) {
	ctx := testutil.SetupContext()
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "cust_1", Currency: "usd", BaseModel: types.GetDefaultBaseModel(ctx)}
	newRefund := func(id string, destination types.RefundDestination, settled int64) *refund.Refund {
		return &refund.Refund{ID: id, InvoiceID: inv.ID, Amount: decimal.NewFromInt(100), SettledAmount: decimal.NewFromInt(settled),
			Currency: "usd", RefundStatus: types.RefundStatusSucceeded, RefundDestination: destination, SucceededAt: lo.ToPtr(voidedAt),
			BaseModel: types.GetDefaultBaseModel(ctx)}
	}

	tests := []struct {
		name      string
		refund    *refund.Refund
		wantLines []string
	}{
		{name: "to the wallet", refund: newRefund("ref_1", types.RefundDestinationWallet, 100), wantLines: []string{"debit accounts_receivable 100", "credit customer_credit 100"}},
		{name: "to the gateway", refund: newRefund("ref_2", types.RefundDestinationGateway, 100), wantLines: []string{"debit accounts_receivable 100", "credit cash 100"}},
		{name: "out of band", refund: newRefund("ref_3", types.RefundDestinationOutOfBand, 100), wantLines: []string{"debit accounts_receivable 100", "credit cash 100"}},
		{name: "partially settled posts the settled amount", refund: newRefund("ref_4", types.RefundDestinationGateway, 60), wantLines: []string{"debit accounts_receivable 60", "credit cash 60"}},
		{name: "pending posts nothing", refund: func() *refund.Refund {
			r := newRefund("ref_5", types.RefundDestinationGateway, 0)
			r.RefundStatus, r.SucceededAt = types.RefundStatusPending, nil
			return r
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
			require.NoError(t, svc.PostRefund(ctx, tt.refund))
			require.NoError(t, svc.PostRefund(ctx, tt.refund), "a repeat posts nothing new")

			saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
			require.NoError(t, err)
			if tt.wantLines == nil {
				require.Empty(t, saved)
				return
			}
			require.Len(t, saved, 1)
			require.ElementsMatch(t, tt.wantLines, lineSummary(saved[0]))
			require.Equal(t, "", saved[0].ContractID())
			require.Equal(t, "2026-11-05", saved[0].BusinessDate().Format(time.DateOnly))
		})
	}
}

// postLifecycle posts the ERD example: invoice 100 with 20 credits applied, paid 80, voided, then the void refunds
// 100 to the wallet.
func postLifecycle(t *testing.T, ctx context.Context) (JournalService, *testutil.InMemoryJournalStore) {
	inv := &invoice.Invoice{ID: "inv_1", CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID), Currency: "usd",
		InvoiceStatus: types.InvoiceStatusVoided, Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(80),
		TotalPrepaidCreditsApplied: decimal.NewFromInt(20), FinalizedAt: lo.ToPtr(finalizedAt), VoidedAt: lo.ToPtr(voidedAt),
		LineItems: []*invoice.InvoiceLineItem{{ID: "l_1", SubscriptionLineItemID: lo.ToPtr("sli_1"), Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)}},
		BaseModel: types.GetDefaultBaseModel(ctx)}
	svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
	require.NoError(t, svc.PostInvoice(ctx, inv))
	require.NoError(t, svc.PostPayment(ctx, &payment.Payment{ID: "pay_1", DestinationType: types.PaymentDestinationTypeInvoice, DestinationID: inv.ID,
		PaymentMethodType: types.PaymentMethodTypeCard, Amount: decimal.NewFromInt(80), Currency: "usd",
		PaymentStatus: types.PaymentStatusSucceeded, SucceededAt: lo.ToPtr(finalizedAt), BaseModel: types.GetDefaultBaseModel(ctx)}))
	require.NoError(t, svc.PostRefund(ctx, &refund.Refund{ID: "ref_1", InvoiceID: inv.ID, Amount: decimal.NewFromInt(100),
		SettledAmount: decimal.NewFromInt(100), Currency: "usd", RefundStatus: types.RefundStatusSucceeded,
		RefundDestination: types.RefundDestinationWallet, SucceededAt: lo.ToPtr(voidedAt), BaseModel: types.GetDefaultBaseModel(ctx)}))
	return svc, store
}

func TestBillingLifecycleBalances(t *testing.T) {
	ctx := testutil.SetupContext()
	_, store := postLifecycle(t, ctx)

	saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
	require.NoError(t, err)
	balances := map[types.LedgerAccountKey]decimal.Decimal{}
	for _, e := range saved {
		for _, l := range e.Lines() {
			balances[l.Account()] = balances[l.Account()].Add(lo.Ternary(l.Direction() == l.Account().NormalSide(), l.Amount(), l.Amount().Neg()))
		}
	}
	require.True(t, balances[types.LedgerAccountReceivable].IsZero(), "AR nets to zero, got %s", balances[types.LedgerAccountReceivable])
	require.Equal(t, "80", balances[types.LedgerAccountCustomerCredit].String(), "the paid cash moved to the wallet, the 20 credits restored")
	require.Equal(t, "80", balances[types.LedgerAccountCash].String())
	require.True(t, balances[types.LedgerAccountDeferredRevenue].IsZero())
}

func TestReconcile(t *testing.T) {
	ctx := testutil.SetupContext()
	base := types.GetDefaultBaseModel(ctx)
	newInvoice := func(id, sli string) *invoice.Invoice {
		return &invoice.Invoice{ID: id, CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID), Currency: "usd",
			InvoiceStatus: types.InvoiceStatusFinalized, Subtotal: decimal.NewFromInt(100), Total: decimal.NewFromInt(100),
			FinalizedAt: lo.ToPtr(finalizedAt), BaseModel: base,
			LineItems: []*invoice.InvoiceLineItem{{ID: "l_" + id, SubscriptionLineItemID: lo.ToPtr(sli), Amount: decimal.NewFromInt(100), BaseModel: base}}}
	}
	svc, store := newTestService(t, ctx, testSources{
		invoices: []*invoice.Invoice{newInvoice("inv_ok", "sli_1"), newInvoice("inv_unsynced", "sli_unsynced")},
		creditNotes: []*creditnote.CreditNote{{ID: "cn_1", InvoiceID: "inv_ok", CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID),
			CreditNoteStatus: types.CreditNoteStatusFinalized, CreditNoteType: types.CreditNoteTypeAdjustment, Currency: "usd",
			TotalAmount: decimal.NewFromInt(10), FinalizedAt: lo.ToPtr(finalizedAt), BaseModel: base,
			LineItems: []*creditnote.CreditNoteLineItem{{ID: "cnl_1", InvoiceLineItemID: "l_inv_ok", Amount: decimal.NewFromInt(10), BaseModel: base}}}},
		payments: []*payment.Payment{{ID: "pay_1", DestinationType: types.PaymentDestinationTypeInvoice, DestinationID: "inv_ok",
			PaymentMethodType: types.PaymentMethodTypeCard, Amount: decimal.NewFromInt(90), Currency: "usd",
			PaymentStatus: types.PaymentStatusSucceeded, SucceededAt: lo.ToPtr(finalizedAt), BaseModel: base}},
		refunds: []*refund.Refund{{ID: "ref_1", InvoiceID: "inv_ok", Amount: decimal.NewFromInt(5), SettledAmount: decimal.NewFromInt(5),
			Currency: "usd", RefundStatus: types.RefundStatusSucceeded, RefundDestination: types.RefundDestinationWallet,
			SucceededAt: lo.ToPtr(voidedAt), BaseModel: base}},
	})

	// The unsynced invoice fails; every other source still posts, and a rerun posts nothing new.
	for range 2 {
		err := svc.Reconcile(ctx, time.Time{})
		require.ErrorContains(t, err, "invoice inv_unsynced")

		saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
		require.NoError(t, err)
		require.ElementsMatch(t, []types.JournalEventType{
			types.JournalEventInvoiceFinalized, types.JournalEventCreditNoteFinalized,
			types.JournalEventPaymentSucceeded, types.JournalEventRefundSucceeded,
		}, lo.Map(saved, func(e *domainJournal.Entry, _ int) types.JournalEventType { return e.EventType() }))
	}
}

func TestGetJournalEntries(t *testing.T) {
	ctx := testutil.SetupContext()
	svc, _ := postLifecycle(t, ctx)

	tests := []struct {
		name      string
		filter    *journalRepo.Filter
		wantCount int
	}{
		{name: "all", filter: journalRepo.NewFilter(), wantCount: 4},
		{name: "by event type", filter: &journalRepo.Filter{EventTypes: []types.JournalEventType{types.JournalEventPaymentSucceeded}}, wantCount: 1},
		{name: "by account", filter: &journalRepo.Filter{Accounts: []types.LedgerAccountKey{types.LedgerAccountDeferredRevenue}}, wantCount: 2},
		{name: "by POB", filter: &journalRepo.Filter{POBIDs: []string{"pob_1"}}, wantCount: 2},
		{name: "by source", filter: &journalRepo.Filter{SourceEntityType: types.RevRecEntityTypeInvoice, SourceEntityIDs: []string{"inv_1"}}, wantCount: 2},
		{name: "by contract", filter: &journalRepo.Filter{ContractIDs: []string{"contract_1"}}, wantCount: 4},
		{name: "by other customer", filter: &journalRepo.Filter{CustomerIDs: []string{"cust_2"}}, wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.GetJournalEntries(ctx, tt.filter)
			require.NoError(t, err)
			require.Len(t, resp.Items, tt.wantCount)
		})
	}

	t.Run("get with lines", func(t *testing.T) {
		list, err := svc.GetJournalEntries(ctx, &journalRepo.Filter{EventTypes: []types.JournalEventType{types.JournalEventInvoiceVoided}})
		require.NoError(t, err)
		require.Len(t, list.Items, 1)

		got, err := svc.GetJournalEntry(ctx, list.Items[0].ID)
		require.NoError(t, err)
		require.Len(t, got.Lines, 2)
		require.NotEmpty(t, got.ReversalOf)

		_, err = svc.GetJournalEntry(ctx, "je_missing")
		require.True(t, ierr.IsNotFound(err))
	})
}

// requireReversal checks a posted reverse entry points at its original.
func requireReversal(t *testing.T, saved []*domainJournal.Entry, reverse, original types.JournalEventType) {
	byType := lo.KeyBy(saved, func(e *domainJournal.Entry) types.JournalEventType { return e.EventType() })
	if r, ok := byType[reverse]; ok {
		require.Equal(t, byType[original].ID(), r.ReversalOf())
	}
}

// testSources are the rows newTestService seeds besides its fixed fixtures.
type testSources struct {
	taxes       []*taxapplied.TaxApplied
	invoices    []*invoice.Invoice
	creditNotes []*creditnote.CreditNote
	payments    []*payment.Payment
	refunds     []*refund.Refund
}

// newTestService seeds subs_1 and cust_1 (IST), contract_1, sli_1 under pob_1, and the given sources.
func newTestService(t *testing.T, ctx context.Context, src testSources) (JournalService, *testutil.InMemoryJournalStore) {
	store := testutil.NewInMemoryJournalStore()
	periods := accountingperiodsvc.NewAccountingPeriodService(testutil.NewInMemoryAccountingPeriodStore())
	contracts := testutil.NewInMemoryContractStore()
	pobs := testutil.NewInMemoryPOBStore(contracts)
	subs := testutil.NewInMemorySubscriptionStore()
	customers := testutil.NewInMemoryCustomerStore()

	ist := "Asia/Kolkata"
	require.NoError(t, subs.Create(ctx, &subscription.Subscription{ID: testSubscriptionID, Timezone: ist, BaseModel: types.GetDefaultBaseModel(ctx)}))
	require.NoError(t, customers.Create(ctx, &customer.Customer{ID: "cust_1", Timezone: ist, BaseModel: types.GetDefaultBaseModel(ctx)}))
	c := domainContract.NewBuilder(nil).WithID("contract_1").WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, domainContract.NewAssociationBuilder(nil).
		WithID("contract_assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID(testSubscriptionID).WithStatus(types.StatusPublished).Build()))
	require.NoError(t, pobs.CreateWithAssociation(ctx, domainPOB.NewBuilder(nil).WithID("pob_1").WithContractID(c.ID()).WithStatus(types.StatusPublished).Build(),
		domainPOB.NewAssociationBuilder(nil).WithID("pob_assoc_1").WithEntityType(types.RevRecEntityTypeSubscriptionLineItem).WithEntityID("sli_1").WithStatus(types.StatusPublished).Build()))

	taxes := testutil.NewInMemoryTaxAppliedStore()
	for _, ta := range src.taxes {
		require.NoError(t, taxes.Create(ctx, ta))
	}
	invoices := testutil.NewInMemoryInvoiceStore()
	for _, inv := range src.invoices {
		require.NoError(t, invoices.CreateWithLineItems(ctx, inv))
	}
	creditNotes := testutil.NewInMemoryCreditNoteStore()
	for _, cn := range src.creditNotes {
		require.NoError(t, creditNotes.CreateWithLineItems(ctx, cn))
	}
	payments := testutil.NewInMemoryPaymentStore()
	for _, p := range src.payments {
		require.NoError(t, payments.Create(ctx, p))
	}
	refunds := testutil.NewInMemoryRefundStore()
	for _, r := range src.refunds {
		require.NoError(t, refunds.Create(ctx, r))
	}

	pobService := pobsvc.NewPerformanceObligationService(pobs, contracts, subs, invoices, taxes, nil)
	return NewJournalService(store, periods, pobService, contracts, subs, customers, invoices, creditNotes, payments, refunds, taxes), store
}

func lineSummary(e *domainJournal.Entry) []string {
	return lo.Map(e.Lines(), func(l *domainJournal.Line, _ int) string {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", l.Direction(), l.Account(), l.Amount(), l.POBID()))
	})
}
