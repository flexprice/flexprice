//go:build ee

package alerts

import (
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
)

func init() {
	temporal.RegisterEEContributor(func(params service.ServiceParams, taskQueue types.TemporalTaskQueue) temporal.WorkerConfig {
		if taskQueue != types.TemporalTaskQueueWorkflows {
			return temporal.WorkerConfig{}
		}
		acts := NewAlertActivities(params, params.Logger)
		return temporal.WorkerConfig{
			TaskQueue: types.TemporalTaskQueueWorkflows,
			Workflows: []interface{}{UsageAlertWorkflow},
			Activities: []interface{}{
				acts.SpendAndEntitlementAlertsActivity,
				acts.WalletAlertsActivity,
			},
		}
	})
}
