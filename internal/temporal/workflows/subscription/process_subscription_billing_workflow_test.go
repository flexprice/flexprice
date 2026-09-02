package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func checkDraftStub(_ context.Context, _ subscriptionModels.CheckDraftSubscriptionActivityInput) (*subscriptionModels.CheckDraftSubscriptionActivityOutput, error) {
	return nil, nil
}

func calculatePeriodsStub(_ context.Context, _ subscriptionModels.CalculatePeriodsActivityInput) (*subscriptionModels.CalculatePeriodsActivityOutput, error) {
	return nil, nil
}

func createDraftInvoicesStub(_ context.Context, _ subscriptionModels.CreateInvoicesActivityInput) (*subscriptionModels.CreateInvoicesActivityOutput, error) {
	return nil, nil
}

func updateCurrentPeriodStub(_ context.Context, _ subscriptionModels.UpdateSubscriptionPeriodActivityInput) (*subscriptionModels.UpdateSubscriptionPeriodActivityOutput, error) {
	return nil, nil
}

func checkCancellationStub(_ context.Context, _ subscriptionModels.CheckSubscriptionCancellationActivityInput) (*subscriptionModels.CheckSubscriptionCancellationActivityOutput, error) {
	return nil, nil
}

func processPlanChangeStub(_ context.Context, _ subscriptionModels.ProcessPendingPlanChangesActivityInput) (*subscriptionModels.ProcessPendingPlanChangesActivityOutput, error) {
	return nil, nil
}

func triggerInvoiceWorkflowStub(_ context.Context, _ invoiceModels.TriggerInvoiceWorkflowActivityInput) (*invoiceModels.TriggerInvoiceWorkflowActivityOutput, error) {
	return nil, nil
}

// billingEnv wires every activity the workflow reaches, with the happy-path answer
// for all of them except STEP 6, which each test drives on its own.
func billingEnv(t *testing.T) *testsuite.TestWorkflowEnvironment {
	t.Helper()

	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	boundary := periodStart.AddDate(0, 1, 0)

	register := func(fn any, name string) {
		env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	register(checkDraftStub, ActivityCheckDraftSubscription)
	register(calculatePeriodsStub, ActivityCalculatePeriods)
	register(createDraftInvoicesStub, ActivityCreateDraftInvoices)
	register(updateCurrentPeriodStub, ActivityUpdateCurrentPeriod)
	register(checkCancellationStub, ActivityCheckCancellation)
	register(processPlanChangeStub, ActivityProcessPlanChange)
	register(triggerInvoiceWorkflowStub, ActivityTriggerInvoiceWorkflow)

	env.OnActivity(ActivityCheckDraftSubscription, mock.Anything, mock.Anything).
		Return(&subscriptionModels.CheckDraftSubscriptionActivityOutput{IsDraft: false}, nil)
	env.OnActivity(ActivityCalculatePeriods, mock.Anything, mock.Anything).
		Return(&subscriptionModels.CalculatePeriodsActivityOutput{
			ShouldProcess: true,
			Periods: []dto.Period{
				{Start: periodStart, End: boundary},
				{Start: boundary, End: boundary.AddDate(0, 1, 0)},
			},
		}, nil)
	env.OnActivity(ActivityCreateDraftInvoices, mock.Anything, mock.Anything).
		Return(&subscriptionModels.CreateInvoicesActivityOutput{InvoiceIDs: []string{"inv_1"}}, nil)
	env.OnActivity(ActivityUpdateCurrentPeriod, mock.Anything, mock.Anything).
		Return(&subscriptionModels.UpdateSubscriptionPeriodActivityOutput{Success: true}, nil)
	env.OnActivity(ActivityCheckCancellation, mock.Anything, mock.Anything).
		Return(&subscriptionModels.CheckSubscriptionCancellationActivityOutput{IsCancelled: false, Success: true}, nil)
	env.OnActivity(ActivityTriggerInvoiceWorkflow, mock.Anything, mock.Anything).
		Return(&invoiceModels.TriggerInvoiceWorkflowActivityOutput{TriggeredCount: 1}, nil)

	return env
}

func billingInput() subscriptionModels.ProcessSubscriptionBillingWorkflowInput {
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return subscriptionModels.ProcessSubscriptionBillingWorkflowInput{
		SubscriptionID: "subs_test",
		TenantID:       "tenant_test",
		EnvironmentID:  "env_test",
		UserID:         "user_test",
		PeriodStart:    periodStart,
		PeriodEnd:      periodStart.AddDate(0, 1, 0),
	}
}

// STEP 6 carries its own retry policy because the workflow-wide one is
// MaximumAttempts: 1 and a lost plan change cannot be recovered on a later scan.
func TestProcessSubscriptionBillingWorkflow_PlanChangeRetriesTransientFailure(t *testing.T) {
	env := billingEnv(t)

	attempts := 0
	env.OnActivity(ActivityProcessPlanChange, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ subscriptionModels.ProcessPendingPlanChangesActivityInput) (*subscriptionModels.ProcessPendingPlanChangesActivityOutput, error) {
			attempts++
			if attempts < 3 {
				return nil, temporal.NewApplicationError("database unavailable", "TransientFailure")
			}
			return &subscriptionModels.ProcessPendingPlanChangesActivityOutput{Success: true, WasChanged: true}, nil
		})

	env.ExecuteWorkflow(ProcessSubscriptionBillingWorkflow, billingInput())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, attempts, "STEP 6 must retry a transient failure rather than give up after one attempt")
	env.AssertCalled(t, ActivityTriggerInvoiceWorkflow, mock.Anything, mock.Anything)
}

// Exhausting the retries must not fail the workflow: the period has already rolled,
// and failing here would leave the subscription without its renewal invoices.
func TestProcessSubscriptionBillingWorkflow_PlanChangeExhaustedDoesNotFailWorkflow(t *testing.T) {
	env := billingEnv(t)

	attempts := 0
	env.OnActivity(ActivityProcessPlanChange, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ subscriptionModels.ProcessPendingPlanChangesActivityInput) (*subscriptionModels.ProcessPendingPlanChangesActivityOutput, error) {
			attempts++
			return nil, temporal.NewApplicationError("database unavailable", "TransientFailure")
		})

	env.ExecuteWorkflow(ProcessSubscriptionBillingWorkflow, billingInput())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "a lost plan change is logged and skipped, not fatal")
	require.Equal(t, 3, attempts, "the policy caps STEP 6 at three attempts")
	env.AssertCalled(t, ActivityTriggerInvoiceWorkflow, mock.Anything, mock.Anything)
}

// A terminal failure is returned non-retryable by the activity, so the retries are
// skipped instead of spending backoff on an outcome that cannot change.
func TestProcessSubscriptionBillingWorkflow_PlanChangeTerminalFailureIsNotRetried(t *testing.T) {
	env := billingEnv(t)

	attempts := 0
	env.OnActivity(ActivityProcessPlanChange, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ subscriptionModels.ProcessPendingPlanChangesActivityInput) (*subscriptionModels.ProcessPendingPlanChangesActivityOutput, error) {
			attempts++
			return nil, temporal.NewNonRetryableApplicationError(
				"scheduled plan change cannot succeed", "TerminalPlanChangeFailure", nil)
		})

	env.ExecuteWorkflow(ProcessSubscriptionBillingWorkflow, billingInput())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, attempts, "a non-retryable terminal failure must stop after one attempt")
	env.AssertCalled(t, ActivityTriggerInvoiceWorkflow, mock.Anything, mock.Anything)
}

// The new policy must be scoped to STEP 6 alone: every other step keeps the
// workflow-wide MaximumAttempts: 1 and still fails the run on the first error.
func TestProcessSubscriptionBillingWorkflow_OtherStepsKeepSingleAttempt(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(checkDraftStub, activity.RegisterOptions{Name: ActivityCheckDraftSubscription})
	env.RegisterActivityWithOptions(calculatePeriodsStub, activity.RegisterOptions{Name: ActivityCalculatePeriods})

	env.OnActivity(ActivityCheckDraftSubscription, mock.Anything, mock.Anything).
		Return(&subscriptionModels.CheckDraftSubscriptionActivityOutput{IsDraft: false}, nil)

	attempts := 0
	env.OnActivity(ActivityCalculatePeriods, mock.Anything, mock.Anything).
		Return(func(_ context.Context, _ subscriptionModels.CalculatePeriodsActivityInput) (*subscriptionModels.CalculatePeriodsActivityOutput, error) {
			attempts++
			return nil, temporal.NewApplicationError("database unavailable", "TransientFailure")
		})

	env.ExecuteWorkflow(ProcessSubscriptionBillingWorkflow, billingInput())

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError(), "steps other than STEP 6 still fail the workflow")
	require.Equal(t, 1, attempts, "the scoped policy must not leak onto other activities")
}
