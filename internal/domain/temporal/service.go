package temporal

import (
	"context"
	"time"
)

type Service interface {
	StartBillingWorkflow(ctx context.Context, customerID, subscriptionID string, periodStart, periodEnd time.Time) (*BillingWorkflowResult, error)
}
