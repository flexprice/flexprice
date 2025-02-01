package dto

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

// CreateInvoiceRequest represents the request to create a new invoice
type CreateInvoiceRequest struct {
	CustomerID     string                         `json:"customer_id" validate:"required"`
	SubscriptionID *string                        `json:"subscription_id,omitempty"`
	IdempotencyKey *string                        `json:"idempotency_key"`
	InvoiceType    types.InvoiceType              `json:"invoice_type" validate:"required"`
	Currency       string                         `json:"currency" validate:"required"`
	AmountDue      decimal.Decimal                `json:"amount_due" validate:"required"`
	Description    string                         `json:"description,omitempty"`
	DueDate        *time.Time                     `json:"due_date,omitempty"`
	PeriodStart    *time.Time                     `json:"period_start,omitempty"`
	PeriodEnd      *time.Time                     `json:"period_end,omitempty"`
	BillingReason  types.InvoiceBillingReason     `json:"billing_reason"`
	InvoiceStatus  *types.InvoiceStatus           `json:"invoice_status,omitempty"`
	PaymentStatus  *types.InvoicePaymentStatus    `json:"payment_status,omitempty"`
	AmountPaid     *decimal.Decimal               `json:"amount_paid,omitempty"`
	LineItems      []CreateInvoiceLineItemRequest `json:"line_items,omitempty"`
	Metadata       types.Metadata                 `json:"metadata,omitempty"`
}

func (r *CreateInvoiceRequest) Validate() error {
	validate := validator.New()
	if err := validate.Struct(r); err != nil {
		return err
	}

	if r.AmountDue.IsNegative() {
		return fmt.Errorf("amount_due must be non-negative")
	}

	if r.InvoiceType == types.InvoiceTypeSubscription {
		if r.SubscriptionID == nil {
			return fmt.Errorf("subscription_id is required for subscription invoice")
		}

		if r.PeriodStart == nil {
			return fmt.Errorf("period_start is required for subscription invoice")
		}

		if r.PeriodEnd == nil {
			return fmt.Errorf("period_end is required for subscription invoice")
		}

		if r.PeriodEnd.Before(*r.PeriodStart) {
			return fmt.Errorf("period_end must be after period_start")
		}
	}

	// Validate line items if present
	if len(r.LineItems) > 0 {
		var totalAmount decimal.Decimal
		for _, item := range r.LineItems {
			if err := item.Validate(); err != nil {
				return fmt.Errorf("invalid line item: %w", err)
			}
			totalAmount = totalAmount.Add(item.Amount)
		}

		// Verify total amount matches invoice amount
		if !totalAmount.Equal(r.AmountDue) {
			return fmt.Errorf("sum of line item amounts (%s) must equal invoice amount_due (%s)", totalAmount.String(), r.AmountDue.String())
		}
	}

	return nil
}

func (r *CreateInvoiceRequest) ToInvoice(ctx context.Context) (*invoice.Invoice, error) {
	inv := &invoice.Invoice{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
		CustomerID:      r.CustomerID,
		SubscriptionID:  r.SubscriptionID,
		InvoiceType:     r.InvoiceType,
		Currency:        r.Currency,
		AmountDue:       r.AmountDue,
		Description:     r.Description,
		DueDate:         r.DueDate,
		PeriodStart:     r.PeriodStart,
		PeriodEnd:       r.PeriodEnd,
		BillingReason:   string(r.BillingReason),
		Metadata:        r.Metadata,
		BaseModel:       types.GetDefaultBaseModel(ctx),
		AmountRemaining: decimal.Zero,
	}

	// Default invoice status and payment status
	if r.InvoiceStatus != nil {
		inv.InvoiceStatus = *r.InvoiceStatus
	}

	if r.PaymentStatus != nil {
		inv.PaymentStatus = *r.PaymentStatus
	} else {
		inv.PaymentStatus = types.InvoicePaymentStatusPending
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
	PriceID          string          `json:"price_id" validate:"required"`
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

func (r *CreateInvoiceLineItemRequest) Validate() error {
	validate := validator.New()
	if err := validate.Struct(r); err != nil {
		return err
	}

	if r.Amount.IsNegative() {
		return fmt.Errorf("amount must be non-negative")
	}

	if r.Quantity.IsNegative() {
		return fmt.Errorf("quantity must be non-negative")
	}

	if r.PeriodStart != nil && r.PeriodEnd != nil {
		if r.PeriodEnd.Before(*r.PeriodStart) {
			return fmt.Errorf("period_end must be after period_start")
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
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}

// InvoiceLineItemResponse represents a line item in responses
type InvoiceLineItemResponse struct {
	ID               string          `json:"id"`
	InvoiceID        string          `json:"invoice_id"`
	CustomerID       string          `json:"customer_id"`
	SubscriptionID   *string         `json:"subscription_id,omitempty"`
	PriceID          string          `json:"price_id"`
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
	PaymentStatus types.InvoicePaymentStatus `json:"payment_status" validate:"required"`
}

func (r *UpdateInvoicePaymentRequest) Validate() error {
	if r.PaymentStatus == "" {
		return fmt.Errorf("payment_status is required")
	}
	return nil
}

// UpdateInvoicePaymentStatusRequest represents a request to update an invoice's payment status
type UpdateInvoicePaymentStatusRequest struct {
	PaymentStatus types.InvoicePaymentStatus `json:"payment_status" binding:"required"`
	Amount        *decimal.Decimal           `json:"amount,omitempty"`
}

func (r *UpdateInvoicePaymentStatusRequest) Validate() error {
	if r.Amount != nil && r.Amount.IsNegative() {
		return fmt.Errorf("amount must be non-negative")
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
	PaymentStatus   types.InvoicePaymentStatus `json:"payment_status"`
	Currency        string                     `json:"currency"`
	AmountDue       decimal.Decimal            `json:"amount_due"`
	AmountPaid      decimal.Decimal            `json:"amount_paid"`
	AmountRemaining decimal.Decimal            `json:"amount_remaining"`
	InvoiceNumber   *string                    `json:"invoice_number,omitempty"`
	IdempotencyKey  *string                    `json:"idempotency_key,omitempty"`
	BillingSequence *int                       `json:"billing_sequence,omitempty"`
	Description     string                     `json:"description,omitempty"`
	DueDate         *time.Time                 `json:"due_date,omitempty"`
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

	// Edges
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`
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
