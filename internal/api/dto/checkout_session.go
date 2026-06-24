package dto

import (
	"context"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateCheckoutSessionRequest is the request body for POST /checkout/sessions.
type CreateCheckoutSessionRequest struct {
	CustomerID      string                         `json:"customer_id" binding:"required"`
	Action          types.CheckoutAction           `json:"action" binding:"required"`
	PaymentProvider *types.CheckoutPaymentProvider `json:"payment_provider" binding:"required"`
	Configuration   types.CheckoutConfiguration    `json:"configuration"`
	IdempotencyKey  *string                        `json:"idempotency_key,omitempty"`
	SuccessURL      *string                        `json:"success_url,omitempty"`
	FailureURL      *string                        `json:"failure_url,omitempty"`
	CancelURL       *string                        `json:"cancel_url,omitempty"`
	ExpiresAt       time.Time                      `json:"expires_at" binding:"required"`
	Metadata        map[string]string              `json:"metadata,omitempty"`
}

func (r *CreateCheckoutSessionRequest) Validate() error {

	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.Action.Validate(); err != nil {
		return err
	}

	if r.PaymentProvider == nil {
		return ierr.NewError("payment_provider is required").
			WithHint("payment_provider cannot be empty").
			Mark(ierr.ErrValidation)
	}
	if err := r.PaymentProvider.Validate(); err != nil {
		return err
	}
	if r.ExpiresAt.IsZero() {
		return ierr.NewError("expires_at is required").
			WithHint("expires_at cannot be zero").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *CreateCheckoutSessionRequest) ToCheckoutSession(ctx context.Context) *domainCheckout.CheckoutSession {
	return &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      r.CustomerID,
		Action:          r.Action,
		CheckoutStatus:  types.CheckoutStatusInitiated,
		PaymentProvider: r.PaymentProvider,
		Configuration:   r.Configuration,
		IdempotencyKey:  r.IdempotencyKey,
		SuccessURL:      r.SuccessURL,
		FailureURL:      r.FailureURL,
		CancelURL:       r.CancelURL,
		ExpiresAt:       r.ExpiresAt,
		Metadata:        r.Metadata,
		BaseModel:       types.GetDefaultBaseModel(ctx),
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

// PaymentAction is derived from ProviderResult at response-build time; never stored.
type PaymentAction struct {
	Type types.PaymentActionType `json:"type"`
	URL  string                  `json:"url"`
}

// CheckoutSessionResponse is the API response for a single checkout session.
type CheckoutSessionResponse struct {
	*domainCheckout.CheckoutSession
	PaymentAction *PaymentAction `json:"payment_action,omitempty"`
}

// ListCheckoutSessionsResponse is the paginated list response.
type ListCheckoutSessionsResponse = types.ListResponse[*CheckoutSessionResponse]

// ToCheckoutSessionResponse maps a domain session to its API response, deriving PaymentAction.
func ToCheckoutSessionResponse(s *domainCheckout.CheckoutSession) *CheckoutSessionResponse {
	resp := &CheckoutSessionResponse{CheckoutSession: s}
	if s.ProviderResult != nil && s.ProviderResult.CreateSubscriptionResult != nil {
		url := s.ProviderResult.CreateSubscriptionResult.SessionURL
		if url != "" {
			resp.PaymentAction = &PaymentAction{
				Type: types.PaymentActionTypeCheckoutURL,
				URL:  url,
			}
		}
	}
	return resp
}
