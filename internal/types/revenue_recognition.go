package types

import "github.com/shopspring/decimal"

// AmortizationMethod weights the days of a revenue schedule segment.
type AmortizationMethod string

const (
	AmortizationDaily              AmortizationMethod = "daily"
	AmortizationPeriodEven         AmortizationMethod = "period_even"
	AmortizationPeriodEvenProrated AmortizationMethod = "period_even_prorated"
)

type ContractStatus string

const (
	ContractStatusDraft     ContractStatus = "draft"
	ContractStatusActive    ContractStatus = "active"
	ContractStatusEnded     ContractStatus = "ended"
	ContractStatusCancelled ContractStatus = "cancelled"
)

type PerformanceObligationStatus string

const (
	PerformanceObligationStatusDraft     PerformanceObligationStatus = "draft"
	PerformanceObligationStatusActive    PerformanceObligationStatus = "active"
	PerformanceObligationStatusSatisfied PerformanceObligationStatus = "satisfied"
	PerformanceObligationStatusCancelled PerformanceObligationStatus = "cancelled"
)

type AccountingPeriodStatus string

const (
	AccountingPeriodStatusOpen   AccountingPeriodStatus = "open"
	AccountingPeriodStatusClosed AccountingPeriodStatus = "closed" // reserved; close is deferred
)

// RevRecEntityType is the kind of entity a revrec record links to or is sourced from.
type RevRecEntityType string

const (
	RevRecEntityTypeSubscription         RevRecEntityType = "subscription"
	RevRecEntityTypeSubscriptionLineItem RevRecEntityType = "subscription_line_item"
	RevRecEntityTypeInvoice              RevRecEntityType = "invoice"
	RevRecEntityTypeCreditNote           RevRecEntityType = "credit_note"
	RevRecEntityTypePayment              RevRecEntityType = "payment"
	RevRecEntityTypeRefund               RevRecEntityType = "refund"
	RevRecEntityTypePOB                  RevRecEntityType = "pob"
)

type LedgerAccountKey string

const (
	LedgerAccountCash               LedgerAccountKey = "cash"
	LedgerAccountReceivable         LedgerAccountKey = "accounts_receivable"
	LedgerAccountUnbilledReceivable LedgerAccountKey = "unbilled_receivable"
	LedgerAccountDeferredRevenue    LedgerAccountKey = "deferred_revenue"
	LedgerAccountCustomerCredit     LedgerAccountKey = "customer_credit"
	LedgerAccountTaxPayable         LedgerAccountKey = "tax_payable"
	LedgerAccountRevenue            LedgerAccountKey = "revenue"
	LedgerAccountContraRevenue      LedgerAccountKey = "contra_revenue"
)

// AccountClass groups ledger accounts; it fixes their normal side and statement.
type AccountClass string

const (
	AccountClassAsset         AccountClass = "asset"
	AccountClassLiability     AccountClass = "liability"
	AccountClassRevenue       AccountClass = "revenue"
	AccountClassContraRevenue AccountClass = "contra_revenue"
)

// Class returns the account's class, or "" for an unknown key.
func (k LedgerAccountKey) Class() AccountClass {
	switch k {
	case LedgerAccountCash, LedgerAccountReceivable, LedgerAccountUnbilledReceivable:
		return AccountClassAsset
	case LedgerAccountDeferredRevenue, LedgerAccountCustomerCredit, LedgerAccountTaxPayable:
		return AccountClassLiability
	case LedgerAccountRevenue:
		return AccountClassRevenue
	case LedgerAccountContraRevenue:
		return AccountClassContraRevenue
	default:
		return ""
	}
}

// NormalSide is the side that increases the balance: debit for assets and contra revenue, credit otherwise.
func (k LedgerAccountKey) NormalSide() JournalDirection {
	switch k.Class() {
	case AccountClassAsset, AccountClassContraRevenue:
		return JournalDirectionDebit
	case AccountClassLiability, AccountClassRevenue:
		return JournalDirectionCredit
	default:
		return ""
	}
}

// CarriesPOB is true for accounts whose lines carry the POB they belong to.
func (k LedgerAccountKey) CarriesPOB() bool {
	switch k {
	case LedgerAccountDeferredRevenue, LedgerAccountUnbilledReceivable, LedgerAccountRevenue, LedgerAccountContraRevenue:
		return true
	default:
		return false
	}
}

// NormalBalance turns a credit − debit net into the account's normal direction; debit-normal accounts flip.
// e.g. unbilled −100 → +100; deferred +100 → +100.
func (k LedgerAccountKey) NormalBalance(net decimal.Decimal) decimal.Decimal {
	if k.NormalSide() == JournalDirectionDebit {
		return net.Neg()
	}
	return net
}

type JournalDirection string

const (
	JournalDirectionDebit  JournalDirection = "debit"
	JournalDirectionCredit JournalDirection = "credit"
)

type JournalEventType string

const (
	JournalEventInvoiceFinalized    JournalEventType = "invoice_finalized"
	JournalEventInvoiceVoided       JournalEventType = "invoice_voided"
	JournalEventCreditNoteFinalized JournalEventType = "credit_note_finalized"
	JournalEventCreditNoteVoided    JournalEventType = "credit_note_voided"
	JournalEventPaymentSucceeded    JournalEventType = "payment_succeeded"
	JournalEventPaymentReversed     JournalEventType = "payment_reversed"
	JournalEventRefundSucceeded     JournalEventType = "refund_succeeded"
	JournalEventRevenueEarned       JournalEventType = "revenue_earned"
	JournalEventRevenueOffset       JournalEventType = "revenue_offset"
)

// RevenueReportGroupBy is a key a revenue report can group journal lines by.
type RevenueReportGroupBy string

const (
	RevenueReportGroupByCustomer RevenueReportGroupBy = "customer_id"
	RevenueReportGroupByContract RevenueReportGroupBy = "contract_id"
	RevenueReportGroupByPOB      RevenueReportGroupBy = "pob_id"
)

// MovementCategory groups a rollforward's movements by what changed the balance.
type MovementCategory string

const (
	MovementCategoryInvoiced    MovementCategory = "invoiced"
	MovementCategoryEarned      MovementCategory = "earned"
	MovementCategoryAdjustments MovementCategory = "adjustments"
	MovementCategoryPayments    MovementCategory = "payments"
	MovementCategoryRefunds     MovementCategory = "refunds"
)
