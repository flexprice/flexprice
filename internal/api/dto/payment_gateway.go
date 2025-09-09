package dto

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreatePaymentLinkRequest represents a generic payment link creation request
type CreatePaymentLinkRequest struct {
	InvoiceID              string                    `json:"invoice_id" binding:"required"`
	CustomerID             string                    `json:"customer_id" binding:"required"`
	Amount                 decimal.Decimal           `json:"amount" binding:"required"`
	Currency               string                    `json:"currency" binding:"required"`
	Gateway                *types.PaymentGatewayType `json:"gateway,omitempty"` // Optional, will use preferred if not specified
	SuccessURL             string                    `json:"success_url,omitempty"`
	CancelURL              string                    `json:"cancel_url,omitempty"`
	Metadata               types.Metadata            `json:"metadata,omitempty"`
	Description            string                    `json:"description,omitempty"`
	SaveCardAndMakeDefault bool                      `json:"save_card_and_make_default" default:"false"`
}

// Validate validates the payment link request
func (r *CreatePaymentLinkRequest) Validate() error {
	if r.Amount.IsZero() || r.Amount.IsNegative() {
		return NewValidationError("amount", "Amount must be greater than zero")
	}

	if r.Currency == "" {
		return NewValidationError("currency", "Currency is required")
	}

	if r.InvoiceID == "" {
		return NewValidationError("invoice_id", "Invoice ID is required")
	}

	if r.CustomerID == "" {
		return NewValidationError("customer_id", "Customer ID is required")
	}

	// Validate gateway if provided
	if r.Gateway != nil {
		if err := r.Gateway.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// PaymentLinkResponse represents a generic payment link creation response
type PaymentLinkResponse struct {
	ID              string          `json:"id"`
	PaymentURL      string          `json:"payment_url"`
	PaymentIntentID string          `json:"payment_intent_id,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	Status          string          `json:"status"`
	CreatedAt       int64           `json:"created_at"`
	PaymentID       string          `json:"payment_id,omitempty"`
	Gateway         string          `json:"gateway"`
	ExpiresAt       *int64          `json:"expires_at,omitempty"`
}

// PaymentStatusRequest represents a request to get payment status
type PaymentStatusRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	PaymentID string `json:"payment_id,omitempty"`
}

// GenericPaymentStatusResponse represents a generic payment status response
type GenericPaymentStatusResponse struct {
	SessionID       string            `json:"session_id"`
	PaymentIntentID string            `json:"payment_intent_id,omitempty"`
	Status          string            `json:"status"`
	Amount          decimal.Decimal   `json:"amount"`
	Currency        string            `json:"currency"`
	CustomerID      string            `json:"customer_id,omitempty"`
	CreatedAt       int64             `json:"created_at"`
	ExpiresAt       int64             `json:"expires_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Gateway         string            `json:"gateway"`
}

// GetSupportedGatewaysResponse represents the list of supported gateways
type GetSupportedGatewaysResponse struct {
	Gateways []GatewayInfo `json:"gateways"`
}

// GatewayInfo represents information about a payment gateway
type GatewayInfo struct {
	Type        types.PaymentGatewayType `json:"type"`
	Name        string                   `json:"name"`
	IsActive    bool                     `json:"is_active"`
	IsPreferred bool                     `json:"is_preferred"`
	Metadata    types.Metadata           `json:"metadata,omitempty"`
}

// ValidationError represents a validation error for a specific field
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return e.Message
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) error {
	return ValidationError{
		Field:   field,
		Message: message,
	}
}
