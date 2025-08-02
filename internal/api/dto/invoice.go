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

// CreateInvoiceRequest represents the request payload for creating a new invoice
type CreateInvoiceRequest struct {
	// invoice_number is an optional human-readable identifier for the invoice
	InvoiceNumber *string `json:"invoice_number,omitempty"`

	// customer_id is the unique identifier of the customer this invoice belongs to
	CustomerID string `json:"customer_id" validate:"required"`

	// subscription_id is the optional unique identifier of the subscription associated with this invoice
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// idempotency_key is an optional key used to prevent duplicate invoice creation
	IdempotencyKey *string `json:"idempotency_key"`

	// invoice_type indicates the type of invoice (subscription, one_time, etc.)
	InvoiceType types.InvoiceType `json:"invoice_type"`

	// currency is the three-letter ISO currency code (e.g., USD, EUR) for the invoice
	Currency string `json:"currency" validate:"required"`

	// amount_due is the total amount that needs to be paid for this invoice
	AmountDue decimal.Decimal `json:"amount_due" validate:"required"`

	// total is the total amount of the invoice including taxes and discounts
	Total decimal.Decimal `json:"total" validate:"required"`

	// subtotal is the amount before taxes and discounts are applied
	Subtotal decimal.Decimal `json:"subtotal" validate:"required"`

	// description is an optional text description of the invoice
	Description string `json:"description,omitempty"`

	// due_date is the date by which payment is expected
	DueDate *time.Time `json:"due_date,omitempty"`

	// billing_period is the period this invoice covers (e.g., "monthly", "yearly")
	BillingPeriod *string `json:"billing_period,omitempty"`

	// period_start is the start date of the billing period
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the end date of the billing period
	PeriodEnd *time.Time `json:"period_end,omitempty"`

	// billing_reason indicates why this invoice was created (subscription_cycle, manual, etc.)
	BillingReason types.InvoiceBillingReason `json:"billing_reason"`

	// invoice_status represents the current status of the invoice (draft, finalized, etc.)
	InvoiceStatus *types.InvoiceStatus `json:"invoice_status,omitempty"`

	// payment_status represents the payment status of the invoice (unpaid, paid, etc.)
	PaymentStatus *types.PaymentStatus `json:"payment_status,omitempty"`

	// amount_paid is the amount that has been paid towards this invoice
	AmountPaid *decimal.Decimal `json:"amount_paid,omitempty"`

	// line_items contains the individual items that make up this invoice
	LineItems []CreateInvoiceLineItemRequest `json:"line_items,omitempty"`

	// metadata contains additional custom key-value pairs for storing extra information
	Metadata types.Metadata `json:"metadata,omitempty"`

	// environment_id is the unique identifier of the environment this invoice belongs to
	EnvironmentID string `json:"environment_id,omitempty"`

	// invoice_pdf_url is the URL where customers can download the PDF version of this invoice
	InvoicePDFURL *string `json:"invoice_pdf_url,omitempty"`
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

	// url validation if url is provided
	if r.InvoicePDFURL != nil {
		if err := validator.ValidateURL(r.InvoicePDFURL); err != nil {
			return ierr.WithError(err).
				WithHint("invalid invoice_pdf_url").
				Mark(ierr.ErrValidation)
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
		Total:           r.Total,
		Subtotal:        r.Subtotal,
		Description:     r.Description,
		DueDate:         r.DueDate,
		PeriodStart:     r.PeriodStart,
		BillingPeriod:   r.BillingPeriod,
		PeriodEnd:       r.PeriodEnd,
		BillingReason:   string(r.BillingReason),
		Metadata:        r.Metadata,
		InvoicePDFURL:   r.InvoicePDFURL,
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

// CreateInvoiceLineItemRequest represents a single line item in an invoice creation request
type CreateInvoiceLineItemRequest struct {
	// price_id is the optional unique identifier of the price associated with this line item
	PriceID *string `json:"price_id,omitempty"`

	// plan_id is the optional unique identifier of the plan associated with this line item
	PlanID *string `json:"plan_id,omitempty"`

	// plan_display_name is the optional human-readable name of the plan
	PlanDisplayName *string `json:"plan_display_name,omitempty"`

	// price_type indicates the type of pricing (fixed, usage, tiered, etc.)
	PriceType *string `json:"price_type,omitempty"`

	// meter_id is the optional unique identifier of the meter used for usage tracking
	MeterID *string `json:"meter_id,omitempty"`

	// meter_display_name is the optional human-readable name of the meter
	MeterDisplayName *string `json:"meter_display_name,omitempty"`

	// price_unit is the optional 3-digit ISO code of the price unit associated with this line item
	PriceUnit *string `json:"price_unit,omitempty"`

	// price_unit_amount is the optional amount converted to the price unit currency
	PriceUnitAmount *decimal.Decimal `json:"price_unit_amount,omitempty"`

	// display_name is the optional human-readable name for this line item
	DisplayName *string `json:"display_name,omitempty"`

	// amount is the monetary amount for this line item
	Amount decimal.Decimal `json:"amount" validate:"required"`

	// quantity is the quantity of units for this line item
	Quantity decimal.Decimal `json:"quantity" validate:"required"`

	// period_start is the optional start date of the period this line item covers
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the optional end date of the period this line item covers
	PeriodEnd *time.Time `json:"period_end,omitempty"`

	// metadata contains additional custom key-value pairs for storing extra information about this line item
	Metadata types.Metadata `json:"metadata,omitempty"`
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
		PriceUnit:        r.PriceUnit,
		PriceUnitAmount:  r.PriceUnitAmount,
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

// InvoiceLineItemResponse represents a line item in invoice response payloads
type InvoiceLineItemResponse struct {
	// id is the unique identifier for this line item
	ID string `json:"id"`

	// invoice_id is the unique identifier of the invoice this line item belongs to
	InvoiceID string `json:"invoice_id"`

	// customer_id is the unique identifier of the customer associated with this line item
	CustomerID string `json:"customer_id"`

	// subscription_id is the optional unique identifier of the subscription associated with this line item
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// price_id is the optional unique identifier of the price associated with this line item
	PriceID *string `json:"price_id"`

	// plan_id is the optional unique identifier of the plan associated with this line item
	PlanID *string `json:"plan_id,omitempty"`

	// plan_display_name is the optional human-readable name of the plan
	PlanDisplayName *string `json:"plan_display_name,omitempty"`

	// price_type indicates the type of pricing (fixed, usage, tiered, etc.)
	PriceType *string `json:"price_type,omitempty"`

	// meter_id is the optional unique identifier of the meter used for usage tracking
	MeterID *string `json:"meter_id,omitempty"`

	// meter_display_name is the optional human-readable name of the meter
	MeterDisplayName *string `json:"meter_display_name,omitempty"`

	// price_unit_id is the optional unique identifier of the price unit associated with this line item
	PriceUnitID *string `json:"price_unit_id,omitempty"`

	// price_unit is the optional 3-digit ISO code of the price unit associated with this line item
	PriceUnit *string `json:"price_unit,omitempty"`

	// price_unit_amount is the optional amount converted to the price unit currency
	PriceUnitAmount *decimal.Decimal `json:"price_unit_amount,omitempty"`

	// display_name is the optional human-readable name for this line item
	DisplayName *string `json:"display_name,omitempty"`

	// amount is the monetary amount for this line item
	Amount decimal.Decimal `json:"amount"`

	// quantity is the quantity of units for this line item
	Quantity decimal.Decimal `json:"quantity"`

	// currency is the three-letter ISO currency code for this line item
	Currency string `json:"currency"`

	// period_start is the optional start date of the period this line item covers
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the optional end date of the period this line item covers
	PeriodEnd *time.Time `json:"period_end,omitempty"`

	// metadata contains additional custom key-value pairs for storing extra information about this line item
	Metadata types.Metadata `json:"metadata,omitempty"`

	// tenant_id is the unique identifier of the tenant this line item belongs to
	TenantID string `json:"tenant_id"`

	// status represents the current status of this line item
	Status string `json:"status"`

	// created_at is the timestamp when this line item was created
	CreatedAt time.Time `json:"created_at"`

	// updated_at is the timestamp when this line item was last updated
	UpdatedAt time.Time `json:"updated_at"`

	// created_by is the identifier of the user who created this line item
	CreatedBy string `json:"created_by,omitempty"`

	// updated_by is the identifier of the user who last updated this line item
	UpdatedBy string `json:"updated_by,omitempty"`
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
		PriceUnitID:      item.PriceUnitID,
		PriceUnit:        item.PriceUnit,
		PriceUnitAmount:  item.PriceUnitAmount,
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

// UpdateInvoicePaymentRequest represents the request payload for updating invoice payment status
type UpdateInvoicePaymentRequest struct {
	// payment_status is the new payment status to set for the invoice (paid, unpaid, etc.)
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

// UpdatePaymentStatusRequest represents the request payload for updating an invoice's payment status
type UpdatePaymentStatusRequest struct {
	// payment_status is the new payment status to set for the invoice (paid, unpaid, etc.)
	PaymentStatus types.PaymentStatus `json:"payment_status" binding:"required"`

	// amount is the optional payment amount to record
	Amount *decimal.Decimal `json:"amount,omitempty"`
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

// UpdateInvoiceRequest represents the request payload for updating an invoice
type UpdateInvoiceRequest struct {
	// invoice_pdf_url is the URL where customers can download the PDF version of this invoice
	InvoicePDFURL *string `json:"invoice_pdf_url,omitempty"`
}

func (r *UpdateInvoiceRequest) Validate() error {
	// url validation if url is provided
	if r.InvoicePDFURL != nil {
		if err := validator.ValidateURL(r.InvoicePDFURL); err != nil {
			return ierr.WithError(err).
				WithHint("invalid invoice_pdf_url").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// InvoiceResponse represents the response payload containing invoice information
type InvoiceResponse struct {
	// id is the unique identifier for this invoice
	ID string `json:"id"`

	// customer_id is the unique identifier of the customer this invoice belongs to
	CustomerID string `json:"customer_id"`

	// subscription_id is the optional unique identifier of the subscription associated with this invoice
	SubscriptionID *string `json:"subscription_id,omitempty"`

	// invoice_type indicates the type of invoice (subscription, one_time, etc.)
	InvoiceType types.InvoiceType `json:"invoice_type"`

	// invoice_status represents the current status of the invoice (draft, finalized, etc.)
	InvoiceStatus types.InvoiceStatus `json:"invoice_status"`

	// payment_status represents the payment status of the invoice (unpaid, paid, etc.)
	PaymentStatus types.PaymentStatus `json:"payment_status"`

	// currency is the three-letter ISO currency code (e.g., USD, EUR) for the invoice
	Currency string `json:"currency"`

	// amount_due is the total amount that needs to be paid for this invoice
	AmountDue decimal.Decimal `json:"amount_due"`

	// total is the total amount of the invoice including taxes and discounts
	Total decimal.Decimal `json:"total"`

	// subtotal is the amount before taxes and discounts are applied
	Subtotal decimal.Decimal `json:"subtotal"`

	// amount_paid is the amount that has been paid towards this invoice
	AmountPaid decimal.Decimal `json:"amount_paid"`

	// amount_remaining is the amount still outstanding on this invoice
	AmountRemaining decimal.Decimal `json:"amount_remaining"`

	// invoice_number is the optional human-readable identifier for the invoice
	InvoiceNumber *string `json:"invoice_number,omitempty"`

	// idempotency_key is the optional key used to prevent duplicate invoice creation
	IdempotencyKey *string `json:"idempotency_key,omitempty"`

	// billing_sequence is the optional sequence number for billing cycles
	BillingSequence *int `json:"billing_sequence,omitempty"`

	// description is the optional text description of the invoice
	Description string `json:"description,omitempty"`

	// due_date is the date by which payment is expected
	DueDate *time.Time `json:"due_date,omitempty"`

	// billing_period is the period this invoice covers (e.g., "monthly", "yearly")
	BillingPeriod *string `json:"billing_period,omitempty"`

	// period_start is the start date of the billing period
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the end date of the billing period
	PeriodEnd *time.Time `json:"period_end,omitempty"`

	// paid_at is the timestamp when this invoice was paid
	PaidAt *time.Time `json:"paid_at,omitempty"`

	// voided_at is the timestamp when this invoice was voided
	VoidedAt *time.Time `json:"voided_at,omitempty"`

	// finalized_at is the timestamp when this invoice was finalized
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`

	// invoice_pdf_url is the optional URL to the PDF version of this invoice
	InvoicePDFURL *string `json:"invoice_pdf_url,omitempty"`

	// billing_reason indicates why this invoice was created (subscription_cycle, manual, etc.)
	BillingReason string `json:"billing_reason,omitempty"`

	// line_items contains the individual items that make up this invoice
	LineItems []*InvoiceLineItemResponse `json:"line_items,omitempty"`

	// metadata contains additional custom key-value pairs for storing extra information
	Metadata types.Metadata `json:"metadata,omitempty"`

	// version is the version number of this invoice
	Version int `json:"version"`

	// tenant_id is the unique identifier of the tenant this invoice belongs to
	TenantID string `json:"tenant_id"`

	// status represents the current status of this invoice
	Status string `json:"status"`

	// created_at is the timestamp when this invoice was created
	CreatedAt time.Time `json:"created_at"`

	// updated_at is the timestamp when this invoice was last updated
	UpdatedAt time.Time `json:"updated_at"`

	// created_by is the identifier of the user who created this invoice
	CreatedBy string `json:"created_by,omitempty"`

	// updated_by is the identifier of the user who last updated this invoice
	UpdatedBy string `json:"updated_by,omitempty"`

	// subscription contains the associated subscription information if requested
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`

	// customer contains the customer information associated with this invoice
	Customer *CustomerResponse `json:"customer,omitempty"`
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
		Total:           inv.Total,
		Subtotal:        inv.Subtotal,
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

// ListInvoicesResponse represents the paginated response for listing invoices
type ListInvoicesResponse = types.ListResponse[*InvoiceResponse]

// GetPreviewInvoiceRequest represents the request payload for previewing an invoice
type GetPreviewInvoiceRequest struct {
	// subscription_id is the unique identifier of the subscription to preview invoice for
	SubscriptionID string `json:"subscription_id" binding:"required"`

	// period_start is the optional start date of the period to preview
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// period_end is the optional end date of the period to preview
	PeriodEnd *time.Time `json:"period_end,omitempty"`
}

// CustomerInvoiceSummary represents a summary of customer's invoice status for a specific currency
type CustomerInvoiceSummary struct {
	// customer_id is the unique identifier of the customer
	CustomerID string `json:"customer_id"`

	// currency is the three-letter ISO currency code for this summary
	Currency string `json:"currency"`

	// total_revenue_amount is the total revenue generated from this customer in this currency
	TotalRevenueAmount decimal.Decimal `json:"total_revenue_amount"`

	// total_unpaid_amount is the total amount of unpaid invoices in this currency
	TotalUnpaidAmount decimal.Decimal `json:"total_unpaid_amount"`

	// total_overdue_amount is the total amount of overdue invoices in this currency
	TotalOverdueAmount decimal.Decimal `json:"total_overdue_amount"`

	// total_invoice_count is the total number of invoices for this customer in this currency
	TotalInvoiceCount int `json:"total_invoice_count"`

	// unpaid_invoice_count is the number of unpaid invoices for this customer in this currency
	UnpaidInvoiceCount int `json:"unpaid_invoice_count"`

	// overdue_invoice_count is the number of overdue invoices for this customer in this currency
	OverdueInvoiceCount int `json:"overdue_invoice_count"`

	// unpaid_usage_charges is the total amount of unpaid usage-based charges in this currency
	UnpaidUsageCharges decimal.Decimal `json:"unpaid_usage_charges"`

	// unpaid_fixed_charges is the total amount of unpaid fixed charges in this currency
	UnpaidFixedCharges decimal.Decimal `json:"unpaid_fixed_charges"`
}

// CustomerMultiCurrencyInvoiceSummary represents invoice summaries across all currencies for a customer
type CustomerMultiCurrencyInvoiceSummary struct {
	// customer_id is the unique identifier of the customer
	CustomerID string `json:"customer_id"`

	// default_currency is the primary currency for this customer
	DefaultCurrency string `json:"default_currency"`

	// summaries contains the invoice summaries for each currency
	Summaries []*CustomerInvoiceSummary `json:"summaries"`
}

// CreateSubscriptionInvoiceRequest represents the request payload for creating a subscription invoice
type CreateSubscriptionInvoiceRequest struct {
	// subscription_id is the unique identifier of the subscription to create an invoice for
	SubscriptionID string `json:"subscription_id" binding:"required"`

	// period_start is the start date of the billing period for this invoice
	PeriodStart time.Time `json:"period_start" binding:"required"`

	// period_end is the end date of the billing period for this invoice
	PeriodEnd time.Time `json:"period_end" binding:"required"`

	// is_preview indicates whether this is a preview invoice (not saved to database)
	IsPreview bool `json:"is_preview"`

	// reference_point defines the point in time used for calculating usage and charges
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
