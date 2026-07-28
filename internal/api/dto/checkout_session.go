package dto

import (
	"context"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
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
	default:
		return nil
	}
}

// CheckoutSessionResponse is the API response for a single checkout session.
type CheckoutSessionResponse struct {
	*domainCheckout.CheckoutSession
	PaymentAction *types.PaymentAction `json:"payment_action,omitempty"`
}

// ListCheckoutSessionsResponse is the paginated list response.
type ListCheckoutSessionsResponse = types.ListResponse[*CheckoutSessionResponse]

// ToCheckoutSessionResponse maps a domain session to its API response.
// PaymentAction is derived from ProviderResult; the raw ProviderResult is omitted
// from the response because it contains sensitive gateway tokens.
func ToCheckoutSessionResponse(s *domainCheckout.CheckoutSession) *CheckoutSessionResponse {
	session := lo.FromPtr(s)
	paymentAction := session.ProviderResult.ToProviderResult().PaymentAction()
	session.ProviderResult = nil
	session.Result = nil
	session.Configuration = domainCheckout.JSONBCheckoutConfiguration{}
	session.PaymentProviderConfig = nil
	return &CheckoutSessionResponse{
		CheckoutSession: lo.ToPtr(session),
		PaymentAction:   paymentAction,
	}
}
