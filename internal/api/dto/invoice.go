package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// CreateInvoiceRequest represents the request to create a new invoice
type CreateInvoiceRequest struct {
	InvoiceNumber  *string                        `json:"invoice_number,omitempty"`
	CustomerID     string                         `json:"customer_id" validate:"required"`
	SubscriptionID *string                        `json:"subscription_id,omitempty"`
	IdempotencyKey *string                        `json:"idempotency_key"`
	InvoiceType    types.InvoiceType              `json:"invoice_type"`
	Currency       string                         `json:"currency" validate:"required"`
	AmountDue      decimal.Decimal                `json:"amount_due" validate:"required"`
	Description    string                         `json:"description,omitempty"`
	DueDate        *time.Time                     `json:"due_date,omitempty"`
	BillingPeriod  *string                        `json:"billing_period,omitempty"`
	PeriodStart    *time.Time                     `json:"period_start,omitempty"`
	PeriodEnd      *time.Time                     `json:"period_end,omitempty"`
	BillingReason  types.InvoiceBillingReason     `json:"billing_reason"`
	InvoiceStatus  *types.InvoiceStatus           `json:"invoice_status,omitempty"`
	PaymentStatus  *types.PaymentStatus           `json:"payment_status,omitempty"`
	AmountPaid     *decimal.Decimal               `json:"amount_paid,omitempty"`
	LineItems      []CreateInvoiceLineItemRequest `json:"line_items,omitempty"`
	Metadata       types.Metadata                 `json:"metadata,omitempty"`
	EnvironmentID  string                         `json:"environment_id,omitempty"`
}

func (r *CreateInvoiceRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.InvoiceType.Validate(); err != nil {
		return err
	}

	if r.AmountDue.IsNegative() {
		return ierr.NewError("amount_due must be non-negative").
			WithHint("amount due is negative").
			WithReportableDetails(map[string]any{
				"amount_due": r.AmountDue.String(),
			}).Mark(ierr.ErrValidation)
	}

	if r.InvoiceType == types.InvoiceTypeSubscription {
		if r.SubscriptionID == nil {
			return ierr.NewError("subscription_id is required for subscription invoice").
				WithHint("subscription_id is required for subscription invoice").
				Mark(ierr.ErrValidation)
		}

		if r.BillingPeriod == nil {
			return ierr.NewError("billing_period is required for subscription invoice").
				WithHint("billing_period is required for subscription invoice").
				Mark(ierr.ErrValidation)
		}

		if r.PeriodStart == nil {
			return ierr.NewError("period_start is required for subscription invoice").
				WithHint("period_start is required for subscription invoice").
				Mark(ierr.ErrValidation)
		}

		if r.PeriodEnd == nil {
			return ierr.NewError("period_end is required for subscription invoice").
				WithHint("period_end is required for subscription invoice").
				Mark(ierr.ErrValidation)
		}

		if r.PeriodEnd.Before(*r.PeriodStart) {
			return ierr.NewError("period_end must be after period_start").
				WithHint("period_end must be after period_start").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate line items if present
	if len(r.LineItems) > 0 {
		var totalAmount decimal.Decimal
		for _, item := range r.LineItems {
			if err := item.Validate(r.InvoiceType); err != nil {
				return ierr.WithError(err).WithHint("invalid line item").Mark(ierr.ErrValidation)
			}
			totalAmount = totalAmount.Add(item.Amount)
		}

		// Verify total amount matches invoice amount
		if !totalAmount.Equal(r.AmountDue) {
			return ierr.NewError("sum of line item amounts must equal invoice amount_due").WithHintf("sum of line item amounts %s must equal invoice amount_due %s", totalAmount.String(), r.AmountDue.String()).Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ToInvoice converts a create invoice request to an invoice
func (r *CreateInvoiceRequest) ToInvoice(ctx context.Context) (*invoice.Invoice, error) {
	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return nil, err
	}

	inv := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      r.CustomerID,
		SubscriptionID:  r.SubscriptionID,
		InvoiceType:     r.InvoiceType,
		Currency:        strings.ToLower(r.Currency),
		AmountDue:       r.AmountDue,
		Description:     r.Description,
		DueDate:         r.DueDate,
		PeriodStart:     r.PeriodStart,
		BillingPeriod:   r.BillingPeriod,
		PeriodEnd:       r.PeriodEnd,
		BillingReason:   string(r.BillingReason),
		Metadata:        r.Metadata,
		BaseModel:       types.GetDefaultBaseModel(ctx),
		AmountRemaining: decimal.Zero,
	}

	if r.EnvironmentID != "" {
		inv.EnvironmentID = r.EnvironmentID
	} else {
		inv.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Default invoice status and payment status
	if r.InvoiceStatus != nil {
		inv.InvoiceStatus = *r.InvoiceStatus
	}

	if r.PaymentStatus != nil {
		inv.PaymentStatus = *r.PaymentStatus
	}

	if r.AmountPaid != nil {
		inv.AmountPaid = *r.AmountPaid
	}

	// Convert line items
	if len(r.LineItems) > 0 {
		inv.LineItems = make([]*invoice.InvoiceLineItem, len(r.LineItems))
		for i, item := range r.LineItems {
			inv.LineItems[i] = item.ToInvoiceLineItem(ctx, inv)
		}
	}

	return inv, nil
}

// CreateInvoiceLineItemRequest represents a request to create a line item
type CreateInvoiceLineItemRequest struct {
	PriceID          *string         `json:"price_id,omitempty"`
	PlanID           *string         `json:"plan_id,omitempty"`
	PlanDisplayName  *string         `json:"plan_display_name,omitempty"`
	PriceType        *string         `json:"price_type,omitempty"`
	MeterID          *string         `json:"meter_id,omitempty"`
	MeterDisplayName *string         `json:"meter_display_name,omitempty"`
	DisplayName      *string         `json:"display_name,omitempty"`
	Amount           decimal.Decimal `json:"amount" validate:"required"`
	Quantity         decimal.Decimal `json:"quantity" validate:"required"`
	PeriodStart      *time.Time      `json:"period_start,omitempty"`
	PeriodEnd        *time.Time      `json:"period_end,omitempty"`
	Metadata         types.Metadata  `json:"metadata,omitempty"`
}

func (r *CreateInvoiceLineItemRequest) Validate(invoiceType types.InvoiceType) error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.Amount.IsNegative() {
		return ierr.NewError("amount must be non-negative").
			WithHint("Amount cannot be negative").
			Mark(ierr.ErrValidation)
	}

	if r.Quantity.IsNegative() {
		return ierr.NewError("quantity must be non-negative").
			WithHint("Quantity cannot be negative").
			Mark(ierr.ErrValidation)
	}

	if r.PeriodStart != nil && r.PeriodEnd != nil {
		if r.PeriodEnd.Before(*r.PeriodStart) {
			return ierr.NewError("period_end must be after period_start").
				WithHint("Subscription cannot end before it starts").
				Mark(ierr.ErrValidation)
		}
	}

	if invoiceType == types.InvoiceTypeSubscription {
		if r.PriceID == nil {
			return ierr.NewError("price_id is required for subscription invoice").
				WithHint("price_id is required for subscription invoice").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (r *CreateInvoiceLineItemRequest) ToInvoiceLineItem(ctx context.Context, inv *invoice.Invoice) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
		InvoiceID:        inv.ID,
		CustomerID:       inv.CustomerID,
		SubscriptionID:   inv.SubscriptionID,
		PriceID:          r.PriceID,
		PlanID:           r.PlanID,
		PlanDisplayName:  r.PlanDisplayName,
		PriceType:        r.PriceType,
		MeterID:          r.MeterID,
		MeterDisplayName: r.MeterDisplayName,
		DisplayName:      r.DisplayName,
		Amount:           r.Amount,
		Quantity:         r.Quantity,
		Currency:         inv.Currency,
		PeriodStart:      r.PeriodStart,
		PeriodEnd:        r.PeriodEnd,
		Metadata:         r.Metadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}

// InvoiceLineItemResponse represents a line item in responses
type InvoiceLineItemResponse struct {
	ID               string          `json:"id"`
	InvoiceID        string          `json:"invoice_id"`
	CustomerID       string          `json:"customer_id"`
	SubscriptionID   *string         `json:"subscription_id,omitempty"`
	PriceID          *string         `json:"price_id"`
	PlanID           *string         `json:"plan_id,omitempty"`
	PlanDisplayName  *string         `json:"plan_display_name,omitempty"`
	PriceType        *string         `json:"price_type,omitempty"`
	MeterID          *string         `json:"meter_id,omitempty"`
	MeterDisplayName *string         `json:"meter_display_name,omitempty"`
	DisplayName      *string         `json:"display_name,omitempty"`
	Amount           decimal.Decimal `json:"amount"`
	Quantity         decimal.Decimal `json:"quantity"`
	Currency         string          `json:"currency"`
	PeriodStart      *time.Time      `json:"period_start,omitempty"`
	PeriodEnd        *time.Time      `json:"period_end,omitempty"`
	Metadata         types.Metadata  `json:"metadata,omitempty"`
	TenantID         string          `json:"tenant_id"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	CreatedBy        string          `json:"created_by,omitempty"`
	UpdatedBy        string          `json:"updated_by,omitempty"`
}

func NewInvoiceLineItemResponse(item *invoice.InvoiceLineItem) *InvoiceLineItemResponse {
	if item == nil {
		return nil
	}

	return &InvoiceLineItemResponse{
		ID:               item.ID,
		InvoiceID:        item.InvoiceID,
		CustomerID:       item.CustomerID,
		SubscriptionID:   item.SubscriptionID,
		PlanID:           item.PlanID,
		PlanDisplayName:  item.PlanDisplayName,
		PriceID:          item.PriceID,
		PriceType:        item.PriceType,
		MeterID:          item.MeterID,
		MeterDisplayName: item.MeterDisplayName,
		DisplayName:      item.DisplayName,
		Amount:           item.Amount,
		Quantity:         item.Quantity,
		Currency:         item.Currency,
		PeriodStart:      item.PeriodStart,
		PeriodEnd:        item.PeriodEnd,
		Metadata:         item.Metadata,
		TenantID:         item.TenantID,
		Status:           string(item.Status),
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		CreatedBy:        item.CreatedBy,
		UpdatedBy:        item.UpdatedBy,
	}
}

// UpdateInvoicePaymentRequest represents the request to update invoice payment status
type UpdateInvoicePaymentRequest struct {
	PaymentStatus types.PaymentStatus `json:"payment_status" validate:"required"`
}

func (r *UpdateInvoicePaymentRequest) Validate() error {
	if r.PaymentStatus == "" {
		return ierr.NewError("payment_status is required").
			WithHint("Payment status is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdatePaymentStatusRequest represents a request to update an invoice's payment status
type UpdatePaymentStatusRequest struct {
	PaymentStatus types.PaymentStatus `json:"payment_status" binding:"required"`
	Amount        *decimal.Decimal    `json:"amount,omitempty"`
}

func (r *UpdatePaymentStatusRequest) Validate() error {
	if r.Amount != nil && r.Amount.IsNegative() {
		return ierr.NewError("amount must be non-negative").
			WithHint("Amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"amount": r.Amount.String(),
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// InvoiceResponse represents the response for invoice operations
type InvoiceResponse struct {
	ID              string                     `json:"id"`
	CustomerID      string                     `json:"customer_id"`
	SubscriptionID  *string                    `json:"subscription_id,omitempty"`
	InvoiceType     types.InvoiceType          `json:"invoice_type"`
	InvoiceStatus   types.InvoiceStatus        `json:"invoice_status"`
	PaymentStatus   types.PaymentStatus        `json:"payment_status"`
	Currency        string                     `json:"currency"`
	AmountDue       decimal.Decimal            `json:"amount_due"`
	AmountPaid      decimal.Decimal            `json:"amount_paid"`
	AmountRemaining decimal.Decimal            `json:"amount_remaining"`
	InvoiceNumber   *string                    `json:"invoice_number,omitempty"`
	IdempotencyKey  *string                    `json:"idempotency_key,omitempty"`
	BillingSequence *int                       `json:"billing_sequence,omitempty"`
	Description     string                     `json:"description,omitempty"`
	DueDate         *time.Time                 `json:"due_date,omitempty"`
	BillingPeriod   *string                    `json:"billing_period,omitempty"`
	PeriodStart     *time.Time                 `json:"period_start,omitempty"`
	PeriodEnd       *time.Time                 `json:"period_end,omitempty"`
	PaidAt          *time.Time                 `json:"paid_at,omitempty"`
	VoidedAt        *time.Time                 `json:"voided_at,omitempty"`
	FinalizedAt     *time.Time                 `json:"finalized_at,omitempty"`
	InvoicePDFURL   *string                    `json:"invoice_pdf_url,omitempty"`
	BillingReason   string                     `json:"billing_reason,omitempty"`
	LineItems       []*InvoiceLineItemResponse `json:"line_items,omitempty"`
	Metadata        types.Metadata             `json:"metadata,omitempty"`
	Version         int                        `json:"version"`
	TenantID        string                     `json:"tenant_id"`
	Status          string                     `json:"status"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
	CreatedBy       string                     `json:"created_by,omitempty"`
	UpdatedBy       string                     `json:"updated_by,omitempty"`

	Subscription *SubscriptionResponse `json:"subscription,omitempty"`
	Customer     *CustomerResponse     `json:"customer,omitempty"`
}

// NewInvoiceResponse creates a new invoice response from domain invoice
func NewInvoiceResponse(inv *invoice.Invoice) *InvoiceResponse {
	if inv == nil {
		return nil
	}

	resp := &InvoiceResponse{
		ID:              inv.ID,
		CustomerID:      inv.CustomerID,
		SubscriptionID:  inv.SubscriptionID,
		InvoiceType:     inv.InvoiceType,
		InvoiceStatus:   inv.InvoiceStatus,
		PaymentStatus:   inv.PaymentStatus,
		Currency:        inv.Currency,
		AmountDue:       inv.AmountDue,
		AmountPaid:      inv.AmountPaid,
		AmountRemaining: inv.AmountRemaining,
		InvoiceNumber:   inv.InvoiceNumber,
		IdempotencyKey:  inv.IdempotencyKey,
		BillingSequence: inv.BillingSequence,
		Description:     inv.Description,
		DueDate:         inv.DueDate,
		BillingPeriod:   inv.BillingPeriod,
		PeriodStart:     inv.PeriodStart,
		PeriodEnd:       inv.PeriodEnd,
		PaidAt:          inv.PaidAt,
		VoidedAt:        inv.VoidedAt,
		FinalizedAt:     inv.FinalizedAt,
		InvoicePDFURL:   inv.InvoicePDFURL,
		BillingReason:   inv.BillingReason,
		Metadata:        inv.Metadata,
		Version:         inv.Version,
		TenantID:        inv.TenantID,
		Status:          string(inv.Status),
		CreatedAt:       inv.CreatedAt,
		UpdatedAt:       inv.UpdatedAt,
		CreatedBy:       inv.CreatedBy,
		UpdatedBy:       inv.UpdatedBy,
	}

	if inv.LineItems != nil {
		resp.LineItems = make([]*InvoiceLineItemResponse, len(inv.LineItems))
		for i, item := range inv.LineItems {
			resp.LineItems[i] = NewInvoiceLineItemResponse(item)
		}
	}

	return resp
}

func (r *InvoiceResponse) WithSubscription(sub *SubscriptionResponse) *InvoiceResponse {
	r.Subscription = sub
	return r
}

// WithCustomer adds customer information to the invoice response
func (r *InvoiceResponse) WithCustomer(customer *CustomerResponse) *InvoiceResponse {
	r.Customer = customer
	return r
}

// ListInvoicesResponse represents the response for listing invoices
type ListInvoicesResponse = types.ListResponse[*InvoiceResponse]

type GetPreviewInvoiceRequest struct {
	SubscriptionID string     `json:"subscription_id" binding:"required"`
	PeriodStart    *time.Time `json:"period_start,omitempty"`
	PeriodEnd      *time.Time `json:"period_end,omitempty"`
}

// CustomerInvoiceSummary represents a summary of customer's invoice status
type CustomerInvoiceSummary struct {
	CustomerID          string          `json:"customer_id"`
	Currency            string          `json:"currency"`
	TotalRevenueAmount  decimal.Decimal `json:"total_revenue_amount"`
	TotalUnpaidAmount   decimal.Decimal `json:"total_unpaid_amount"`
	TotalOverdueAmount  decimal.Decimal `json:"total_overdue_amount"`
	TotalInvoiceCount   int             `json:"total_invoice_count"`
	UnpaidInvoiceCount  int             `json:"unpaid_invoice_count"`
	OverdueInvoiceCount int             `json:"overdue_invoice_count"`
	UnpaidUsageCharges  decimal.Decimal `json:"unpaid_usage_charges"`
	UnpaidFixedCharges  decimal.Decimal `json:"unpaid_fixed_charges"`
}

// CustomerMultiCurrencyInvoiceSummary represents invoice summaries across all currencies
type CustomerMultiCurrencyInvoiceSummary struct {
	CustomerID      string                    `json:"customer_id"`
	DefaultCurrency string                    `json:"default_currency"`
	Summaries       []*CustomerInvoiceSummary `json:"summaries"`
}

// CreateSubscriptionInvoiceRequest represents a request to create a subscription invoice
type CreateSubscriptionInvoiceRequest struct {
	SubscriptionID string                      `json:"subscription_id" binding:"required"`
	PeriodStart    time.Time                   `json:"period_start" binding:"required"`
	PeriodEnd      time.Time                   `json:"period_end" binding:"required"`
	IsPreview      bool                        `json:"is_preview"`
	ReferencePoint types.InvoiceReferencePoint `json:"reference_point"`
}

func (r *CreateSubscriptionInvoiceRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.ReferencePoint.Validate(); err != nil {
		return err
	}

	if r.PeriodStart.After(r.PeriodEnd) {
		return ierr.NewError("period_start must be before period_end").
			WithHint("Invoice period start must be before period end").
			Mark(ierr.ErrValidation)
	}
	return nil
}
