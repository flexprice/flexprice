package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CheckoutSessionService interface {
	Create(ctx context.Context, req dto.CreateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error)
	Get(ctx context.Context, id string) (*dto.CheckoutSessionResponse, error)
	List(ctx context.Context, filter *types.CheckoutSessionFilter) (*dto.ListCheckoutSessionsResponse, error)
	Delete(ctx context.Context, id string) error
	// CleanupCheckoutSession archives all entities created during fulfillment (subscription,
	// invoice, payment) and marks the session as failed with the given reason.
	// Pass reason=nil when cleaning up without an error (e.g. session expiry).
	CleanupCheckoutSession(ctx context.Context, session *domainCheckout.CheckoutSession, reason error) error
}

type checkoutSessionService struct {
	ServiceParams
}

func NewCheckoutSessionService(params ServiceParams) CheckoutSessionService {
	return &checkoutSessionService{ServiceParams: params}
}

func (s *checkoutSessionService) Create(ctx context.Context, req dto.CreateCheckoutSessionRequest) (*dto.CheckoutSessionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	session := req.ToCheckoutSession(ctx)

	if err := s.CheckoutSessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	if err := s.executeCheckoutAction(ctx, session); err != nil {
		// Best-effort cleanup: archive entities + mark session failed.
		// Log cleanup errors but return the original fulfillment error.
		if cleanupErr := s.CleanupCheckoutSession(ctx, session, err); cleanupErr != nil {
			s.Logger.Error(ctx, "checkout cleanup failed after fulfillment error",
				"session_id", session.ID,
				"cleanup_err", cleanupErr,
				"original_err", err,
			)
		}
		return nil, err
	}

	return dto.ToCheckoutSessionResponse(session), nil
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

	return s.CheckoutSessionRepo.Delete(ctx, id)
}

func (s *checkoutSessionService) CleanupCheckoutSession(ctx context.Context, session *domainCheckout.CheckoutSession, reason error) error {
	// Archive all entities created during fulfillment.
	if session.Result != nil && session.Result.CreateSubscriptionResult != nil {
		res := session.Result.CreateSubscriptionResult
		if res.PaymentID != "" {
			if err := s.PaymentRepo.Delete(ctx, res.PaymentID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout payment", "payment_id", res.PaymentID, "err", err)
			}
		}
		if res.InvoiceID != "" {
			if err := s.InvoiceRepo.Delete(ctx, res.InvoiceID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout invoice", "invoice_id", res.InvoiceID, "err", err)
			}
		}
		if res.SubscriptionID != "" {
			if err := s.SubRepo.Delete(ctx, res.SubscriptionID); err != nil {
				s.Logger.Error(ctx, "failed to archive checkout subscription", "subscription_id", res.SubscriptionID, "err", err)
			}
		}
	}

	// Mark session failed.
	session.CheckoutStatus = types.CheckoutStatusFailed
	if reason != nil {
		errMsg := reason.Error()
		session.FailureReason = &errMsg
	}
	return s.CheckoutSessionRepo.Update(ctx, session)
}

func (s *checkoutSessionService) executeCheckoutAction(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	switch session.Action {
	case types.CheckoutActionCreateSubscription:
		subResp, invResp, err := s.createDraftSubscriptionWithInvoice(ctx, session)
		if err != nil {
			return err
		}

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

	default:
		return ierr.NewError("unsupported checkout action").
			WithHint("No fulfillment handler for this action type").
			WithReportableDetails(map[string]any{"action": session.Action}).
			Mark(ierr.ErrValidation)
	}

	return s.CheckoutSessionRepo.Update(ctx, session)
}

func (s *checkoutSessionService) createDraftSubscriptionWithInvoice(ctx context.Context, session *domainCheckout.CheckoutSession) (*dto.SubscriptionResponse, *dto.InvoiceResponse, error) {
	params := session.Configuration.CreateSubscriptionParams
	if params == nil {
		return nil, nil, ierr.NewError("create_subscription_params is required for create_subscription action").
			Mark(ierr.ErrValidation)
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

	subSvc := NewSubscriptionService(s.ServiceParams)
	subResp, err := subSvc.CreateSubscription(ctx, subReq)
	if err != nil {
		return nil, nil, err
	}

	invoiceReq := &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: subResp.ID,
		PeriodStart:    subResp.CurrentPeriodStart,
		PeriodEnd:      subResp.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodStart,
	}

	// Pass default_incomplete behavior so ProcessDraftInvoice does not attempt to charge.
	// The checkout payment step handles collection separately.
	paymentParams := dto.NewPaymentParameters(
		types.CollectionMethodChargeAutomatically,
		types.PaymentBehaviorDefaultIncomplete,
		nil,
	).NormalizePaymentParameters()

	invSvc := NewInvoiceService(s.ServiceParams)
	invResp, _, err := invSvc.CreateSubscriptionInvoice(
		ctx, invoiceReq, paymentParams,
		types.InvoiceFlowSubscriptionCreation,
		true, // isDraftSubscription — suppresses finalization, gateway, webhooks
	)
	if err != nil {
		return nil, nil, err
	}
	if invResp == nil {
		return nil, nil, ierr.NewError("checkout requires a non-zero invoice; plan produced no charges").
			Mark(ierr.ErrValidation)
	}

	return subResp, invResp, nil
}

func (s *checkoutSessionService) createCheckoutPayment(ctx context.Context, inv *invoice.Invoice, provider types.CheckoutPaymentProvider) (*dto.PaymentResponse, error) {
	var gateway types.PaymentGatewayType
	switch provider {
	case types.CheckoutPaymentProviderStripe:
		gateway = types.PaymentGatewayTypeStripe
	default:
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

// ensure interface compliance at compile time
var _ CheckoutSessionService = (*checkoutSessionService)(nil)
