package billingterms

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	models "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func prepareStub(context.Context, models.PrepareInput) (*core.DueBatch, error) { return nil, nil }
func createStub(context.Context, models.CreateInput) (*core.DueInvoice, error) { return nil, nil }

func delayedInvoice(ctx workflow.Context, _ invoiceModels.ProcessInvoiceWorkflowInput) error {
	return workflow.Sleep(ctx, time.Hour)
}

func dueEnvironment() *testsuite.TestWorkflowEnvironment {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(prepareStub, activity.RegisterOptions{Name: prepareActivity})
	env.RegisterActivityWithOptions(createStub, activity.RegisterOptions{Name: createActivity})
	env.RegisterWorkflowWithOptions(delayedInvoice, workflow.RegisterOptions{Name: types.TemporalProcessInvoiceWorkflow.String()})
	return env
}

func dueInput() models.DueInput {
	return models.DueInput{TenantID: "tenant_a", EnvironmentID: "env_a", AsOf: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestDueWorkflowDurableHandoffAndDeferral(t *testing.T) {
	env := dueEnvironment()
	input := dueInput()
	batch := &core.DueBatch{Scanned: 2, Items: []core.DueCandidate{{ItemID: "item_a"}, {ItemID: "item_b"}}}
	env.OnActivity(prepareActivity, mock.Anything, mock.MatchedBy(func(req models.PrepareInput) bool {
		return req.TenantID == input.TenantID && req.EnvironmentID == input.EnvironmentID && req.Request.AsOf.Equal(input.AsOf) && req.Request.Limit == 100
	})).Return(batch, nil).Once()
	env.OnActivity(createActivity, mock.Anything, mock.MatchedBy(func(req models.CreateInput) bool { return req.Request.ItemID == "item_a" })).Return(&core.DueInvoice{InvoiceID: "inv_a"}, nil).Once()
	env.OnActivity(createActivity, mock.Anything, mock.MatchedBy(func(req models.CreateInput) bool { return req.Request.ItemID == "item_b" })).Return(&core.DueInvoice{DeferredReason: "subscription_paused"}, nil).Once()
	var childStarted bool
	env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		var invoice invoiceModels.ProcessInvoiceWorkflowInput
		require.NoError(t, args.Get(&invoice))
		require.Equal(t, "inv_a", invoice.InvoiceID)
		require.True(t, invoice.SkipCompute)
		require.Equal(t, input.TenantID, invoice.TenantID)
		require.Equal(t, input.EnvironmentID, invoice.EnvironmentID)
		require.Equal(t, types.TemporalTaskQueueInvoice.String(), info.TaskQueueName)
		childStarted = true
	})
	env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, input)
	require.NoError(t, env.GetWorkflowError())
	require.True(t, childStarted)
	var result models.DueResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, models.DueResult{Scanned: 2, Started: 1, Deferred: 1}, result)
	env.AssertExpectations(t)
}

func TestDueWorkflowRetriesSameSourceAndContinuesPastFailure(t *testing.T) {
	env := dueEnvironment()
	env.OnActivity(prepareActivity, mock.Anything, mock.Anything).Return(&core.DueBatch{Scanned: 3, Items: []core.DueCandidate{{ItemID: "lost_response"}, {ItemID: "invalid"}, {ItemID: "next"}}}, nil).Once()
	lost := mock.MatchedBy(func(req models.CreateInput) bool { return req.Request.ItemID == "lost_response" })
	env.OnActivity(createActivity, mock.Anything, lost).Return(nil, errors.New("response lost after commit")).Once()
	env.OnActivity(createActivity, mock.Anything, lost).Return(&core.DueInvoice{InvoiceID: "inv_existing"}, nil).Once()
	env.OnActivity(createActivity, mock.Anything, mock.MatchedBy(func(req models.CreateInput) bool { return req.Request.ItemID == "invalid" })).Return(nil, temporal.NewNonRetryableApplicationError("broken source", "Integrity", nil)).Once()
	env.OnActivity(createActivity, mock.Anything, mock.MatchedBy(func(req models.CreateInput) bool { return req.Request.ItemID == "next" })).Return(&core.DueInvoice{InvoiceID: "inv_next"}, nil).Once()
	started := 0
	env.SetOnChildWorkflowStartedListener(func(*workflow.Info, workflow.Context, converter.EncodedValues) { started++ })
	env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, dueInput())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, 2, started)
	env.AssertExpectations(t)
}

func TestDueWorkflowAlreadyStartedIsSuccessfulRecovery(t *testing.T) {
	env := dueEnvironment()
	env.OnActivity(prepareActivity, mock.Anything, mock.Anything).Return(&core.DueBatch{Scanned: 1, Items: []core.DueCandidate{{ItemID: "item"}}}, nil).Once()
	env.OnActivity(createActivity, mock.Anything, mock.Anything).Return(&core.DueInvoice{InvoiceID: "inv_existing"}, nil).Once()
	env.OnWorkflow(types.TemporalProcessInvoiceWorkflow.String(), mock.Anything, mock.Anything).Return(&temporal.ChildWorkflowExecutionAlreadyStartedError{}).Once()
	env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, dueInput())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestDueWorkflowContinuePreservesCutoffAndCursor(t *testing.T) {
	env := dueEnvironment()
	input := dueInput()
	for i := 0; i < 5; i++ {
		cursor := ""
		if i > 0 {
			cursor = fmt.Sprintf("page_%d", i)
		}
		env.OnActivity(prepareActivity, mock.Anything, mock.MatchedBy(func(req models.PrepareInput) bool { return req.Request.Cursor == cursor })).Return(&core.DueBatch{Scanned: 100, NextCursor: fmt.Sprintf("page_%d", i+1)}, nil).Once()
	}
	env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, input)
	var continued *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continued)
	var next models.DueInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continued.Input, &next))
	require.True(t, next.AsOf.Equal(input.AsOf))
	require.Equal(t, "page_5", next.Cursor)
	require.Equal(t, 500, next.Progress.Scanned)
	env.AssertExpectations(t)
}

func TestDueWorkflowRejectsScopeAndStalledCursor(t *testing.T) {
	t.Run("missing scope", func(t *testing.T) {
		env := dueEnvironment()
		env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, models.DueInput{})
		require.Error(t, env.GetWorkflowError())
	})
	t.Run("cursor cannot repeat", func(t *testing.T) {
		env := dueEnvironment()
		input := dueInput()
		input.Cursor = "cursor"
		env.OnActivity(prepareActivity, mock.Anything, mock.Anything).Return(&core.DueBatch{NextCursor: input.Cursor}, nil).Once()
		env.ExecuteWorkflow(ProcessBillingScheduleItemsDueWorkflow, input)
		require.Error(t, env.GetWorkflowError())
	})
}
