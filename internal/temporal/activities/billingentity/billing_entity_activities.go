package billingentity

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

const assignBatchSize = 1000

type BillingEntityActivities struct {
	params service.ServiceParams
}

func NewBillingEntityActivities(params service.ServiceParams) *BillingEntityActivities {
	return &BillingEntityActivities{params: params}
}

func scopedCtx(ctx context.Context, in models.BillingEntityEnableWorkflowInput) context.Context {
	ctx = types.SetTenantID(ctx, in.TenantID)
	ctx = types.SetEnvironmentID(ctx, in.EnvironmentID)
	ctx = types.SetUserID(ctx, in.UserID)
	return types.WithForceWriter(ctx)
}

func (a *BillingEntityActivities) PrepareBillingEntityEnable(ctx context.Context, in models.BillingEntityEnableWorkflowInput) error {
	err := service.NewBillingEntityService(a.params).PrepareEnable(scopedCtx(ctx, in))
	if err != nil && ierr.IsValidation(err) {
		return temporal.NewNonRetryableApplicationError(err.Error(), "BillingEntityEnableRejected", err)
	}
	return err
}

func (a *BillingEntityActivities) AssignBillingEntityCustomers(ctx context.Context, in models.BillingEntityEnableWorkflowInput) error {
	ctx = scopedCtx(ctx, in)
	total, err := assignInBatches(ctx, service.NewBillingEntityService(a.params).AssignCustomerBatch)
	if err != nil {
		return err
	}
	a.params.Logger.Info(ctx, "assigned customers to default billing entities",
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
		"customers", total)
	return nil
}

func (a *BillingEntityActivities) AssignBillingEntityInvoices(ctx context.Context, in models.BillingEntityEnableWorkflowInput) error {
	ctx = scopedCtx(ctx, in)
	total, err := assignInBatches(ctx, service.NewBillingEntityService(a.params).AssignInvoiceBatch)
	if err != nil {
		return err
	}
	a.params.Logger.Info(ctx, "assigned invoices to default billing entities",
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
		"invoices", total)
	return nil
}

// assignInBatches runs assign until a batch assigns nothing, heartbeating the running total.
func assignInBatches(ctx context.Context, assign func(ctx context.Context, limit int) (int, error)) (int, error) {
	total := 0
	for {
		assigned, err := assign(ctx, assignBatchSize)
		if err != nil {
			return total, err
		}
		total += assigned
		activity.RecordHeartbeat(ctx, total)
		if assigned == 0 {
			return total, nil
		}
	}
}

func (a *BillingEntityActivities) CompleteBillingEntityEnable(ctx context.Context, in models.BillingEntityEnableWorkflowInput) error {
	return service.NewBillingEntityService(a.params).CompleteEnable(scopedCtx(ctx, in))
}
