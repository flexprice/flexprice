package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"
)

const checkoutExpiryBatchSize = 100

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

// ExpireCheckoutSessionsActivity finds and expires checkout sessions that have passed their
// expiry date across all tenants and environments.
func (a *CheckoutSessionExpiryActivities) ExpireCheckoutSessionsActivity(ctx context.Context) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting checkout session expiry activity")
	a.logger.Info(ctx, "starting checkout session expiry cron", "time", time.Now().UTC().Format(time.RFC3339))

	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		return nil, err
	}

	result := &cronModels.CheckoutSessionExpiryWorkflowResult{}
	now := time.Now().UTC()

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

			filter := &types.CheckoutSessionFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(checkoutExpiryBatchSize),
					Offset: lo.ToPtr(0),
				},
				CheckoutStatuses: []types.CheckoutStatus{
					types.CheckoutStatusInitiated,
					types.CheckoutStatusPending,
				},
				ExpiresAtLT: &now,
			}

			sessions, err := a.checkoutSvc.List(envCtx, filter)
			if err != nil {
				a.logger.Error(ctx, "failed to list expired checkout sessions",
					"tenant_id", tenant.ID, "env_id", environment.ID, "error", err)
				continue
			}

			for i, sess := range sessions.Items {
				if i%10 == 0 {
					activity.RecordHeartbeat(ctx, "tenant="+tenant.ID+" env="+environment.ID)
				}
				result.Total++

				// Fetch the full domain session for CleanupCheckoutSession.
				fullSession, err := a.checkoutSvc.Get(envCtx, sess.ID)
				if err != nil {
					a.logger.Error(ctx, "failed to get checkout session for expiry", "session_id", sess.ID, "error", err)
					result.Failed++
					continue
				}

				if err := a.checkoutSvc.CleanupCheckoutSession(envCtx, fullSession.CheckoutSession, nil); err != nil {
					a.logger.Error(ctx, "failed to expire checkout session", "session_id", sess.ID, "error", err)
					result.Failed++
					continue
				}
				a.logger.Info(ctx, "expired checkout session", "session_id", sess.ID)
				result.Succeeded++
			}
		}
	}

	a.logger.Info(ctx, "completed checkout session expiry cron",
		"total", result.Total, "succeeded", result.Succeeded, "failed", result.Failed)
	log.Info("Completed checkout session expiry activity",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return result, nil
}
