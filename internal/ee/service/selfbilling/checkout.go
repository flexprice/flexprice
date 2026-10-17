package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// statusPayInPortal means the trial already ended unpaid; the open invoice is paid in the customer portal.
const statusPayInPortal = "pay_in_portal"

func (s *selfBillingService) StartCheckout(ctx context.Context, tenantID string, req dto.SelfBillingCheckoutRequest) (*dto.SelfBillingCheckoutResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ext := externalID(s.region(), tenantID)
	if err := s.provision(ctx, tenantID, t.Name, req.Email); err != nil {
		return nil, err
	}
	if err := s.client.UpdateBillingDetails(ctx, ext, toBillingDetails(req)); err != nil {
		return nil, err
	}
	sub, err := s.client.LatestSubscription(ctx, ext)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ierr.NewError("billing subscription not found").Mark(ierr.ErrNotFound)
	}

	switch sub.status {
	case "active":
		return s.completedCheckout(ctx, tenantID, "")
	case "incomplete":
		p, err := s.client.PortalSession(ctx, ext)
		if err != nil {
			return nil, err
		}
		return &dto.SelfBillingCheckoutResponse{Status: statusPayInPortal, URL: p.url, Tenant: t.InternalStatus}, nil
	case "trialing":
	default:
		return nil, ierr.NewErrorf("billing subscription %s is %s", sub.id, sub.status).
			WithHint("Your billing subscription cannot be paid for here; contact support").
			WithReportableDetails(map[string]any{"subscription_id": sub.id, "status": sub.status}).
			Mark(ierr.ErrInvalidOperation)
	}

	co, err := s.client.StartTrialEndCheckout(ctx, checkoutInput{
		extID:          ext,
		subscriptionID: sub.id,
		successURL:     s.Config.Billing.DashboardURL + "/billing/checkout/success",
		failureURL:     s.Config.Billing.DashboardURL + "/billing/checkout/failure",
	})
	if isConflict(err) {
		return s.checkoutInProgress(ctx, t, sub.id, err)
	}
	if err != nil {
		return nil, err
	}
	if co.status == string(types.CheckoutStatusCompleted) {
		// $0 first period: Mumbai already ended the trial, so there is nothing to pay.
		return s.completedCheckout(ctx, tenantID, co.id)
	}
	s.Logger.Info(ctx, "self-billing checkout started", "tenant_id", tenantID, "checkout_id", co.id, "subscription_id", sub.id)
	return &dto.SelfBillingCheckoutResponse{CheckoutID: co.id, URL: co.url, Status: co.status, Terminal: co.terminal, Tenant: t.InternalStatus}, nil
}

func (s *selfBillingService) completedCheckout(ctx context.Context, tenantID, checkoutID string) (*dto.SelfBillingCheckoutResponse, error) {
	if err := s.activate(ctx, tenantID); err != nil {
		return nil, err
	}
	return &dto.SelfBillingCheckoutResponse{CheckoutID: checkoutID, Status: string(types.CheckoutStatusCompleted), Terminal: true, Tenant: types.TenantInternalStatusActive}, nil
}

// checkoutInProgress resumes the tenant's open trial-end checkout, or reports a 409 when it cannot be found.
func (s *selfBillingService) checkoutInProgress(ctx context.Context, t *tenant.Tenant, subscriptionID string, cause error) (*dto.SelfBillingCheckoutResponse, error) {
	if id := conflictCheckoutID(cause); id != "" {
		if co, err := s.ownedCheckout(ctx, t.ID, id); err == nil && !co.terminal && co.url != "" {
			return &dto.SelfBillingCheckoutResponse{CheckoutID: co.id, URL: co.url, Status: co.status, Terminal: co.terminal, Tenant: t.InternalStatus}, nil
		}
	}
	return nil, ierr.WithError(cause).
		WithMessage("billing checkout already in progress").
		WithHint("a checkout is already in progress; finish or cancel it").
		WithReportableDetails(map[string]any{"subscription_id": subscriptionID}).
		Mark(ierr.ErrAlreadyExists)
}

func (s *selfBillingService) PollCheckout(ctx context.Context, tenantID, checkoutID string) (*dto.SelfBillingCheckoutResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	co, err := s.ownedCheckout(ctx, tenantID, checkoutID)
	if err != nil {
		return nil, err
	}
	if co.status == string(types.CheckoutStatusCompleted) {
		if err := s.activate(ctx, tenantID); err != nil {
			return nil, err
		}
	}
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &dto.SelfBillingCheckoutResponse{CheckoutID: co.id, Status: co.status, Terminal: co.terminal, Tenant: t.InternalStatus}, nil
}

func (s *selfBillingService) CancelCheckout(ctx context.Context, tenantID, checkoutID string) error {
	if !s.Enabled() {
		return errDisabled
	}
	if _, err := s.ownedCheckout(ctx, tenantID, checkoutID); err != nil {
		return err
	}
	return s.client.CancelCheckout(ctx, checkoutID)
}

// ownedCheckout loads a session and refuses one that belongs to another tenant.
func (s *selfBillingService) ownedCheckout(ctx context.Context, tenantID, checkoutID string) (*remoteCheckout, error) {
	co, err := s.client.GetCheckout(ctx, checkoutID)
	if err != nil {
		return nil, err
	}
	if co.extID != externalID(s.region(), tenantID) {
		return nil, ierr.NewError("checkout session not found").
			WithHint("Checkout session not found").
			Mark(ierr.ErrNotFound)
	}
	return co, nil
}

// activate marks the tenant active, allows one production environment and creates it. Safe to repeat.
func (s *selfBillingService) activate(ctx context.Context, tenantID string) error {
	ctx = types.SetTenantID(ctx, tenantID)
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if t.InternalStatus != types.TenantInternalStatusActive {
		if err := s.TenantRepo.Update(ctx, tenant.NewBuilder(t).WithInternalStatus(types.TenantInternalStatusActive).Build()); err != nil {
			return err
		}
		s.Logger.Info(ctx, "self-billing tenant activated", "tenant_id", tenantID, "from", t.InternalStatus)
	}
	if err := s.raiseProductionLimit(ctx); err != nil {
		return err
	}
	return s.ensureProductionEnv(ctx)
}

func (s *selfBillingService) raiseProductionLimit(ctx context.Context) error {
	setting, err := s.settings.GetSettingByKeyUnchecked(ctx, types.SettingKeyTenantConfig)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if setting != nil {
		if cfg, err := utils.ToStruct[types.TenantConfig](setting.Value); err == nil && cfg.Production >= 1 {
			return nil
		}
	}
	_, err = s.settings.UpdateSettingByKey(ctx, types.SettingKeyTenantConfig, &dto.UpdateSettingRequest{Value: map[string]interface{}{"production": 1}})
	return err
}

func (s *selfBillingService) ensureProductionEnv(ctx context.Context) error {
	n, err := s.EnvironmentRepo.CountByType(ctx, types.EnvironmentProduction)
	if err != nil || n > 0 {
		return err
	}
	_, err = s.envs.CreateEnvironment(ctx, dto.CreateEnvironmentRequest{Name: types.EnvironmentProduction.DisplayTitle(), Type: string(types.EnvironmentProduction)})
	if err == nil {
		return nil
	}
	// A concurrent activation may have created it first; the limit then refuses ours.
	if n, cerr := s.EnvironmentRepo.CountByType(ctx, types.EnvironmentProduction); cerr == nil && n > 0 {
		return nil
	}
	return err
}

func toBillingDetails(r dto.SelfBillingCheckoutRequest) billingDetails {
	return billingDetails{name: r.Name, email: r.Email, line1: r.Line1, line2: r.Line2, city: r.City, state: r.State, country: r.Country, postalCode: r.PostalCode, taxID: r.TaxID}
}
