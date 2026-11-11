package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
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
func (r SelfBillingCheckoutRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// SelfBillingCheckoutResponse is the state of a self-billing checkout.
type SelfBillingCheckoutResponse struct {
	CheckoutID string               `json:"checkout_id,omitempty"`
	URL        string               `json:"url,omitempty"`
	Status     types.CheckoutStatus `json:"status"`
	Terminal   bool                 `json:"terminal"`

	Tenant types.TenantInternalStatus `json:"tenant_status"`
}

// SelfBillingInvoice is one Flexprice Cloud invoice shown on the billing page.
type SelfBillingInvoice struct {
	ID            string              `json:"id"`
	Number        string              `json:"number"`
	Status        types.InvoiceStatus `json:"status"`
	PaymentStatus types.PaymentStatus `json:"payment_status"`
	Total         decimal.Decimal     `json:"total" swaggertype:"string"`
	Currency      string              `json:"currency"`
	DueDate       *time.Time          `json:"due_date,omitempty"`
	PDFURL        string              `json:"pdf_url,omitempty"`
}

// SelfBillingSummaryResponse is the tenant's billing page: status, subscription, invoices and usage.
type SelfBillingSummaryResponse struct {
	Status             types.TenantInternalStatus    `json:"status"`
	SubscriptionStatus types.SubscriptionStatus      `json:"subscription_status,omitempty"`
	TrialEndsAt        *time.Time                    `json:"trial_ends_at,omitempty"`
	Invoices           []SelfBillingInvoice          `json:"invoices"`
	Usage              *CustomerUsageSummaryResponse `json:"usage,omitempty"`
	Plan               *SelfBillingPlanResponse      `json:"plan"`
}

// SelfBillingPlanResponse describes subscribed terms, or the configured offer when there is no current subscription.
type SelfBillingPlanResponse struct {
	ID          string                       `json:"id"`
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Charges     []*SelfBillingChargeResponse `json:"charges"`
}

// SelfBillingChargeResponse is a price and its effective subscription quantity, or its offered default quantity.
type SelfBillingChargeResponse struct {
	PriceID     string          `json:"price_id"`
	LineItemID  string          `json:"line_item_id,omitempty"`
	DisplayName string          `json:"display_name"`
	Type        types.PriceType `json:"type"`

	// Quantity is the configured quantity; metered usage is returned in summary.usage.
	Quantity decimal.Decimal `json:"quantity" swaggertype:"string"`

	// Amount is a unit rate; tiered charges use tiers instead.
	Amount             decimal.Decimal          `json:"amount" swaggertype:"string"`
	Currency           string                   `json:"currency"`
	BillingModel       types.BillingModel       `json:"billing_model"`
	BillingPeriod      types.BillingPeriod      `json:"billing_period"`
	BillingPeriodCount int                      `json:"billing_period_count"`
	InvoiceCadence     types.InvoiceCadence     `json:"invoice_cadence"`
	TierMode           types.BillingTier        `json:"tier_mode,omitempty"`
	Tiers              price.JSONBTiers         `json:"tiers,omitempty"`
	TransformQuantity  *price.TransformQuantity `json:"transform_quantity,omitempty"`
	MeterID            string                   `json:"meter_id,omitempty"`
	MeterDisplayName   string                   `json:"meter_display_name,omitempty"`
	BucketSize         types.WindowSize         `json:"bucket_size,omitempty"`
	PriceUnitType      types.PriceUnitType      `json:"price_unit_type"`
	PriceUnit          *string                  `json:"price_unit,omitempty"`
	PriceUnitAmount    *decimal.Decimal         `json:"price_unit_amount,omitempty" swaggertype:"string"`
	PriceUnitTiers     price.JSONBTiers         `json:"price_unit_tiers,omitempty"`
}

// SelfBillingPortalResponse is a short-lived customer portal link.
type SelfBillingPortalResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}
