package subscription

import "github.com/flexprice/flexprice/internal/types"

type ScheduleSubscriptionBillingWorkflowInput struct {
	BatchSize int `json:"batch_size"`
}

// Validate validates the schedule subscription billing workflow input
func (i *ScheduleSubscriptionBillingWorkflowInput) Validate() error {
	if i.BatchSize <= 0 {
		i.BatchSize = types.DEFAULT_BATCH_SIZE
	}
	return nil
}

type ScheduleSubscriptionBillingWorkflowResult struct {
	SubscriptionIDs []string `json:"subscription_ids"`
}
