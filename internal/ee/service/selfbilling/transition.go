package selfbilling

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// targetStatus is the status a tenant should have for its Flexprice Cloud subscription.
// A paid subscription activates the tenant, a trial that ended unpaid or was cancelled suspends it,
// and an active tenant is never suspended automatically.
func targetStatus(current types.TenantInternalStatus, sub *remoteSubscription) types.TenantInternalStatus {
	if sub == nil {
		return current
	}

	switch sub.status {
	case types.SubscriptionStatusActive:
		return types.TenantInternalStatusActive

	case types.SubscriptionStatusIncomplete, types.SubscriptionStatusCancelled:
		if current == types.TenantInternalStatusTrialing {
			return types.TenantInternalStatusSuspended
		}
	}
	return current
}

// updateTenantStatus moves the tenant to its target status; an active tenant always gets a production environment. Safe to repeat.
func (s *selfBillingService) updateTenantStatus(ctx context.Context, t *tenant.Tenant, sub *remoteSubscription) (types.TenantInternalStatus, error) {
	ctx = types.SetTenantID(ctx, t.ID)

	next := targetStatus(t.InternalStatus, sub)
	if next != t.InternalStatus {
		updated := tenant.NewBuilder(t).WithInternalStatus(next).Build()
		if err := s.TenantRepo.Update(ctx, updated); err != nil {
			return "", err
		}
		s.Logger.Info(ctx, "self-billing tenant status updated", "tenant_id", t.ID, "from", t.InternalStatus, "to", next)

		event := alertTenantSuspended
		if next == types.TenantInternalStatusActive {
			event = alertTenantActivated
		}
		s.notify(ctx, alert{
			Event:      event,
			TenantID:   t.ID,
			TenantName: t.Name,
			FromStatus: t.InternalStatus,
			ToStatus:   next,
		})
	}

	if next != types.TenantInternalStatusActive {
		return next, nil
	}

	if err := s.raiseProductionLimit(ctx); err != nil {
		return "", err
	}
	if err := s.ensureProductionEnv(ctx); err != nil {
		return "", err
	}
	return next, nil
}

// raiseProductionLimit allows at least one production environment and keeps a higher limit.
func (s *selfBillingService) raiseProductionLimit(ctx context.Context) error {
	setting, err := s.settings.GetSettingByKeyUnchecked(ctx, types.SettingKeyTenantConfig)
	if err != nil {
		return err
	}

	cfg, err := utils.ToStruct[types.TenantConfig](setting.Value)
	if err == nil && cfg.Production >= 1 {
		return nil
	}

	_, err = s.settings.UpdateSettingByKey(ctx, types.SettingKeyTenantConfig, &dto.UpdateSettingRequest{
		Value: map[string]interface{}{"production": 1},
	})
	return err
}

func (s *selfBillingService) ensureProductionEnv(ctx context.Context) error {
	n, err := s.EnvironmentRepo.CountByType(ctx, types.EnvironmentProduction)
	if err != nil || n > 0 {
		return err
	}

	_, err = s.envs.CreateEnvironment(ctx, dto.CreateEnvironmentRequest{
		Name: types.EnvironmentProduction.DisplayTitle(),
		Type: string(types.EnvironmentProduction),
	})
	if err == nil {
		return nil
	}

	// A concurrent activation may have created it first, so the limit refused ours.
	if n, cerr := s.EnvironmentRepo.CountByType(ctx, types.EnvironmentProduction); cerr == nil && n > 0 {
		return nil
	}
	return err
}
