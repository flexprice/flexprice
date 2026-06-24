package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// ── Enums ────────────────────────────────────────────────────────────────────

type CheckoutStatus string

const (
	CheckoutStatusInitiated CheckoutStatus = "initiated"
	CheckoutStatusPending   CheckoutStatus = "pending"
	CheckoutStatusCompleted CheckoutStatus = "completed"
	CheckoutStatusFailed    CheckoutStatus = "failed"
	CheckoutStatusExpired   CheckoutStatus = "expired"
)

func (s CheckoutStatus) String() string { return string(s) }

func (s CheckoutStatus) Validate() error {
	allowed := []CheckoutStatus{
		CheckoutStatusInitiated,
		CheckoutStatusPending,
		CheckoutStatusCompleted,
		CheckoutStatusFailed,
		CheckoutStatusExpired,
	}
	if s != "" && !lo.Contains(allowed, s) {
		return ierr.NewError("invalid checkout status").
			WithHint("Allowed values: initiated, pending, completed, failed, expired").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type CheckoutAction string

const (
	CheckoutActionCreateSubscription CheckoutAction = "create_subscription"
)

func (a CheckoutAction) String() string { return string(a) }

func (a CheckoutAction) Validate() error {
	allowed := []CheckoutAction{
		CheckoutActionCreateSubscription,
	}
	if a != "" && !lo.Contains(allowed, a) {
		return ierr.NewError("invalid checkout action").
			WithHint("Allowed values: create_subscription").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type CheckoutPaymentProvider string

const (
	CheckoutPaymentProviderStripe CheckoutPaymentProvider = "stripe"
)

func (p CheckoutPaymentProvider) String() string { return string(p) }

func (p CheckoutPaymentProvider) Validate() error {
	allowed := []CheckoutPaymentProvider{
		CheckoutPaymentProviderStripe,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid checkout payment provider").
			WithHint("Allowed values: stripe").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type PaymentActionType string

const (
	PaymentActionTypeCheckoutURL PaymentActionType = "checkout_url"
	PaymentActionTypePaymentLink PaymentActionType = "payment_link"
)

func (t PaymentActionType) String() string { return string(t) }

func (t PaymentActionType) Validate() error {
	allowed := []PaymentActionType{PaymentActionTypeCheckoutURL, PaymentActionTypePaymentLink}
	if t != "" && !lo.Contains(allowed, t) {
		return ierr.NewError("invalid payment action type").
			WithHint("Allowed values: checkout_url, payment_link").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ── JSONB configuration structs ───────────────────────────────────────────────

// CheckoutConfiguration holds the action-specific parameters for a checkout session.
// Placed in internal/types/ (not internal/domain/checkout/) to avoid an import cycle:
// ent/schema → internal/types is safe; ent/schema → internal/domain/checkout → ent/ would cycle.
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

// ── Filter ───────────────────────────────────────────────────────────────────

type CheckoutSessionFilter struct {
	*QueryFilter
	CustomerIDs        []string                  `json:"customer_ids,omitempty"`
	Actions            []CheckoutAction          `json:"actions,omitempty"`
	PaymentProviders   []CheckoutPaymentProvider `json:"payment_providers,omitempty"`
	CheckoutStatuses   []CheckoutStatus          `json:"checkout_statuses,omitempty"`
	ExpiresAtLT        *time.Time                `json:"expires_at_lt,omitempty"`
	CheckoutInvoiceIDs []string                  `json:"checkout_invoice_ids,omitempty"`
	CheckoutPaymentIDs []string                  `json:"checkout_payment_ids,omitempty"`
}

func NewDefaultCheckoutSessionFilter() *CheckoutSessionFilter {
	return &CheckoutSessionFilter{QueryFilter: NewDefaultQueryFilter()}
}

func (f *CheckoutSessionFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}
	for _, a := range f.Actions {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	for _, p := range f.PaymentProviders {
		if err := p.Validate(); err != nil {
			return err
		}
	}

	for _, s := range f.CheckoutStatuses {
		if err := s.Validate(); err != nil {
			return err
		}
	}

	return nil
}
