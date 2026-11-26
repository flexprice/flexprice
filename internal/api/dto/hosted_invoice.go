package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// NewHostedInvoice copies an invoice for the public hosted link without free-form metadata,
// idempotency keys, the subscription or the customer's contacts and integrations.
func NewHostedInvoice(inv *InvoiceResponse) *InvoiceResponse {
	out := *inv
	out.Metadata = nil
	out.IdempotencyKey = nil
	out.Subscription = nil
	out.CheckoutSession = nil

	if inv.Customer != nil && inv.Customer.Customer != nil {
		c := *inv.Customer.Customer
		c.Metadata = nil
		c.ContactPersons = nil
		out.Customer = &CustomerResponse{Customer: &c}
	}

	out.LineItems = lo.Map(inv.LineItems, func(li *InvoiceLineItemResponse, _ int) *InvoiceLineItemResponse {
		c := *li
		c.Metadata = nil
		return &c
	})
	out.Taxes = lo.Map(inv.Taxes, func(t *TaxAppliedResponse, _ int) *TaxAppliedResponse {
		c := *t
		c.Metadata = nil
		c.IdempotencyKey = nil
		return &c
	})
	out.CouponApplications = lo.FilterMap(inv.CouponApplications, func(ca *CouponApplicationResponse, _ int) (*CouponApplicationResponse, bool) {
		if ca == nil || ca.CouponApplication == nil {
			return nil, false
		}
		c := *ca.CouponApplication
		c.Metadata = nil
		return &CouponApplicationResponse{CouponApplication: &c}, true
	})
	return &out
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
