package dto

import (
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreateStripePaymentLinkRequest represents a request to create a Stripe payment link
type CreateStripePaymentLinkRequest struct {
	InvoiceID              string          `json:"invoice_id" binding:"required"`
	CustomerID             string          `json:"customer_id" binding:"required"`
	Amount                 decimal.Decimal `json:"amount" binding:"required"`
	Currency               string          `json:"currency" binding:"required"`
	SuccessURL             string          `json:"success_url,omitempty"`
	CancelURL              string          `json:"cancel_url,omitempty"`
	EnvironmentID          string          `json:"environment_id" binding:"required"`
	Metadata               types.Metadata  `json:"metadata,omitempty"`
	SaveCardAndMakeDefault bool            `json:"save_card_and_make_default" default:"false"`
}

// StripePaymentLinkResponse represents a response from creating a Stripe payment link
type StripePaymentLinkResponse struct {
	ID              string          `json:"id"`
	PaymentURL      string          `json:"payment_url"`
	PaymentIntentID string          `json:"payment_intent_id"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	Status          string          `json:"status"`
	CreatedAt       int64           `json:"created_at"`
	PaymentID       string          `json:"payment_id,omitempty"`
}

// Validate validates the create Stripe payment link request
func (r *CreateStripePaymentLinkRequest) Validate() error {
	if r.InvoiceID == "" {
		return errors.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
			Mark(errors.ErrValidation)
	}

	if r.CustomerID == "" {
		return errors.NewError("customer_id is required").
			WithHint("Customer ID is required").
			Mark(errors.ErrValidation)
	}

	if r.Amount.IsZero() || r.Amount.IsNegative() {
		return errors.NewError("invalid amount").
			WithHint("Amount must be greater than 0").
			Mark(errors.ErrValidation)
	}

	if r.Currency == "" {
		return errors.NewError("currency is required").
			WithHint("Currency is required").
			Mark(errors.ErrValidation)
	}

	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}

	if r.EnvironmentID == "" {
		return errors.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(errors.ErrValidation)
	}

	return nil
}

// PaymentStatusResponse represents the payment status from Stripe
type PaymentStatusResponse struct {
	SessionID       string            `json:"session_id"`
	PaymentIntentID string            `json:"payment_intent_id"`
	PaymentMethodID string            `json:"payment_method_id,omitempty"`
	Status          string            `json:"status"`
	Amount          decimal.Decimal   `json:"amount"`
	Currency        string            `json:"currency"`
	CustomerID      string            `json:"customer_id"`
	CreatedAt       int64             `json:"created_at"`
	ExpiresAt       int64             `json:"expires_at"`
	Metadata        map[string]string `json:"metadata"`
}
