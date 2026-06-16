package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CheckoutService manages hosted checkout sessions for deferred subscription activation.
type CheckoutService interface {
	// Create opens a checkout for a new subscription (payment objective in v1).
	Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error)
	// CreateChange opens a payment-gated checkout for an in-place plan UPGRADE: the new
	// plan's subscription is created `incomplete` (opening invoice raised, proration credit
	// netted) and the OLD subscription stays active until the invoice is paid.
	CreateChange(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionChangeCheckoutRequest) (*dto.CheckoutResponse, error)
	// Complete marks a checkout completed (idempotent). Subscription activation is
	// driven by the existing payment-completion hook, not here.
	Complete(ctx context.Context, checkoutID string) error
	// Get returns a checkout by ID.
	Get(ctx context.Context, id string) (*dto.CheckoutResponse, error)
}

type checkoutService struct {
	ServiceParams
	// providerFn resolves the checkout provider; overridable in tests.
	providerFn func(ctx context.Context, provider string) (checkout.CheckoutProvider, error)
}

// NewCheckoutService creates a new checkout service.
func NewCheckoutService(params ServiceParams) CheckoutService {
	s := &checkoutService{ServiceParams: params}
	s.providerFn = func(ctx context.Context, provider string) (checkout.CheckoutProvider, error) {
		return s.IntegrationFactory.GetCheckoutProvider(ctx, provider,
			NewCustomerService(s.ServiceParams), NewInvoiceService(s.ServiceParams))
	}
	return s
}

func (s *checkoutService) Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	switch req.Objective {
	case types.CheckoutObjectivePayment:
		return s.createPayment(ctx, req)
	case types.CheckoutObjectiveSetup:
		return s.createSetup(ctx, req)
	default:
		return nil, ierr.NewError("unsupported checkout objective").
			WithHint("Objective must be 'payment' or 'setup'").
			WithReportableDetails(map[string]any{"objective": req.Objective}).
			Mark(ierr.ErrValidation)
	}
}

func (s *checkoutService) createPayment(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	billingPeriodCount := req.BillingPeriodCount
	if billingPeriodCount == 0 {
		billingPeriodCount = 1
	}

	var chk *checkout.Checkout
	var invoiceID, paymentID string
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// 1. Create the subscription in an incomplete, invoice-collected state.
		subSvc := NewSubscriptionService(s.ServiceParams)
		subResp, err := subSvc.CreateSubscription(txCtx, dto.CreateSubscriptionRequest{
			CustomerID:         req.CustomerID,
			PlanID:             req.PlanID,
			Currency:           req.Currency,
			BillingPeriod:      req.BillingPeriod,
			BillingPeriodCount: billingPeriodCount,
			CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
			PaymentBehavior:    lo.ToPtr(types.PaymentBehaviorDefaultIncomplete),
		})
		if err != nil {
			return err
		}

		// 2. Resolve the opening invoice.
		if subResp.LatestInvoice == nil {
			return ierr.NewError("subscription has no opening invoice").
				WithHint("Cannot open a payment checkout without an opening invoice").
				WithReportableDetails(map[string]any{"subscription_id": subResp.Subscription.ID}).
				Mark(ierr.ErrInvalidOperation)
		}
		inv := subResp.LatestInvoice
		invoiceID = inv.ID

		// 3. Create the (unprocessed) payment record bound to the invoice.
		paymentID, err = s.createInvoicePaymentRecord(txCtx, inv, req.Currency, req.Metadata)
		if err != nil {
			return err
		}

		// 4. Build and persist the pending checkout. The external provider session
		// is opened AFTER commit (see below) so the Stripe call never holds the DB
		// transaction open and never orphans a session if the tx rolls back.
		now := time.Now().UTC()
		chk = &checkout.Checkout{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:    req.CustomerID,
			EntityType:    types.CheckoutEntityTypeSubscription,
			EntityID:      subResp.Subscription.ID,
			CheckoutType:  types.CheckoutTypeSubscriptionCreation,
			Objective:     types.CheckoutObjectivePayment,
			Status:        types.CheckoutStatusPending,
			Amount:        inv.AmountDue,
			Currency:      req.Currency,
			Provider:      string(types.SecretProviderStripe),
			SuccessURL:    lo.ToPtr(req.SuccessURL),
			CancelURL:     lo.ToPtr(req.CancelURL),
			ExpiresAt:     now.Add(24 * time.Hour),
			EnvironmentID: types.GetEnvironmentID(txCtx),
			BaseModel:     types.GetDefaultBaseModel(txCtx),
		}
		return s.CheckoutRepo.Create(txCtx, chk)
	})
	if err != nil {
		return nil, err
	}

	// 5. Post-commit: open the hosted provider session (external call, outside the
	// transaction). On failure the checkout is marked failed; the parked
	// subscription/invoice are reaped by the abandonment cron.
	return s.openSessionAndRespond(ctx, chk, checkout.CheckoutSessionRequest{
		Objective:  types.CheckoutObjectivePayment,
		CheckoutID: chk.ID,
		CustomerID: req.CustomerID,
		InvoiceID:  invoiceID,
		PaymentID:  paymentID,
		Amount:     chk.Amount,
		Currency:   req.Currency,
		SaveCard:   req.SaveCard,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
	})
}

// createSetup opens a setup-objective checkout: the subscription is parked in
// DRAFT (no invoice raised) and a Stripe setup-mode session captures a card
// without charging. Activation happens later via Complete -> ActivateDraftSubscription.
func (s *checkoutService) createSetup(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	billingPeriodCount := req.BillingPeriodCount
	if billingPeriodCount == 0 {
		billingPeriodCount = 1
	}

	var chk *checkout.Checkout
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		subSvc := NewSubscriptionService(s.ServiceParams)
		subResp, err := subSvc.CreateSubscription(txCtx, dto.CreateSubscriptionRequest{
			CustomerID:         req.CustomerID,
			PlanID:             req.PlanID,
			Currency:           req.Currency,
			BillingPeriod:      req.BillingPeriod,
			BillingPeriodCount: billingPeriodCount,
			SubscriptionStatus: types.SubscriptionStatusDraft,
			// On activation (after the card is captured), charge the saved card and
			// land active on success / incomplete on failure — the incomplete case
			// self-heals via the invoice.paid hook. default_active would activate
			// unconditionally and never reach incomplete, so use allow_incomplete.
			CollectionMethod: lo.ToPtr(types.CollectionMethodChargeAutomatically),
			PaymentBehavior:  lo.ToPtr(types.PaymentBehaviorAllowIncomplete),
		})
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		chk = &checkout.Checkout{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:    req.CustomerID,
			EntityType:    types.CheckoutEntityTypeSubscription,
			EntityID:      subResp.Subscription.ID,
			CheckoutType:  types.CheckoutTypeSubscriptionCreation,
			Objective:     types.CheckoutObjectiveSetup,
			Status:        types.CheckoutStatusPending,
			Amount:        decimal.Zero,
			Currency:      req.Currency,
			Provider:      string(types.SecretProviderStripe),
			SuccessURL:    lo.ToPtr(req.SuccessURL),
			CancelURL:     lo.ToPtr(req.CancelURL),
			ExpiresAt:     now.Add(24 * time.Hour),
			EnvironmentID: types.GetEnvironmentID(txCtx),
			BaseModel:     types.GetDefaultBaseModel(txCtx),
		}
		return s.CheckoutRepo.Create(txCtx, chk)
	})
	if err != nil {
		return nil, err
	}

	return s.openSessionAndRespond(ctx, chk, checkout.CheckoutSessionRequest{
		Objective:  types.CheckoutObjectiveSetup,
		CheckoutID: chk.ID,
		CustomerID: req.CustomerID,
		Currency:   req.Currency,
		SaveCard:   true,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
	})
}

// openSessionAndRespond opens the hosted provider session AFTER the DB transaction
// has committed (external call, outside the tx). On failure the checkout is marked
// failed; the parked subscription/invoice are reaped by the abandonment cron. On
// success the session details are persisted on the now-committed checkout.
func (s *checkoutService) openSessionAndRespond(ctx context.Context, chk *checkout.Checkout, sessReq checkout.CheckoutSessionRequest) (*dto.CheckoutResponse, error) {
	provider, err := s.providerFn(ctx, chk.Provider)
	if err != nil {
		s.failCheckout(ctx, chk, err)
		return nil, err
	}
	sess, err := provider.CreateCheckoutSession(ctx, sessReq)
	if err != nil {
		s.failCheckout(ctx, chk, err)
		return nil, err
	}
	chk.ProviderSessionID = &sess.SessionID
	chk.CheckoutURL = &sess.URL
	if err := s.CheckoutRepo.Update(ctx, chk); err != nil {
		return nil, err
	}
	return &dto.CheckoutResponse{
		ID:          chk.ID,
		Status:      string(chk.Status),
		CheckoutURL: sess.URL,
	}, nil
}

// createInvoicePaymentRecord creates an unprocessed payment-link payment record
// bound to the given invoice, returning the new payment's ID.
func (s *checkoutService) createInvoicePaymentRecord(ctx context.Context, inv *dto.InvoiceResponse, currency string, metadata map[string]string) (string, error) {
	paymentSvc := NewPaymentService(s.ServiceParams)
	pay, err := paymentSvc.CreatePayment(ctx, &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(types.PaymentGatewayTypeStripe),
		Amount:            inv.AmountDue,
		Currency:          currency,
		Metadata:          types.Metadata(metadata),
		ProcessPayment:    false,
	})
	if err != nil {
		return "", err
	}
	return pay.ID, nil
}

// CreateChange opens a payment-gated checkout for an in-place plan UPGRADE. The new
// plan's subscription is created `incomplete` (opening invoice raised, proration credit
// netted) inside the tx; the OLD subscription stays active and is only cancelled in
// Complete once the invoice is paid.
func (s *checkoutService) CreateChange(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionChangeCheckoutRequest) (*dto.CheckoutResponse, error) {
	// 1. Dedupe: a pending change-checkout for this source sub must be unique.
	existing, err := s.CheckoutRepo.GetPendingBySourceSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ierr.NewError("a checkout is already pending for this subscription change").
			WithHint("A checkout is already pending for this subscription change").
			WithReportableDetails(map[string]any{"subscription_id": subscriptionID}).
			Mark(ierr.ErrAlreadyExists)
	}

	// 2. Load the source subscription to inherit its billing settings + currency.
	srcSub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// 3. Default proration behavior.
	prorationBehavior := req.ProrationBehavior
	if prorationBehavior == "" {
		prorationBehavior = types.ProrationBehaviorCreateProrations
	}

	var chk *checkout.Checkout
	var invoiceID, paymentID string
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// 3a. Prepare the upgrade: creates the new sub `incomplete` and raises the
		// opening invoice (proration credit netted). The req carries the inherited
		// billing fields because PrepareCheckoutChange does NOT call req.Validate().
		prep, err := NewSubscriptionChangeService(s.ServiceParams).PrepareCheckoutChange(txCtx, subscriptionID, dto.SubscriptionChangeRequest{
			TargetPlanID:       req.TargetPlanID,
			ProrationBehavior:  prorationBehavior,
			BillingCadence:     srcSub.BillingCadence,
			BillingPeriod:      srcSub.BillingPeriod,
			BillingPeriodCount: srcSub.BillingPeriodCount,
			BillingCycle:       srcSub.BillingCycle,
			Metadata:           req.Metadata,
		})
		if err != nil {
			return err
		}

		if prep.Invoice == nil {
			return ierr.NewError("subscription change has no opening invoice").
				WithHint("Cannot open a payment checkout without an opening invoice").
				WithReportableDetails(map[string]any{"subscription_id": subscriptionID}).
				Mark(ierr.ErrInvalidOperation)
		}
		inv := prep.Invoice
		invoiceID = inv.ID

		// 3b. Create the (unprocessed) payment record bound to the opening invoice.
		paymentID, err = s.createInvoicePaymentRecord(txCtx, inv, srcSub.Currency, req.Metadata)
		if err != nil {
			return err
		}

		// 3c. Build and persist the pending change-checkout. EntityID is the NEW sub;
		// SourceSubscriptionID is the OLD sub cancelled on completion.
		now := time.Now().UTC()
		chk = &checkout.Checkout{
			ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:           srcSub.CustomerID,
			EntityType:           types.CheckoutEntityTypeSubscription,
			EntityID:             prep.NewSubscriptionID,
			SourceSubscriptionID: &prep.OldSubscriptionID,
			CheckoutType:         types.CheckoutTypeSubscriptionChange,
			Objective:            types.CheckoutObjectivePayment,
			Status:               types.CheckoutStatusPending,
			Amount:               inv.AmountDue,
			Currency:             srcSub.Currency,
			Provider:             string(types.SecretProviderStripe),
			SuccessURL:           lo.ToPtr(req.SuccessURL),
			CancelURL:            lo.ToPtr(req.CancelURL),
			ExpiresAt:            now.Add(24 * time.Hour),
			EnvironmentID:        types.GetEnvironmentID(txCtx),
			BaseModel:            types.GetDefaultBaseModel(txCtx),
		}
		return s.CheckoutRepo.Create(txCtx, chk)
	})
	if err != nil {
		return nil, err
	}

	// 4. Post-commit: open the hosted provider session.
	return s.openSessionAndRespond(ctx, chk, checkout.CheckoutSessionRequest{
		Objective:  types.CheckoutObjectivePayment,
		CheckoutID: chk.ID,
		CustomerID: srcSub.CustomerID,
		InvoiceID:  invoiceID,
		PaymentID:  paymentID,
		Amount:     chk.Amount,
		Currency:   srcSub.Currency,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
	})
}

// failCheckout best-effort marks a checkout as failed after a post-commit provider
// error, recording the cause. Errors here are logged, not propagated.
func (s *checkoutService) failCheckout(ctx context.Context, chk *checkout.Checkout, cause error) {
	chk.Status = types.CheckoutStatusFailed
	msg := cause.Error()
	chk.ErrorMessage = &msg
	if uerr := s.CheckoutRepo.Update(ctx, chk); uerr != nil {
		s.Logger.Error(ctx, "failed to mark checkout failed after provider error",
			"checkout_id", chk.ID, "error", uerr)
	}
}

func (s *checkoutService) Complete(ctx context.Context, checkoutID string) error {
	chk, err := s.CheckoutRepo.Get(ctx, checkoutID)
	if err != nil {
		return err
	}

	// Idempotent: only a pending checkout transitions to completed.
	if !chk.IsPending() {
		return nil
	}

	now := time.Now().UTC()

	// Setup objective: activate the parked draft subscription (raises the opening
	// invoice and charges the now-saved card). Payment objective is already
	// activated by the invoice.paid hook, so there is nothing to do for it here.
	if chk.Objective == types.CheckoutObjectiveSetup &&
		chk.EntityType == types.CheckoutEntityTypeSubscription {
		subSvc := NewSubscriptionService(s.ServiceParams)
		if _, err := subSvc.ActivateDraftSubscription(ctx, chk.EntityID,
			dto.ActivateDraftSubscriptionRequest{StartDate: &now}); err != nil {
			// Tolerate a retry arriving after the sub was already activated: if it is
			// no longer draft, treat activation as done and complete the checkout.
			sub, getErr := s.SubRepo.Get(ctx, chk.EntityID)
			if getErr != nil || sub.SubscriptionStatus == types.SubscriptionStatusDraft {
				return err
			}
		}
	}

	// Subscription change (upgrade): the new sub is already activated by the invoice.paid
	// hook; here we only cancel the old subscription.
	if chk.CheckoutType == types.CheckoutTypeSubscriptionChange && chk.SourceSubscriptionID != nil {
		if err := NewSubscriptionChangeService(s.ServiceParams).FinalizeCheckoutChange(ctx, *chk.SourceSubscriptionID); err != nil {
			return err
		}
	}

	chk.Status = types.CheckoutStatusCompleted
	chk.CompletedAt = &now
	return s.CheckoutRepo.Update(ctx, chk)
}

func (s *checkoutService) Get(ctx context.Context, id string) (*dto.CheckoutResponse, error) {
	chk, err := s.CheckoutRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &dto.CheckoutResponse{
		ID:          chk.ID,
		Status:      string(chk.Status),
		CheckoutURL: lo.FromPtr(chk.CheckoutURL),
	}, nil
}
