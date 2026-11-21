package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// UsageCheckpointActivities lists the environments with aggregated usage and checkpoints one at a time.
type UsageCheckpointActivities struct {
	checkpointService service.UsageCheckpointService
	logger            *logger.Logger
}

func NewUsageCheckpointActivities(checkpointService service.UsageCheckpointService, log *logger.Logger) *UsageCheckpointActivities {
	return &UsageCheckpointActivities{checkpointService: checkpointService, logger: log}
}

// ListUsageCheckpointEnvironmentsActivity returns every environment of the tenants with aggregated usage on.
func (a *UsageCheckpointActivities) ListUsageCheckpointEnvironmentsActivity(ctx context.Context) ([]cronModels.UsageCheckpointEnvironment, error) {
	environments, err := a.checkpointService.ActiveEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]cronModels.UsageCheckpointEnvironment, 0, len(environments))
	for _, e := range environments {
		result = append(result, cronModels.UsageCheckpointEnvironment{TenantID: e.TenantID, EnvironmentID: e.EnvironmentID})
	}
	return result, nil
}

// UsageCheckpointEnvironmentActivity brings one environment's per-period totals up to input.Cutoff.
func (a *UsageCheckpointActivities) UsageCheckpointEnvironmentActivity(ctx context.Context, input cronModels.UsageCheckpointEnvironmentInput) error {
	ctx = context.WithValue(ctx, types.CtxTenantID, input.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, input.EnvironmentID)
	result, err := a.checkpointService.UpdateCustomerCheckpoints(ctx, input.Cutoff)
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
