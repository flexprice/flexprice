package selfbilling

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
)

// provisionTimeout bounds the detached provisioning of a new tenant on the billing tenant.
const provisionTimeout = 2 * time.Minute

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
	spawn    func(run func()) // runs detached work; nil means a goroutine
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

// OnTenantCreated provisions the tenant in the background so signup never waits on the billing tenant; the hourly sync repairs failures.
func (s *selfBillingService) OnTenantCreated(ctx context.Context, tenantID, name, email string) {
	if !s.Enabled() || s.Config.Billing.IsBillingTenant(tenantID) {
		return
	}
	detached := context.WithoutCancel(ctx)
	s.detach(func() {
		ctx, cancel := context.WithTimeout(detached, provisionTimeout)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Error(ctx, "self-billing provisioning panicked", "error", fmt.Errorf("panic: %v", r), "tenant_id", tenantID)
			}
		}()
		if err := s.provision(ctx, tenantID, name, email); err != nil {
			s.Logger.Error(ctx, "self-billing provisioning failed", "error", err, "tenant_id", tenantID)
		}
	})
}

func (s *selfBillingService) detach(run func()) {
	if s.spawn != nil {
		s.spawn(run)
		return
	}
	go run()
}

// logRemoteFailure logs a billing tenant failure once, at the handler-facing method that returns it.
func (s *selfBillingService) logRemoteFailure(ctx context.Context, op, tenantID string, err error) {
	if isRemoteFailure(err) {
		s.Logger.Error(ctx, "self-billing call to the billing tenant failed", "error", err, "operation", op, "tenant_id", tenantID)
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
