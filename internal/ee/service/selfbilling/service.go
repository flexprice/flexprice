package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
)

var errDisabled = ierr.NewError("self-billing is not enabled").Mark(ierr.ErrNotFound)

// Service bills this region's tenants through the Flexprice billing tenant in Mumbai.
type Service interface {
	interfaces.SelfBillingHook
	Enabled() bool
	Status(ctx context.Context, tenantID string) (*dto.SelfBillingStatusResponse, error)
	StartCheckout(ctx context.Context, tenantID string, req dto.SelfBillingCheckoutRequest) (*dto.SelfBillingCheckoutResponse, error)
	PollCheckout(ctx context.Context, tenantID, checkoutID string) (*dto.SelfBillingCheckoutResponse, error)
	CancelCheckout(ctx context.Context, tenantID, checkoutID string) error
	Summary(ctx context.Context, tenantID string) (*dto.SelfBillingSummaryResponse, error)
	PortalSession(ctx context.Context, tenantID string) (*dto.SelfBillingPortalResponse, error)
	RunHourly(ctx context.Context) error
}

type selfBillingService struct {
	service.ServiceParams
	client   billingClient // nil when disabled
	envs     service.EnvironmentService
	settings service.SettingsService
	events   domainsb.EventCountRepository
	invoices domainsb.InvoiceValueRepository
}

// NewService builds the self-billing service; it is a no-op unless billing config enables self-billing.
func NewService(
	params service.ServiceParams,
	envs service.EnvironmentService,
	settings service.SettingsService,
	events domainsb.EventCountRepository,
	invoices domainsb.InvoiceValueRepository,
) Service {
	s := &selfBillingService{ServiceParams: params, envs: envs, settings: settings, events: events, invoices: invoices}
	if params.Config != nil && params.Config.Billing.IsSelfBillingEnabled() {
		s.client = newRemoteClient(params.Config.Billing)
	}
	return s
}

func (s *selfBillingService) Enabled() bool { return s.client != nil }

func (s *selfBillingService) region() string { return s.Config.Billing.Region }

func (s *selfBillingService) Status(ctx context.Context, tenantID string) (*dto.SelfBillingStatusResponse, error) {
	if !s.Enabled() {
		return &dto.SelfBillingStatusResponse{Enabled: false}, nil
	}
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &dto.SelfBillingStatusResponse{Enabled: true, Status: t.InternalStatus}, nil
}

func (s *selfBillingService) OnTenantCreated(ctx context.Context, tenantID, name, email string) {
	if !s.Enabled() || s.Config.Billing.IsBillingTenant(tenantID) {
		return
	}
	if err := s.provision(ctx, tenantID, name, email); err != nil {
		s.Logger.Error(ctx, "self-billing provisioning failed", "error", err, "tenant_id", tenantID)
	}
}

// provision ensures the billing customer and starts its trial when it has no self-serve subscription yet.
func (s *selfBillingService) provision(ctx context.Context, tenantID, name, email string) error {
	in := customerInput{extID: externalID(s.region(), tenantID), name: name, email: email, tenantID: tenantID}
	if s.Config.Billing.TenantID != "" {
		in.legacyExtID = tenantID // Mumbai customers created by the removed in-process path
	}
	if err := s.client.EnsureCustomer(ctx, in); err != nil {
		return err
	}
	sub, err := s.client.LatestSubscription(ctx, in.extID)
	if err != nil || sub != nil {
		return err
	}
	return s.client.StartTrial(ctx, in.extID)
}

var errNotImplemented = ierr.NewError("not implemented").Mark(ierr.ErrInternal)

func (s *selfBillingService) StartCheckout(context.Context, string, dto.SelfBillingCheckoutRequest) (*dto.SelfBillingCheckoutResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	return nil, errNotImplemented
}

func (s *selfBillingService) PollCheckout(context.Context, string, string) (*dto.SelfBillingCheckoutResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	return nil, errNotImplemented
}

func (s *selfBillingService) CancelCheckout(context.Context, string, string) error {
	if !s.Enabled() {
		return errDisabled
	}
	return errNotImplemented
}

func (s *selfBillingService) Summary(context.Context, string) (*dto.SelfBillingSummaryResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	return nil, errNotImplemented
}

func (s *selfBillingService) PortalSession(context.Context, string) (*dto.SelfBillingPortalResponse, error) {
	if !s.Enabled() {
		return nil, errDisabled
	}
	return nil, errNotImplemented
}

func (s *selfBillingService) RunHourly(context.Context) error { return nil }
