package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type CheckoutSessionService = interfaces.CheckoutSessionService

type checkoutSessionService struct {
	ServiceParams
}

func NewCheckoutSessionService(params ServiceParams) interfaces.CheckoutSessionService {
	return &checkoutSessionService{ServiceParams: params}
}

func (s *checkoutSessionService) Create(ctx context.Context, req dto.CreateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	cfg := &types.CheckoutPaymentProviderConfig{}
	if req.PaymentProviderConfig != nil {
		cfg = req.PaymentProviderConfig
	}
	if cfg.CollectionMethod == "" {
		cfg.CollectionMethod = types.CollectionMethodSendInvoice
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Write the normalized config back so ToCheckoutSession persists the resolved defaults.
	req.PaymentProviderConfig = cfg

	customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.CustomerExternalID)
	if err != nil {
		return nil, err
	}

	if customer.Status != types.StatusPublished {
		return nil, ierr.NewError("customer is not active").
			WithHint("The customer must be active to create a checkout session").
			WithReportableDetails(map[string]any{"customer_id": customer.ID, "status": customer.Status}).
			Mark(ierr.ErrValidation)
	}

	session := req.ToCheckoutSession(ctx, customer.ID)

	if err := s.CheckoutSessionRepo.Create(ctx, session); err != nil {
		// TODO: on ErrAlreadyExists (idempotency key conflict), consider fetching and returning
		// the existing session transparently (HTTP 200) instead of propagating 409
		return nil, err
	}

	if err := s.executeCheckoutAction(ctx, session); err != nil {
		// Best-effort cleanup: archive entities + mark session failed.
		// Log cleanup errors but return the original fulfillment error.
		if cleanupErr := s.cleanupCheckoutSession(ctx, session, err); cleanupErr != nil {
			s.Logger.Error(ctx, "checkout cleanup failed after fulfillment error",
				"session_id", session.ID,
				"error", cleanupErr,
				"original_err", err,
			)
		}
		return nil, err
	}

	resp := dto.ToCheckoutSessionResponse(session)
	s.publishCheckoutEvent(ctx, resp, types.WebhookEventCheckoutSessionInitiated)
	return resp, nil
}

func (s *checkoutSessionService) Get(ctx context.Context, id string) (*dto.CheckoutSessionResponse, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	session, err := s.CheckoutSessionRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.ToCheckoutSessionResponse(session), nil
}

func (s *checkoutSessionService) List(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultCheckoutSessionFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	sessions, err := s.CheckoutSessionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.CheckoutSessionRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.CheckoutSessionResponse, len(sessions))
	for i, sess := range sessions {
		items[i] = dto.ToCheckoutSessionResponse(sess)
	}

	result := types.NewListResponse(items, count, filter.GetLimit(), filter.GetOffset())
	return &result, nil
}

func (s *checkoutSessionService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("id is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	session, err := s.CheckoutSessionRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Archiving alone leaves checkout_status pending, so the row keeps holding its
	// idempotency key and keeps blocking the per-wallet pending guard while being
	// invisible to every service query. Reach a terminal state first.
	if err := s.cleanupCheckoutSession(ctx, session, nil); err != nil {
		return err
	}

	return s.CheckoutSessionRepo.Delete(ctx, id)
}

func (s *checkoutSessionService) CleanupCheckoutSession(ctx context.Context, sessionID string, reason error) error {
	if sessionID == "" {
		return ierr.NewError("session ID is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}
	session, err := s.CheckoutSessionRepo.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return s.cleanupCheckoutSession(ctx, session, reason)
}

func (s *checkoutSessionService) cleanupCheckoutSession(ctx context.Context, session *domainCheckout.CheckoutSession, reason error) error {
	// Guard: already in a terminal state — idempotent no-op.
	switch session.CheckoutStatus {
	case types.CheckoutStatusCompleted, types.CheckoutStatusFailed, types.CheckoutStatusExpired:
		return nil
	}

	// Soft-delete (archive) all entities created during fulfillment.
	// Each repo.Delete sets status=archived; errors are non-fatal.
	if session.Result != nil && session.Result.CreateSubscriptionResult != nil {
		res := session.Result.CreateSubscriptionResult
		if res.PaymentID != "" {
			if err := s.PaymentRepo.Delete(ctx, res.PaymentID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout payment", "payment_id", res.PaymentID, "error", err)
			}
		}
		if res.InvoiceID != "" {
			if err := s.InvoiceRepo.Delete(ctx, res.InvoiceID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout invoice", "invoice_id", res.InvoiceID, "error", err)
			}
		}
		if res.SubscriptionID != "" {
			if err := s.SubRepo.Delete(ctx, res.SubscriptionID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout subscription", "subscription_id", res.SubscriptionID, "error", err)
			}
		}
	}

	cfg := session.Configuration.ToCheckoutConfiguration()

	if cfg.CreateSubscriptionParams != nil && cfg.CreateSubscriptionParams.SubscriptionID != "" {
		subSvc := &subscriptionService{ServiceParams: s.ServiceParams}
		subSvc.archiveDraftCheckoutSubscription(ctx, cfg.CreateSubscriptionParams.SubscriptionID)
	}

	if cfg.AddAddonParams != nil {
		for _, ref := range cfg.AddAddonParams.Addons {
			association, err := s.AddonAssociationRepo.GetByID(ctx, ref.AssociationID)
			if err != nil {
				s.Logger.Error(ctx, "failed to load pending addon association for checkout cleanup",
					"association_id", ref.AssociationID, "error", err)
				continue
			}
			if association.AddonStatus != types.AddonStatusPending {
				continue
			}
			if err := s.AddonAssociationRepo.Delete(ctx, ref.AssociationID); err != nil {
				s.Logger.Error(ctx, "failed to archive pending addon association",
					"association_id", ref.AssociationID, "error", err)
			}
		}
	}

	// The pending wallet transaction is not reachable from any archived row, so
	// without this it stays PENDING forever and wedges the auto-top-up guard.
	if cfg.WalletTopupParams != nil && cfg.WalletTopupParams.WalletTransactionID != "" {
		failureReason := "checkout session expired before payment"
		if reason != nil {
			failureReason = reason.Error()
		}

		walletSvc := NewWalletService(s.ServiceParams)
		if err := walletSvc.FailPurchasedCreditTransaction(ctx, cfg.WalletTopupParams.WalletTransactionID, failureReason); err != nil {
			s.Logger.Error(ctx, "failed to fail wallet top-up transaction during checkout cleanup",
				"error", err,
				"session_id", session.ID,
				"wallet_transaction_id", cfg.WalletTopupParams.WalletTransactionID)
		}
	}

	// modify_subscription (and other actions) store ids on the session columns.
	if session.CheckoutPaymentID != nil && *session.CheckoutPaymentID != "" {
		if err := s.PaymentRepo.Delete(ctx, *session.CheckoutPaymentID); err != nil {
			s.Logger.Error(ctx, "failed to archive checkout payment", "payment_id", *session.CheckoutPaymentID, "error", err)
		}
	}
	if session.CheckoutInvoiceID != nil && *session.CheckoutInvoiceID != "" {
		if err := s.InvoiceRepo.Delete(ctx, *session.CheckoutInvoiceID); err != nil {
			s.Logger.Error(ctx, "failed to archive checkout invoice", "invoice_id", *session.CheckoutInvoiceID, "error", err)
		}
	}

	// Terminal status depends on whether this is a natural expiry or an error.
	if reason != nil {
		session.CheckoutStatus = types.CheckoutStatusFailed
		msg := reason.Error()
		session.FailureReason = &msg
	} else {
		session.CheckoutStatus = types.CheckoutStatusExpired
	}

	if err := s.CheckoutSessionRepo.Update(ctx, session); err != nil {
		return err
	}

	// Publish the appropriate lifecycle webhook.
	resp := dto.ToCheckoutSessionResponse(session)
	if reason != nil {
		s.publishCheckoutEvent(ctx, resp, types.WebhookEventCheckoutSessionFailed)
	} else {
		s.publishCheckoutEvent(ctx, resp, types.WebhookEventCheckoutSessionExpired)
	}
	return nil
}

const cleanupExpiredBatchSize = 1000

func (s *checkoutSessionService) CleanupAllExpiredSessions(ctx context.Context, effectiveDate *time.Time) (*types.CheckoutSessionCleanupResult, error) {
	cutoff := time.Now().UTC()
	if effectiveDate != nil {
		cutoff = effectiveDate.UTC()
	}

	result := &types.CheckoutSessionCleanupResult{}

	for {
		sessions, err := s.CheckoutSessionRepo.ListExpiredCheckoutSessions(ctx, cutoff, cleanupExpiredBatchSize, 0)
		if err != nil {
			return result, err
		}

		for _, sess := range sessions {
			result.Total++
			sessCtx := context.WithValue(ctx, types.CtxTenantID, sess.TenantID)
			sessCtx = context.WithValue(sessCtx, types.CtxEnvironmentID, sess.EnvironmentID)
			if err := s.cleanupCheckoutSession(sessCtx, sess, nil); err != nil {
				s.Logger.Error(ctx, "failed to cleanup expired checkout session",
					"session_id", sess.ID, "error", err)
				result.Failed++
				continue
			}
			result.Succeeded++
		}

		if len(sessions) < cleanupExpiredBatchSize {
			break
		}
	}

	return result, nil
}

func (s *checkoutSessionService) CompleteCheckoutSession(ctx context.Context, sessionID string, providerResult *types.CheckoutProviderResult) error {
	if sessionID == "" {
		return ierr.NewError("session ID is required").
			WithHint("checkout session ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Fetch session for completeCheckoutAction context (subscription/invoice/payment IDs).
	session, err := s.CheckoutSessionRepo.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Fast-path guard: already in a terminal state — nothing to do.
	switch session.CheckoutStatus {
	case types.CheckoutStatusCompleted, types.CheckoutStatusFailed, types.CheckoutStatusExpired:
		return ierr.NewError("checkout session already in terminal state").
			WithHintf("session %s is %s", sessionID, session.CheckoutStatus).
			Mark(ierr.ErrAlreadyExists)
	}

	// Run sub-steps idempotently before claiming the session.
	// Safe to run in parallel with a duplicate webhook — each step is conditional.
	if err := s.completeCheckoutAction(ctx, session, providerResult); err != nil {
		return err
	}

	// Atomic claim: only one concurrent caller gets n > 0.
	now := time.Now().UTC()
	claimed, err := s.CheckoutSessionRepo.MarkCompleted(ctx, sessionID, now, providerResult)
	if err != nil {
		return err
	}
	if !claimed {
		// Another process completed it simultaneously — idempotent no-op.
		return ierr.NewError("checkout session already completed by concurrent request").
			WithHintf("session %s was claimed by another process", sessionID).
			Mark(ierr.ErrAlreadyExists)
	}

	session.CheckoutStatus = types.CheckoutStatusCompleted
	session.CompletedAt = &now
	if providerResult != nil {
		session.ProviderResult = domainCheckout.ToJSONBCheckoutProviderResult(providerResult)
	}
	s.publishCheckoutEvent(ctx, dto.ToCheckoutSessionResponse(session), types.WebhookEventCheckoutSessionCompleted)
	return nil
}

func (s *checkoutSessionService) publishCheckoutEvent(ctx context.Context, session *dto.CheckoutSessionResponse, eventName types.WebhookEventName) {
	internal := webhookDto.InternalCheckoutSessionEvent{
		SessionID: session.ID,
		TenantID:  types.GetTenantID(ctx),
	}
	payload, err := json.Marshal(internal)
	if err != nil {
		s.Logger.Error(ctx, "failed to marshal checkout webhook payload", "event_name", eventName, "error", err)
		return
	}
	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(payload),
		EntityType:    types.SystemEntityTypeCheckoutSession,
		EntityID:      session.ID,
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish checkout webhook event", "event_name", eventName, "error", err)
	}
}

func (s *checkoutSessionService) createDraftSubscription(ctx context.Context, session *domainCheckout.CheckoutSession) (*dto.SubscriptionResponse, *dto.InvoiceResponse, error) {
	params := session.Configuration.CreateSubscriptionParams
	if params == nil {
		return nil, nil, ierr.NewError("create_subscription_params is required for create_subscription action").
			Mark(ierr.ErrValidation)
	}
	if err := params.Validate(); err != nil {
		return nil, nil, err
	}

	subReq := dto.CreateSubscriptionRequest{
		CustomerID:         session.CustomerID,
		PlanID:             params.PlanID,
		Currency:           params.Currency,
		LookupKey:          params.LookupKey,
		StartDate:          params.StartDate,
		EndDate:            params.EndDate,
		BillingPeriod:      params.BillingPeriod,
		Metadata:           params.Metadata,
		SubscriptionStatus: types.SubscriptionStatusDraft,
	}

	if session.PaymentProviderConfig != nil && session.PaymentProviderConfig.CollectionMethod != "" {
		subReq.CollectionMethod = lo.ToPtr(session.PaymentProviderConfig.CollectionMethod)
	}

	subSvc := NewSubscriptionService(s.ServiceParams)
	subResp, err := subSvc.CreateSubscription(ctx, subReq)
	if err != nil {
		return nil, nil, err
	}

	invResp, skipped, err := buildCheckoutDraftInvoice(ctx, s.ServiceParams, subResp)
	if err != nil {
		return nil, nil, err
	}
	if skipped {
		return nil, nil, ierr.NewError("checkout requires a non-zero invoice; plan produced no charges").
			Mark(ierr.ErrValidation)
	}

	return subResp, invResp, nil
}

func buildCheckoutDraftInvoice(
	ctx context.Context,
	params ServiceParams,
	subResp *dto.SubscriptionResponse,
) (*dto.InvoiceResponse, bool, error) {
	invSvc := NewInvoiceService(params)
	invResp, err := invSvc.CreateDraftInvoiceForSubscription(
		ctx,
		subResp.ID,
		subResp.CurrentPeriodStart,
		subResp.CurrentPeriodEnd,
		types.ReferencePointPeriodStart,
	)
	if err != nil {
		return nil, false, err
	}

	inv, skipped, err := invSvc.ComputeInvoice(ctx, invResp.ID, nil)
	if err != nil {
		return nil, false, err
	}
	if skipped {
		return invResp, true, nil
	}

	if _, err := invSvc.RecalculateTaxesOnInvoice(ctx, inv); err != nil {
		return nil, false, err
	}

	invResp, err = invSvc.GetInvoice(ctx, inv.ID)
	if err != nil {
		return nil, false, err
	}

	return invResp, false, nil
}

func (s *checkoutSessionService) createCheckoutPayment(ctx context.Context, inv *invoice.Invoice, provider types.CheckoutPaymentProvider) (*dto.PaymentResponse, error) {
	gateway, ok := provider.ToPaymentGateway()
	if !ok {
		return nil, ierr.NewError("unsupported payment provider for checkout").
			WithHint("No gateway mapping exists for this provider").
			WithReportableDetails(map[string]any{"provider": provider}).
			Mark(ierr.ErrValidation)
	}

	paySvc := NewPaymentService(s.ServiceParams)
	return paySvc.CreatePaymentForCheckout(ctx, &dto.CreateCheckoutPaymentRequest{
		Invoice: inv,
		Gateway: gateway,
	})
}

// StartPayFirstCheckoutSession creates a checkout session on an existing DRAFT invoice,
// fulfills payment + provider link, and publishes checkout.session.initiated.
// On session create failure the draft is archived. On fulfill failure the session is cleaned up.
func (s *checkoutSessionService) StartPayFirstCheckoutSession(
	ctx context.Context,
	req *dto.PayFirstCheckoutRequest,
) (*dto.CheckoutSessionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	providerCfg := &types.CheckoutPaymentProviderConfig{}
	if req.Checkout.PaymentProviderConfig != nil {
		providerCfg = req.Checkout.PaymentProviderConfig
	}
	if providerCfg.CollectionMethod == "" {
		providerCfg.CollectionMethod = types.CollectionMethodSendInvoice
	}
	if err := providerCfg.Validate(); err != nil {
		return nil, err
	}
	req.Checkout.PaymentProviderConfig = providerCfg

	draftInvoiceID := req.DraftInvoice.ID
	session := req.ToCheckoutSession(ctx, req.CustomerID)
	session.CheckoutInvoiceID = lo.ToPtr(draftInvoiceID)

	if err := s.CheckoutSessionRepo.Create(ctx, session); err != nil {
		if delErr := s.InvoiceRepo.Delete(ctx, draftInvoiceID); delErr != nil {
			s.Logger.Error(ctx, "failed to archive draft invoice after checkout session create failure",
				"invoice_id", draftInvoiceID,
				"error", delErr,
				"original_err", err,
			)
		}
		return nil, err
	}

	if err := s.fulfillCheckoutSession(ctx, session, req.DraftInvoice); err != nil {
		if cleanupErr := s.cleanupCheckoutSession(ctx, session, err); cleanupErr != nil {
			s.Logger.Error(ctx, "checkout cleanup failed after pay-first fulfillment error",
				"session_id", session.ID,
				"error", cleanupErr,
				"original_err", err,
			)
		}
		return nil, err
	}

	sessionResp := dto.ToCheckoutSessionResponse(session)
	s.publishCheckoutEvent(ctx, sessionResp, types.WebhookEventCheckoutSessionInitiated)
	return sessionResp, nil
}

// fulfillCheckoutSession creates the INITIATED payment, calls the provider for a
// payment link / next action, and marks the session pending.
func (s *checkoutSessionService) fulfillCheckoutSession(
	ctx context.Context,
	session *domainCheckout.CheckoutSession,
	inv *invoice.Invoice,
) error {
	payResp, err := s.createCheckoutPayment(ctx, inv, session.PaymentProvider)
	if err != nil {
		return err
	}
	session.CheckoutInvoiceID = &inv.ID
	session.CheckoutPaymentID = &payResp.ID

	providerResult, err := s.callCheckoutProvider(ctx, session, payResp)
	if err != nil {
		return err
	}
	session.ProviderResult = (*domainCheckout.JSONBCheckoutProviderResult)(providerResult)
	session.CheckoutStatus = types.CheckoutStatusPending
	return s.CheckoutSessionRepo.Update(ctx, session)
}
