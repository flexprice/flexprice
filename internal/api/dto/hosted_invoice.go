package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// HostedInvoiceResponse is everything the public hosted invoice page shows.
type HostedInvoiceResponse struct {
	Tenant   HostedInvoiceTenant     `json:"tenant"`
	Invoice  *InvoiceResponse        `json:"invoice"`
	Payments []*HostedInvoicePayment `json:"payments"`
	Pay      HostedInvoicePay        `json:"pay"`
}

// HostedInvoiceTenant is the issuer shown on the hosted invoice page.
type HostedInvoiceTenant struct {
	Name           string                      `json:"name"`
	BillingDetails tenant.TenantBillingDetails `json:"billing_details"`
}

// HostedInvoicePayment is a payment on the invoice without gateway payloads or internal ids.
type HostedInvoicePayment struct {
	ID                string                  `json:"id"`
	Amount            decimal.Decimal         `json:"amount" swaggertype:"string"`
	Currency          string                  `json:"currency"`
	PaymentStatus     types.PaymentStatus     `json:"payment_status"`
	PaymentGateway    *string                 `json:"payment_gateway"`
	PaymentMethodType types.PaymentMethodType `json:"payment_method_type"`
	CreatedAt         time.Time               `json:"created_at"`
	SucceededAt       *time.Time              `json:"succeeded_at"`
	FailedAt          *time.Time              `json:"failed_at"`
}

// HostedInvoicePay says whether the page can offer Pay.
type HostedInvoicePay struct {
	Enabled bool `json:"enabled"`
}

// HostedInvoicePayRequest starts a payment from the hosted invoice page.
type HostedInvoicePayRequest struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

// HostedInvoicePayResponse is the payment Pay started and where to send the customer.
type HostedInvoicePayResponse struct {
	PaymentID     string               `json:"payment_id"`
	InvoiceID     string               `json:"invoice_id"`
	Status        types.PaymentStatus  `json:"status"`
	Amount        decimal.Decimal      `json:"amount" swaggertype:"string"`
	Currency      string               `json:"currency"`
	PaymentAction *types.PaymentAction `json:"payment_action,omitempty"`
}
