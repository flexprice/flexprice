package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *checkoutSessionService) executeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	s.Logger.Debug(ctx, "executeCheckoutAction: starting", "session_id", session.ID, "action", session.Action)
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		s.Logger.Debug(ctx, "executeCheckoutAction: creating draft subscription", "session_id", session.ID)
		subResp, invResp, err := s.createDraftSubscription(ctx, session)
		if err != nil {
			s.Logger.Error(ctx, "executeCheckoutAction: createDraftSubscription failed", "session_id", session.ID, "err", err)
			return err
		}
		s.Logger.Debug(ctx, "executeCheckoutAction: draft subscription created", "sub_id", subResp.ID, "inv_id", invResp.ID)

		// Stage sub and invoice IDs immediately so CleanupCheckoutSession can
		// archive them if the payment step below fails.
		result := types.CheckoutResult{
			CreateSubscriptionResult: &types.CreateSubscriptionResult{
				SubscriptionID: subResp.ID,
				InvoiceID:      invResp.ID,
			},
		}
		session.Result = (*domainCheckout.JSONBCheckoutResult)(&result)

		payResp, err := s.createCheckoutPayment(ctx, &invResp.Invoice, session.PaymentProvider)
		if err != nil {
			return err
		}

		result.CreateSubscriptionResult.PaymentID = payResp.ID
		session.CheckoutInvoiceID = &invResp.ID
		session.CheckoutPaymentID = &payResp.ID
		session.CheckoutStatus = types.CheckoutStatusPending

	default:
		return ierr.NewError("unsupported checkout action").
			WithHint("No fulfillment handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}

	return s.CheckoutSessionRepo.Update(ctx, session)
}

func (s *checkoutSessionService) completeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession, providerResult *types.CheckoutProviderResult) error {
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		return s.completeSubscriptionCheckout(ctx, session, providerResult)
	default:
		return ierr.NewError("unsupported checkout action for completion").
			WithHint("No completion handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}
}

func (s *checkoutSessionService) completeSubscriptionCheckout(ctx context.Context, session *domainCheckout.CheckoutSession, providerResult *types.CheckoutProviderResult) error {
	if session.Result == nil || session.Result.CreateSubscriptionResult == nil {
		return ierr.NewError("session has no fulfillment result").
			WithHint("checkout session must have been fulfilled before it can be completed").
			Mark(ierr.ErrValidation)
	}
	res := session.Result.CreateSubscriptionResult

	// 1. Activate subscription: draft → active (no billing side effects needed)
	sub, err := s.SubRepo.Get(ctx, res.SubscriptionID)
	if err != nil {
		return err
	}
	sub.SubscriptionStatus = types.SubscriptionStatusActive
	if err := s.SubRepo.Update(ctx, sub); err != nil {
		return err
	}

	// 2. Finalize the draft invoice (assigns invoice number, seals it)
	invSvc := NewInvoiceService(s.ServiceParams)
	if err := invSvc.FinalizeInvoice(ctx, res.InvoiceID); err != nil {
		return err
	}

	// 3. Mark the checkout payment as SUCCEEDED, storing the gateway payment ID
	statusStr := string(types.PaymentStatusSucceeded)
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &statusStr,
		SucceededAt:   &now,
	}
	if providerResult != nil && providerResult.CreateSubscriptionResult != nil {
		if id := providerResult.CreateSubscriptionResult.PaymentIntentID; id != "" {
			updateReq.GatewayPaymentID = &id
		}
	}
	paySvc := NewPaymentService(s.ServiceParams)
	if _, err := paySvc.UpdatePayment(ctx, res.PaymentID, updateReq); err != nil {
		return err
	}

	// 4. Reconcile invoice payment status (marks invoice as paid)
	return invSvc.ReconcilePaymentStatus(ctx, res.InvoiceID, types.PaymentStatusSucceeded, nil)
}
