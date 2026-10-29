package service

import (
	"context"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	due "github.com/flexprice/flexprice/internal/temporal/models/billingterms"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

type billingSchedulePollers struct {
	client.Client
	missing string
	queries int
}

func (s *billingSchedulePollers) DescribeTaskQueue(_ context.Context, queue string, _ enumspb.TaskQueueType) (*workflowservice.DescribeTaskQueueResponse, error) {
	s.queries++
	response := &workflowservice.DescribeTaskQueueResponse{}
	if queue != s.missing {
		response.Pollers = []*taskqueue.PollerInfo{{Identity: "test_worker"}}
	}
	return response, nil
}

type scopedScheduleClient struct {
	temporalclient.TemporalClient
	pollers *billingSchedulePollers
	entries map[string]*scopedScheduleHandle
	creates int
}

func (s *scopedScheduleClient) GetRawClient() client.Client { return s.pollers }
func (s *scopedScheduleClient) GetScheduleHandle(_ context.Context, id string) models.ScheduleHandle {
	if s.entries[id] == nil {
		s.entries[id] = &scopedScheduleHandle{}
	}
	return s.entries[id]
}
func (s *scopedScheduleClient) CreateSchedule(_ context.Context, options models.CreateScheduleOptions) (models.ScheduleHandle, error) {
	s.creates++
	handle := s.entries[options.ID]
	handle.description = &client.ScheduleDescription{Schedule: client.Schedule{
		Spec: &options.Spec, Action: options.Action, Policy: &client.SchedulePolicies{Overlap: options.Overlap}, State: &client.ScheduleState{Paused: options.Paused},
	}}
	return handle, nil
}

type scopedScheduleHandle struct {
	models.ScheduleHandle
	description *client.ScheduleDescription
}

func (s *scopedScheduleHandle) Describe(context.Context) (*client.ScheduleDescription, error) {
	if s.description == nil {
		return nil, serviceerror.NewNotFound("schedule missing")
	}
	return s.description, nil
}
func (s *scopedScheduleHandle) Update(_ context.Context, options client.ScheduleUpdateOptions) error {
	updated, err := options.DoUpdate(client.ScheduleUpdateInput{Description: *s.description})
	if err == nil {
		s.description.Schedule = *updated.Schedule
	}
	return err
}

func TestEnsureBillingScheduleScopedAndIdempotent(t *testing.T) {
	connection := &scopedScheduleClient{pollers: &billingSchedulePollers{}, entries: map[string]*scopedScheduleHandle{}}
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	for range 2 {
		require.NoError(t, EnsureBillingSchedule(ctx, connection))
	}
	require.Equal(t, 1, connection.creates)
	require.Equal(t, 8, connection.pollers.queries)
	for _, handle := range connection.entries {
		schedule := handle.description.Schedule
		require.Equal(t, 2*time.Minute, schedule.Spec.Intervals[0].Every)
		require.Equal(t, enumspb.SCHEDULE_OVERLAP_POLICY_SKIP, schedule.Policy.Overlap)
		action := schedule.Action.(*client.ScheduleWorkflowAction)
		require.Equal(t, types.TemporalTaskQueueCron.String(), action.TaskQueue)
		input := action.Args[0].(due.DueInput)
		require.Equal(t, "tenant", input.TenantID)
		require.Equal(t, "env", input.EnvironmentID)
		require.True(t, input.AsOf.IsZero())
	}
	require.NoError(t, EnsureBillingSchedule(types.SetEnvironmentID(ctx, "env_b"), connection))
	require.Equal(t, 2, connection.creates)
}

func TestEnsureBillingScheduleFailsClosed(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant"), "env")
	require.True(t, ierr.IsValidation(EnsureBillingSchedule(context.Background(), nil)))
	require.True(t, ierr.IsServiceUnavailable(EnsureBillingSchedule(ctx, nil)))
	connection := &scopedScheduleClient{pollers: &billingSchedulePollers{missing: types.TemporalTaskQueueInvoice.String()}, entries: map[string]*scopedScheduleHandle{}}
	require.True(t, ierr.IsServiceUnavailable(EnsureBillingSchedule(ctx, connection)))
	require.Empty(t, connection.entries)
	connection.pollers.missing = ""
	require.NoError(t, EnsureBillingSchedule(ctx, connection))
	for _, handle := range connection.entries {
		handle.description.Schedule.State.Paused = true
	}
	require.True(t, ierr.IsServiceUnavailable(EnsureBillingSchedule(ctx, connection)))
	transaction := context.WithValue(ctx, types.CtxDBTransaction, new(int))
	require.True(t, ierr.IsValidation(EnsureBillingSchedule(transaction, connection)))
}
