package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSessionWebhookPayload is the minimal webhook representation of a checkout session.
type CheckoutSessionWebhookPayload struct {
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

func NewCheckoutSessionWebhookPayload(resp *dto.CheckoutSessionResponse) *CheckoutSessionWebhookPayload {
	if resp == nil || resp.CheckoutSession == nil {
		return nil
	}
	return &CheckoutSessionWebhookPayload{
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
