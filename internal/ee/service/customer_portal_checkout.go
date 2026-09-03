package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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

// CancelCheckoutSession terminates an in-flight session.
//
// Routes to CleanupCheckoutSession, NOT the checkout Delete endpoint: Delete only
// sets status=archived and leaves checkout_status untouched, so the session keeps
// blocking the per-wallet pending guard AND keeps holding its idempotency key
// against a row every service query hides.
func (s *customerPortalService) CancelCheckoutSession(ctx context.Context, sessionID string) (*dto.PortalCheckoutSessionResponse, error) {
	session, err := s.authorizeSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	switch session.CheckoutStatus {
	case types.CheckoutStatusCompleted:
		return nil, ierr.NewError("checkout session already completed").
			WithHint("A completed session cannot be cancelled").
			Mark(ierr.ErrValidation)
	case types.CheckoutStatusFailed, types.CheckoutStatusExpired:
		return toPortalCheckoutSession(dto.ToCheckoutSessionResponse(session)), nil
	}

	checkoutSvc := NewCheckoutSessionService(s.ServiceParams)
	// nil reason -> marked expired rather than failed (a cancel is not an error).
	if err := checkoutSvc.CleanupCheckoutSession(ctx, sessionID, nil); err != nil {
		return nil, err
	}

	final, err := s.CheckoutSessionRepo.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return toPortalCheckoutSession(dto.ToCheckoutSessionResponse(final)), nil
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
	}
}
