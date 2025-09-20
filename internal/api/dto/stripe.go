package dto

import (
	"strings"

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

// StripeInvoiceSyncRequest represents the request for syncing an invoice to Stripe
type StripeInvoiceSyncRequest struct {
	InvoiceID        string                 `json:"invoice_id"`
	CollectionMethod types.CollectionMethod `json:"collection_method"`
	PaymentBehavior  *types.PaymentBehavior `json:"payment_behavior,omitempty"`
}

// StripeInvoiceSyncResponse represents the response from Stripe invoice sync
type StripeInvoiceSyncResponse struct {
	StripeInvoiceID  string                 `json:"stripe_invoice_id"`
	Status           string                 `json:"status"`
	PaymentIntentID  string                 `json:"payment_intent_id,omitempty"`
	HostedInvoiceURL string                 `json:"hosted_invoice_url,omitempty"`
	InvoicePDF       string                 `json:"invoice_pdf,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// CreateSetupIntentRequest represents a request to create a Setup Intent session
type CreateSetupIntentRequest struct {
	Provider           string         `json:"provider" binding:"required"`    // Payment provider: "stripe", "razorpay", etc.
	Usage              string         `json:"usage,omitempty"`                // "on_session" or "off_session" (default: "off_session")
	PaymentMethodTypes []string       `json:"payment_method_types,omitempty"` // defaults to ["card"]
	SuccessURL         string         `json:"success_url,omitempty"`          // User-configurable success redirect URL
	CancelURL          string         `json:"cancel_url,omitempty"`           // User-configurable cancel redirect URL
	SetDefault         bool           `json:"set_default,omitempty"`          // Whether to set the payment method as default when setup succeeds
	Metadata           types.Metadata `json:"metadata,omitempty"`
}

// SetupIntentResponse represents a response from creating a Setup Intent session
type SetupIntentResponse struct {
	SetupIntentID     string `json:"setup_intent_id"`
	CheckoutSessionID string `json:"checkout_session_id"`
	CheckoutURL       string `json:"checkout_url"`
	ClientSecret      string `json:"client_secret"`
	Status            string `json:"status"`
	Usage             string `json:"usage"`
	CustomerID        string `json:"customer_id"`
	CreatedAt         int64  `json:"created_at"`
	ExpiresAt         int64  `json:"expires_at"`
}

// ListSetupIntentsRequest represents a request to list Setup Intents for a customer
type ListSetupIntentsRequest struct {
	CustomerID      string `json:"customer_id" binding:"required"`
	PaymentMethodID string `json:"payment_method_id,omitempty"` // Filter by specific payment method
	Status          string `json:"status,omitempty"`            // Filter by status (succeeded, requires_payment_method, etc.)
	Limit           int    `json:"limit,omitempty"`             // Number of results to return (default: 10, max: 100)
	StartingAfter   string `json:"starting_after,omitempty"`    // Pagination cursor
	EndingBefore    string `json:"ending_before,omitempty"`     // Pagination cursor
}

// SetupIntentListItem represents a single Setup Intent in the list response
type SetupIntentListItem struct {
	ID                   string                 `json:"id"`
	Status               string                 `json:"status"`
	Usage                string                 `json:"usage"`
	CustomerID           string                 `json:"customer_id"`
	PaymentMethodID      string                 `json:"payment_method_id,omitempty"`
	PaymentMethodDetails *PaymentMethodResponse `json:"payment_method_details,omitempty"`
	IsDefault            bool                   `json:"is_default"`
	CreatedAt            int64                  `json:"created_at"`
	CancellationReason   string                 `json:"cancellation_reason,omitempty"`
	LastSetupError       string                 `json:"last_setup_error,omitempty"`
	Metadata             map[string]string      `json:"metadata,omitempty"`
}

// ListSetupIntentsResponse represents the response from listing Setup Intents
type ListSetupIntentsResponse struct {
	Data       []SetupIntentListItem `json:"data"`
	HasMore    bool                  `json:"has_more"`
	TotalCount int                   `json:"total_count"`
}

// MultiProviderPaymentMethodsResponse represents payment methods grouped by provider
type MultiProviderPaymentMethodsResponse struct {
	Stripe []SetupIntentListItem `json:"stripe,omitempty"`
	// Add more providers as needed
}

// ListPaymentMethodsRequest represents a request to list payment methods for a customer (GET request)
type ListPaymentMethodsRequest struct {
	Provider      string `json:"provider" binding:"required"` // Payment provider: "stripe", "razorpay", etc.
	Limit         int    `json:"limit,omitempty"`             // Number of results to return (default: 10, max: 100)
	StartingAfter string `json:"starting_after,omitempty"`    // Pagination cursor
	EndingBefore  string `json:"ending_before,omitempty"`     // Pagination cursor
}

// Validate validates the create Setup Intent request
func (r *CreateSetupIntentRequest) Validate() error {
	// Validate provider parameter
	if r.Provider == "" {
		return errors.NewError("provider is required").
			WithHint("Please provide a payment provider").
			Mark(errors.ErrValidation)
	}

	switch r.Provider {
	case string(types.PaymentMethodProviderStripe):
	default:
		return errors.NewError("unsupported payment provider").
			WithHint("Currently only 'stripe' provider is supported").
			WithReportableDetails(map[string]interface{}{
				"provider":            r.Provider,
				"supported_providers": []types.PaymentMethodProvider{types.PaymentMethodProviderStripe},
			}).
			Mark(errors.ErrValidation)
	}

	// Validate usage parameter
	if r.Usage != "" && r.Usage != "on_session" && r.Usage != "off_session" {
		return errors.NewError("invalid usage parameter").
			WithHint("Usage must be 'on_session' or 'off_session'").
			Mark(errors.ErrValidation)
	}

	// Validate payment method types
	if len(r.PaymentMethodTypes) > 0 {
		for _, pmType := range r.PaymentMethodTypes {
			if pmType != "card" && pmType != "us_bank_account" && pmType != "sepa_debit" {
				return errors.NewError("unsupported payment method type").
					WithHint("Supported payment method types: card, us_bank_account, sepa_debit").
					WithReportableDetails(map[string]interface{}{
						"payment_method_type": pmType,
					}).
					Mark(errors.ErrValidation)
			}
		}
	}

	// Validate URLs if provided (basic URL format validation)
	if r.SuccessURL != "" {
		if !isValidURL(r.SuccessURL) {
			return errors.NewError("invalid success_url format").
				WithHint("Success URL must be a valid HTTP/HTTPS URL").
				WithReportableDetails(map[string]interface{}{
					"success_url": r.SuccessURL,
				}).
				Mark(errors.ErrValidation)
		}
	}

	if r.CancelURL != "" {
		if !isValidURL(r.CancelURL) {
			return errors.NewError("invalid cancel_url format").
				WithHint("Cancel URL must be a valid HTTP/HTTPS URL").
				WithReportableDetails(map[string]interface{}{
					"cancel_url": r.CancelURL,
				}).
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

// Validate validates the list Setup Intents request
func (r *ListSetupIntentsRequest) Validate() error {
	if r.CustomerID == "" {
		return errors.NewError("customer_id is required").
			WithHint("Customer ID is required").
			Mark(errors.ErrValidation)
	}

	// Validate limit parameter
	if r.Limit < 0 || r.Limit > 100 {
		return errors.NewError("invalid limit parameter").
			WithHint("Limit must be between 0 and 100").
			WithReportableDetails(map[string]interface{}{
				"limit": r.Limit,
			}).
			Mark(errors.ErrValidation)
	}

	// Validate status parameter if provided
	if r.Status != "" {
		validStatuses := []string{"requires_payment_method", "requires_confirmation", "requires_action", "processing", "succeeded", "canceled"}
		isValid := false
		for _, validStatus := range validStatuses {
			if r.Status == validStatus {
				isValid = true
				break
			}
		}
		if !isValid {
			return errors.NewError("invalid status parameter").
				WithHint("Status must be one of: requires_payment_method, requires_confirmation, requires_action, processing, succeeded, canceled").
				WithReportableDetails(map[string]interface{}{
					"status": r.Status,
				}).
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

// Validate validates the list payment methods request
func (r *ListPaymentMethodsRequest) Validate() error {
	// Validate provider parameter
	if r.Provider == "" {
		return errors.NewError("provider is required").
			WithHint("Please provide a payment provider").
			Mark(errors.ErrValidation)
	}

	if r.Provider != string(types.PaymentMethodProviderStripe) {
		return errors.NewError("unsupported payment provider").
			WithHint("Currently only 'stripe' provider is supported").
			WithReportableDetails(map[string]interface{}{
				"provider":            r.Provider,
				"supported_providers": []types.PaymentMethodProvider{types.PaymentMethodProviderStripe},
			}).
			Mark(errors.ErrValidation)
	}

	// Validate limit parameter
	if r.Limit < 0 || r.Limit > 100 {
		return errors.NewError("invalid limit parameter").
			WithHint("Limit must be between 0 and 100").
			WithReportableDetails(map[string]interface{}{
				"limit": r.Limit,
			}).
			Mark(errors.ErrValidation)
	}

	return nil
}

// isValidURL checks if a string is a valid HTTP/HTTPS URL
func isValidURL(urlStr string) bool {
	if urlStr == "" {
		return true // Empty URLs are allowed (optional)
	}

	// Must start with http:// or https://
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return false
	}

	// Basic length check
	if len(urlStr) > 2048 {
		return false // URLs longer than 2048 chars are generally not supported
	}

	return true
}
