package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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

	cfg := lo.FromPtr(session.PaymentProviderConfig.ToCheckoutPaymentProviderConfig())

	var resp *interfaces.CheckoutProviderResponse

	var maxAmount *decimal.Decimal

	switch cfg.CollectionMethod {

	case types.CollectionMethodChargeAutomatically:
		maxAmount, err = s.resolveMaxMandateLimit(ctx, cfg, req.Currency)
		if err != nil {
			return nil, err
		}

		authReq := interfaces.AuthorizationLinkRequest{
			InvoiceID:       req.InvoiceID,
			CustomerID:      req.CustomerID,
			PaymentID:       req.PaymentID,
			Amount:          req.Amount,
			Currency:        req.Currency,
			MaxAmount:       maxAmount,
			PreferredMethod: cfg.PaymentMethod,
			SuccessURL:      req.SuccessURL,
			CancelURL:       req.CancelURL,
			Metadata:        req.Metadata,
		}

		// Prefer an existing confirmed token (off-session). If none / amount above
		// mandate max, fall back to registration+charge auth link.
		if chargedResp, charged, chargeErr := provider.TryAutoChargingSavedMethod(ctx, authReq); chargeErr != nil {
			return nil, chargeErr
		} else if charged {
			resp = chargedResp
			break
		}

		resp, err = provider.CreateAuthorizationLink(ctx, authReq)

	case types.CollectionMethodSendInvoice:
		resp, err = provider.CreatePaymentLink(ctx, req)

	default:
		return nil, ierr.NewError("unsupported collection method").
			WithHint("Unsupported collection method").
			WithReportableDetails(map[string]any{"collection_method": cfg.CollectionMethod}).
			Mark(ierr.ErrValidation)
	}

	if err != nil {
		return nil, err
	}

	// Tighten session expiry if the provider URL expires sooner.
	if resp.ExpiresAt != nil && resp.ExpiresAt.Before(session.ExpiresAt) {
		session.ExpiresAt = *resp.ExpiresAt
	}

	// Record ProviderSessionID → FlexPrice PaymentID so incoming webhooks can route back.
	if resp.ProviderSessionID != "" {
		mappingSvc := NewEntityIntegrationMappingService(s.ServiceParams)
		if _, err := mappingSvc.CreateEntityIntegrationMapping(ctx, dto.CreateEntityIntegrationMappingRequest{
			EntityID:         payResp.ID,
			EntityType:       types.IntegrationEntityTypePayment,
			ProviderType:     session.PaymentProvider.String(),
			ProviderEntityID: resp.ProviderSessionID,
		}); err != nil {
			return nil, err
		}
	}

	result := &types.CheckoutProviderResult{
		ProviderSessionID:       resp.ProviderSessionID,
		ProviderPaymentIntentID: resp.ProviderPaymentIntentID,
		ExpiresAt:               resp.ExpiresAt,
		ProviderMetadata:        resp.ProviderMetadata,
		NextAction:              lo.ToPtr(resp.NextAction),
	}
	return result, nil
}

// resolveMaxMandateLimit caps MaxMandateLimit against the tenant's
// PaymentMandateLimits ceiling. UPI only — Card has no ceiling.
func (s *checkoutSessionService) resolveMaxMandateLimit(
	ctx context.Context,
	cfg types.CheckoutPaymentProviderConfig,
	currency string,
) (*decimal.Decimal, error) {
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	limits, err := GetSetting[types.PaymentMandateLimits](settingsSvc, ctx, types.SettingKeyPaymentMandateLimits)
	if err != nil {
		return nil, err
	}

	return capMandateLimit(cfg.PaymentMethod, cfg.MaxMandateLimit, currency, limits), nil
}

func capMandateLimit(
	method types.PaymentMethodType,
	callerLimit *decimal.Decimal,
	currency string,
	limits types.PaymentMandateLimits,
) *decimal.Decimal {
	if method == "" {
		method = types.PaymentMethodTypeUPI
	}
	if method != types.PaymentMethodTypeUPI {
		return callerLimit
	}

	limit, ok := limits.MandateLimits[method]
	if !ok || (limit.Currency != "" && !strings.EqualFold(limit.Currency, currency)) {
		return callerLimit
	}

	if callerLimit == nil || callerLimit.GreaterThan(limit.MaxAmount) {
		return &limit.MaxAmount
	}
	return callerLimit
}

func (s *checkoutSessionService) completeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession, providerResult *types.CheckoutProviderResult) error {
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		return s.completeSubscriptionCheckout(ctx, session, providerResult)
	case types.CheckoutActionModifySubscription:
		return s.completeModifySubscriptionCheckout(ctx, session, providerResult)
	case types.CheckoutActionWalletTopup:
		return s.completeWalletTopupCheckout(ctx, session, providerResult)
	case types.CheckoutActionAddAddon:
		return s.completeAddAddonCheckout(ctx, session, providerResult)
	default:
		return ierr.NewError("unsupported checkout action for completion").
			WithHint("No completion handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}
}

// completeModifySubscriptionCheckout applies the saved quantity-change request (C), then
// finalizes the existing DRAFT proration invoice and reconciles payment.
// Does not recompute proration — amount is locked on the draft from pay-first execute.
func (s *checkoutSessionService) completeModifySubscriptionCheckout(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	providerResult *types.CheckoutProviderResult,
) error {
	if err := dto.ValidateCheckoutSessionForCompletion(session); err != nil {
		return err
	}

	cfg := session.Configuration.ToCheckoutConfiguration()
	params := cfg.ModifySubscriptionParams
	invoiceID := *session.CheckoutInvoiceID
	paymentID := *session.CheckoutPaymentID

	modSvc := &subscriptionModificationService{serviceParams: s.ServiceParams}
	quantityChangeReq, err := modSvc.requestFromModifySubscriptionParams(ctx, params)
	if err != nil {
		return err
	}
	if _, err := modSvc.applyQuantityChange(ctx, quantityChangeReq); err != nil {
		return err
	}

	if err := s.finalizeCheckoutInvoiceAndPayment(ctx, invoiceID, paymentID, providerResult); err != nil {
		return err
	}

	modSvc.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, params.SubscriptionID)
	return nil
}

// completeAddAddonCheckout activates the pending addon associations the session gated, then
// finalizes the existing DRAFT proration invoice and reconciles payment.
func (s *checkoutSessionService) completeAddAddonCheckout(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	providerResult *types.CheckoutProviderResult,
) error {
	if err := dto.ValidateCheckoutSessionForCompletion(session); err != nil {
		return err
	}

	cfg := session.Configuration.ToCheckoutConfiguration()
	params := cfg.AddAddonParams
	invoiceID := *session.CheckoutInvoiceID
	paymentID := *session.CheckoutPaymentID

	subSvc := &subscriptionService{ServiceParams: s.ServiceParams}
	if err := subSvc.applyAddAddonCheckoutParams(ctx, params); err != nil {
		return err
	}

	if err := s.finalizeCheckoutInvoiceAndPayment(ctx, invoiceID, paymentID, providerResult); err != nil {
		return err
	}

	subSvc.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, params.SubscriptionID)
	return nil
}

// completeWalletTopupCheckout finalizes the DRAFT credit-purchase invoice and reconciles
// payment. Wallet credits are applied by the existing ReconcilePaymentStatus hook via
// invoice metadata.wallet_transaction_id.
func (s *checkoutSessionService) completeWalletTopupCheckout(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	providerResult *types.CheckoutProviderResult,
) error {
	if err := dto.ValidateCheckoutSessionForCompletion(session); err != nil {
		return err
	}

	cfg := session.Configuration.ToCheckoutConfiguration()
	params := cfg.WalletTopupParams

	w, err := s.WalletRepo.GetWalletByID(ctx, params.WalletID)
	if err != nil {
		return err
	}
	if w.WalletStatus != types.WalletStatusActive {
		return ierr.NewError("wallet is not active").
			WithHint("The wallet must be active to complete a payment-gated top-up").
			WithReportableDetails(map[string]any{
				"wallet_id":     params.WalletID,
				"wallet_status": w.WalletStatus,
				"status":        w.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	return s.finalizeCheckoutInvoiceAndPayment(
		ctx,
		*session.CheckoutInvoiceID,
		*session.CheckoutPaymentID,
		providerResult,
	)
}

func (s *checkoutSessionService) completeSubscriptionCheckout(ctx context.Context, session *domainCheckout.CheckoutSession, providerResult *types.CheckoutProviderResult) error {
	var subscriptionId string
	var invoiceId string
	var paymentId string

	if cfg := session.Configuration.ToCheckoutConfiguration(); cfg.CreateSubscriptionParams != nil {
		subscriptionId = cfg.CreateSubscriptionParams.SubscriptionID
	}
	invoiceId = lo.FromPtr(session.CheckoutInvoiceID)
	paymentId = lo.FromPtr(session.CheckoutPaymentID)

	if session.Result != nil && session.Result.CreateSubscriptionResult != nil {
		legacy := session.Result.CreateSubscriptionResult
		if subscriptionId == "" {
			subscriptionId = legacy.SubscriptionID
		}
		if invoiceId == "" {
			invoiceId = legacy.InvoiceID
		}
		if paymentId == "" {
			paymentId = legacy.PaymentID
		}
	}

	if subscriptionId == "" || invoiceId == "" || paymentId == "" {
		return ierr.NewError("session has no fulfillment result").
			WithHint("checkout session must have been fulfilled before it can be completed").
			WithReportableDetails(map[string]any{
				"session_id":      session.ID,
				"subscription_id": subscriptionId,
				"invoice_id":      invoiceId,
				"payment_id":      paymentId,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := s.finalizeCheckoutInvoiceAndPayment(ctx, invoiceId, paymentId, providerResult); err != nil {
		return err
	}

	sub, err := s.SubRepo.Get(ctx, subscriptionId)
	if err != nil {
		return err
	}

	subSvc := &subscriptionService{ServiceParams: s.ServiceParams}
	if err := subSvc.activateDraftSubscription(ctx, sub); err != nil {
		return err
	}

	return nil
}

// finalizeCheckoutInvoiceAndPayment finalizes a DRAFT checkout invoice (idempotent),
// marks the checkout payment SUCCEEDED, and reconciles invoice payment status.
func (s *checkoutSessionService) finalizeCheckoutInvoiceAndPayment(
	ctx context.Context,
	invoiceID string,
	paymentID string,
	providerResult *types.CheckoutProviderResult,
) error {
	invSvc := NewInvoiceService(s.ServiceParams)
	invResp, err := invSvc.GetInvoice(ctx, invoiceID)
	if err != nil {
		return err
	}
	if invResp.InvoiceStatus != types.InvoiceStatusFinalized {
		if err := invSvc.FinalizeInvoice(ctx, invoiceID); err != nil {
			return err
		}
	}

	statusStr := string(types.PaymentStatusSucceeded)
	now := time.Now().UTC()
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &statusStr,
		SucceededAt:   &now,
	}
	attemptReq := dto.RecordAttemptRequest{PaymentStatus: types.PaymentStatusSucceeded}
	if providerResult != nil && providerResult.ProviderPaymentIntentID != "" {
		id := providerResult.ProviderPaymentIntentID
		updateReq.GatewayPaymentID = &id
		attemptReq.GatewayAttemptID = id
	}

	paySvc := NewPaymentService(s.ServiceParams)
	if _, err := paySvc.UpdatePayment(ctx, paymentID, updateReq); err != nil {
		return err
	}

	// After the settle, so a payment that never succeeded leaves no succeeded attempt.
	if err := paySvc.RecordAttempt(ctx, paymentID, attemptReq); err != nil {
		s.Logger.Error(ctx, "failed to record succeeded attempt",
			"payment_id", paymentID, "error", err)
	}

	return invSvc.ReconcilePaymentStatus(ctx, invoiceID, types.PaymentStatusSucceeded, nil)
}
