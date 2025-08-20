package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
)

// CreatePaddlePaymentLinkRequest represents a request to create a Paddle checkout session
type CreatePaddlePaymentLinkRequest struct {
	InvoiceID     string            `json:"invoice_id" validate:"required"`
	CustomerID    string            `json:"customer_id" validate:"required"`
	Amount        decimal.Decimal   `json:"amount" validate:"required"`
	Currency      string            `json:"currency" validate:"required"`
	EnvironmentID string            `json:"environment_id" validate:"required"`
	SuccessURL    string            `json:"success_url,omitempty"`
	CancelURL     string            `json:"cancel_url,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// Validate validates the Paddle payment link request
func (r *CreatePaddlePaymentLinkRequest) Validate() error {
	if r.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required to create payment link").
			Mark(ierr.ErrValidation)
	}
	if r.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Customer ID is required to create payment link").
			Mark(ierr.ErrValidation)
	}
	if r.Amount.IsZero() || r.Amount.IsNegative() {
		return ierr.NewError("amount must be positive").
			WithHint("Payment amount must be greater than zero").
			Mark(ierr.ErrValidation)
	}
	if r.Currency == "" {
		return ierr.NewError("currency is required").
			WithHint("Currency is required to create payment link").
			Mark(ierr.ErrValidation)
	}
	if r.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required to create payment link").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// PaddlePaymentLinkResponse represents the response from creating a Paddle checkout session
type PaddlePaymentLinkResponse struct {
	ID            string          `json:"id"`
	PaymentURL    string          `json:"payment_url"`
	TransactionID string          `json:"transaction_id,omitempty"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	Status        string          `json:"status"`
	CreatedAt     int64           `json:"created_at"`
	PaymentID     string          `json:"payment_id,omitempty"`
}

// PaddleTransactionItem represents an item in a Paddle transaction
type PaddleTransactionItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Quantity    int             `json:"quantity"`
	UnitPrice   decimal.Decimal `json:"unit_price"`
}

// PaddleCustomerData represents customer data for Paddle checkout
type PaddleCustomerData struct {
	Email      string            `json:"email"`
	Name       string            `json:"name,omitempty"`
	CustomData map[string]string `json:"custom_data,omitempty"`
}

// PaddleWebhookEvent represents a Paddle webhook event
type PaddleWebhookEvent struct {
	EventID    string                 `json:"event_id"`
	EventType  string                 `json:"event_type"`
	Data       map[string]interface{} `json:"data"`
	OccurredAt string                 `json:"occurred_at"`
}

// PaddleTransactionWebhookData represents transaction data from Paddle webhooks
type PaddleTransactionWebhookData struct {
	ID          string                   `json:"id"`
	Status      string                   `json:"status"`
	CustomerID  string                   `json:"customer_id"`
	CheckoutURL string                   `json:"checkout_url,omitempty"`
	Items       []map[string]interface{} `json:"items"`
	Details     map[string]interface{}   `json:"details"`
	Payments    []map[string]interface{} `json:"payments,omitempty"`
	CustomData  map[string]interface{}   `json:"custom_data,omitempty"`
}

// PaddlePaymentStatusResponse represents the response from getting payment status
type PaddlePaymentStatusResponse struct {
	TransactionID string          `json:"transaction_id"`
	Status        string          `json:"status"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	PaymentMethod string          `json:"payment_method,omitempty"`
	PaidAt        *int64          `json:"paid_at,omitempty"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
}


