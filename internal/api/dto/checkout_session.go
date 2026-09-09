package dto

import (
	"context"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// PaymentParams groups payment-provider settings for checkout flows.
type PaymentParams struct {
	PaymentProvider       types.CheckoutPaymentProvider        `json:"payment_provider" binding:"required" validate:"required"`
	PaymentProviderConfig *types.CheckoutPaymentProviderConfig `json:"payment_provider_config,omitempty"`
}

func (p *PaymentParams) Validate() error {
	if p == nil {
		return nil
	}
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}
	if err := p.PaymentProvider.Validate(); err != nil {
		return err
	}
	if p.PaymentProviderConfig != nil {
		if err := p.PaymentProviderConfig.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// RedirectionParams groups post-checkout redirect URLs.
type RedirectionParams struct {
	SuccessURL *string `json:"success_url,omitempty"`
	FailureURL *string `json:"failure_url,omitempty"`
	CancelURL  *string `json:"cancel_url,omitempty"`
}

// CheckoutParams is the reusable checkout opt-in payload shared by
// create-session, payment-gated subscription modify, and wallet top-up.
type CheckoutParams struct {
	PaymentParams
	RedirectionParams
	IdempotencyKey *string           `json:"idempotency_key,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func (p *CheckoutParams) Validate() error {
	if p == nil {
		return nil
	}
	return p.PaymentParams.Validate()
}

// CreateCheckoutSessionRequest is the request body for POST /checkout/sessions.
type CreateCheckoutSessionRequest struct {
	CustomerExternalID string                      `json:"customer_external_id" binding:"required"`
	Action             types.CheckoutAction        `json:"action" binding:"required"`
	Configuration      types.CheckoutConfiguration `json:"configuration"`
	CheckoutParams
}

func (r *CreateCheckoutSessionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.Action.Validate(); err != nil {
		return err
	}

	// modify_subscription sessions are created only via subscription modify/execute (pay-first).
	if r.Action == types.CheckoutActionModifySubscription {
		return ierr.NewError("modify_subscription is not supported via create checkout session").
			WithHint("Use POST /subscriptions/{id}/modify/execute with a checkout object instead").
			Mark(ierr.ErrValidation)
	}

	// wallet_topup sessions are created only via wallet top-up (pay-first).
	if r.Action == types.CheckoutActionWalletTopup {
		return ierr.NewError("wallet_topup is not supported via create checkout session").
			WithHint("Use POST /wallets/{id}/top-up with a checkout object instead").
			Mark(ierr.ErrValidation)
	}

	// add_addon sessions are created only via subscription addon attach (pay-first).
	if r.Action == types.CheckoutActionAddAddon {
		return ierr.NewError("add_addon is not supported via create checkout session").
			WithHint("Use POST /subscriptions/addon with a checkout object instead").
			Mark(ierr.ErrValidation)
	}

	if cfg := r.Configuration.CreateSubscriptionParams; cfg != nil && cfg.SubscriptionID != "" {
		return ierr.NewError("subscription_id is not supported via create checkout session").
			WithHint("Use POST /subscriptions with a checkout object to gate an existing draft subscription").
			Mark(ierr.ErrValidation)
	}

	if err := r.CheckoutParams.Validate(); err != nil {
		return err
	}

	if err := r.Configuration.Validate(r.Action); err != nil {
		return err
	}

	return nil
}

func (r *CreateCheckoutSessionRequest) ResolveExpiresAt(now time.Time) time.Time {
	return now.UTC().Add(r.PaymentProvider.SessionExpiry())
}

func (r *CreateCheckoutSessionRequest) ToCheckoutSession(ctx context.Context, customerID string) *domainCheckout.CheckoutSession {
	return &domainCheckout.CheckoutSession{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:         types.GetEnvironmentID(ctx),
		CustomerID:            customerID,
		Action:                r.Action,
		CheckoutStatus:        types.CheckoutStatusInitiated,
		PaymentProvider:       r.PaymentProvider,
		Configuration:         domainCheckout.ToJSONBCheckoutConfiguration(r.Configuration),
		PaymentProviderConfig: domainCheckout.ToJSONBCheckoutPaymentProviderConfig(r.PaymentProviderConfig),
		IdempotencyKey:        r.IdempotencyKey,
		SuccessURL:            r.SuccessURL,
		FailureURL:            r.FailureURL,
		CancelURL:             r.CancelURL,
		ExpiresAt:             r.ResolveExpiresAt(time.Now()),
		Metadata:              r.Metadata,
		BaseModel:             types.GetDefaultBaseModel(ctx),
	}
}

// UpdateCheckoutSessionRequest carries lifecycle-only patch fields.
// Only non-nil fields are applied.
type UpdateCheckoutSessionRequest struct {
	CheckoutStatus    *types.CheckoutStatus         `json:"checkout_status,omitempty"`
	CheckoutInvoiceID *string                       `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string                       `json:"checkout_payment_id,omitempty"`
	Result            *types.CheckoutResult         `json:"result,omitempty"`
	ProviderResult    *types.CheckoutProviderResult `json:"provider_result,omitempty"`
	CompletedAt       *time.Time                    `json:"completed_at,omitempty"`
	CancelledAt       *time.Time                    `json:"cancelled_at,omitempty"`
	FailureReason     *string                       `json:"failure_reason,omitempty"`
}

// CreateCheckoutPaymentRequest holds parameters for creating an INITIATED payment
// record during checkout fulfillment. Uses the domain invoice directly to avoid
// a redundant DB lookup. Extend this struct to add metadata, idempotency keys,
// or additional gateway fields without changing the service interface signature.
type CreateCheckoutPaymentRequest struct {
	Invoice *invoice.Invoice
	Gateway types.PaymentGatewayType
}

type PayFirstCheckoutRequest struct {
	CustomerID    string
	Action        types.CheckoutAction
	Configuration types.CheckoutConfiguration
	DraftInvoice  *invoice.Invoice
	Checkout      *CheckoutParams
}

func (r *PayFirstCheckoutRequest) Validate() error {
	if r == nil || r.Checkout == nil {
		return ierr.NewError("pay-first checkout requires checkout params").
			Mark(ierr.ErrValidation)
	}

	if r.DraftInvoice == nil || r.DraftInvoice.ID == "" {
		return ierr.NewError("pay-first checkout requires a draft invoice").
			Mark(ierr.ErrValidation)
	}

	if r.CustomerID == "" {
		return ierr.NewError("pay-first checkout requires customer_id").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *PayFirstCheckoutRequest) ToCheckoutSession(ctx context.Context, customerID string) *domainCheckout.CheckoutSession {
	return &domainCheckout.CheckoutSession{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:         types.GetEnvironmentID(ctx),
		CustomerID:            customerID,
		Action:                r.Action,
		CheckoutStatus:        types.CheckoutStatusInitiated,
		PaymentProvider:       r.Checkout.PaymentProvider,
		Configuration:         domainCheckout.ToJSONBCheckoutConfiguration(r.Configuration),
		PaymentProviderConfig: domainCheckout.ToJSONBCheckoutPaymentProviderConfig(r.Checkout.PaymentProviderConfig),
		IdempotencyKey:        r.Checkout.IdempotencyKey,
		SuccessURL:            r.Checkout.SuccessURL,
		FailureURL:            r.Checkout.FailureURL,
		CancelURL:             r.Checkout.CancelURL,
		ExpiresAt:             time.Now().UTC().Add(r.Checkout.PaymentProvider.SessionExpiry()),
		Metadata:              r.Checkout.Metadata,
		BaseModel:             types.GetDefaultBaseModel(ctx),
	}
}

func ValidateCheckoutSessionForCompletion(session *domainCheckout.CheckoutSession) error {
	if session == nil {
		return ierr.NewError("checkout session is required").
			Mark(ierr.ErrValidation)
	}
	if session.CheckoutInvoiceID == nil || *session.CheckoutInvoiceID == "" {
		return ierr.NewError("session has no checkout invoice").
			WithHint("checkout session must have checkout_invoice_id before it can be completed").
			Mark(ierr.ErrValidation)
	}
	if session.CheckoutPaymentID == nil || *session.CheckoutPaymentID == "" {
		return ierr.NewError("session has no checkout payment").
			WithHint("checkout session must have checkout_payment_id before it can be completed").
			Mark(ierr.ErrValidation)
	}

	cfg := session.Configuration.ToCheckoutConfiguration()
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		if cfg.CreateSubscriptionParams == nil {
			return ierr.NewError("session has no create_subscription_params").
				WithHint("checkout session must have create_subscription_params before it can be completed").
				Mark(ierr.ErrValidation)
		}
		return cfg.CreateSubscriptionParams.Validate()
	case types.CheckoutActionModifySubscription:
		if cfg.ModifySubscriptionParams == nil {
			return ierr.NewError("session has no modify_subscription_params").
				WithHint("checkout session must have modify_subscription_params before it can be completed").
				Mark(ierr.ErrValidation)
		}
		return cfg.ModifySubscriptionParams.Validate()
	case types.CheckoutActionWalletTopup:
		if cfg.WalletTopupParams == nil {
			return ierr.NewError("session has no wallet_topup_params").
				WithHint("checkout session must have wallet_topup_params before it can be completed").
				Mark(ierr.ErrValidation)
		}
		return cfg.WalletTopupParams.Validate()
	case types.CheckoutActionAddAddon:
		if cfg.AddAddonParams == nil {
			return ierr.NewError("session has no add_addon_params").
				WithHint("checkout session must have add_addon_params before it can be completed").
				Mark(ierr.ErrValidation)
		}
		return cfg.AddAddonParams.Validate()
	default:
		return nil
	}
}

// CheckoutPaymentBlock is the customer-facing view of the payment a checkout
// session is waiting on. Null until fulfilment has created one.
type CheckoutPaymentBlock struct {
	ID      string              `json:"id"`
	Status  types.PaymentStatus `json:"status"`
	Gateway string              `json:"gateway,omitempty"`
}

// CheckoutSessionResponse is the API response for a single checkout session.
//
// Fields are listed explicitly rather than embedding the domain model. The domain
// model carries types.BaseModel, whose `status` is the row-lifecycle value
// ("published" / "archived") and means nothing to a caller — a response carrying
// both `status` and `checkout_status` invites reading the wrong one — alongside
// tenant_id, created_by and updated_by, which are internal audit fields.
type CheckoutSessionResponse struct {
	ID              string                        `json:"id"`
	CustomerID      string                        `json:"customer_id"`
	Action          types.CheckoutAction          `json:"action"`
	CheckoutStatus  types.CheckoutStatus          `json:"checkout_status"`
	PaymentProvider types.CheckoutPaymentProvider `json:"payment_provider"`

	CheckoutInvoiceID *string `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string `json:"checkout_payment_id,omitempty"`
	IdempotencyKey    *string `json:"idempotency_key,omitempty"`

	SuccessURL *string `json:"success_url,omitempty"`
	FailureURL *string `json:"failure_url,omitempty"`
	CancelURL  *string `json:"cancel_url,omitempty"`

	ExpiresAt     time.Time      `json:"expires_at"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	CancelledAt   *time.Time     `json:"cancelled_at,omitempty"`
	FailureReason *string        `json:"failure_reason,omitempty"`
	Metadata      types.Metadata `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`

	PaymentAction *types.PaymentAction `json:"payment_action,omitempty"`

	// Terminal reports whether the session has finished. Clients poll until this is
	// true rather than hardcoding the status set, which would go stale if a status
	// is ever added.
	Terminal bool `json:"terminal"`

	// NextPollAfterMs is how long a client should wait before reading again.
	// Zero means stop — either the session is terminal, or this response did not
	// come from a polling read.
	NextPollAfterMs int64 `json:"next_poll_after_ms"`

	// Stale reports that this response is stored state that was not checked against
	// the payment provider on this request — the read was debounced, or the gateway
	// did not answer. A UI should say "still checking" rather than presenting a
	// stale answer as fact.
	Stale bool `json:"stale"`

	// Payment is the payment this session is waiting on, when one exists.
	Payment *CheckoutPaymentBlock `json:"payment,omitempty"`
}

// ListCheckoutSessionsResponse is the paginated list response.
type ListCheckoutSessionsResponse = types.ListResponse[*CheckoutSessionResponse]

// ToCheckoutSessionResponse maps a domain session to its API response.
//
// PaymentAction is derived from ProviderResult; the raw ProviderResult is never
// exposed because it holds gateway session identifiers. Configuration, Result and
// PaymentProviderConfig are likewise internal and have no field here.
func ToCheckoutSessionResponse(s *domainCheckout.CheckoutSession) *CheckoutSessionResponse {
	if s == nil {
		return nil
	}
	return &CheckoutSessionResponse{
		ID:                s.ID,
		CustomerID:        s.CustomerID,
		Action:            s.Action,
		CheckoutStatus:    s.CheckoutStatus,
		PaymentProvider:   s.PaymentProvider,
		CheckoutInvoiceID: s.CheckoutInvoiceID,
		CheckoutPaymentID: s.CheckoutPaymentID,
		IdempotencyKey:    s.IdempotencyKey,
		SuccessURL:        s.SuccessURL,
		FailureURL:        s.FailureURL,
		CancelURL:         s.CancelURL,
		ExpiresAt:         s.ExpiresAt,
		CompletedAt:       s.CompletedAt,
		CancelledAt:       s.CancelledAt,
		FailureReason:     s.FailureReason,
		Metadata:          s.Metadata,
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
		PaymentAction:     s.ProviderResult.ToProviderResult().PaymentAction(),
		Terminal:          s.CheckoutStatus.IsTerminal(),
	}
}
