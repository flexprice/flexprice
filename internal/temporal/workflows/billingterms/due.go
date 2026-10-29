package billingterms

import (
	"time"

	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	models "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	invoiceWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	prepareActivity = "PrepareBillingScheduleBatchActivity"
	createActivity  = "CreateOrRecoverBillingScheduleInvoiceActivity"
	maximumPerRun   = 500
	maximumParallel = 20
)

// ProcessBillingScheduleItemsDueWorkflow delivers scoped batches and durably starts invoice processing.
func ProcessBillingScheduleItemsDueWorkflow(ctx workflow.Context, input models.DueInput) (*models.DueResult, error) {
	if err := input.Validate(); err != nil {
		return nil, temporal.NewNonRetryableApplicationError("invalid billing schedule workflow input", "BillingScheduleInvalid", err)
	}
	if input.AsOf.IsZero() {
		input.AsOf = workflow.Now(ctx).UTC()
	}
	if input.BatchSize == 0 {
		input.BatchSize = 100
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue: types.TemporalTaskQueueCron.String(), StartToCloseTimeout: 5 * time.Minute, ScheduleToCloseTimeout: 20 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: 5},
	})
	result := input.Progress
	scanned, batches := 0, 0
	for {
		request := models.PrepareInput{TenantID: input.TenantID, EnvironmentID: input.EnvironmentID, UserID: input.UserID,
			Request: core.DueBatchRequest{AsOf: input.AsOf, Cursor: input.Cursor, Limit: min(input.BatchSize, maximumPerRun-scanned)}}
		var batch core.DueBatch
		if err := workflow.ExecuteActivity(ctx, prepareActivity, request).Get(ctx, &batch); err != nil {
			return nil, err
		}
		if batch.Scanned < 0 || batch.Scanned > request.Request.Limit || len(batch.Items)+len(batch.FailedItemIDs) > batch.Scanned || len(batch.NextCursor) > 2048 || (batch.NextCursor != "" && batch.NextCursor == input.Cursor) {
			return nil, temporal.NewNonRetryableApplicationError("invalid billing schedule batch", "BillingScheduleIntegrity", nil)
		}
		result.Scanned += batch.Scanned
		result.Failed += len(batch.FailedItemIDs)
		scanned += batch.Scanned
		batches++
		for start := 0; start < len(batch.Items); start += maximumParallel {
			items := batch.Items[start:min(start+maximumParallel, len(batch.Items))]
			futures := make([]workflow.Future, len(items))
			for i, item := range items {
				futures[i] = workflow.ExecuteActivity(ctx, createActivity, models.CreateInput{
					TenantID: input.TenantID, EnvironmentID: input.EnvironmentID, UserID: input.UserID,
					Request: core.DueInvoiceRequest{ItemID: item.ItemID, AsOf: input.AsOf},
				})
			}
			for i, future := range futures {
				var created core.DueInvoice
				if err := future.Get(ctx, &created); err != nil {
					if temporal.IsCanceledError(err) {
						return nil, err
					}
					workflow.GetLogger(ctx).Error("billing schedule invoice failed", "item_id", items[i].ItemID, "error", err)
					result.Failed++
					continue
				}
				if created.InvoiceID == "" {
					result.Deferred++
					continue
				}
				if err := startInvoice(ctx, input, created.InvoiceID); err != nil {
					if temporal.IsCanceledError(err) {
						return nil, err
					}
					workflow.GetLogger(ctx).Error("billing schedule invoice handoff failed", "invoice_id", created.InvoiceID, "error", err)
					result.Failed++
					continue
				}
				result.Started++
			}
		}
		if batch.NextCursor == "" {
			if result.Failed != 0 {
				return &result, temporal.NewNonRetryableApplicationError("billing schedule items require retry", "BillingScheduleItemsFailed", nil, result)
			}
			return &result, nil
		}
		input.Cursor = batch.NextCursor
		if scanned >= maximumPerRun || batches >= 10 || workflow.GetInfo(ctx).GetContinueAsNewSuggested() {
			input.Progress = result
			return nil, workflow.NewContinueAsNewError(ctx, ProcessBillingScheduleItemsDueWorkflow, input)
		}
	}
}

func startInvoice(ctx workflow.Context, input models.DueInput, invoiceID string) error {
	return invoiceWorkflows.StartInvoiceProcessing(ctx, invoiceModels.ProcessInvoiceWorkflowInput{
		InvoiceID: invoiceID, TenantID: input.TenantID, EnvironmentID: input.EnvironmentID, UserID: input.UserID,
		SkipCompute: true,
	})
}
