package types

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

// CatchUpMode is where a superseding schedule segment starts.
type CatchUpMode string

const (
	CatchUpProspective   CatchUpMode = "prospective"   // from the effective date
	CatchUpRetrospective CatchUpMode = "retrospective" // over the original interval
)

type AccountingPeriodStatus string

const (
	AccountingPeriodStatusOpen   AccountingPeriodStatus = "open"
	AccountingPeriodStatusClosed AccountingPeriodStatus = "closed" // reserved; close is deferred
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

type StatementType string

const (
	StatementTypeBalanceSheet    StatementType = "balance_sheet"
	StatementTypeIncomeStatement StatementType = "income_statement"
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

func (k LedgerAccountKey) Statement() StatementType {
	switch k.Class() {
	case AccountClassRevenue, AccountClassContraRevenue:
		return StatementTypeIncomeStatement
	case AccountClassAsset, AccountClassLiability:
		return StatementTypeBalanceSheet
	default:
		return ""
	}
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
