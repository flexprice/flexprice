package service_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service"
	dueActivities "github.com/flexprice/flexprice/internal/temporal/activities/billingterms"
	invoiceActivities "github.com/flexprice/flexprice/internal/temporal/activities/invoice"
	dueModels "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	dueWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/billingterms"
	invoiceWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func TestBillingScheduleTemporalPostgres(t *testing.T) {
	address := os.Getenv("FLEXPRICE_BILLING_TERMS_TEST_TEMPORAL_ADDRESS")
	namespace := os.Getenv("FLEXPRICE_BILLING_TERMS_TEST_TEMPORAL_NAMESPACE")
	if address == "" || namespace == "" {
		t.Skip("set the isolated billing-terms Temporal address and namespace to run integration coverage")
	}
	ctx, params, items, item := service.BillingScheduleTemporalFixture(t)
	configuration, err := params.SettingsRepo.GetByKey(ctx, types.SettingKeyInvoiceConfig)
	require.NoError(t, err)
	configuration.Value["finalization_delay_seconds"] = 3600
	require.NoError(t, params.SettingsRepo.Update(ctx, configuration))
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
	require.NoError(t, err)
	defer temporalClient.Close()

	cron := worker.New(temporalClient, types.TemporalTaskQueueCron.String(), worker.Options{})
	cron.RegisterWorkflowWithOptions(dueWorkflows.ProcessBillingScheduleItemsDueWorkflow, workflow.RegisterOptions{Name: types.TemporalProcessBillingScheduleItemsDueWorkflow.String()})
	cron.RegisterActivity(dueActivities.NewActivities(service.NewBillingScheduleService(params, items)))
	invoiceWorker := worker.New(temporalClient, types.TemporalTaskQueueInvoice.String(), worker.Options{})
	invoiceWorker.RegisterWorkflowWithOptions(invoiceWorkflows.ProcessInvoiceWorkflow, workflow.RegisterOptions{Name: types.TemporalProcessInvoiceWorkflow.String()})
	activities := invoiceActivities.NewInvoiceActivities(params, params.Logger)
	invoiceWorker.RegisterActivity(activities.ComputeInvoiceActivity)
	invoiceWorker.RegisterActivity(activities.FinalizeInvoiceActivity)
	var paymentAttempts atomic.Int32
	invoiceWorker.RegisterActivityWithOptions(func(activityCtx context.Context, input invoiceModels.PaymentActivityInput) (*invoiceModels.PaymentActivityOutput, error) {
		if paymentAttempts.Add(1) == 1 {
			return nil, temporal.NewNonRetryableApplicationError("injected payment failure", "PaymentFixture", nil)
		}
		activityCtx = types.SetTenantID(activityCtx, input.TenantID)
		activityCtx = types.SetEnvironmentID(activityCtx, input.EnvironmentID)
		inv, err := params.InvoiceRepo.Get(activityCtx, input.InvoiceID)
		if err != nil {
			return nil, err
		}
		updated := invoice.NewInvoiceBuilder(inv).WithPaymentStatus(types.PaymentStatusSucceeded).Build()
		if err := params.InvoiceRepo.Update(activityCtx, updated); err != nil {
			return nil, err
		}
		return &invoiceModels.PaymentActivityOutput{Success: true}, nil
	}, activity.RegisterOptions{Name: invoiceWorkflows.ActivityAttemptInvoicePayment})
	require.NoError(t, cron.Start())
	defer cron.Stop()
	require.NoError(t, invoiceWorker.Start())
	defer invoiceWorker.Stop()

	startDue := func() (client.WorkflowRun, *historypb.History) {
		input := dueModels.DueInput{TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), AsOf: time.Now().UTC()}
		run, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID: types.GenerateUUIDWithPrefix("billing_due_test"), TaskQueue: types.TemporalTaskQueueCron.String(),
		}, types.TemporalProcessBillingScheduleItemsDueWorkflow.String(), input)
		require.NoError(t, err)
		var result dueModels.DueResult
		require.NoError(t, run.Get(ctx, &result))
		return run, billingTemporalHistory(t, ctx, temporalClient, run.GetID(), run.GetRunID())
	}
	first, firstHistory := startDue()
	firstChildID, firstChildRun := billingChildExecution(t, firstHistory)
	require.NoError(t, temporalClient.GetWorkflow(ctx, firstChildID, firstChildRun).Get(ctx, nil))
	claimed, err := items.Get(ctx, item.ID)
	require.NoError(t, err)
	require.NotNil(t, claimed.InvoiceID)
	inv, err := params.InvoiceRepo.Get(ctx, *claimed.InvoiceID)
	require.NoError(t, err)
	require.Equal(t, types.InvoiceStatusDraft, inv.InvoiceStatus)
	require.Equal(t, types.PaymentStatusPending, inv.PaymentStatus)
	require.Len(t, inv.LineItems, 1)
	require.NotNil(t, inv.LastComputedAt)
	computedAt := *inv.LastComputedAt
	_, waitingHistory := startDue()
	for _, event := range waitingHistory.Events {
		require.NotEqual(t, enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_STARTED, event.EventType)
	}
	inv, err = params.InvoiceRepo.Get(ctx, *claimed.InvoiceID)
	require.NoError(t, err)
	require.True(t, inv.LastComputedAt.Equal(computedAt))
	require.Zero(t, paymentAttempts.Load())
	elapsed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	require.NoError(t, params.InvoiceRepo.Update(ctx, invoice.NewInvoiceBuilder(inv).WithLastComputedAt(&elapsed).Build()))
	failed, failedHistory := startDue()
	failedChildID, failedChildRun := billingChildExecution(t, failedHistory)
	require.Equal(t, firstChildID, failedChildID)
	require.NotEqual(t, firstChildRun, failedChildRun)
	require.Error(t, temporalClient.GetWorkflow(ctx, failedChildID, failedChildRun).Get(ctx, nil))
	inv, err = params.InvoiceRepo.Get(ctx, *claimed.InvoiceID)
	require.NoError(t, err)
	require.Equal(t, types.InvoiceStatusFinalized, inv.InvoiceStatus)
	require.True(t, inv.LastComputedAt.Equal(elapsed))

	require.NoError(t, params.DB.WithTx(ctx, func(tx context.Context) error {
		owner := domain.OwnerRef{Type: item.OwnerType, ID: item.OwnerID}
		if err := params.BillingTermsStore.LockOwner(tx, owner); err != nil {
			return err
		}
		return items.Archive(tx, owner)
	}))
	second, secondHistory := startDue()
	secondChildID, secondChildRun := billingChildExecution(t, secondHistory)
	require.Equal(t, firstChildID, secondChildID)
	require.NotEqual(t, failedChildRun, secondChildRun)
	require.NoError(t, temporalClient.GetWorkflow(ctx, secondChildID, secondChildRun).Get(ctx, nil))
	inv, err = params.InvoiceRepo.Get(ctx, *claimed.InvoiceID)
	require.NoError(t, err)
	require.Equal(t, types.PaymentStatusSucceeded, inv.PaymentStatus)
	require.Len(t, inv.LineItems, 1)
	require.Equal(t, item.ID, lo.FromPtr(inv.LineItems[0].BillingScheduleItemID))
	require.Equal(t, int32(2), paymentAttempts.Load())
	_, thirdHistory := startDue()
	for _, event := range thirdHistory.Events {
		require.NotEqual(t, enumspb.EVENT_TYPE_CHILD_WORKFLOW_EXECUTION_STARTED, event.EventType)
	}

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(dueWorkflows.ProcessBillingScheduleItemsDueWorkflow, workflow.RegisterOptions{Name: types.TemporalProcessBillingScheduleItemsDueWorkflow.String()})
	replayer.RegisterWorkflowWithOptions(invoiceWorkflows.ProcessInvoiceWorkflow, workflow.RegisterOptions{Name: types.TemporalProcessInvoiceWorkflow.String()})
	for _, execution := range [][2]string{{first.GetID(), first.GetRunID()}, {failed.GetID(), failed.GetRunID()}, {second.GetID(), second.GetRunID()}, {firstChildID, firstChildRun}, {failedChildID, failedChildRun}, {secondChildID, secondChildRun}} {
		history := billingTemporalHistory(t, ctx, temporalClient, execution[0], execution[1])
		require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
	}
}

func billingTemporalHistory(t *testing.T, ctx context.Context, temporalClient client.Client, id, runID string) *historypb.History {
	t.Helper()
	iterator := temporalClient.GetWorkflowHistory(ctx, id, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	history := &historypb.History{}
	for iterator.HasNext() {
		event, err := iterator.Next()
		require.NoError(t, err)
		history.Events = append(history.Events, event)
	}
	return history
}

func billingChildExecution(t *testing.T, history *historypb.History) (string, string) {
	t.Helper()
	for _, event := range history.Events {
		if child := event.GetChildWorkflowExecutionStartedEventAttributes(); child != nil {
			return child.WorkflowExecution.WorkflowId, child.WorkflowExecution.RunId
		}
	}
	t.Fatal("due workflow completed without a durable invoice child")
	return "", ""
}
