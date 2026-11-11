package export

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/activities/export"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func getDetailsStub(context.Context, export.GetScheduledTaskDetailsInput) (*export.ScheduledTaskDetails, error) {
	return nil, nil
}

func createTaskStub(context.Context, export.CreateTaskInput) (*export.CreateTaskOutput, error) {
	return nil, nil
}

func updateStatusStub(context.Context, export.UpdateTaskStatusInput) error { return nil }

func exportDataStub(context.Context, export.ExportDataInput) (*export.ExportDataOutput, error) {
	return nil, nil
}

func completeTaskStub(context.Context, export.CompleteTaskInput) error { return nil }

func sendEmailsStub(context.Context, export.SendExportEmailsInput) (*export.SendExportEmailsOutput, error) {
	return nil, nil
}

func recordDeliveriesStub(context.Context, export.RecordDeliveriesInput) error { return nil }

func TestExecuteExportWorkflow_CopiesManagedCopyToExportInput(t *testing.T) {
	for _, managed := range []bool{true, false} {
		ts := &testsuite.WorkflowTestSuite{}
		env := ts.NewTestWorkflowEnvironment()
		register := func(fn any, name string) {
			env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
		}
		register(getDetailsStub, "GetScheduledTaskDetails")
		register(createTaskStub, "CreateTask")
		register(updateStatusStub, "UpdateTaskStatus")
		register(exportDataStub, "ExportData")
		register(completeTaskStub, "CompleteTask")
		register(sendEmailsStub, "SendExportEmails")
		register(recordDeliveriesStub, "RecordDeliveries")
		env.OnActivity("RecordDeliveries", mock.Anything, mock.Anything).Return(nil)

		env.OnActivity("GetScheduledTaskDetails", mock.Anything, mock.Anything).Return(&export.ScheduledTaskDetails{
			ScheduledTaskID: "st_1", TenantID: "t1", EnvID: "e1", Enabled: true,
			EntityType: types.ScheduledTaskEntityTypeEvents, ManagedCopy: managed,
			StartTime: time.Now().Add(-time.Hour), EndTime: time.Now(),
		}, nil)
		env.OnActivity("CreateTask", mock.Anything, mock.Anything).Return(&export.CreateTaskOutput{TaskID: "task_1"}, nil)
		env.OnActivity("UpdateTaskStatus", mock.Anything, mock.Anything).Return(nil)
		env.OnActivity("CompleteTask", mock.Anything, mock.Anything).Return(nil)

		var got export.ExportDataInput
		env.OnActivity("ExportData", mock.Anything, mock.Anything).Return(
			func(_ context.Context, in export.ExportDataInput) (*export.ExportDataOutput, error) {
				got = in
				return &export.ExportDataOutput{FileURL: "s3://b/k", RecordCount: 1}, nil
			})

		env.ExecuteWorkflow(ExecuteExportWorkflow, ExecuteExportWorkflowInput{ScheduledTaskID: "st_1", TenantID: "t1", EnvID: "e1"})

		require.True(t, env.IsWorkflowCompleted())
		require.NoError(t, env.GetWorkflowError())
		require.Equal(t, managed, got.ManagedCopy)
	}
}

type deliveryRun struct {
	env      *testsuite.TestWorkflowEnvironment
	emailIn  []export.SendExportEmailsInput
	recorded []export.RecordDeliveriesInput
}

func newDeliveryRun(details export.ScheduledTaskDetails, exportErr error, emailErr error) *deliveryRun {
	ts := &testsuite.WorkflowTestSuite{}
	r := &deliveryRun{env: ts.NewTestWorkflowEnvironment()}
	register := func(fn any, name string) {
		r.env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	register(getDetailsStub, "GetScheduledTaskDetails")
	register(createTaskStub, "CreateTask")
	register(updateStatusStub, "UpdateTaskStatus")
	register(exportDataStub, "ExportData")
	register(completeTaskStub, "CompleteTask")
	register(sendEmailsStub, "SendExportEmails")
	register(recordDeliveriesStub, "RecordDeliveries")

	details.ScheduledTaskID, details.TenantID, details.EnvID, details.Enabled = "st_1", "t1", "e1", true
	details.EntityType = types.ScheduledTaskEntityTypeEvents
	r.env.OnActivity("GetScheduledTaskDetails", mock.Anything, mock.Anything).Return(&details, nil)
	r.env.OnActivity("CreateTask", mock.Anything, mock.Anything).Return(&export.CreateTaskOutput{TaskID: "task_1"}, nil)
	r.env.OnActivity("UpdateTaskStatus", mock.Anything, mock.Anything).Return(nil)
	r.env.OnActivity("CompleteTask", mock.Anything, mock.Anything).Return(nil)
	r.env.OnActivity("ExportData", mock.Anything, mock.Anything).Return(&export.ExportDataOutput{FileURL: "f", RecordCount: 3}, exportErr)
	r.env.OnActivity("SendExportEmails", mock.Anything, mock.Anything).Return(
		func(_ context.Context, in export.SendExportEmailsInput) (*export.SendExportEmailsOutput, error) {
			r.emailIn = append(r.emailIn, in)
			if emailErr != nil {
				return nil, emailErr
			}
			ds := make([]types.ExportDelivery, len(in.Recipients))
			for i := range ds {
				ds[i] = types.ExportDelivery{Type: types.ExportDeliveryTypeEmail, Status: types.ExportDeliveryStatusDelivered}
			}
			return &export.SendExportEmailsOutput{Deliveries: ds}, nil
		})
	r.env.OnActivity("RecordDeliveries", mock.Anything, mock.Anything).Return(
		func(_ context.Context, in export.RecordDeliveriesInput) error {
			r.recorded = append(r.recorded, in)
			return nil
		})
	return r
}

func (r *deliveryRun) run() {
	r.env.ExecuteWorkflow(ExecuteExportWorkflow, ExecuteExportWorkflowInput{ScheduledTaskID: "st_1", TenantID: "t1", EnvID: "e1"})
}

func TestExecuteExportWorkflow_SuccessEmailsAndRecordsOnce(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Name: "rev", Interval: types.ScheduledTaskIntervalDaily, ConnectionID: "conn_1",
		JobConfig:       &types.S3JobConfig{Bucket: "b", KeyPrefix: "p/"},
		EmailRecipients: []string{"a@x.com", "b@x.com"},
	}, nil, nil)
	r.run()

	require.NoError(t, r.env.GetWorkflowError())
	require.Len(t, r.emailIn, 1)
	require.Equal(t, types.TaskStatusCompleted, r.emailIn[0].RunStatus)
	require.Equal(t, 3, r.emailIn[0].RecordCount)
	require.Len(t, r.recorded, 1)
	ds := r.recorded[0].Deliveries
	require.Len(t, ds, 3)
	require.Equal(t, types.ExportDeliveryTypeS3, ds[0].Type)
	require.Equal(t, "b/p/", ds[0].Target)
	require.Equal(t, types.ExportDeliveryStatusDelivered, ds[0].Status)
}

func TestExecuteExportWorkflow_GCSConnectionDeliveryType(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Interval: types.ScheduledTaskIntervalDaily, ConnectionID: "conn_1",
		JobConfig: &types.S3JobConfig{Bucket: "b", Provider: types.SecretProviderGCS},
	}, nil, nil)
	r.run()

	require.NoError(t, r.env.GetWorkflowError())
	require.Len(t, r.recorded, 1)
	require.Equal(t, types.ExportDeliveryTypeGCS, r.recorded[0].Deliveries[0].Type)
}

func TestExecuteExportWorkflow_FailedRunEmailsFailure(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Interval: types.ScheduledTaskIntervalDaily, EmailRecipients: []string{"a@x.com"},
	}, errors.New("boom"), nil)
	r.run()

	require.Error(t, r.env.GetWorkflowError())
	require.Len(t, r.emailIn, 1)
	require.Equal(t, types.TaskStatusFailed, r.emailIn[0].RunStatus)
	require.Len(t, r.recorded, 1)
	require.Equal(t, types.ExportDeliveryTypeFlexprice, r.recorded[0].Deliveries[0].Type)
	require.Equal(t, types.ExportDeliveryStatusFailed, r.recorded[0].Deliveries[0].Status)
}

func TestExecuteExportWorkflow_HourlySendsNoEmail(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Interval: types.ScheduledTaskIntervalHourly, EmailRecipients: []string{"a@x.com"},
	}, nil, nil)
	r.run()

	require.NoError(t, r.env.GetWorkflowError())
	require.Empty(t, r.emailIn)
	require.Len(t, r.recorded, 1)
	require.Len(t, r.recorded[0].Deliveries, 1)
}

func TestExecuteExportWorkflow_EmailActivityErrorKeepsRunSuccess(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Interval: types.ScheduledTaskIntervalDaily, EmailRecipients: []string{"a@x.com"},
	}, nil, errors.New("down"))
	r.run()

	require.NoError(t, r.env.GetWorkflowError())
	require.Len(t, r.recorded, 1)
	require.Len(t, r.recorded[0].Deliveries, 1)
}

func TestExecuteExportWorkflow_DefaultVersionSkipsDeliveries(t *testing.T) {
	r := newDeliveryRun(export.ScheduledTaskDetails{
		Interval: types.ScheduledTaskIntervalDaily, EmailRecipients: []string{"a@x.com"},
	}, nil, nil)
	r.env.OnGetVersion("export-deliveries", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	r.run()

	require.NoError(t, r.env.GetWorkflowError())
	require.Empty(t, r.emailIn)
	require.Empty(t, r.recorded)
}
