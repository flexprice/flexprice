package selfbilling

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/types"
)

// syncStatuses aligns every tenant's status with its Flexprice Cloud subscription; one tenant's failure never stops the rest.
func (s *selfBillingService) syncStatuses(ctx context.Context) error {
	tenants, err := s.TenantRepo.List(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, t := range tenants {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}

		if t.Status != types.StatusPublished || s.Config.Billing.IsBillingTenant(t.ID) {
			continue
		}

		tenantCtx := types.SetEnvironmentID(types.SetTenantID(ctx, t.ID), "")
		if err := s.syncTenant(tenantCtx, t.ID); err != nil {
			s.Logger.Error(tenantCtx, "self-billing status sync failed", "error", err, "tenant_id", t.ID)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// syncTenant reads Flexprice Cloud before the tenant row, so a checkout that lands meanwhile is not overwritten with stale state.
func (s *selfBillingService) syncTenant(ctx context.Context, tenantID string) error {
	remote, err := s.client.LatestSubscription(ctx, s.extID(tenantID))
	if err != nil {
		return err
	}

	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return err
	}

	// A trialing tenant without a subscription missed provisioning at signup, so retry it.
	if remote == nil && t.InternalStatus == types.TenantInternalStatusTrialing {
		return s.provision(ctx, t.ID, t.Name, t.BillingDetails.Email)
	}

	_, err = s.updateTenantStatus(ctx, t, remote)
	return err
}
