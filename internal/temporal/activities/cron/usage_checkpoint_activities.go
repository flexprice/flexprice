package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// UsageCheckpointActivities lists the environments with aggregated usage and checkpoints one at a time.
type UsageCheckpointActivities struct {
	checkpointService  service.UsageCheckpointService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	config             *config.Configuration
	logger             *logger.Logger
}

func NewUsageCheckpointActivities(
	checkpointService service.UsageCheckpointService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	cfg *config.Configuration,
	log *logger.Logger,
) *UsageCheckpointActivities {
	return &UsageCheckpointActivities{
		checkpointService:  checkpointService,
		tenantService:      tenantService,
		environmentService: environmentService,
		config:             cfg,
		logger:             log,
	}
}

// ListUsageCheckpointEnvironmentsActivity returns every published environment of each tenant with
// aggregated usage enabled; none when aggregated_usage is off.
func (a *UsageCheckpointActivities) ListUsageCheckpointEnvironmentsActivity(ctx context.Context) ([]cronModels.UsageCheckpointEnvironment, error) {
	aggregatedUsage := a.config.AggregatedUsage
	if !aggregatedUsage.IsEnabledForAnyTenant() {
		return nil, nil
	}
	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		return nil, err
	}

	var environments []cronModels.UsageCheckpointEnvironment
	for _, tenant := range tenants {
		if !aggregatedUsage.IsEnabledForTenant(tenant.ID) {
			continue
		}
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)
		filter := types.GetDefaultFilter()
		for {
			page, err := a.environmentService.GetEnvironments(tenantCtx, filter)
			if err != nil {
				return nil, err
			}
			for _, environment := range page.Environments {
				environments = append(environments, cronModels.UsageCheckpointEnvironment{TenantID: tenant.ID, EnvironmentID: environment.ID})
			}
			if len(page.Environments) < filter.Limit {
				break
			}
			filter.Offset += filter.Limit
		}
	}
	return environments, nil
}

// UsageCheckpointEnvironmentActivity brings one environment's per-period totals up to input.Cutoff.
func (a *UsageCheckpointActivities) UsageCheckpointEnvironmentActivity(ctx context.Context, input cronModels.UsageCheckpointEnvironmentInput) error {
	ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)
	result, err := a.checkpointService.CheckpointEnvironment(ctx, input.Cutoff)
	if err != nil {
		a.logger.Error(ctx, "usage checkpoint: environment failed", "error", err,
			"tenant_id", input.TenantID, "environment_id", input.EnvironmentID)
		return err
	}
	a.logger.Info(ctx, "usage checkpoint: environment done",
		"tenant_id", input.TenantID, "environment_id", input.EnvironmentID,
		"cutoff", input.Cutoff, "customers", result.Customers, "outcomes", result.Outcomes)
	return nil
}
