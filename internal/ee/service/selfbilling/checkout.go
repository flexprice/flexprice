package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	checkoutSuccessPath = "/billing/checkout/success"
	checkoutFailurePath = "/billing/checkout/failure"
)

// StartCheckout only checks out a trialing subscription; an active one activates and an incomplete one pays in the portal.
func (s *selfBillingService) StartCheckout(ctx context.Context, tenantID string, req dto.SelfBillingCheckoutRequest) (_ *dto.SelfBillingCheckoutResponse, err error) {
	defer func() { s.logRemoteFailure(ctx, "start_checkout", tenantID, err) }()

	if !s.enabled() {
		return nil, errDisabled
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	extID := s.extID(tenantID)
	if err := s.provision(ctx, tenantID, t.Name, req.Email); err != nil {
		return nil, err
	}

	if err := s.client.UpdateBillingDetails(ctx, extID, toBillingDetails(req)); err != nil {
		return nil, err
	}

	sub, err := s.client.LatestSubscription(ctx, extID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ierr.NewError("flexprice cloud subscription not found").
			WithHint("Your billing subscription could not be found; contact support").
			Mark(ierr.ErrNotFound)
	}

	switch sub.status {
	case types.SubscriptionStatusTrialing:
	case types.SubscriptionStatusActive:
		status, err := s.updateTenantStatus(ctx, t, sub)
		if err != nil {
			return nil, err
		}
		return &dto.SelfBillingCheckoutResponse{
			Status:   types.CheckoutStatusCompleted,
			Terminal: true,
			Tenant:   status,
		}, nil
	case types.SubscriptionStatusIncomplete:
		// The trial ended unpaid, so its invoice is paid from the customer portal.
		p, err := s.client.PortalSession(ctx, extID)
		if err != nil {
			return nil, err
		}
		return &dto.SelfBillingCheckoutResponse{
			URL:    p.url,
			Status: types.CheckoutStatusPending,
			Tenant: t.InternalStatus,
		}, nil
	default:
		return nil, ierr.NewErrorf("flexprice cloud subscription %s is %s", sub.id, sub.status).
			WithHint("Your billing subscription cannot be paid for here; contact support").
			WithReportableDetails(map[string]any{
				"subscription_id": sub.id,
				"status":          sub.status,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	co, err := s.client.StartTrialEndCheckout(ctx, checkoutInput{
		extID:          extID,
		subscriptionID: sub.id,
		successURL:     s.Config.Billing.DashboardURL + checkoutSuccessPath,
		failureURL:     s.Config.Billing.DashboardURL + checkoutFailurePath,
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "self-billing checkout started", "tenant_id", tenantID, "checkout_id", co.id, "subscription_id", sub.id)
	return s.checkoutResponse(ctx, t, co)
}

// PollCheckout reports a checkout's state and applies the Flexprice Cloud subscription once it completes.
func (s *selfBillingService) PollCheckout(ctx context.Context, tenantID, checkoutID string) (_ *dto.SelfBillingCheckoutResponse, err error) {
	defer func() { s.logRemoteFailure(ctx, "poll_checkout", tenantID, err) }()

	if !s.enabled() {
		return nil, errDisabled
	}

	co, err := s.getCheckout(ctx, tenantID, checkoutID)
	if err != nil {
		return nil, err
	}

	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	return s.checkoutResponse(ctx, t, co)
}

func (s *selfBillingService) CancelCheckout(ctx context.Context, tenantID, checkoutID string) (err error) {
	defer func() { s.logRemoteFailure(ctx, "cancel_checkout", tenantID, err) }()

	if !s.enabled() {
		return errDisabled
	}

	if _, err := s.getCheckout(ctx, tenantID, checkoutID); err != nil {
		return err
	}
	return s.client.CancelCheckout(ctx, checkoutID)
}

// getCheckout loads a checkout session and hides one that belongs to another tenant.
func (s *selfBillingService) getCheckout(ctx context.Context, tenantID, checkoutID string) (*remoteCheckout, error) {
	co, err := s.client.GetCheckout(ctx, checkoutID)
	if err != nil {
		return nil, err
	}

	if co.extID != s.extID(tenantID) {
		return nil, ierr.NewError("checkout session not found").
			WithHint("Checkout session not found").
			Mark(ierr.ErrNotFound)
	}
	return co, nil
}

// checkoutResponse applies the Flexprice Cloud subscription to the tenant once the checkout has completed.
func (s *selfBillingService) checkoutResponse(ctx context.Context, t *tenant.Tenant, co *remoteCheckout) (*dto.SelfBillingCheckoutResponse, error) {
	status := t.InternalStatus
	if co.status == types.CheckoutStatusCompleted {
		sub, err := s.client.LatestSubscription(ctx, s.extID(t.ID))
		if err != nil {
			return nil, err
		}

		status, err = s.updateTenantStatus(ctx, t, sub)
		if err != nil {
			return nil, err
		}
	}

	return &dto.SelfBillingCheckoutResponse{
		CheckoutID: co.id,
		URL:        co.url,
		Status:     co.status,
		Terminal:   co.terminal,
		Tenant:     status,
	}, nil
}

func toBillingDetails(req dto.SelfBillingCheckoutRequest) billingDetails {
	return billingDetails{
		name:       req.Name,
		email:      req.Email,
		line1:      req.Line1,
		line2:      req.Line2,
		city:       req.City,
		state:      req.State,
		country:    req.Country,
		postalCode: req.PostalCode,
		taxID:      req.TaxID,
	}
}
