package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
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
	CheckoutActionModifySubscription CheckoutAction = "modify_subscription"
)

func (a CheckoutAction) String() string { return string(a) }

func (a CheckoutAction) Validate() error {
	allowed := []CheckoutAction{
		CheckoutActionCreateSubscription,
		CheckoutActionModifySubscription,
	}
	if a != "" && !lo.Contains(allowed, a) {
		return ierr.NewError("invalid checkout action").
			WithHint("Allowed values: create_subscription, modify_subscription").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type CheckoutPaymentProvider string

const (
	CheckoutPaymentProviderRazorpay CheckoutPaymentProvider = "razorpay"
)

func (p CheckoutPaymentProvider) String() string { return string(p) }

func (p CheckoutPaymentProvider) Validate() error {
	allowed := []CheckoutPaymentProvider{
		CheckoutPaymentProviderRazorpay,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid checkout payment provider").
			WithHint("Allowed values: razorpay").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SessionExpiry returns the default lifetime for a checkout session with this provider.
func (p CheckoutPaymentProvider) SessionExpiry() time.Duration {
	switch p {
	case CheckoutPaymentProviderRazorpay:
		return 15 * time.Minute
	default:
		return 30 * time.Minute // Default to 30 minutes
	}
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

// PaymentAction is the customer-facing next step to complete payment.
// Surfaced in CheckoutSessionResponse; the full CheckoutProviderResult is never exposed.
type PaymentAction struct {
	Type PaymentActionType `json:"type"`
	URL  string            `json:"url"`
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

// CheckoutSessionCleanupResult holds per-run counts from CleanupAllExpiredSessions.
type CheckoutSessionCleanupResult struct {
	Total     int
	Succeeded int
	Failed    int
}
