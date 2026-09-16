package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
)

func (s *customerPortalService) GetCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error) {
	session, err := s.authorizeSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// This is the read a customer's browser sits on while the provider settles, so
	// it carries the same read-triggered reconciliation as the tenant-facing GET: a
	// lost webhook must not leave them watching a spinner. Never fails the read.
	checkoutSvc := &checkoutSessionService{ServiceParams: s.ServiceParams}
	reconciled := checkoutSvc.refreshSessionFromGateway(ctx, session)
	if reconciled.completed {
		// Completion mutated the row; the copy above is behind it.
		session, err = s.CheckoutSessionRepo.Get(ctx, sessionID)
		if err != nil {
			return nil, err
		}
	}

	resp := toPortalCheckoutSession(dto.ToCheckoutSessionResponse(session))
	resp.Stale = reconciled.stale
	resp.NextPollAfterMs = checkoutPollInterval(session).Milliseconds()
	return resp, nil
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
		Terminal:          resp.Terminal,
		ID:                resp.ID,
		CheckoutStatus:    resp.CheckoutStatus,
		PaymentProvider:   gateway,
		PaymentAction:     resp.PaymentAction,
		CheckoutInvoiceID: resp.CheckoutInvoiceID,
		CheckoutPaymentID: resp.CheckoutPaymentID,
		ExpiresAt:         resp.ExpiresAt,
		CompletedAt:       resp.CompletedAt,
		CancelledAt:       resp.CancelledAt,
		FailureReason:     resp.FailureReason,
		EntityCreationResult: resp.EntityCreationResult,
	}
}
