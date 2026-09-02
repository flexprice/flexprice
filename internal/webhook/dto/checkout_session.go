package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalCheckoutSessionEvent is the internal payload stored in system_events.
// The builder re-fetches the session by ID to build the full outbound payload.
type InternalCheckoutSessionEvent struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
}

type CheckoutSession struct {
	ID                string                        `json:"id"`
	CustomerID        string                        `json:"customer_id"`
	Action            types.CheckoutAction          `json:"action"`
	CheckoutStatus    types.CheckoutStatus          `json:"checkout_status"`
	PaymentProvider   types.CheckoutPaymentProvider `json:"payment_provider"`
	CheckoutInvoiceID *string                       `json:"checkout_invoice_id,omitempty"`
	CheckoutPaymentID *string                       `json:"checkout_payment_id,omitempty"`
	FailureReason     *string                       `json:"failure_reason,omitempty"`
	ExpiresAt         time.Time                     `json:"expires_at"`
	CompletedAt       *time.Time                    `json:"completed_at,omitempty"`
	CancelledAt       *time.Time                    `json:"cancelled_at,omitempty"`
	PaymentAction     *types.PaymentAction          `json:"payment_action,omitempty"`
}

func NewCheckoutSession(resp *dto.CheckoutSessionResponse) *CheckoutSession {
	if resp == nil || resp.CheckoutSession == nil {
		return nil
	}
	return &CheckoutSession{
		ID:                resp.ID,
		CustomerID:        resp.CustomerID,
		Action:            resp.Action,
		CheckoutStatus:    resp.CheckoutStatus,
		PaymentProvider:   resp.PaymentProvider,
		CheckoutInvoiceID: resp.CheckoutInvoiceID,
		CheckoutPaymentID: resp.CheckoutPaymentID,
		FailureReason:     resp.FailureReason,
		ExpiresAt:         resp.ExpiresAt,
		CompletedAt:       resp.CompletedAt,
		CancelledAt:       resp.CancelledAt,
		PaymentAction:     resp.PaymentAction,
	}
}

// CheckoutSessionWebhookPayload is the outbound payload delivered to subscribers.
type CheckoutSessionWebhookPayload struct {
	EventType       types.WebhookEventName `json:"event_type"`
	CheckoutSession *CheckoutSession       `json:"checkout_session"`
}

func NewCheckoutSessionWebhookPayload(session *dto.CheckoutSessionResponse, eventType types.WebhookEventName) *CheckoutSessionWebhookPayload {
	return &CheckoutSessionWebhookPayload{
		EventType:       eventType,
		CheckoutSession: NewCheckoutSession(session),
	}
}
