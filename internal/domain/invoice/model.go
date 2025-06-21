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
	ID              string              `json:"id"`
	CustomerID      string              `json:"customer_id"`
	SubscriptionID  *string             `json:"subscription_id,omitempty"`
	InvoiceType     types.InvoiceType   `json:"invoice_type"`
	InvoiceStatus   types.InvoiceStatus `json:"invoice_status"`
	PaymentStatus   types.PaymentStatus `json:"payment_status"`
	Currency        string              `json:"currency"`
	AmountDue       decimal.Decimal     `json:"amount_due"`
	AmountPaid      decimal.Decimal     `json:"amount_paid"`
	Subtotal        decimal.Decimal     `json:"subtotal"`
	Total           decimal.Decimal     `json:"total"`
	AmountRemaining decimal.Decimal     `json:"amount_remaining"`
	InvoiceNumber   *string             `json:"invoice_number"`
	IdempotencyKey  *string             `json:"idempotency_key"`
	BillingSequence *int                `json:"billing_sequence"`
	Description     string              `json:"description,omitempty"`
	DueDate         *time.Time          `json:"due_date,omitempty"`
	PaidAt          *time.Time          `json:"paid_at,omitempty"`
	VoidedAt        *time.Time          `json:"voided_at,omitempty"`
	BillingPeriod   *string             `json:"billing_period,omitempty"`
	FinalizedAt     *time.Time          `json:"finalized_at,omitempty"`
	PeriodStart     *time.Time          `json:"period_start,omitempty"`
	PeriodEnd       *time.Time          `json:"period_end,omitempty"`
	InvoicePDFURL   *string             `json:"invoice_pdf_url,omitempty"`
	BillingReason   string              `json:"billing_reason,omitempty"`
	Metadata        types.Metadata      `json:"metadata,omitempty"`
	LineItems       []*InvoiceLineItem  `json:"line_items,omitempty"`
	Version         int                 `json:"version"`
	EnvironmentID   string              `json:"environment_id"`
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
		ID:              e.ID,
		CustomerID:      e.CustomerID,
		SubscriptionID:  e.SubscriptionID,
		InvoiceType:     types.InvoiceType(e.InvoiceType),
		InvoiceStatus:   types.InvoiceStatus(e.InvoiceStatus),
		PaymentStatus:   types.PaymentStatus(e.PaymentStatus),
		Currency:        e.Currency,
		AmountDue:       e.AmountDue,
		AmountPaid:      e.AmountPaid,
		Subtotal:        e.Subtotal,
		Total:           e.Total,
		AmountRemaining: e.AmountRemaining,
		InvoiceNumber:   e.InvoiceNumber,
		IdempotencyKey:  e.IdempotencyKey,
		BillingSequence: e.BillingSequence,
		Description:     e.Description,
		DueDate:         e.DueDate,
		PaidAt:          e.PaidAt,
		VoidedAt:        e.VoidedAt,
		FinalizedAt:     e.FinalizedAt,
		BillingPeriod:   e.BillingPeriod,
		PeriodStart:     e.PeriodStart,
		PeriodEnd:       e.PeriodEnd,
		InvoicePDFURL:   e.InvoicePdfURL,
		BillingReason:   e.BillingReason,
		Metadata:        e.Metadata,
		LineItems:       lineItems,
		Version:         e.Version,
		EnvironmentID:   e.EnvironmentID,
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
