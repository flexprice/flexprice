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
	CheckoutActionWalletTopup        CheckoutAction = "wallet_topup"
	CheckoutActionAddAddon           CheckoutAction = "add_addon"
)

func (a CheckoutAction) String() string { return string(a) }

func (a CheckoutAction) Validate() error {
	allowed := []CheckoutAction{
		CheckoutActionCreateSubscription,
		CheckoutActionModifySubscription,
		CheckoutActionWalletTopup,
		CheckoutActionAddAddon,
	}
	if a != "" && !lo.Contains(allowed, a) {
		return ierr.NewError("invalid checkout action").
			WithHint("Allowed values: create_subscription, modify_subscription, wallet_topup, add_addon").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type CheckoutPaymentProvider string

const (
	CheckoutPaymentProviderRazorpay  CheckoutPaymentProvider = "razorpay"
	CheckoutPaymentProviderChargebee CheckoutPaymentProvider = "chargebee"
)

func (p CheckoutPaymentProvider) String() string { return string(p) }

func (p CheckoutPaymentProvider) Validate() error {
	allowed := []CheckoutPaymentProvider{
		CheckoutPaymentProviderRazorpay,
		CheckoutPaymentProviderChargebee,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid checkout payment provider").
			WithHint("Allowed values: razorpay, chargebee").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToPaymentGateway maps a checkout provider onto the gateway that settles it.
func (p CheckoutPaymentProvider) ToPaymentGateway() (PaymentGatewayType, bool) {
	switch p {
	case CheckoutPaymentProviderRazorpay:
		return PaymentGatewayTypeRazorpay, true
	case CheckoutPaymentProviderChargebee:
		return PaymentGatewayTypeChargebee, true
	default:
		return "", false
	}
}

// CheckoutProviderFromGateway is the reverse. ok=false means the gateway has no
// hosted-checkout adapter, so it cannot back a checkout session.
func CheckoutProviderFromGateway(g PaymentGatewayType) (CheckoutPaymentProvider, bool) {
	switch g {
	case PaymentGatewayTypeRazorpay:
		return CheckoutPaymentProviderRazorpay, true
	case PaymentGatewayTypeChargebee:
		return CheckoutPaymentProviderChargebee, true
	default:
		return "", false
	}
}

// SessionExpiry returns the default lifetime for a checkout session with this provider.
func (p CheckoutPaymentProvider) SessionExpiry() time.Duration {
	switch p {
	case CheckoutPaymentProviderRazorpay:
		return 15 * time.Minute
	case CheckoutPaymentProviderChargebee:
		// Chargebee hosted pages live 5 days and payment_intents 30 min. We pin the
		// shorter of the two so an abandoned session dies before the intent's fund hold does.
		return 30 * time.Minute
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
	CustomerIDs        []string                     `json:"customer_ids,omitempty"`
	Actions            []CheckoutAction             `json:"actions,omitempty"`
	PaymentProviders   []CheckoutPaymentProvider    `json:"payment_providers,omitempty"`
	CheckoutStatuses   []CheckoutStatus             `json:"checkout_statuses,omitempty"`
	ExpiresAtLT        *time.Time                   `json:"expires_at_lt,omitempty"`
	CheckoutInvoiceIDs []string                     `json:"checkout_invoice_ids,omitempty"`
	CheckoutPaymentIDs []string                     `json:"checkout_payment_ids,omitempty"`
	Configuration      *CheckoutConfigurationFilter `json:"configuration,omitempty"`
}

// CheckoutConfigurationFilter matches fields inside checkout_sessions.configuration
// JSONB. Each non-empty field is ANDed as a path equality predicate.
type CheckoutConfigurationFilter struct {
	WalletID       string `json:"wallet_id,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
}

func (f *CheckoutConfigurationFilter) IsEmpty() bool {
	return f == nil || (f.WalletID == "" && f.SubscriptionID == "")
}

func NewDefaultCheckoutSessionFilter() *CheckoutSessionFilter {
	return &CheckoutSessionFilter{QueryFilter: NewDefaultQueryFilter()}
}

func (f *CheckoutSessionFilter) GetStatus() string {
	if f == nil || f.QueryFilter == nil {
		return ""
	}

	return f.QueryFilter.GetStatus()
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
