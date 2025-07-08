package invoice

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Invoice represents the invoice domain model
type Invoice struct {
	// id is the unique identifier for this invoice
	ID string `json:"id"`

	// customer_id is the ID of the customer who will receive this invoice
	CustomerID string `json:"customer_id"`

	// subscription_id is the ID of the subscription this invoice is associated with (only present for subscription-based invoices)
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// invoice_type indicates the type of invoice - whether this is a subscription invoice, one-time charge, or other billing type
	InvoiceType types.InvoiceType `json:"invoice_type"`

	// invoice_status represents the current status of the invoice - values include draft, open, paid, void, etc.
	InvoiceStatus types.InvoiceStatus `json:"invoice_status"`

	// payment_status indicates whether the invoice has been paid, is pending, or failed
	PaymentStatus types.PaymentStatus `json:"payment_status"`

	// currency is the three-letter ISO currency code (e.g., USD, EUR, GBP) that applies to all monetary amounts on this invoice
	Currency string `json:"currency"`

	// amount_due is the total amount that needs to be paid for this invoice
	AmountDue decimal.Decimal `json:"amount_due"`

	// amount_paid is the amount that has already been paid towards this invoice
	AmountPaid decimal.Decimal `json:"amount_paid"`

	// subtotal is the sum of all line items before any taxes, discounts, or additional fees
	Subtotal decimal.Decimal `json:"subtotal"`

	// total is the final amount including taxes, fees, and discounts
	Total decimal.Decimal `json:"total"`

	// amount_remaining is the outstanding amount still owed on this invoice (calculated as amount_due minus amount_paid)
	AmountRemaining decimal.Decimal `json:"amount_remaining"`

	// invoice_number is the human-readable invoice number displayed to customers (e.g., INV-2024-001)
	InvoiceNumber *string `json:"invoice_number"`

	// idempotency_key is a unique key used to prevent duplicate invoice creation when retrying API calls
	IdempotencyKey *string `json:"idempotency_key"`

	// billing_sequence is the sequential number indicating the billing cycle for subscription invoices
	BillingSequence *int `json:"billing_sequence"`

	// description is an optional description or notes about this invoice
	Description string `json:"description,omitempty"`

	// due_date is the date when payment for this invoice is due
	DueDate *time.Time `json:"due_date,omitempty"`

	// paid_at is the timestamp when this invoice was fully paid
	PaidAt *time.Time `json:"paid_at,omitempty"`

	// voided_at is the timestamp when this invoice was voided or cancelled
	VoidedAt *time.Time `json:"voided_at,omitempty"`

	// billing_period describes the billing period this invoice covers (e.g., "January 2024", "Q1 2024")
	BillingPeriod *string `json:"billing_period,omitempty"`

	// finalized_at is the timestamp when this invoice was finalized and made ready for payment
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`

	// period_start is the start date of the billing period covered by this invoice
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the end date of the billing period covered by this invoice
	PeriodEnd *time.Time `json:"period_end,omitempty"`

	// invoice_pdf_url is the URL where customers can download the PDF version of this invoice
	InvoicePDFURL *string `json:"invoice_pdf_url,omitempty"`

	// billing_reason indicates why this invoice was generated (e.g., "subscription_billing", "manual_charge")
	BillingReason string `json:"billing_reason,omitempty"`

	// metadata contains custom key-value pairs for storing additional information about this invoice
	Metadata types.Metadata `json:"metadata,omitempty"`

	// line_items contains the individual items and charges that make up this invoice
	LineItems []*InvoiceLineItem `json:"line_items,omitempty"`

	// version is the version number for tracking changes to this invoice
	Version int `json:"version"`

	// environment_id is the ID of the environment this invoice belongs to (for multi-environment setups)
	EnvironmentID string `json:"environment_id"`

	// adjustment_amount is the total sum of credit notes of type "adjustment".
	// These are non-cash reductions applied to the invoice (e.g. goodwill credit, billing correction).
	AdjustmentAmount decimal.Decimal `json:"adjustment_amount"`

	// refunded_amount is the total sum of credit notes of type "refund".
	// These are actual refunds issued to the customer.
	RefundedAmount decimal.Decimal `json:"refunded_amount"`
	// TotalTax holds the value of the "total_tax" field.
	TotalTax decimal.Decimal `json:"total_tax"`

	// common fields including tenant information, creation/update timestamps, and status
	types.BaseModel
}

// FromEnt converts an ent.Invoice to domain Invoice
func FromEnt(e *ent.Invoice) *Invoice {
	if e == nil {
		return nil
	}

	var lineItems []*InvoiceLineItem
	if e.Edges.LineItems != nil {
		lineItems = make([]*InvoiceLineItem, len(e.Edges.LineItems))
		for i, item := range e.Edges.LineItems {
			lineItem := &InvoiceLineItem{}
			lineItems[i] = lineItem.FromEnt(item)
		}
	}

	return &Invoice{
		ID:               e.ID,
		CustomerID:       e.CustomerID,
		SubscriptionID:   e.SubscriptionID,
		InvoiceType:      types.InvoiceType(e.InvoiceType),
		InvoiceStatus:    types.InvoiceStatus(e.InvoiceStatus),
		PaymentStatus:    types.PaymentStatus(e.PaymentStatus),
		Currency:         e.Currency,
		AmountDue:        e.AmountDue,
		AmountPaid:       e.AmountPaid,
		Subtotal:         e.Subtotal,
		Total:            e.Total,
		AmountRemaining:  e.AmountRemaining,
		AdjustmentAmount: e.AdjustmentAmount,
		RefundedAmount:   e.RefundedAmount,
		InvoiceNumber:    e.InvoiceNumber,
		IdempotencyKey:   e.IdempotencyKey,
		BillingSequence:  e.BillingSequence,
		Description:      e.Description,
		DueDate:          e.DueDate,
		PaidAt:           e.PaidAt,
		VoidedAt:         e.VoidedAt,
		FinalizedAt:      e.FinalizedAt,
		BillingPeriod:    e.BillingPeriod,
		PeriodStart:      e.PeriodStart,
		PeriodEnd:        e.PeriodEnd,
		InvoicePDFURL:    e.InvoicePdfURL,
		BillingReason:    e.BillingReason,
		Metadata:         e.Metadata,
		LineItems:        lineItems,
		Version:          e.Version,
		EnvironmentID:    e.EnvironmentID,
		TotalTax:         e.TotalTax,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// Default helper methods

func (i *Invoice) GetRemainingAmount() decimal.Decimal {
	return i.AmountDue.Sub(i.AmountPaid)
}

func (i *Invoice) Validate() error {
	// amount validations
	if i.AmountDue.IsNegative() {
		return ierr.NewError("invoice validation failed").WithHint("amount_due must be non negative").Mark(ierr.ErrValidation)
	}

	if i.AmountPaid.IsNegative() {
		return ierr.NewError("invoice validation failed").WithHint("amount_paid must be non negative").Mark(ierr.ErrValidation)
	}

	if i.AmountPaid.GreaterThan(i.AmountDue) {
		return ierr.NewError("invoice validation failed").WithHint("amount_paid must be less than or equal to amount_due").Mark(ierr.ErrValidation)
	}

	if i.AmountRemaining.IsNegative() {
		return ierr.NewError("invoice validation failed").WithHint("amount_remaining must be non negative").Mark(ierr.ErrValidation)
	}

	if i.AmountRemaining.GreaterThan(i.AmountDue) {
		return ierr.NewError("invoice validation failed").WithHint("amount_remaining must be less than or equal to amount_due").Mark(ierr.ErrValidation)
	}

	if !i.AmountPaid.Add(i.AmountRemaining).Equal(i.AmountDue) {
		return ierr.NewError("invoice validation failed").WithHint("amount_remaining must equal amount_due - amount_paid").Mark(ierr.ErrValidation)
	}

	if i.PeriodStart != nil && i.PeriodEnd != nil {
		if i.PeriodEnd.Before(*i.PeriodStart) {
			return ierr.NewError("invoice validation failed").WithHint("period_end must be after period_start").Mark(ierr.ErrValidation)
		}
	}

	if i.InvoiceType == types.InvoiceTypeSubscription && i.BillingPeriod == nil {
		return ierr.NewError("invoice validation failed").WithHint("billing_period must be set for subscription invoices").Mark(ierr.ErrValidation)
	}

	// validate line items if present
	if i.LineItems != nil {
		for _, item := range i.LineItems {
			if item.Currency != i.Currency {
				return ierr.NewError("invoice validation failed").WithHint("line_items currency must match invoice currency").Mark(ierr.ErrValidation)
			}
			if err := item.Validate(); err != nil {
				return err
			}
		}
	}

	return nil
}
