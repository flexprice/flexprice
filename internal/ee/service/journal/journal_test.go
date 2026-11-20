package journal

import (
	"context"
	"fmt"
	"sort"
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
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	accountingperiodsvc "github.com/flexprice/flexprice/internal/ee/service/accountingperiod"
	contractsvc "github.com/flexprice/flexprice/internal/ee/service/contract"
	pobsvc "github.com/flexprice/flexprice/internal/ee/service/pob"
	"github.com/flexprice/flexprice/internal/ee/service/recognition"
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
			periodStore := testutil.NewInMemoryAccountingPeriodStore()
			store := testutil.NewInMemoryJournalStore(periodStore)
			periods := accountingperiodsvc.NewAccountingPeriodService(periodStore)
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
		periodStore := testutil.NewInMemoryAccountingPeriodStore()
		svc := &journalService{
			journalRepo:   testutil.NewInMemoryJournalStore(periodStore),
			periodService: accountingperiodsvc.NewAccountingPeriodService(periodStore),
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
		return &invoice.InvoiceLineItem{ID: id, SubscriptionLineItemID: sli, PriceID: linePrice(sli), Amount: decimal.NewFromInt(amount), BaseModel: types.GetDefaultBaseModel(ctx)}
	}
	sub := lo.ToPtr(testSubscriptionID)

	tests := []struct {
		name      string
		invoice   func() *invoice.Invoice
		taxes     []*taxapplied.TaxApplied
		wantLines map[types.JournalEventType][]string
		wantDate  string
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
			name: "backdated invoice is dated on its issue date",
			invoice: func() *invoice.Invoice {
				inv := newInvoice("inv_7", nil, newLine("l_7", nil, 50))
				inv.IssueDate = lo.ToPtr(time.Date(2026, 1, 14, 20, 0, 0, 0, time.UTC))
				return inv
			},
			wantLines: map[types.JournalEventType][]string{
				types.JournalEventInvoiceFinalized: {"debit accounts_receivable 50", "credit revenue 50"},
			},
			wantDate: "2026-01-15",
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
				wantDate := lo.Ternary(e.EventType() == types.JournalEventInvoiceVoided, "2026-11-05", lo.CoalesceOrEmpty(tt.wantDate, "2026-11-01"))
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
			{ID: "l_pob", SubscriptionLineItemID: lo.ToPtr("sli_1"), PriceID: linePrice(lo.ToPtr("sli_1")), Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)},
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
		LineItems: []*invoice.InvoiceLineItem{{ID: "l_1", SubscriptionLineItemID: lo.ToPtr("sli_1"), PriceID: linePrice(lo.ToPtr("sli_1")), Amount: decimal.NewFromInt(100), BaseModel: types.GetDefaultBaseModel(ctx)}},
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
			balances[l.Account()] = balances[l.Account()].Add(l.Account().NormalBalance(l.Net()))
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
			LineItems: []*invoice.InvoiceLineItem{{ID: "l_" + id, SubscriptionLineItemID: lo.ToPtr(sli), PriceID: linePrice(lo.ToPtr(sli)), Amount: decimal.NewFromInt(100), BaseModel: base}}}
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
	// pob1 replaces the default pob_1; pobs and prices seed more POBs and the prices their lines bill.
	pob1   *domainPOB.PerformanceObligation
	pobs   []*domainPOB.PerformanceObligation
	prices []*price.Price
}

// testPOB1 is pob_1 of contract_1: prod_1, straight line, period even from Jan 1 2026 to end, allocated amount.
func testPOB1(amount decimal.Decimal, end time.Time) *domainPOB.PerformanceObligation {
	return domainPOB.NewBuilder(nil).WithID("pob_1").WithContractID("contract_1").WithProductID("prod_1").
		WithRecognitionMethod(types.RecognitionMethodStraightLine).WithAmortizationMethod(types.AmortizationPeriodEven).
		WithAllocatedAmount(amount).WithServiceStart(inIST(time.January, 1)).WithServiceEnd(lo.ToPtr(end)).
		WithPOBStatus(types.PerformanceObligationStatusActive).WithStatus(types.StatusPublished).Build()
}

// linePrice is the price a fixture line bills for its subscription line item: price_<sli>, nil without one.
func linePrice(sli *string) *string {
	if sli == nil {
		return nil
	}
	return lo.ToPtr("price_" + *sli)
}

// newTestService seeds subs_1 and cust_1 (IST), contract_1 (active, usd), sli_1 under pob_1 (active, straight line, period
// even, from Jan 1 2026), and the given sources.
func newTestService(t *testing.T, ctx context.Context, src testSources) (JournalService, *testutil.InMemoryJournalStore) {
	periodStore := testutil.NewInMemoryAccountingPeriodStore()
	store := testutil.NewInMemoryJournalStore(periodStore)
	periods := accountingperiodsvc.NewAccountingPeriodService(periodStore)
	contracts := testutil.NewInMemoryContractStore()
	pobs := testutil.NewInMemoryPOBStore(contracts)
	subs := testutil.NewInMemorySubscriptionStore()
	customers := testutil.NewInMemoryCustomerStore()

	ist := "Asia/Kolkata"
	require.NoError(t, subs.Create(ctx, &subscription.Subscription{ID: testSubscriptionID, Timezone: ist, BaseModel: types.GetDefaultBaseModel(ctx)}))
	require.NoError(t, customers.Create(ctx, &customer.Customer{ID: "cust_1", Timezone: ist, BaseModel: types.GetDefaultBaseModel(ctx)}))
	c := domainContract.NewBuilder(nil).WithID("contract_1").WithCustomerID("cust_1").WithCurrency("usd").
		WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build()
	require.NoError(t, contracts.CreateWithAssociation(ctx, c, domainContract.NewAssociationBuilder(nil).
		WithID("contract_assoc_1").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID(testSubscriptionID).WithStatus(types.StatusPublished).Build()))
	prices := testutil.NewInMemoryPriceStore()
	require.NoError(t, prices.Create(ctx, &price.Price{ID: "price_sli_1", ProductID: "prod_1", Type: types.PRICE_TYPE_FIXED,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BaseModel: types.GetDefaultBaseModel(ctx)}))
	pob1 := src.pob1
	if pob1 == nil {
		pob1 = testPOB1(decimal.NewFromInt(1200), inIST(time.January, 1).AddDate(1, 0, 0))
	}
	require.NoError(t, pobs.Create(ctx, pob1))
	for _, p := range src.pobs {
		require.NoError(t, pobs.Create(ctx, p))
	}
	for _, pr := range src.prices {
		require.NoError(t, prices.Create(ctx, pr))
	}

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

	pobService := pobsvc.NewPerformanceObligationService(pobs, contracts, subs, invoices, taxes, testutil.NewInMemoryRevenueFactStore(), prices)
	contractRecognizer := recognition.NewContractRecognizer(contracts, pobs, pobService, store, invoices, subs, customers)
	invoiceRecognizer := recognition.NewInvoiceRecognizer(invoices, taxes, contracts, store, subs, customers)
	return NewJournalService(testutil.NewMockPostgresClient(nil), store, periods, pobService, contractRecognizer, invoiceRecognizer,
		contractsvc.NewContractService(contracts), contracts, subs, customers, invoices, creditNotes, payments, refunds, taxes, testutil.NewInMemorySettingsStore()), store
}

func lineSummary(e *domainJournal.Entry) []string {
	return lo.Map(e.Lines(), func(l *domainJournal.Line, _ int) string {
		return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", l.Direction(), l.Account(), l.Amount(), l.POBID()))
	})
}

// inIST is local midnight of a 2026 day in subs_1's timezone.
func inIST(m time.Month, day int) time.Time {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return time.Date(2026, m, day, 0, 0, 0, 0, ist)
}

// pobBalances is each account's balance on its normal side over pob_1's lines dated on or before the 2026 day.
// e.g. a 1,200 invoice and a 100 offset by Jan 31 → deferred_revenue 1100.
func pobBalances(t *testing.T, ctx context.Context, store *testutil.InMemoryJournalStore, m time.Month, day int) map[types.LedgerAccountKey]string {
	saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
	require.NoError(t, err)

	balances := map[types.LedgerAccountKey]decimal.Decimal{
		types.LedgerAccountDeferredRevenue: decimal.Zero, types.LedgerAccountUnbilledReceivable: decimal.Zero, types.LedgerAccountRevenue: decimal.Zero,
	}
	for _, e := range saved {
		if e.BusinessDate().After(time.Date(2026, m, day, 0, 0, 0, 0, time.UTC)) {
			continue
		}
		for _, l := range e.Lines() {
			if l.POBID() == "pob_1" {
				balances[l.Account()] = balances[l.Account()].Add(l.Account().NormalBalance(l.Net()))
			}
		}
	}
	return lo.MapValues(balances, func(v decimal.Decimal, _ types.LedgerAccountKey) string { return v.String() })
}

// subscriptionInvoice is a finalized invoice of subs_1 with one sli_1 line over [periodStart, periodEnd).
func subscriptionInvoice(ctx context.Context, id string, invoiceType types.InvoiceType, amount int64, finalizedAt, periodStart, periodEnd time.Time) *invoice.Invoice {
	return &invoice.Invoice{ID: id, CustomerID: "cust_1", SubscriptionID: lo.ToPtr(testSubscriptionID), InvoiceType: invoiceType,
		InvoiceStatus: types.InvoiceStatusFinalized, Currency: "usd", Subtotal: decimal.NewFromInt(amount), Total: decimal.NewFromInt(amount),
		FinalizedAt: lo.ToPtr(finalizedAt), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		LineItems: []*invoice.InvoiceLineItem{{ID: id + "_line", SubscriptionLineItemID: lo.ToPtr("sli_1"), PriceID: linePrice(lo.ToPtr("sli_1")), Amount: decimal.NewFromInt(amount),
			PeriodStart: lo.ToPtr(periodStart), PeriodEnd: lo.ToPtr(periodEnd), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}}}
}

// TestRecognizeExample1 is ERD Example 1: $1,200 annual prepaid from Jan 1, period even.
func TestRecognizeExample1(t *testing.T) {
	ctx := testutil.SetupContext()
	annual := subscriptionInvoice(ctx, "inv_annual", types.InvoiceTypeSubscription, 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
	svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{annual}})
	require.NoError(t, svc.PostInvoice(ctx, annual))

	recognize := func(m time.Month, day int) []*domainJournal.Entry {
		_, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: inIST(m, day).Add(2 * time.Hour)})
		require.NoError(t, err)
		saved, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(),
			EventTypes: []types.JournalEventType{types.JournalEventRevenueEarned, types.JournalEventRevenueOffset}})
		require.NoError(t, err)
		return saved
	}

	require.Empty(t, recognize(time.January, 20), "January has not ended")

	// Billed ahead with nothing in unbilled, so each month is one entry straight out of deferred.
	saved := recognize(time.February, 1)
	require.Len(t, saved, 1)
	require.Equal(t, types.JournalEventRevenueEarned, saved[0].EventType())
	require.Equal(t, "2026-01-31", saved[0].BusinessDate().Format(time.DateOnly))
	require.Equal(t, types.RevRecEntityTypePOB, saved[0].SourceEntityType())
	require.Empty(t, saved[0].IdempotencyKey())
	require.ElementsMatch(t, []string{"debit deferred_revenue 100 pob_1", "credit revenue 100 pob_1"}, lineSummary(saved[0]))

	require.Len(t, recognize(time.February, 2), 1, "a rerun posts nothing")
	require.Len(t, recognize(time.March, 1), 2, "February posts its entry")

	require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "1100", types.LedgerAccountUnbilledReceivable: "0", types.LedgerAccountRevenue: "100"},
		pobBalances(t, ctx, store, time.January, 31))
	require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "1000", types.LedgerAccountUnbilledReceivable: "0", types.LedgerAccountRevenue: "200"},
		pobBalances(t, ctx, store, time.February, 28))

	t.Run("a backdated billing cut lowers what is billed, not what is earned", func(t *testing.T) {
		cut := subscriptionInvoice(ctx, "inv_cut", types.InvoiceTypeOneOff, -10, inIST(time.March, 1), inIST(time.January, 1), inIST(time.February, 1))
		require.NoError(t, svc.(*journalService).invoiceRepo.CreateWithLineItems(ctx, cut))
		require.NoError(t, svc.PostInvoice(ctx, cut))

		require.Len(t, recognize(time.March, 2), 2, "no correction: the allocation still earns 100 a month")
		require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "990", types.LedgerAccountUnbilledReceivable: "0", types.LedgerAccountRevenue: "200"},
			pobBalances(t, ctx, store, time.March, 1))
	})
}

// TestRecognizeDryRun previews ERD Example 1: the missing ended months plus the open month up to AsOf, saving nothing.
func TestRecognizeDryRun(t *testing.T) {
	tests := []struct {
		name       string
		recognized *time.Time // a real run before the preview
		previewAt  time.Time
		want       []string
	}{
		{
			name:      "nothing posted: January and February to date",
			previewAt: inIST(time.February, 15),
			want: []string{
				"2026-01-31 revenue_earned 100", "2026-02-14 revenue_earned 50",
			},
		},
		{
			name:       "January posted: February to date only",
			recognized: lo.ToPtr(inIST(time.February, 1)),
			previewAt:  inIST(time.February, 15),
			want:       []string{"2026-02-14 revenue_earned 50"},
		},
		{
			name:      "on the 1st the open month has no days",
			previewAt: inIST(time.March, 1),
			want: []string{
				"2026-01-31 revenue_earned 100", "2026-02-28 revenue_earned 100",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			annual := subscriptionInvoice(ctx, "inv_annual", types.InvoiceTypeSubscription, 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{annual}})
			require.NoError(t, svc.PostInvoice(ctx, annual))
			if tt.recognized != nil {
				_, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: tt.recognized.Add(2 * time.Hour)})
				require.NoError(t, err)
			}
			before, err := store.Count(ctx, journalRepo.NewFilter())
			require.NoError(t, err)

			preview, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: tt.previewAt.Add(2 * time.Hour), DryRun: true})
			require.NoError(t, err)

			// e.g. "2026-02-14 revenue_earned 50": both lines of an entry carry the same amount.
			require.ElementsMatch(t, tt.want, lo.Map(preview, func(e *domainJournal.Entry, _ int) string {
				return fmt.Sprintf("%s %s %s", e.BusinessDate().Format(time.DateOnly), e.EventType(), e.Lines()[0].Amount())
			}))
			after, err := store.Count(ctx, journalRepo.NewFilter())
			require.NoError(t, err)
			require.Equal(t, before, after, "a dry run saves nothing")
		})
	}
}

// TestRecognizeExample2 bills in arrears against an allocation earned evenly: 550 over January and February earns 275 a
// month, while January's 300 is invoiced Feb 1 and February's 250 Mar 1, posted before or after the runs.
func TestRecognizeExample2(t *testing.T) {
	for _, invoicesFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("invoices posted first: %v", invoicesFirst), func(t *testing.T) {
			ctx := testutil.SetupContext()
			january := subscriptionInvoice(ctx, "inv_jan", types.InvoiceTypeSubscription, 300, inIST(time.February, 1).Add(5*time.Minute), inIST(time.January, 1), inIST(time.February, 1))
			february := subscriptionInvoice(ctx, "inv_feb", types.InvoiceTypeSubscription, 250, inIST(time.March, 1).Add(5*time.Minute), inIST(time.February, 1), inIST(time.March, 1))
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{january, february}, pob1: testPOB1(decimal.NewFromInt(550), inIST(time.March, 1))})

			postInvoices := func() {
				require.NoError(t, svc.PostInvoice(ctx, january))
				require.NoError(t, svc.PostInvoice(ctx, february))
			}
			if invoicesFirst {
				postInvoices()
			}
			_, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: inIST(time.February, 1).Add(2 * time.Hour)})
			require.NoError(t, err)
			_, err = svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: inIST(time.March, 1).Add(2 * time.Hour)})
			require.NoError(t, err)
			if !invoicesFirst {
				postInvoices()
				_, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: inIST(time.March, 2).Add(2 * time.Hour)})
				require.NoError(t, err)
			}

			require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "0", types.LedgerAccountUnbilledReceivable: "275", types.LedgerAccountRevenue: "275"},
				pobBalances(t, ctx, store, time.January, 31), "nothing billed by Jan 31")
			require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "0", types.LedgerAccountUnbilledReceivable: "250", types.LedgerAccountRevenue: "550"},
				pobBalances(t, ctx, store, time.February, 28))
		})
	}
}

func TestRunRecognition(t *testing.T) {
	ctx := testutil.SetupContext()
	annual := subscriptionInvoice(ctx, "inv_annual", types.InvoiceTypeSubscription, 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))

	newRun := func(t *testing.T, enabled bool) (*journalService, *testutil.InMemoryJournalStore) {
		svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{annual}})
		s := svc.(*journalService)
		require.NoError(t, s.settingsRepo.Create(ctx, &settings.Setting{ID: "setting_revrec", Key: types.SettingKeyRevenueRecognitionConfig,
			Value: map[string]interface{}{"enabled": enabled}, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))
		return s, store
	}
	recognized := func(t *testing.T, store *testutil.InMemoryJournalStore) []string {
		saved, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), EventTypes: []types.JournalEventType{types.JournalEventRevenueEarned}})
		require.NoError(t, err)
		return lo.Map(saved, func(e *domainJournal.Entry, _ int) string { return e.BusinessDate().Format(time.DateOnly) })
	}
	feb1, mar1 := inIST(time.February, 1).Add(2*time.Hour), inIST(time.March, 1).Add(2*time.Hour)

	t.Run("posts missed billing, then recognizes; progress per contract", func(t *testing.T) {
		s, store := newRun(t, true)
		var cursors []RecognitionCursor
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, OnProgress: func(c RecognitionCursor) { cursors = append(cursors, c) }}))

		require.Equal(t, []string{"2026-01-31"}, recognized(t, store))
		env := types.GetEnvironmentID(ctx)
		require.Equal(t, []RecognitionCursor{{EnvironmentID: env}, {EnvironmentID: env, ContractID: "contract_1"}}, cursors,
			"the repair pass keeps the resume point; recognition moves it")
		billing, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), EventTypes: []types.JournalEventType{types.JournalEventInvoiceFinalized}})
		require.NoError(t, err)
		require.Len(t, billing, 1, "Reconcile posted the invoice the hook missed")
	})

	t.Run("a resumed run skips contracts up to the cursor", func(t *testing.T) {
		s, store := newRun(t, true)
		resume := &RecognitionCursor{EnvironmentID: types.GetEnvironmentID(ctx), ContractID: "contract_1"}
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: mar1, Resume: resume, OnProgress: func(RecognitionCursor) { t.Fatal("no contract left") }}))
		require.Empty(t, recognized(t, store))
	})

	t.Run("a cursor of an environment no longer enabled restarts from the top", func(t *testing.T) {
		s, store := newRun(t, true)
		resume := &RecognitionCursor{EnvironmentID: "env_gone", ContractID: "contract_9"}
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, Resume: resume, OnProgress: func(RecognitionCursor) {}}))
		require.Equal(t, []string{"2026-01-31"}, recognized(t, store))
	})

	t.Run("a scoped run covers only its tenant and environment", func(t *testing.T) {
		s, store := newRun(t, true)
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, EnvironmentID: "env_other", OnProgress: func(RecognitionCursor) { t.Fatal("other environment") }}))
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, TenantID: "tenant_other", OnProgress: func(RecognitionCursor) { t.Fatal("other tenant") }}))
		require.Empty(t, recognized(t, store))

		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), OnProgress: func(RecognitionCursor) {}}))
		require.Equal(t, []string{"2026-01-31"}, recognized(t, store))
	})

	t.Run("a disabled environment is skipped", func(t *testing.T) {
		s, store := newRun(t, false)
		require.NoError(t, s.RunRecognition(ctx, RecognitionRunRequest{AsOf: feb1, OnProgress: func(RecognitionCursor) { t.Fatal("disabled") }}))
		require.Empty(t, recognized(t, store))
	})
}

func TestRecognitionContracts(t *testing.T) {
	ctx := testutil.SetupContext()
	asOf := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	svc, _ := newTestService(t, ctx, testSources{})
	s := svc.(*journalService)

	add := func(id string, status types.ContractStatus, endedAt *time.Time) {
		require.NoError(t, s.contractRepo.(*testutil.InMemoryContractStore).CreateWithAssociation(ctx,
			domainContract.NewBuilder(nil).WithID(id).WithContractStatus(status).WithEndedAt(endedAt).WithStatus(types.StatusPublished).Build(),
			domainContract.NewAssociationBuilder(nil).WithID(id+"_assoc").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID("subs_"+id).WithStatus(types.StatusPublished).Build()))
	}
	add("contract_ended_march", types.ContractStatusEnded, lo.ToPtr(time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)))
	add("contract_ended_2025", types.ContractStatusEnded, lo.ToPtr(time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)))
	add("contract_invoiced", types.ContractStatusEnded, lo.ToPtr(time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)))
	add("contract_draft", types.ContractStatusDraft, nil)

	since := asOf.AddDate(0, 0, -1)
	changed := subscriptionInvoice(ctx, "inv_late_void", types.InvoiceTypeSubscription, 50, since, since, asOf)
	changed.SubscriptionID = lo.ToPtr("subs_contract_invoiced")
	require.NoError(t, s.invoiceRepo.CreateWithLineItems(ctx, changed))

	ids, err := s.contracts.Due(ctx, since, asOf, recognition.Scope{})
	require.NoError(t, err)
	require.Equal(t, []string{"contract_1", "contract_ended_march", "contract_invoiced"}, ids,
		"active, ended in the last two months, and ended long ago but with a changed invoice; never draft")
}

// TestBackfill rebuilds a contract's subscription created before enabling: a prepaid invoice never posted or recognized.
func TestBackfill(t *testing.T) {
	ctx := testutil.SetupContext()
	svc, store := newTestService(t, ctx, testSources{
		pobs: []*domainPOB.PerformanceObligation{domainPOB.NewBuilder(nil).WithID("pob_2").WithContractID("contract_2").WithProductID("prod_2").WithName("Annual").
			WithRecognitionMethod(types.RecognitionMethodStraightLine).WithAmortizationMethod(types.AmortizationPeriodEvenProrated).WithServiceStart(inIST(time.January, 1)).
			WithAllocatedAmount(decimal.NewFromInt(1200)).WithServiceEnd(lo.ToPtr(inIST(time.January, 1).AddDate(1, 0, 0))).
			WithPOBStatus(types.PerformanceObligationStatusActive).WithStatus(types.StatusPublished).Build()},
		prices: []*price.Price{{ID: "price_sli_2", ProductID: "prod_2", Type: types.PRICE_TYPE_FIXED, BillingPeriod: types.BILLING_PERIOD_ANNUAL, BaseModel: types.GetDefaultBaseModel(ctx)}},
	})
	s := svc.(*journalService)

	require.NoError(t, s.subRepo.CreateWithLineItems(ctx, &subscription.Subscription{ID: "subs_2", CustomerID: "cust_1", Currency: "usd",
		Timezone: "Asia/Kolkata", SubscriptionStatus: types.SubscriptionStatusActive, StartDate: inIST(time.January, 1), BaseModel: types.GetDefaultBaseModel(ctx)},
		[]*subscription.SubscriptionLineItem{{ID: "sli_2", SubscriptionID: "subs_2", PriceID: "price_2", PriceType: types.PRICE_TYPE_FIXED,
			BillingPeriod: types.BILLING_PERIOD_ANNUAL, StartDate: inIST(time.January, 1), BaseModel: types.GetDefaultBaseModel(ctx)}}))
	annual := subscriptionInvoice(ctx, "inv_2", types.InvoiceTypeSubscription, 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
	annual.SubscriptionID = lo.ToPtr("subs_2")
	annual.LineItems[0].SubscriptionLineItemID, annual.LineItems[0].PriceID = lo.ToPtr("sli_2"), linePrice(lo.ToPtr("sli_2"))
	require.NoError(t, s.invoiceRepo.CreateWithLineItems(ctx, annual))
	require.NoError(t, s.contractRepo.(*testutil.InMemoryContractStore).CreateWithAssociation(ctx,
		domainContract.NewBuilder(nil).WithID("contract_2").WithContractStatus(types.ContractStatusActive).WithStatus(types.StatusPublished).Build(),
		domainContract.NewAssociationBuilder(nil).WithID("contract_assoc_2").WithEntityType(types.RevRecEntityTypeSubscription).WithEntityID("subs_2").WithStatus(types.StatusPublished).Build()))

	saved := func() []*domainJournal.Entry {
		entries, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter()})
		require.NoError(t, err)
		return entries
	}
	backfill := func() {
		require.NoError(t, s.Backfill(ctx, BackfillRequest{AsOf: inIST(time.March, 1).Add(2 * time.Hour), OnProgress: func(RecognitionCursor) {}}))
	}

	backfill()
	c, err := s.contractRepo.GetBySubscriptionID(ctx, "subs_2")
	require.NoError(t, err)
	p, err := s.pobService.POBIDForInvoiceLine(ctx, c.ID(), annual.LineItems[0])
	require.NoError(t, err)

	balances := map[types.LedgerAccountKey]decimal.Decimal{}
	for _, e := range saved() {
		require.True(t, e.ContractID() == c.ID() || e.ContractID() == "contract_1")
		for _, l := range e.Lines() {
			if l.POBID() == p && !e.BusinessDate().After(time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)) {
				balances[l.Account()] = balances[l.Account()].Add(l.Account().NormalBalance(l.Net()))
			}
		}
	}
	require.Equal(t, "1000", balances[types.LedgerAccountDeferredRevenue].String(), "billing posted, two months offset")
	require.Equal(t, "200", balances[types.LedgerAccountRevenue].String(), "January and February recognized")
	require.True(t, balances[types.LedgerAccountUnbilledReceivable].IsZero())

	count := len(saved())
	backfill()
	require.Len(t, saved(), count, "a rerun posts nothing")
}

// activeByDefault narrows a list without statuses to active subscriptions, as the Postgres repository does.
type activeByDefault struct{ subscription.Repository }

func (r activeByDefault) List(ctx context.Context, f *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	if len(f.SubscriptionStatus) == 0 {
		f.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive}
	}
	return r.Repository.List(ctx, f)
}

func TestBackfillCoversEverySubscriptionStatus(t *testing.T) {
	ctx := testutil.SetupContext()
	svc, _ := newTestService(t, ctx, testSources{})
	s := svc.(*journalService)
	s.subRepo = activeByDefault{s.subRepo}
	contracts := s.contractRepo.(*testutil.InMemoryContractStore)

	tests := []struct {
		status      types.SubscriptionStatus
		cancelledAt *time.Time
		want        types.ContractStatus
	}{
		{types.SubscriptionStatusCancelled, lo.ToPtr(inIST(time.February, 1)), types.ContractStatusEnded},
		{types.SubscriptionStatusPaused, nil, types.ContractStatusActive},
	}
	for _, tt := range tests {
		subID := "subs_" + string(tt.status)
		require.NoError(t, s.subRepo.CreateWithLineItems(ctx, &subscription.Subscription{ID: subID, CustomerID: "cust_1", Currency: "usd",
			Timezone: "Asia/Kolkata", SubscriptionStatus: tt.status, StartDate: inIST(time.January, 1), CancelledAt: tt.cancelledAt,
			BaseModel: types.GetDefaultBaseModel(ctx)}, nil))
		require.NoError(t, contracts.CreateWithAssociation(ctx,
			domainContract.NewBuilder(nil).WithID("contract_"+string(tt.status)).WithContractStatus(types.ContractStatusDraft).WithStatus(types.StatusPublished).Build(),
			domainContract.NewAssociationBuilder(nil).WithID("assoc_"+string(tt.status)).WithEntityType(types.RevRecEntityTypeSubscription).
				WithEntityID(subID).WithStatus(types.StatusPublished).Build()))
	}

	require.NoError(t, s.Backfill(ctx, BackfillRequest{AsOf: inIST(time.March, 1), OnProgress: func(RecognitionCursor) {}}))
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			c, err := s.contractRepo.GetBySubscriptionID(ctx, "subs_"+string(tt.status))
			require.NoError(t, err)
			require.Equal(t, tt.want, c.ContractStatus(), "backfill synced the contract")
		})
	}
}

// TestRecognizeStandaloneInvoices recognizes invoices of a subscription with no contract from their own lines.
func TestRecognizeStandaloneInvoices(t *testing.T) {
	asOf := inIST(time.April, 2).Add(2 * time.Hour)
	standaloneInvoice := func(ctx context.Context, id string, amount int64, finalizedAt, start, end time.Time) *invoice.Invoice {
		inv := subscriptionInvoice(ctx, id, types.InvoiceTypeSubscription, amount, finalizedAt, start, end)
		inv.SubscriptionID = lo.ToPtr("subs_standalone")
		inv.LineItems[0].SubscriptionLineItemID = lo.ToPtr("sli_standalone")
		return inv
	}
	byDate := func(entries []*domainJournal.Entry, account types.LedgerAccountKey) map[string]string {
		amounts := map[string]decimal.Decimal{}
		for _, e := range entries {
			for _, l := range e.Lines() {
				if l.Account() == account {
					day := e.BusinessDate().Format("2006-01-02")
					amounts[day] = amounts[day].Add(l.Net())
				}
			}
		}
		return lo.MapValues(amounts, func(d decimal.Decimal, _ string) string { return d.String() })
	}

	tests := []struct {
		name        string
		invoice     func(context.Context) *invoice.Invoice
		wantRevenue map[string]string
	}{
		{
			name: "annual prepaid earns daily from January",
			invoice: func(ctx context.Context) *invoice.Invoice {
				return standaloneInvoice(ctx, "inv_annual", 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
			},
			// 31, 59 and 90 of 365 days: 101.92, 193.97 and 295.89 to date
			wantRevenue: map[string]string{"2026-01-31": "101.92", "2026-02-28": "92.05", "2026-03-31": "101.92"},
		},
		{
			name: "arrears catch up in the month billed",
			invoice: func(ctx context.Context) *invoice.Invoice {
				return standaloneInvoice(ctx, "inv_arrears", 590, inIST(time.March, 1), inIST(time.January, 1), inIST(time.March, 1))
			},
			wantRevenue: map[string]string{"2026-03-31": "590"},
		},
		{
			name: "a void reverses what was recognized",
			invoice: func(ctx context.Context) *invoice.Invoice {
				inv := standaloneInvoice(ctx, "inv_voided", 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
				inv.InvoiceStatus, inv.VoidedAt = types.InvoiceStatusVoided, lo.ToPtr(inIST(time.February, 10))
				return inv
			},
			wantRevenue: map[string]string{"2026-01-31": "101.92", "2026-02-28": "-101.92"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			inv := tt.invoice(ctx)
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
			s := svc.(*journalService)
			require.NoError(t, s.subRepo.Create(ctx, &subscription.Subscription{ID: "subs_standalone", Timezone: "Asia/Kolkata", BaseModel: types.GetDefaultBaseModel(ctx)}))

			run := func() []*domainJournal.Entry {
				require.NoError(t, s.recognizeEnvironment(ctx, RecognitionRunRequest{AsOf: asOf, OnProgress: func(RecognitionCursor) {}}, ""))
				entries, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(),
					SourceEntityType: types.RevRecEntityTypeInvoice, SourceEntityIDs: []string{inv.ID}})
				require.NoError(t, err)
				return entries
			}
			entries := run()

			billing := lo.Filter(entries, func(e *domainJournal.Entry, _ int) bool { return e.EventType() == types.JournalEventInvoiceFinalized })
			require.Len(t, billing, 1)
			require.Empty(t, billing[0].ContractID())
			for _, l := range billing[0].Lines() {
				require.Empty(t, l.POBID(), "a standalone line defers without a POB")
			}
			earned := lo.Filter(entries, func(e *domainJournal.Entry, _ int) bool { return e.EventType() == types.JournalEventRevenueEarned })
			require.Equal(t, tt.wantRevenue, byDate(earned, types.LedgerAccountRevenue))

			deferred := decimal.Zero
			for _, e := range entries {
				for _, l := range e.Lines() {
					if l.Account() == types.LedgerAccountDeferredRevenue {
						deferred = deferred.Add(l.Net())
					}
				}
			}
			recognized := decimal.Zero
			for _, amount := range byDate(earned, types.LedgerAccountRevenue) {
				recognized = recognized.Add(decimal.RequireFromString(amount))
			}
			if inv.InvoiceStatus == types.InvoiceStatusVoided {
				require.True(t, deferred.IsZero(), "the void and its reversal clear deferred, got %s", deferred)
			} else {
				require.Equal(t, decimal.NewFromInt(inv.Total.IntPart()).Sub(recognized).String(), deferred.String(), "deferred is what is left to earn")
			}

			require.Len(t, run(), len(entries), "a rerun posts nothing")
		})
	}
}

func TestPostCreditNoteStandalone(t *testing.T) {
	ctx := testutil.SetupContext()
	base := types.GetDefaultBaseModel(ctx)
	inv := subscriptionInvoice(ctx, "inv_standalone", types.InvoiceTypeSubscription, 100, finalizedAt, finalizedAt, finalizedAt.AddDate(0, 1, 0))
	inv.SubscriptionID = lo.ToPtr("subs_standalone")
	cn := &creditnote.CreditNote{ID: "cn_standalone", InvoiceID: inv.ID, CustomerID: "cust_1", SubscriptionID: lo.ToPtr("subs_standalone"),
		CreditNoteStatus: types.CreditNoteStatusFinalized, CreditNoteType: types.CreditNoteTypeAdjustment, Currency: "usd",
		TotalAmount: decimal.NewFromInt(10), FinalizedAt: lo.ToPtr(finalizedAt), BaseModel: base,
		LineItems: []*creditnote.CreditNoteLineItem{{ID: "cnl_standalone", InvoiceLineItemID: inv.LineItems[0].ID, Amount: decimal.NewFromInt(10), BaseModel: base}}}
	svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{inv}})
	require.NoError(t, svc.(*journalService).subRepo.Create(ctx, &subscription.Subscription{ID: "subs_standalone", Timezone: "Asia/Kolkata", BaseModel: base}))

	require.NoError(t, svc.PostCreditNote(ctx, cn))
	entries, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), SourceEntityIDs: []string{cn.ID}})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	contra := lo.Filter(entries[0].Lines(), func(l *domainJournal.Line, _ int) bool { return l.Account() == types.LedgerAccountContraRevenue })
	require.Len(t, contra, 1)
	require.Empty(t, contra[0].POBID())
}

// TestRecognizeAllocationNotBilling is ERD Example 10: two POBs allocated 60,000 each over 2026, billed 18,000 and 12,000
// for Q1 on Apr 1. Each earns 5,000 a month whatever it bills; billing only moves its unbilled.
func TestRecognizeAllocationNotBilling(t *testing.T) {
	ctx := testutil.SetupContext()
	yearEnd := inIST(time.January, 1).AddDate(1, 0, 0)
	q1 := subscriptionInvoice(ctx, "inv_q1", types.InvoiceTypeSubscription, 30000, inIST(time.April, 1), inIST(time.January, 1), inIST(time.April, 1))
	q1.LineItems[0].Amount = decimal.NewFromInt(18000)
	q1.LineItems = append(q1.LineItems, &invoice.InvoiceLineItem{ID: "inv_q1_usage_line", SubscriptionLineItemID: lo.ToPtr("sli_b"), PriceID: linePrice(lo.ToPtr("sli_b")),
		Amount: decimal.NewFromInt(12000), PeriodStart: lo.ToPtr(inIST(time.January, 1)), PeriodEnd: lo.ToPtr(inIST(time.April, 1)),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)})

	svc, store := newTestService(t, ctx, testSources{
		invoices: []*invoice.Invoice{q1},
		pob1:     testPOB1(decimal.NewFromInt(60000), yearEnd),
		pobs: []*domainPOB.PerformanceObligation{domainPOB.NewBuilder(nil).WithID("pob_b").WithContractID("contract_1").WithProductID("prod_b").
			WithRecognitionMethod(types.RecognitionMethodStraightLine).WithAmortizationMethod(types.AmortizationPeriodEven).
			WithAllocatedAmount(decimal.NewFromInt(60000)).WithServiceStart(inIST(time.January, 1)).WithServiceEnd(lo.ToPtr(yearEnd)).
			WithPOBStatus(types.PerformanceObligationStatusActive).WithStatus(types.StatusPublished).Build()},
		prices: []*price.Price{{ID: "price_sli_b", ProductID: "prod_b", Type: types.PRICE_TYPE_FIXED, BillingPeriod: types.BILLING_PERIOD_QUARTER, BaseModel: types.GetDefaultBaseModel(ctx)}},
	})
	require.NoError(t, svc.PostInvoice(ctx, q1))
	_, err := svc.Recognize(ctx, RecognizeRequest{ContractID: "contract_1", AsOf: inIST(time.May, 1).Add(2 * time.Hour)})
	require.NoError(t, err)

	balances := func(pobID string) map[types.LedgerAccountKey]string {
		saved, err := store.InMemoryStore.List(ctx, nil, nil, nil)
		require.NoError(t, err)
		sums := map[types.LedgerAccountKey]decimal.Decimal{
			types.LedgerAccountDeferredRevenue: decimal.Zero, types.LedgerAccountUnbilledReceivable: decimal.Zero, types.LedgerAccountRevenue: decimal.Zero,
		}
		for _, e := range saved {
			for _, l := range e.Lines() {
				if l.POBID() == pobID {
					sums[l.Account()] = sums[l.Account()].Add(l.Account().NormalBalance(l.Net()))
				}
			}
		}
		return lo.MapValues(sums, func(v decimal.Decimal, _ types.LedgerAccountKey) string { return v.String() })
	}
	require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "0", types.LedgerAccountUnbilledReceivable: "2000", types.LedgerAccountRevenue: "20000"},
		balances("pob_1"), "billed 18,000 against 20,000 earned")
	require.Equal(t, map[types.LedgerAccountKey]string{types.LedgerAccountDeferredRevenue: "0", types.LedgerAccountUnbilledReceivable: "8000", types.LedgerAccountRevenue: "20000"},
		balances("pob_b"), "billed 12,000 against 20,000 earned")
}

// TestRecognizeScoped runs recognition for one contract or one customer only.
func TestRecognizeScoped(t *testing.T) {
	asOf := inIST(time.March, 2).Add(2 * time.Hour)
	tests := []struct {
		name            string
		scope           recognition.Scope
		wantPOB         bool
		wantInvoicesFor []string
	}{
		{name: "a contract: its POBs, no standalone invoices", scope: recognition.Scope{ContractID: "contract_1"}, wantPOB: true},
		{name: "a customer: only that customer's standalone invoices", scope: recognition.Scope{CustomerID: "cust_2"}, wantInvoicesFor: []string{"inv_cust_2"}},
		{name: "the environment: everything due", wantPOB: true, wantInvoicesFor: []string{"inv_cust_1", "inv_cust_2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			annual := subscriptionInvoice(ctx, "inv_annual", types.InvoiceTypeSubscription, 1200, inIST(time.January, 1), inIST(time.January, 1), inIST(time.January, 1).AddDate(1, 0, 0))
			standalone := func(id, customerID string) *invoice.Invoice {
				inv := subscriptionInvoice(ctx, id, types.InvoiceTypeSubscription, 300, inIST(time.January, 1), inIST(time.January, 1), inIST(time.April, 1))
				inv.CustomerID, inv.SubscriptionID = customerID, lo.ToPtr("subs_standalone")
				return inv
			}
			// Another customer's invoice whose billing cannot post: it fails the environment run, never a scoped one.
			broken := standalone("inv_broken", "cust_3")
			broken.SubscriptionID = lo.ToPtr("subs_missing")
			svc, store := newTestService(t, ctx, testSources{invoices: []*invoice.Invoice{annual, standalone("inv_cust_1", "cust_1"), standalone("inv_cust_2", "cust_2"), broken}})
			s := svc.(*journalService)
			require.NoError(t, s.subRepo.Create(ctx, &subscription.Subscription{ID: "subs_standalone", Timezone: "Asia/Kolkata", BaseModel: types.GetDefaultBaseModel(ctx)}))
			require.NoError(t, s.customerRepo.Create(ctx, &customer.Customer{ID: "cust_2", Timezone: "Asia/Kolkata", BaseModel: types.GetDefaultBaseModel(ctx)}))

			err := s.recognizeEnvironment(ctx, RecognitionRunRequest{AsOf: asOf, Scope: tt.scope, OnProgress: func(RecognitionCursor) {}}, "")
			if tt.scope == (recognition.Scope{}) {
				require.ErrorContains(t, err, "invoice inv_broken")
			} else {
				require.NoError(t, err, "a scoped run posts only its customer's billing")
			}

			earned, err := store.List(ctx, &journalRepo.Filter{QueryFilter: types.NewNoLimitQueryFilter(), EventTypes: []types.JournalEventType{types.JournalEventRevenueEarned}})
			require.NoError(t, err)
			recognizedPOB := lo.ContainsBy(earned, func(e *domainJournal.Entry) bool { return e.SourceEntityType() == types.RevRecEntityTypePOB })
			invoices := lo.Uniq(lo.FilterMap(earned, func(e *domainJournal.Entry, _ int) (string, bool) {
				return e.SourceEntityID(), e.SourceEntityType() == types.RevRecEntityTypeInvoice
			}))
			sort.Strings(invoices)
			require.Equal(t, tt.wantPOB, recognizedPOB, "contract_1's POB recognized")
			require.Equal(t, tt.wantInvoicesFor, lo.Ternary(len(invoices) == 0, nil, invoices))
		})
	}
}
