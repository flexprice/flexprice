package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *checkoutSessionService) executeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		subResp, invResp, err := s.createDraftSubscription(ctx, session)
		if err != nil {
			return err
		}

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

		// Contact the payment gateway and get the hosted checkout URL.
		providerResult, err := s.callCheckoutProvider(ctx, session, payResp)
		if err != nil {
			return err
		}
		session.ProviderResult = (*domainCheckout.JSONBCheckoutProviderResult)(providerResult)
		session.CheckoutStatus = types.CheckoutStatusPending

	default:
		return ierr.NewError("unsupported checkout action").
			WithHint("No fulfillment handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}

	return s.CheckoutSessionRepo.Update(ctx, session)
}

// callCheckoutProvider contacts the payment gateway, tightens ExpiresAt if the provider URL
// expires sooner, and records an EntityIntegrationMapping (ProviderSessionID → FlexPrice PaymentID).
func (s *checkoutSessionService) callCheckoutProvider(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	payResp *dto.PaymentResponse,
) (*types.CheckoutProviderResult, error) {
	customerSvc := NewCustomerService(s.ServiceParams)
	invoiceSvc := NewInvoiceService(s.ServiceParams)
	provider, err := s.IntegrationFactory.GetCheckoutProvider(ctx, session.PaymentProvider, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}

	req := interfaces.CheckoutProviderRequest{
		InvoiceID:  *session.CheckoutInvoiceID,
		CustomerID: session.CustomerID,
		Amount:     payResp.Amount,
		Currency:   payResp.Currency,
		PaymentID:  payResp.ID,
		SuccessURL: lo.FromPtr(session.SuccessURL),
		FailureURL: lo.FromPtr(session.FailureURL),
		CancelURL:  lo.FromPtr(session.CancelURL),
		Metadata:   session.Metadata,
	}

	cfg := types.CheckoutPaymentProviderConfig{}
	if session.PaymentProviderConfig != nil {
		cfg = *session.PaymentProviderConfig.ToCheckoutPaymentProviderConfig()
	}
	if cfg.CollectionMethod == "" {
		cfg.CollectionMethod = types.CollectionMethodSendInvoice
	}

	var resp *interfaces.CheckoutProviderResponse

	switch cfg.CollectionMethod {

	case types.CollectionMethodChargeAutomatically:
		resp, err = provider.CreateAuthorizationLink(ctx, interfaces.AuthorizationLinkRequest{
			InvoiceID:       req.InvoiceID,
			CustomerID:      req.CustomerID,
			PaymentID:       req.PaymentID,
			Amount:          req.Amount,
			Currency:        req.Currency,
			MaxAmount:       cfg.MaxMandateLimit, // nil = no ceiling
			PreferredMethod: cfg.PreferredMethod,
			SuccessURL:      req.SuccessURL,
			CancelURL:       req.CancelURL,
			Metadata:        req.Metadata,
		})
	case types.CollectionMethodSendInvoice:
		resp, err = provider.CreatePaymentLink(ctx, req)
	}

	if err != nil {
		return nil, err
	}

	// Tighten session expiry if the provider URL expires sooner.
	if resp.ExpiresAt != nil && resp.ExpiresAt.Before(session.ExpiresAt) {
		session.ExpiresAt = *resp.ExpiresAt
	}

	// Record ProviderSessionID → FlexPrice PaymentID so incoming webhooks can route back.
	mappingSvc := NewEntityIntegrationMappingService(s.ServiceParams)
	if _, err := mappingSvc.CreateEntityIntegrationMapping(ctx, dto.CreateEntityIntegrationMappingRequest{
		EntityID:         payResp.ID,
		EntityType:       types.IntegrationEntityTypePayment,
		ProviderType:     session.PaymentProvider.String(),
		ProviderEntityID: resp.ProviderSessionID,
	}); err != nil {
		return nil, err
	}

	return &types.CheckoutProviderResult{
		NextAction:              &resp.NextAction,
		ProviderSessionID:       resp.ProviderSessionID,
		ProviderPaymentIntentID: resp.ProviderPaymentIntentID,
		ExpiresAt:               resp.ExpiresAt,
		ProviderMetadata:        resp.ProviderMetadata,
	}, nil
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

	// 1. Activate subscription: only update if still in draft.
	sub, err := s.SubRepo.Get(ctx, res.SubscriptionID)
	if err != nil {
		return err
	}
	if sub.SubscriptionStatus == types.SubscriptionStatusDraft {
		sub.SubscriptionStatus = types.SubscriptionStatusActive
		if err := s.SubRepo.Update(ctx, sub); err != nil {
			return err
		}
	}

	// 2. Finalize the draft invoice (idempotent: check status first).
	invSvc := NewInvoiceService(s.ServiceParams)
	invResp, err := invSvc.GetInvoice(ctx, res.InvoiceID)
	if err != nil {
		return err
	}
	if invResp.InvoiceStatus != types.InvoiceStatusFinalized {
		if err := invSvc.FinalizeInvoice(ctx, res.InvoiceID); err != nil {
			return err
		}
	}

	// 3. Mark the checkout payment as SUCCEEDED, storing the gateway payment ID.
	statusStr := string(types.PaymentStatusSucceeded)
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &statusStr,
		SucceededAt:   &now,
	}
	if providerResult != nil && providerResult.ProviderPaymentIntentID != "" {
		id := providerResult.ProviderPaymentIntentID
		updateReq.GatewayPaymentID = &id
	}
	paySvc := NewPaymentService(s.ServiceParams)
	if _, err := paySvc.UpdatePayment(ctx, res.PaymentID, updateReq); err != nil {
		return err
	}

	// 4. Reconcile invoice payment status (marks invoice as paid — already idempotent).
	return invSvc.ReconcilePaymentStatus(ctx, res.InvoiceID, types.PaymentStatusSucceeded, nil)
}
