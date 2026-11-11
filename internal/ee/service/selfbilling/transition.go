package selfbilling

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// trialExpiryGrace allows for core's trial-end cron, which converts a trial within minutes. Still
// trialing after that means conversion is failing, e.g. no billing address for tax.
const trialExpiryGrace = time.Hour

// suspensionReason says why a tenant was suspended; it is sent with the tenant_suspended alert.
type suspensionReason string

const (
	suspensionReasonTrialExpired          suspensionReason = "trial_expired"
	suspensionReasonPaymentIncomplete     suspensionReason = "payment_incomplete"
	suspensionReasonSubscriptionCancelled suspensionReason = "subscription_cancelled"
)

// targetStatus is the status a tenant should have for its Flexprice Cloud subscription, and the
// reason when that is a suspension. A paid subscription activates the tenant; a trial that ended
// unpaid, was cancelled or never converted suspends it; an active tenant is never suspended.
func targetStatus(current types.TenantInternalStatus, sub *remoteSubscription, now time.Time) (types.TenantInternalStatus, suspensionReason) {
	if sub == nil {
		return current, ""
	}

	switch sub.status {
	case types.SubscriptionStatusActive:
		return types.TenantInternalStatusActive, ""

	case types.SubscriptionStatusIncomplete:
		if current == types.TenantInternalStatusTrialing {
			return types.TenantInternalStatusSuspended, suspensionReasonPaymentIncomplete
		}

	case types.SubscriptionStatusCancelled:
		if current == types.TenantInternalStatusTrialing {
			return types.TenantInternalStatusSuspended, suspensionReasonSubscriptionCancelled
		}

	case types.SubscriptionStatusTrialing:
		expired := sub.trialEnd != nil && now.After(sub.trialEnd.Add(trialExpiryGrace))
		if current == types.TenantInternalStatusTrialing && expired {
			return types.TenantInternalStatusSuspended, suspensionReasonTrialExpired
		}
	}
	return current, ""
}

// updateTenantStatus moves the tenant to its target status; an active tenant always gets a production environment. Safe to repeat.
func (s *selfBillingService) updateTenantStatus(ctx context.Context, t *tenant.Tenant, sub *remoteSubscription) (types.TenantInternalStatus, error) {
	ctx = types.SetTenantID(ctx, t.ID)

	next, reason := targetStatus(t.InternalStatus, sub, clock.Now())
	if next != t.InternalStatus {
		updated := tenant.NewBuilder(t).WithInternalStatus(next).Build()
		if err := s.TenantRepo.Update(ctx, updated); err != nil {
			return "", err
		}
		s.Logger.Info(ctx, "self-billing tenant status updated", "tenant_id", t.ID, "from", t.InternalStatus, "to", next, "reason", reason)

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
			Reason:     reason,
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
