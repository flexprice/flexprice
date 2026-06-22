package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// CheckoutService manages hosted checkout sessions for deferred subscription activation.
type CheckoutService interface {
	// Create opens a checkout for a new subscription (payment or setup mode).
	Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error)
	// Complete marks a checkout completed (idempotent). Setup-mode activation happens here;
	// payment-mode activation is driven by the invoice.paid hook.
	Complete(ctx context.Context, checkoutID string) error
	// Get returns a checkout by ID.
	Get(ctx context.Context, id string) (*dto.CheckoutResponse, error)
}

type checkoutService struct {
	ServiceParams
	// providerFn resolves the checkout provider; overridable in tests.
	providerFn func(ctx context.Context, provider types.CheckoutProvider) (checkout.CheckoutProvider, error)
}

// NewCheckoutService creates a new checkout service.
func NewCheckoutService(params ServiceParams) CheckoutService {
	s := &checkoutService{ServiceParams: params}
	s.providerFn = func(ctx context.Context, provider types.CheckoutProvider) (checkout.CheckoutProvider, error) {
		return s.IntegrationFactory.GetCheckoutProvider(ctx, string(provider),
			NewCustomerService(s.ServiceParams), NewInvoiceService(s.ServiceParams))
	}
	return s
}

// resolveCheckoutProvider picks the active, checkout-capable payment connection for
// the request's tenant+environment.
func (s *checkoutService) resolveCheckoutProvider(ctx context.Context) (types.CheckoutProvider, error) {
	conns, err := s.ConnectionRepo.List(ctx, types.NewConnectionFilter())
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		if s.IntegrationFactory.IsCheckoutSupported(c.ProviderType) {
			return types.CheckoutProvider(c.ProviderType), nil
		}
	}
	return "", ierr.NewError("no active payment connection found for checkout").
		WithHint("Connect a checkout-capable payment provider (e.g. Stripe) for this environment before opening a checkout").
		Mark(ierr.ErrNotFound)
}

func (s *checkoutService) Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	switch req.CheckoutAction {
	case types.CheckoutActionSubscriptionCreation:
		switch req.Mode {
		case types.CheckoutModePayment:
			return s.createPayment(ctx, req)
		case types.CheckoutModeSetup:
			return s.createSetup(ctx, req)
		}
		return nil, ierr.NewError("unsupported checkout mode").
			WithHint("Mode must be 'payment' or 'setup'").
			WithReportableDetails(map[string]any{"mode": req.Mode}).
			Mark(ierr.ErrValidation)
	default:
		return nil, ierr.NewError("unsupported checkout action").
			WithHint("checkout_action must be subscription_creation").
			WithReportableDetails(map[string]any{"checkout_action": req.CheckoutAction}).
			Mark(ierr.ErrValidation)
	}
}

func (s *checkoutService) createPayment(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	subReq := *req.SubscriptionCreationParams
	subReq.CollectionMethod = lo.ToPtr(types.CollectionMethodSendInvoice)
	subReq.PaymentBehavior = lo.ToPtr(types.PaymentBehaviorDefaultIncomplete)

	provider, err := s.resolveCheckoutProvider(ctx)
	if err != nil {
		return nil, err
	}

	var chk *checkout.Checkout
	var invoiceID, paymentID string
	var customerID string
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		subSvc := NewSubscriptionService(s.ServiceParams)
		subResp, err := subSvc.CreateSubscription(txCtx, subReq)
		if err != nil {
			return err
		}
		customerID = subResp.Subscription.CustomerID

		if subResp.LatestInvoice == nil {
			return ierr.NewError("subscription has no opening invoice").
				WithHint("Cannot open a payment checkout without an opening invoice").
				WithReportableDetails(map[string]any{"subscription_id": subResp.Subscription.ID}).
				Mark(ierr.ErrInvalidOperation)
		}
		inv := subResp.LatestInvoice
		invoiceID = inv.ID

		paymentID, err = s.createInvoicePaymentRecord(txCtx, inv, provider, subReq.Currency, req.Metadata)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		chk = &checkout.Checkout{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:     customerID,
			EntityType:     types.CheckoutEntityTypeSubscription,
			EntityID:       subResp.Subscription.ID,
			CheckoutAction: types.CheckoutActionSubscriptionCreation,
			Mode:           types.CheckoutModePayment,
			Status:         types.CheckoutStatusPending,
			Amount:         &inv.AmountDue,
			Currency:       subReq.Currency,
			Provider:       provider,
			SuccessURL:     lo.ToPtr(req.SuccessURL),
			CancelURL:      lo.ToPtr(req.CancelURL),
			ExpiresAt:      now.Add(24 * time.Hour),
			EnvironmentID:  types.GetEnvironmentID(txCtx),
			BaseModel:      types.GetDefaultBaseModel(txCtx),
		}
		return s.CheckoutRepo.Create(txCtx, chk)
	})
	if err != nil {
		return nil, err
	}

	return s.openSessionAndRespond(ctx, chk, checkout.CheckoutSessionRequest{
		Objective:  types.CheckoutModePayment,
		CheckoutID: chk.ID,
		CustomerID: customerID,
		InvoiceID:  invoiceID,
		PaymentID:  paymentID,
		Amount:     lo.FromPtr(chk.Amount),
		Currency:   subReq.Currency,
		SaveCard:   req.SaveCard,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
	})
}

func (s *checkoutService) createSetup(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	subReq := *req.SubscriptionCreationParams
	subReq.SubscriptionStatus = types.SubscriptionStatusDraft
	subReq.CollectionMethod = lo.ToPtr(types.CollectionMethodChargeAutomatically)
	subReq.PaymentBehavior = lo.ToPtr(types.PaymentBehaviorAllowIncomplete)

	provider, err := s.resolveCheckoutProvider(ctx)
	if err != nil {
		return nil, err
	}

	var chk *checkout.Checkout
	var customerID string
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		subSvc := NewSubscriptionService(s.ServiceParams)
		subResp, err := subSvc.CreateSubscription(txCtx, subReq)
		if err != nil {
			return err
		}
		customerID = subResp.Subscription.CustomerID

		now := time.Now().UTC()
		chk = &checkout.Checkout{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:     customerID,
			EntityType:     types.CheckoutEntityTypeSubscription,
			EntityID:       subResp.Subscription.ID,
			CheckoutAction: types.CheckoutActionSubscriptionCreation,
			Mode:           types.CheckoutModeSetup,
			Status:         types.CheckoutStatusPending,
			Currency:       subReq.Currency,
			Provider:       provider,
			SuccessURL:     lo.ToPtr(req.SuccessURL),
			CancelURL:      lo.ToPtr(req.CancelURL),
			ExpiresAt:      now.Add(24 * time.Hour),
			EnvironmentID:  types.GetEnvironmentID(txCtx),
			BaseModel:      types.GetDefaultBaseModel(txCtx),
		}
		return s.CheckoutRepo.Create(txCtx, chk)
	})
	if err != nil {
		return nil, err
	}

	return s.openSessionAndRespond(ctx, chk, checkout.CheckoutSessionRequest{
		Objective:  types.CheckoutModeSetup,
		CheckoutID: chk.ID,
		CustomerID: customerID,
		Currency:   subReq.Currency,
		SaveCard:   true,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
	})
}

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
	return dto.CheckoutResponseFromDomain(chk), nil
}

// createInvoicePaymentRecord creates an unprocessed payment-link payment record
// bound to the given invoice, returning the new payment's ID.
func (s *checkoutService) createInvoicePaymentRecord(ctx context.Context, inv *dto.InvoiceResponse, provider types.CheckoutProvider, currency string, metadata map[string]string) (string, error) {
	var gateway types.PaymentGatewayType
	switch provider {
	case types.CheckoutProviderStripe:
		gateway = types.PaymentGatewayTypeStripe
	default:
		return "", ierr.NewError("unsupported checkout provider for payment record").
			WithReportableDetails(map[string]any{"provider": provider}).
			Mark(ierr.ErrValidation)
	}

	paymentSvc := NewPaymentService(s.ServiceParams)
	pay, err := paymentSvc.CreatePayment(ctx, &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(gateway),
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

func (s *checkoutService) failCheckout(ctx context.Context, chk *checkout.Checkout, cause error) {
	chk.Status = types.CheckoutStatusFailed
	msg := cause.Error()
	chk.FailureMessage = &msg
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

	if !chk.IsPending() {
		return nil
	}

	now := time.Now().UTC()

	if chk.Mode == types.CheckoutModeSetup &&
		chk.EntityType == types.CheckoutEntityTypeSubscription {
		subSvc := NewSubscriptionService(s.ServiceParams)
		if _, err := subSvc.ActivateDraftSubscription(ctx, chk.EntityID,
			dto.ActivateDraftSubscriptionRequest{StartDate: &now}); err != nil {
			sub, getErr := s.SubRepo.Get(ctx, chk.EntityID)
			if getErr != nil || sub.SubscriptionStatus == types.SubscriptionStatusDraft {
				return err
			}
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
	return dto.CheckoutResponseFromDomain(chk), nil
}
