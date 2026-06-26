package dto

import (
	"context"
	"time"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateCheckoutSessionRequest is the request body for POST /checkout/sessions.
type CreateCheckoutSessionRequest struct {
	CustomerExternalID string                        `json:"customer_external_id" binding:"required"`
	Action             types.CheckoutAction          `json:"action" binding:"required"`
	PaymentProvider    types.CheckoutPaymentProvider `json:"payment_provider" binding:"required"`
	Configuration      types.CheckoutConfiguration   `json:"configuration"`
	IdempotencyKey     *string                       `json:"idempotency_key,omitempty"`
	SuccessURL         *string                       `json:"success_url,omitempty"`
	FailureURL         *string                       `json:"failure_url,omitempty"`
	CancelURL          *string                       `json:"cancel_url,omitempty"`
	Metadata           map[string]string             `json:"metadata,omitempty"`
}

func (r *CreateCheckoutSessionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.Action.Validate(); err != nil {
		return err
	}

	if err := r.PaymentProvider.Validate(); err != nil {
		return err
	}

	if err := r.Configuration.Validate(r.Action); err != nil {
		return err
	}

	return nil
}

// ResolveExpiresAt returns when the session should expire based on the payment provider.
func (r *CreateCheckoutSessionRequest) ResolveExpiresAt(now time.Time) time.Time {
	return now.UTC().Add(r.PaymentProvider.SessionExpiry())
}

func (r *CreateCheckoutSessionRequest) ToCheckoutSession(ctx context.Context, customerID string) *domainCheckout.CheckoutSession {
	return &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      customerID,
		Action:          r.Action,
		CheckoutStatus:  types.CheckoutStatusInitiated,
		PaymentProvider: r.PaymentProvider,
		Configuration:   domainCheckout.JSONBCheckoutConfiguration(r.Configuration),
		IdempotencyKey:  r.IdempotencyKey,
		SuccessURL:      r.SuccessURL,
		FailureURL:      r.FailureURL,
		CancelURL:       r.CancelURL,
		ExpiresAt:       r.ResolveExpiresAt(time.Now()),
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

// CreateCheckoutPaymentRequest holds parameters for creating an INITIATED payment
// record during checkout fulfillment. Uses the domain invoice directly to avoid
// a redundant DB lookup. Extend this struct to add metadata, idempotency keys,
// or additional gateway fields without changing the service interface signature.
type CreateCheckoutPaymentRequest struct {
	Invoice *invoice.Invoice
	Gateway types.PaymentGatewayType
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
	// Shallow-copy so we don't mutate the caller's domain object.
	copy := *s
	resp := &CheckoutSessionResponse{CheckoutSession: &copy}
	if s.ProviderResult != nil {
		resp.PaymentAction = (*types.CheckoutProviderResult)(s.ProviderResult).PaymentAction()
		resp.CheckoutSession.ProviderResult = nil
	}
	return resp
}
