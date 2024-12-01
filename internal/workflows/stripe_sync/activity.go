package stripe_sync

import (
	"context"

	"github.com/flexprice/flexprice/internal/integrations"
)

type SyncUsageActivity struct {
	integration *integrations.StripeIntegration
}

func NewSyncUsageActivity(integration *integrations.StripeIntegration) *SyncUsageActivity {
	return &SyncUsageActivity{
		integration: integration,
	}
}

func (a *SyncUsageActivity) Execute(ctx context.Context, input SyncUsageWorkflowInput) error {

	return a.integration.SyncUsageToStripe(ctx, &integrations.SyncUsageParams{
		MeterID:                  input.MeterID,
		ExternalCustomerID:       input.ExternalCustomerID,
		StartTime:                input.StartTime,
		EndTime:                  input.EndTime,
		StripeSubscriptionItemID: input.StripeSubscriptionItemID,
		TenantID:                 input.TenantID,
	})
}
