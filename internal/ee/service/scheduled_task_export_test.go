package service

import (
	"context"
	"errors"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type exportRepoFake struct {
	scheduledtask.Repository
	task        *scheduledtask.ScheduledTask
	created     *scheduledtask.ScheduledTask
	settings    *scheduledtask.ScheduledTask
	settingsErr error
}

func (r *exportRepoFake) Create(_ context.Context, t *scheduledtask.ScheduledTask) error {
	r.created = t
	return nil
}

func (r *exportRepoFake) Get(context.Context, string) (*scheduledtask.ScheduledTask, error) {
	return r.task, nil
}

func (r *exportRepoFake) UpdateSettings(_ context.Context, t *scheduledtask.ScheduledTask) error {
	cp := *t
	r.settings = &cp
	return r.settingsErr
}

type exportConnRepoFake struct {
	connection.Repository
	conn *connection.Connection
}

func (c exportConnRepoFake) Get(context.Context, string) (*connection.Connection, error) {
	return c.conn, nil
}

type exportStorageFake struct {
	storagetypes.Storage
	provider storage.Provider
}

func (s exportStorageFake) Provider() storage.Provider { return s.provider }

type exportResolverFake struct {
	storage.Resolver
	provider storage.Provider
}

func (r exportResolverFake) ForPlatform(context.Context, storage.Purpose) (storage.Storage, error) {
	return exportStorageFake{provider: r.provider}, nil
}

func (r exportResolverFake) ForConnectionExport(context.Context, string, *types.S3JobConfig) (storage.Storage, error) {
	return nil, nil
}

type exportHandleFake struct {
	models.ScheduleHandle
	spec    client.ScheduleSpec
	paused  bool
	updates []client.ScheduleSpec
}

func (h *exportHandleFake) Update(_ context.Context, opts client.ScheduleUpdateOptions) error {
	spec := h.spec
	out, err := opts.DoUpdate(client.ScheduleUpdateInput{
		Description: client.ScheduleDescription{Schedule: client.Schedule{
			Spec:  &spec,
			State: &client.ScheduleState{Paused: h.paused},
		}},
	})
	if err != nil {
		return err
	}
	h.spec = *out.Schedule.Spec
	h.updates = append(h.updates, h.spec)
	return nil
}

type exportTemporalFake struct {
	temporalClient.TemporalClient
	created *models.CreateScheduleOptions
	handle  *exportHandleFake
	args    []interface{}
}

func (c *exportTemporalFake) CreateSchedule(_ context.Context, o models.CreateScheduleOptions) (models.ScheduleHandle, error) {
	c.created = &o
	return nil, nil
}

func (c *exportTemporalFake) GetScheduleHandle(context.Context, string) models.ScheduleHandle {
	return c.handle
}

func (c *exportTemporalFake) StartWorkflow(_ context.Context, _ models.StartWorkflowOptions, _ interface{}, args ...interface{}) (models.WorkflowRun, error) {
	c.args = args
	return startedRun{}, nil
}

func exportCtx() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "t1")
	return context.WithValue(ctx, types.CtxEnvironmentID, "e1")
}

func exportCfg() *config.Configuration {
	return &config.Configuration{
		FlexpriceS3Exports:  config.FlexpriceS3ExportsConfig{Bucket: "fp-bucket", Region: "us-west-2"},
		FlexpriceGCSExports: config.FlexpriceGCSExportsConfig{Bucket: "fp-gcs"},
	}
}

func newExportService(repo *exportRepoFake, tc *exportTemporalFake) *scheduledTaskService {
	return &scheduledTaskService{
		repo:            repo,
		connectionRepo:  exportConnRepoFake{conn: managedS3Conn()},
		temporalClient:  tc,
		logger:          logger.NewNoopLogger(),
		config:          exportCfg(),
		storageResolver: exportResolverFake{provider: storage.ProviderS3},
	}
}

func managedS3Conn() *connection.Connection {
	return &connection.Connection{
		ID:           "conn_1",
		ProviderType: types.SecretProviderS3,
		SyncConfig: &types.SyncConfig{Storage: &types.StorageExportConfig{
			IsFlexpriceManaged: true,
			KeyPrefix:          "exports/t1/e1/conn",
		}},
	}
}

func kolkata() *types.ExportSchedule {
	return &types.ExportSchedule{Hour: 9, Minute: 30, Timezone: "Asia/Kolkata"}
}

func TestCreate_OldPayloadUnchanged(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		ConnectionID: "conn_1",
		EntityType:   types.ScheduledTaskEntityTypeInvoice,
		Interval:     types.ScheduledTaskIntervalDaily,
		Enabled:      true,
		JobConfig:    &types.S3JobConfig{},
	})
	require.NoError(t, err)
	require.False(t, repo.created.ManagedCopy)
	require.Equal(t, "conn_1", repo.created.ConnectionID)
	require.Equal(t, "fp-bucket", repo.created.JobConfig.Bucket)
	require.Equal(t, []string{"15 0 * * *"}, tc.created.Spec.CronExpressions)
	require.Empty(t, tc.created.Spec.TimeZoneName)
	require.Zero(t, tc.created.CatchupWindow)
	require.Zero(t, tc.created.Overlap)
}

func TestCreate_NoConnectionIsManagedCopy(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		EntityType: types.ScheduledTaskEntityTypeInvoice,
		Interval:   types.ScheduledTaskIntervalDaily,
		Enabled:    true,
	})
	require.NoError(t, err)
	task := repo.created
	require.True(t, task.ManagedCopy)
	require.Empty(t, task.ConnectionID)
	require.Equal(t, types.SecretProviderS3, task.JobConfig.Provider)
	require.Equal(t, "fp-bucket", task.JobConfig.Bucket)
	require.Equal(t, "us-west-2", task.JobConfig.Region)
	require.Equal(t, "exports/t1/e1/"+task.ID, task.JobConfig.KeyPrefix)
	require.Equal(t, types.S3CompressionTypeNone, task.JobConfig.Compression)
}

func TestCreate_NoConnectionGCS(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)
	s.storageResolver = exportResolverFake{provider: storage.ProviderGCS}

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		EntityType: types.ScheduledTaskEntityTypeInvoice,
		Interval:   types.ScheduledTaskIntervalDaily,
	})
	require.NoError(t, err)
	require.Equal(t, types.SecretProviderGCS, repo.created.JobConfig.Provider)
	require.Equal(t, "fp-gcs", repo.created.JobConfig.Bucket)
}

func TestCreate_DailyScheduleUsesTimezone(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		EntityType: types.ScheduledTaskEntityTypeInvoice,
		Interval:   types.ScheduledTaskIntervalDaily,
		Enabled:    true,
		Schedule:   kolkata(),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"30 9 * * *"}, tc.created.Spec.CronExpressions)
	require.Equal(t, "Asia/Kolkata", tc.created.Spec.TimeZoneName)
	require.Equal(t, enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL, tc.created.Overlap)
	require.Equal(t, 24*time.Hour, tc.created.CatchupWindow)
	require.Equal(t, kolkata(), repo.created.Schedule)
}

func TestCreate_ManualNoSchedule(t *testing.T) {
	repo, tc := &exportRepoFake{}, &exportTemporalFake{}
	s := newExportService(repo, tc)

	_, err := s.CreateScheduledTask(exportCtx(), dto.CreateScheduledTaskRequest{
		EntityType: types.ScheduledTaskEntityTypeInvoice,
		Interval:   types.ScheduledTaskIntervalManual,
		Enabled:    true,
	})
	require.NoError(t, err)
	require.Nil(t, tc.created)
	require.Empty(t, repo.created.TemporalScheduleID)
}

func scheduledRow(schedule *types.ExportSchedule) *scheduledtask.ScheduledTask {
	return &scheduledtask.ScheduledTask{
		ID:                 "st_1",
		TenantID:           "t1",
		EnvironmentID:      "e1",
		EntityType:         types.ScheduledTaskEntityTypeInvoice,
		Interval:           types.ScheduledTaskIntervalDaily,
		Enabled:            true,
		TemporalScheduleID: "st_1",
		Schedule:           schedule,
		Status:             types.StatusPublished,
	}
}

func TestUpdate_EnabledOnlyUnchanged(t *testing.T) {
	handle := &exportHandleFake{}
	tc := &exportTemporalFake{handle: handle}
	repo := &exportRepoFake{task: scheduledRow(nil)}
	s := newExportService(repo, tc)

	enabled := true
	resp, err := s.UpdateScheduledTask(exportCtx(), "st_1", dto.UpdateScheduledTaskRequest{Enabled: &enabled})
	require.NoError(t, err)
	require.True(t, resp.Enabled)
	require.Empty(t, handle.updates)
	require.Nil(t, repo.settings)
}

func TestUpdate_ScheduleUpdatesTemporalThenDB(t *testing.T) {
	old := client.ScheduleSpec{CronExpressions: []string{"15 0 * * *"}}
	handle := &exportHandleFake{spec: old, paused: true}
	tc := &exportTemporalFake{handle: handle}
	repo := &exportRepoFake{task: scheduledRow(nil)}
	s := newExportService(repo, tc)

	name := "nightly"
	_, err := s.UpdateScheduledTask(exportCtx(), "st_1", dto.UpdateScheduledTaskRequest{Name: &name, Schedule: kolkata()})
	require.NoError(t, err)
	require.Len(t, handle.updates, 1)
	require.Equal(t, []string{"30 9 * * *"}, handle.updates[0].CronExpressions)
	require.Equal(t, "Asia/Kolkata", handle.updates[0].TimeZoneName)
	require.NotNil(t, repo.settings)
	require.Equal(t, "nightly", repo.settings.Name)
	require.Equal(t, kolkata(), repo.settings.Schedule)
}

func TestUpdate_RevertsTemporalOnDBError(t *testing.T) {
	old := client.ScheduleSpec{CronExpressions: []string{"15 0 * * *"}}
	handle := &exportHandleFake{spec: old}
	tc := &exportTemporalFake{handle: handle}
	dbErr := errors.New("db down")
	repo := &exportRepoFake{task: scheduledRow(nil), settingsErr: dbErr}
	s := newExportService(repo, tc)

	_, err := s.UpdateScheduledTask(exportCtx(), "st_1", dto.UpdateScheduledTaskRequest{Schedule: kolkata()})
	require.ErrorIs(t, err, dbErr)
	require.Len(t, handle.updates, 2)
	require.Equal(t, old, handle.spec)
}

func TestRun_ScheduledRowNoBodyPassesNoWindow(t *testing.T) {
	tc := &exportTemporalFake{}
	repo := &exportRepoFake{task: scheduledRow(kolkata())}
	s := newExportService(repo, tc)

	_, _, _, _, err := s.triggerForceRun(exportCtx(), "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)
	input := tc.args[0].(exportWorkflows.ExecuteExportWorkflowInput)
	require.Nil(t, input.CustomStartTime)
	require.Nil(t, input.CustomEndTime)
}

func TestRun_OldRowUnchanged(t *testing.T) {
	tc := &exportTemporalFake{}
	repo := &exportRepoFake{task: scheduledRow(nil)}
	s := newExportService(repo, tc)

	_, _, _, _, err := s.triggerForceRun(exportCtx(), "st_1", dto.TriggerForceRunRequest{})
	require.NoError(t, err)
	input := tc.args[0].(exportWorkflows.ExecuteExportWorkflowInput)
	require.NotNil(t, input.CustomStartTime)
	require.NotNil(t, input.CustomEndTime)
}
