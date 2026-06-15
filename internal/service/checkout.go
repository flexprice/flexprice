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
	// Create opens a checkout for a new subscription (payment objective in v1).
	Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error)
	// Complete marks a checkout completed (idempotent). Subscription activation is
	// driven by the existing payment-completion hook, not here.
	Complete(ctx context.Context, checkoutID string) error
}

type checkoutService struct {
	ServiceParams
}

// NewCheckoutService creates a new checkout service.
func NewCheckoutService(params ServiceParams) CheckoutService {
	return &checkoutService{ServiceParams: params}
}

func (s *checkoutService) Create(ctx context.Context, req dto.CreateCheckoutRequest) (*dto.CheckoutResponse, error) {
	if req.Objective != types.CheckoutObjectivePayment {
		return nil, ierr.NewError("unsupported checkout objective").
			WithHint("Only the 'payment' objective is supported in v1").
			WithReportableDetails(map[string]any{"objective": req.Objective}).
			Mark(ierr.ErrValidation)
	}

	billingPeriodCount := req.BillingPeriodCount
	if billingPeriodCount == 0 {
		billingPeriodCount = 1
	}

	var resp *dto.CheckoutResponse
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
		amount := inv.AmountDue

		// 3. Create the (unprocessed) payment record bound to the invoice.
		paymentSvc := NewPaymentService(s.ServiceParams)
		pay, err := paymentSvc.CreatePayment(txCtx, &dto.CreatePaymentRequest{
			DestinationType:   types.PaymentDestinationTypeInvoice,
			DestinationID:     inv.ID,
			PaymentMethodType: types.PaymentMethodTypePaymentLink,
			Amount:            amount,
			Currency:          req.Currency,
			Metadata:          types.Metadata(req.Metadata),
			ProcessPayment:    false,
		})
		if err != nil {
			return err
		}

		// 4. Build the checkout aggregate.
		now := time.Now().UTC()
		chk := &checkout.Checkout{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT),
			CustomerID:    req.CustomerID,
			EntityType:    types.CheckoutEntityTypeSubscription,
			EntityID:      subResp.Subscription.ID,
			CheckoutType:  types.CheckoutTypeSubscriptionCreation,
			Objective:     types.CheckoutObjectivePayment,
			Status:        types.CheckoutStatusPending,
			Amount:        amount,
			Currency:      req.Currency,
			Provider:      string(types.SecretProviderStripe),
			SuccessURL:    lo.ToPtr(req.SuccessURL),
			CancelURL:     lo.ToPtr(req.CancelURL),
			ExpiresAt:     now.Add(24 * time.Hour),
			EnvironmentID: types.GetEnvironmentID(txCtx),
			BaseModel:     types.GetDefaultBaseModel(txCtx),
		}

		// 5. Resolve the provider and open the hosted session.
		provider, err := s.IntegrationFactory.GetCheckoutProvider(
			txCtx,
			chk.Provider,
			NewCustomerService(s.ServiceParams),
			NewInvoiceService(s.ServiceParams),
		)
		if err != nil {
			return err
		}

		sess, err := provider.CreateCheckoutSession(txCtx, checkout.CheckoutSessionRequest{
			Objective:  types.CheckoutObjectivePayment,
			CheckoutID: chk.ID,
			CustomerID: req.CustomerID,
			InvoiceID:  inv.ID,
			PaymentID:  pay.ID,
			Amount:     amount,
			Currency:   req.Currency,
			SaveCard:   req.SaveCard,
			SuccessURL: req.SuccessURL,
			CancelURL:  req.CancelURL,
			Metadata:   req.Metadata,
		})
		if err != nil {
			return err
		}

		// 6. Persist the checkout with the provider session details.
		chk.ProviderSessionID = &sess.SessionID
		chk.CheckoutURL = &sess.URL
		if err := s.CheckoutRepo.Create(txCtx, chk); err != nil {
			return err
		}

		resp = &dto.CheckoutResponse{
			ID:          chk.ID,
			Status:      string(chk.Status),
			CheckoutURL: sess.URL,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
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
	chk.Status = types.CheckoutStatusCompleted
	chk.CompletedAt = &now
	return s.CheckoutRepo.Update(ctx, chk)
}
