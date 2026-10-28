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
	svc := service.NewBillingEntityService(a.params)
	ctx = scopedCtx(ctx, in)

	total := 0
	for {
		assigned, err := svc.AssignCustomerBatch(ctx, assignBatchSize)
		if err != nil {
			return err
		}
		total += assigned
		activity.RecordHeartbeat(ctx, total)
		if assigned == 0 {
			break
		}
	}

	a.params.Logger.Info(ctx, "assigned customers to default billing entities",
		"tenant_id", in.TenantID,
		"environment_id", in.EnvironmentID,
		"customers", total)
	return nil
}

func (a *BillingEntityActivities) CompleteBillingEntityEnable(ctx context.Context, in models.BillingEntityEnableWorkflowInput) error {
	return service.NewBillingEntityService(a.params).CompleteEnable(scopedCtx(ctx, in))
}
