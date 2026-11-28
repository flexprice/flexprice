package export

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/activities/export"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
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
