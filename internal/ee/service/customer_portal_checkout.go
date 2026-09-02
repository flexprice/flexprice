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
	return toPortalCheckoutSession(dto.ToCheckoutSessionResponse(session)), nil
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
	if resp == nil || resp.CheckoutSession == nil {
		return nil
	}

	session := resp.CheckoutSession
	gateway, _ := session.PaymentProvider.ToPaymentGateway()
	return &dto.PortalCheckoutSessionResponse{
		ID:                session.ID,
		CheckoutStatus:    session.CheckoutStatus,
		PaymentProvider:   gateway,
		PaymentAction:     resp.PaymentAction,
		CheckoutInvoiceID: session.CheckoutInvoiceID,
		CheckoutPaymentID: session.CheckoutPaymentID,
		ExpiresAt:         session.ExpiresAt,
		CompletedAt:       session.CompletedAt,
		CancelledAt:       session.CancelledAt,
		FailureReason:     session.FailureReason,
	}
}
