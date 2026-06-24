package types

import (
	"time"

	"github.com/shopspring/decimal"
)

type CheckoutConfiguration struct {
	CreateSubscriptionParams *CreateSubscriptionParams `json:"create_subscription_params,omitempty"`
}

type CreateSubscriptionParams struct {
	PlanID              string                `json:"plan_id" validate:"required"`
	Currency            string                `json:"currency" validate:"required,len=3"`
	LookupKey           string                `json:"lookup_key,omitempty"`
	StartDate           *time.Time            `json:"start_date,omitempty"`
	EndDate             *time.Time            `json:"end_date,omitempty"`
	BillingPeriod       BillingPeriod         `json:"billing_period" validate:"required"`
	BillingPeriodCount  int                   `json:"billing_period_count,omitempty"`
	BillingCycle        BillingCycle          `json:"billing_cycle,omitempty"`
	CreditGrants        []CheckoutCreditGrant `json:"credit_grants,omitempty"`
	SubscriptionCoupons []CheckoutCouponInput `json:"subscription_coupons,omitempty"`
	LineItems           []CheckoutLineItem    `json:"line_items,omitempty"`
	Metadata            map[string]string     `json:"metadata,omitempty"`
}

type CheckoutCreditGrant struct {
	Name      string            `json:"name"`
	Amount    decimal.Decimal   `json:"amount" swaggertype:"string"`
	Currency  string            `json:"currency"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type CheckoutCouponInput struct {
	CouponCode string  `json:"coupon_code"`
	PriceID    *string `json:"price_id,omitempty"`
}

type CheckoutLineItem struct {
	PriceID  string `json:"price_id"`
	Quantity int    `json:"quantity,omitempty"`
}

// ── JSONB result structs ──────────────────────────────────────────────────────

type CheckoutResult struct {
	CreateSubscriptionResult *CreateSubscriptionResult `json:"create_subscription_result,omitempty"`
}

type CreateSubscriptionResult struct {
	SubscriptionID string `json:"subscription_id"`
	InvoiceID      string `json:"invoice_id"`
	PaymentID      string `json:"payment_id"`
}

// ── JSONB provider_result structs ────────────────────────────────────────────

type CheckoutProviderResult struct {
	CreateSubscriptionResult *ProviderSubscriptionResult `json:"create_subscription_result,omitempty"`
}

type ProviderSubscriptionResult struct {
	SessionID       string `json:"session_id"`
	SessionURL      string `json:"session_url"`
	PaymentIntentID string `json:"payment_intent_id"`
}
