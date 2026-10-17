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
		if err := ctx.Err(); err != nil {
			return err
		}
		if t.Status != types.StatusPublished || s.Config.Billing.IsBillingTenant(t.ID) {
			continue
		}
		tenantCtx := types.SetEnvironmentID(types.SetTenantID(ctx, t.ID), "")
		if err := s.syncTenant(tenantCtx, t.ID); err != nil {
			s.Logger.Error(ctx, "self-billing status sync failed", "error", err, "tenant_id", t.ID)
		}
	}
	return nil
}

// syncTenant re-reads the tenant so a status changed since the listing is not overwritten.
func (s *selfBillingService) syncTenant(ctx context.Context, tenantID string) error {
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return err
	}
	if t.Status != types.StatusPublished {
		return nil
	}
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
