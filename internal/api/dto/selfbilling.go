package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

// SelfBillingStatusResponse reports whether self-billing is on and the tenant's internal status.
type SelfBillingStatusResponse struct {
	Enabled bool                       `json:"enabled"`
	Status  types.TenantInternalStatus `json:"status,omitempty"`
}

// SelfBillingCheckoutRequest carries the billing details collected before checkout.
type SelfBillingCheckoutRequest struct {
	Name       string `json:"name" validate:"required"`
	Email      string `json:"email" validate:"required,email"`
	Line1      string `json:"address_line1" validate:"required"`
	Line2      string `json:"address_line2"`
	City       string `json:"address_city" validate:"required"`
	State      string `json:"address_state"`
	Country    string `json:"address_country" validate:"required,len=2"`
	PostalCode string `json:"address_postal_code" validate:"required"`
	TaxID      string `json:"tax_id"`
}

// Validate checks the request's struct tags.
func (r SelfBillingCheckoutRequest) Validate() error { return validator.ValidateRequest(r) }

// SelfBillingCheckoutResponse is the state of a self-billing checkout.
type SelfBillingCheckoutResponse struct {
	CheckoutID string                     `json:"checkout_id,omitempty"`
	URL        string                     `json:"url,omitempty"`
	Status     string                     `json:"status"` // initiated, pending, completed, failed, expired, pay_in_portal
	Terminal   bool                       `json:"terminal"`
	Tenant     types.TenantInternalStatus `json:"tenant_status"`
}

// SelfBillingInvoice is one billing-tenant invoice shown on the billing page.
type SelfBillingInvoice struct {
	ID            string          `json:"id"`
	Number        string          `json:"number"`
	Status        string          `json:"status"`
	PaymentStatus string          `json:"payment_status"`
	Total         decimal.Decimal `json:"total" swaggertype:"string"`
	Currency      string          `json:"currency"`
	DueDate       *time.Time      `json:"due_date,omitempty"`
	PDFURL        string          `json:"pdf_url,omitempty"`
}

// SelfBillingSummaryResponse is the tenant's billing page: status, subscription, invoices and usage.
type SelfBillingSummaryResponse struct {
	Status             types.TenantInternalStatus `json:"status"`
	SubscriptionStatus string                     `json:"subscription_status,omitempty"`
	TrialEndsAt        *time.Time                 `json:"trial_ends_at,omitempty"`
	Invoices           []SelfBillingInvoice       `json:"invoices"`
	Usage              any                        `json:"usage,omitempty"`
}

// SelfBillingPortalResponse is a short-lived customer portal link.
type SelfBillingPortalResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
