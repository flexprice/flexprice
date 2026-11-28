package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type scheduledTaskGetter struct {
	scheduledtask.Repository
	task *scheduledtask.ScheduledTask
}

func (g scheduledTaskGetter) Get(context.Context, string) (*scheduledtask.ScheduledTask, error) {
	return g.task, nil
}

// workflowStarter records the arguments of the workflow it is asked to start.
type workflowStarter struct {
	temporalClient.TemporalClient
	args []interface{}
}

type startedRun struct{ models.WorkflowRun }

func (startedRun) GetID() string    { return "wf_1" }
func (startedRun) GetRunID() string { return "run_1" }

func (c *workflowStarter) StartWorkflow(_ context.Context, _ models.StartWorkflowOptions, _ interface{}, args ...interface{}) (models.WorkflowRun, error) {
	c.args = args
	return startedRun{}, nil
}

func TestTriggerForceRunPassesCustomerFilterToTheLedgerExport(t *testing.T) {
	task := &scheduledtask.ScheduledTask{ID: "st_1", EntityType: types.ScheduledTaskEntityTypeWalletLedger, Interval: types.ScheduledTaskIntervalDaily}
	starter := &workflowStarter{}
	s := &scheduledTaskService{repo: scheduledTaskGetter{task: task}, temporalClient: starter, logger: logger.NewNoopLogger()}

	_, _, _, _, err := s.triggerForceRun(context.Background(), task.ID, dto.TriggerForceRunRequest{CustomerExternalIDs: []string{"acme"}})
	require.NoError(t, err)
	require.Len(t, starter.args, 1)
	input, ok := starter.args[0].(exportWorkflows.ExecuteExportWorkflowInput)
	require.True(t, ok, "got %T", starter.args[0])
	require.Equal(t, []string{"acme"}, input.CustomerExternalIDs)
}

func TestTriggerForceRunRejectsCustomerFilterOutsideTheLedger(t *testing.T) {
	task := &scheduledtask.ScheduledTask{ID: "st_1", EntityType: types.ScheduledTaskEntityTypeWalletDebits, Interval: types.ScheduledTaskIntervalDaily}
	s := &scheduledTaskService{repo: scheduledTaskGetter{task: task}, logger: logger.NewNoopLogger()}

	_, _, _, _, err := s.triggerForceRun(context.Background(), task.ID, dto.TriggerForceRunRequest{CustomerExternalIDs: []string{"acme"}})
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err), "got %v", err)
}

func TestTriggerForceRunRequestValidatesCustomerIDs(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "c"
	}
	tests := []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{name: "none", ids: nil},
		{name: "some", ids: []string{"acme", "globex"}},
		{name: "blank id", ids: []string{"acme", ""}, wantErr: true},
		{name: "more than 100", ids: ids, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&dto.TriggerForceRunRequest{CustomerExternalIDs: tt.ids}).Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
