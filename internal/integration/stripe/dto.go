package stripe

import (
	"time"

	"github.com/shopspring/decimal"
)

// CreateStripePaymentLinkRequest represents a request to create a Stripe payment link
type CreateStripePaymentLinkRequest struct {
	InvoiceID              string            `json:"invoice_id" validate:"required"`
	CustomerID             string            `json:"customer_id" validate:"required"`
	Amount                 decimal.Decimal   `json:"amount" validate:"required,gt=0"`
	Currency               string            `json:"currency" validate:"required,len=3"`
	SuccessURL             string            `json:"success_url,omitempty"`
	CancelURL              string            `json:"cancel_url,omitempty"`
	Metadata               map[string]string `json:"metadata,omitempty"`
	SaveCardAndMakeDefault bool              `json:"save_card_and_make_default,omitempty"`
	EnvironmentID          string            `json:"environment_id" validate:"required"`
	PaymentID              string            `json:"payment_id" validate:"required"`
}

// StripePaymentLinkResponse represents the response from creating a Stripe payment link
type StripePaymentLinkResponse struct {
	ID              string          `json:"id"`
	PaymentURL      string          `json:"payment_url"`
	PaymentIntentID string          `json:"payment_intent_id,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	Status          string          `json:"status"`
	CreatedAt       int64           `json:"created_at"`
	PaymentID       string          `json:"payment_id,omitempty"`
}

// ChargeSavedPaymentMethodRequest represents a request to charge a saved payment method
type ChargeSavedPaymentMethodRequest struct {
	CustomerID      string          `json:"customer_id" validate:"required"`
	InvoiceID       string          `json:"invoice_id" validate:"required"`
	PaymentMethodID string          `json:"payment_method_id" validate:"required"`
	Amount          decimal.Decimal `json:"amount" validate:"required,gt=0"`
	Currency        string          `json:"currency" validate:"required,len=3"`
}

// PaymentIntentResponse represents a Stripe PaymentIntent response
type PaymentIntentResponse struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	CustomerID    string          `json:"customer_id"`
	PaymentMethod string          `json:"payment_method"`
	CreatedAt     int64           `json:"created_at"`
}

// PaymentStatusResponse represents payment status from Stripe
type PaymentStatusResponse struct {
	SessionID       string            `json:"session_id"`
	PaymentIntentID string            `json:"payment_intent_id"`
	PaymentMethodID string            `json:"payment_method_id,omitempty"`
	Status          string            `json:"status"`
	Amount          decimal.Decimal   `json:"amount"`
	Currency        string            `json:"currency"`
	CustomerID      string            `json:"customer_id"`
	CreatedAt       int64             `json:"created_at"`
	ExpiresAt       int64             `json:"expires_at,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// GetCustomerPaymentMethodsRequest represents a request to get customer payment methods
type GetCustomerPaymentMethodsRequest struct {
	CustomerID string `json:"customer_id" validate:"required"`
}

// PaymentMethodResponse represents a payment method from Stripe
type PaymentMethodResponse struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Customer string                 `json:"customer"`
	Created  int64                  `json:"created"`
	Card     *CardDetails           `json:"card,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// CardDetails represents card details from a payment method
type CardDetails struct {
	Brand       string `json:"brand"`
	Last4       string `json:"last4"`
	ExpMonth    int    `json:"exp_month"`
	ExpYear     int    `json:"exp_year"`
	Fingerprint string `json:"fingerprint"`
}

// StripeInvoiceSyncRequest represents a request to sync an invoice to Stripe
type StripeInvoiceSyncRequest struct {
	InvoiceID        string `json:"invoice_id" validate:"required"`
	CollectionMethod string `json:"collection_method" validate:"required,oneof=charge_automatically send_invoice"`
}

// StripeInvoiceSyncResponse represents the response from syncing an invoice to Stripe
type StripeInvoiceSyncResponse struct {
	InvoiceID       string          `json:"invoice_id"`
	StripeInvoiceID string          `json:"stripe_invoice_id"`
	Status          string          `json:"status"`
	Amount          decimal.Decimal `json:"amount"`
	Currency        string          `json:"currency"`
	InvoiceURL      string          `json:"invoice_url,omitempty"`
	PaymentURL      string          `json:"payment_url,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// WebhookEventRequest represents a Stripe webhook event request
type WebhookEventRequest struct {
	TenantID      string `json:"tenant_id" validate:"required"`
	EnvironmentID string `json:"environment_id" validate:"required"`
	Signature     string `json:"signature" validate:"required"`
	Payload       []byte `json:"payload" validate:"required"`
}

// WebhookEventResponse represents a Stripe webhook event response
type WebhookEventResponse struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Processed bool   `json:"processed"`
	Message   string `json:"message,omitempty"`
}

// CustomerSyncRequest represents a request to sync a customer to Stripe
type CustomerSyncRequest struct {
	CustomerID string `json:"customer_id" validate:"required"`
}

// CustomerSyncResponse represents the response from syncing a customer to Stripe
type CustomerSyncResponse struct {
	CustomerID       string                 `json:"customer_id"`
	StripeCustomerID string                 `json:"stripe_customer_id"`
	Status           string                 `json:"status"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// IntegrationEntityMapping represents a mapping between FlexPrice entities and Stripe entities
type IntegrationEntityMapping struct {
	ID               string                 `json:"id"`
	EntityID         string                 `json:"entity_id"`
	EntityType       string                 `json:"entity_type"`
	ProviderType     string                 `json:"provider_type"`
	ProviderEntityID string                 `json:"provider_entity_id"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// CreateEntityIntegrationMappingRequest represents a request to create an entity integration mapping
type CreateEntityIntegrationMappingRequest struct {
	EntityID         string                 `json:"entity_id" validate:"required"`
	EntityType       string                 `json:"entity_type" validate:"required"`
	ProviderType     string                 `json:"provider_type" validate:"required"`
	ProviderEntityID string                 `json:"provider_entity_id" validate:"required"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateCustomerMetadataRequest represents a request to update customer metadata in Stripe
type UpdateCustomerMetadataRequest struct {
	CustomerID       string            `json:"customer_id" validate:"required"`
	StripeCustomerID string            `json:"stripe_customer_id" validate:"required"`
	Metadata         map[string]string `json:"metadata" validate:"required"`
}

// SetDefaultPaymentMethodRequest represents a request to set a default payment method
type SetDefaultPaymentMethodRequest struct {
	CustomerID      string `json:"customer_id" validate:"required"`
	PaymentMethodID string `json:"payment_method_id" validate:"required"`
}

// PaymentReconciliationRequest represents a request to reconcile a payment with an invoice
type PaymentReconciliationRequest struct {
	PaymentID     string          `json:"payment_id" validate:"required"`
	PaymentAmount decimal.Decimal `json:"payment_amount" validate:"required,gt=0"`
}

// StripeConnectionConfig represents Stripe connection configuration
type StripeConnectionConfig struct {
	SecretKey      string `json:"secret_key" validate:"required"`
	PublishableKey string `json:"publishable_key" validate:"required"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`
	AccountID      string `json:"account_id,omitempty"`
}

// ValidateStripeConnectionRequest represents a request to validate Stripe connection
type ValidateStripeConnectionRequest struct {
	SecretKey      string `json:"secret_key" validate:"required"`
	PublishableKey string `json:"publishable_key" validate:"required"`
	WebhookSecret  string `json:"webhook_secret,omitempty"`
}

// ValidateStripeConnectionResponse represents the response from validating Stripe connection
type ValidateStripeConnectionResponse struct {
	Valid     bool   `json:"valid"`
	AccountID string `json:"account_id,omitempty"`
	Message   string `json:"message,omitempty"`
}
