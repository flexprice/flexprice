package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutSessionExpiryActivities wraps checkout session expiry logic.
type CheckoutSessionExpiryActivities struct {
	checkoutSvc        service.CheckoutSessionService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	logger             *logger.Logger
}

func NewCheckoutSessionExpiryActivities(
	checkoutSvc service.CheckoutSessionService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	log *logger.Logger,
) *CheckoutSessionExpiryActivities {
	return &CheckoutSessionExpiryActivities{
		checkoutSvc:        checkoutSvc,
		tenantService:      tenantService,
		environmentService: environmentService,
		logger:             log,
	}
}

// ExpireCheckoutSessionsActivity archives expired checkout sessions across all tenants and environments.
func (a *CheckoutSessionExpiryActivities) ExpireCheckoutSessionsActivity(ctx context.Context) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	now := time.Now().UTC()
	a.logger.Info(ctx, "starting checkout session expiry", "effective_date", now.Format(time.RFC3339))

	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		return nil, err
	}

	result := &cronModels.CheckoutSessionExpiryWorkflowResult{}

	for _, tenant := range tenants {
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)
		envFilter := types.GetDefaultFilter()
		envFilter.Limit = 1000
		environments, err := a.environmentService.GetEnvironments(tenantCtx, envFilter)
		if err != nil {
			a.logger.Error(ctx, "failed to get environments", "tenant_id", tenant.ID, "error", err)
			return nil, err
		}

		for _, environment := range environments.Environments {
			envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, environment.ID)
			r, err := a.checkoutSvc.CleanupExpiredSessions(envCtx, &now)
			if err != nil {
				a.logger.Error(ctx, "failed to cleanup expired checkout sessions",
					"tenant_id", tenant.ID, "env_id", environment.ID, "error", err)
				continue
			}
			result.Total += r.Total
			result.Succeeded += r.Succeeded
			result.Failed += r.Failed
		}
	}

	a.logger.Info(ctx, "completed checkout session expiry",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	return result, nil
}
