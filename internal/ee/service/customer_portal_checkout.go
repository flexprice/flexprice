package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
)

func (s *customerPortalService) GetCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error) {
	if _, err := s.authorizeSession(ctx, sessionID); err != nil {
		return nil, err
	}

	// Same read-triggered reconciliation as the tenant-facing GET. A lost webhook
	// must not leave the customer watching a spinner. Never fails the read.
	resp, err := NewCheckoutSessionService(s.ServiceParams).GetAndReconcile(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return toPortalCheckoutSession(resp), nil
}

// CancelCheckoutSession terminates an in-flight session owned by this customer.
//
// Routes to Cancel, not Delete: Delete archives the row after cleanup. Cancel
// leaves it published as expired so the client can poll the terminal state.
func (s *customerPortalService) CancelCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error) {
	if _, err := s.authorizeSession(ctx, sessionID); err != nil {
		return nil, err
	}

	resp, err := NewCheckoutSessionService(s.ServiceParams).Cancel(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return toPortalCheckoutSession(resp), nil
}

func toPortalCheckoutSession(resp *dto.CheckoutSessionResponse) *dto.PortalCheckoutSessionResponse {
	if resp == nil {
		return nil
	}

	gateway, _ := resp.PaymentProvider.ToPaymentGateway()
	return &dto.PortalCheckoutSessionResponse{
		Terminal:             resp.Terminal,
		ID:                   resp.ID,
		CheckoutStatus:       resp.CheckoutStatus,
		PaymentProvider:      gateway,
		PaymentAction:        resp.PaymentAction,
		CheckoutInvoiceID:    resp.CheckoutInvoiceID,
		CheckoutPaymentID:    resp.CheckoutPaymentID,
		ExpiresAt:            resp.ExpiresAt,
		CompletedAt:          resp.CompletedAt,
		CancelledAt:          resp.CancelledAt,
		FailureReason:        resp.FailureReason,
		EntityCreationResult: resp.EntityCreationResult,
		Stale:                resp.Stale,
		NextPollAfterMs:      resp.NextPollAfterMs,
	}
}
