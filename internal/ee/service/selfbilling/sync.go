package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
)

// syncStatuses aligns every tenant's status with its Mumbai subscription and provisions missing customers.
func (s *selfBillingService) syncStatuses(ctx context.Context) error {
	tenants, err := s.TenantRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, t := range tenants {
		if s.Config.Billing.IsBillingTenant(t.ID) {
			continue
		}
		if err := s.syncTenant(types.SetTenantID(ctx, t.ID), t); err != nil {
			s.Logger.Error(ctx, "self-billing status sync failed", "error", err, "tenant_id", t.ID)
		}
	}
	return nil
}

func (s *selfBillingService) syncTenant(ctx context.Context, t *tenant.Tenant) error {
	remote, err := s.client.LatestSubscription(ctx, externalID(s.region(), t.ID))
	if err != nil {
		return err
	}
	d := decide(t.InternalStatus, remote)
	switch {
	case d.provision:
		return s.provision(ctx, t.ID, t.Name, t.BillingDetails.Email)
	case !d.changed:
		return nil
	case d.next == types.TenantInternalStatusActive:
		return s.activate(ctx, t.ID)
	}
	if err := s.TenantRepo.Update(ctx, tenant.NewBuilder(t).WithInternalStatus(d.next).Build()); err != nil {
		return err
	}
	s.Logger.Info(ctx, "self-billing tenant status changed", "tenant_id", t.ID, "from", t.InternalStatus, "to", d.next)
	return nil
}
