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
