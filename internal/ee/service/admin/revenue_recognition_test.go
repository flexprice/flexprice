package admin

import (
	"context"
	"testing"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
)

const revrecEnvironmentID = "env_acme_prod"

type fakeBackfillRuns struct {
	startErr  error
	startedID string
	started   *tmodels.RevenueRecognitionBackfillInput
	state     *replayRunState
}

func (f *fakeBackfillRuns) start(_ context.Context, id string, in tmodels.RevenueRecognitionBackfillInput) (string, error) {
	f.startedID, f.started = id, &in
	return "run-1", f.startErr
}

func (f *fakeBackfillRuns) describe(context.Context, string, string) (*replayRunState, error) {
	if f.state == nil {
		return nil, serviceerror.NewNotFound("missing")
	}
	return f.state, nil
}

// newRevenueRecognitionTestService seeds tenant_acme with env_acme_prod and wires the real shared settings service.
func newRevenueRecognitionTestService(t *testing.T) (*revenueRecognitionService, *fakeBackfillRuns, *settingsTestDeps) {
	t.Helper()
	d := newSettingsTestDeps(t)
	envs := testutil.NewInMemoryEnvironmentStore()
	ctx := types.SetTenantID(context.Background(), settingsTenantID)
	require.NoError(t, envs.Create(ctx, &environment.Environment{ID: revrecEnvironmentID, Name: "Production",
		BaseModel: types.BaseModel{TenantID: settingsTenantID, Status: types.StatusPublished}}))
	d.params.EnvironmentRepo = envs

	runs := &fakeBackfillRuns{}
	return &revenueRecognitionService{ServiceParams: d.params, settings: service.NewSettingsService(d.params), runs: runs}, runs, d
}

// storedEnabled reads the environment's saved revenue_recognition_config.
func storedEnabled(t *testing.T, d *settingsTestDeps) bool {
	t.Helper()
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), settingsTenantID), revrecEnvironmentID)
	setting, err := d.settings.GetByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	require.NoError(t, err)
	cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](setting.Value)
	require.NoError(t, err)
	return cfg.Enabled
}

func TestRevenueRecognitionUpdateConfig(t *testing.T) {
	t.Run("enabling with backfill saves the setting and starts the backfill", func(t *testing.T) {
		svc, runs, d := newRevenueRecognitionTestService(t)
		resp, err := svc.UpdateConfig(context.Background(), admindto.UpdateRevenueRecognitionConfigRequest{
			TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID, Enabled: true, Backfill: true,
		})
		require.NoError(t, err)
		require.True(t, resp.Enabled)
		require.True(t, storedEnabled(t, d))
		require.Equal(t, "revenue-recognition-backfill-"+revrecEnvironmentID, resp.Backfill.WorkflowID)
		require.Equal(t, admindto.ReplayStatusQueued, resp.Backfill.Status)
		require.Equal(t, &tmodels.RevenueRecognitionBackfillInput{TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID}, runs.started)
	})

	t.Run("enabling alone starts nothing", func(t *testing.T) {
		svc, runs, d := newRevenueRecognitionTestService(t)
		resp, err := svc.UpdateConfig(context.Background(), admindto.UpdateRevenueRecognitionConfigRequest{
			TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID, Enabled: true,
		})
		require.NoError(t, err)
		require.Nil(t, resp.Backfill)
		require.Nil(t, runs.started)
		require.True(t, storedEnabled(t, d))
	})

	t.Run("backfill without enabling is rejected", func(t *testing.T) {
		svc, _, _ := newRevenueRecognitionTestService(t)
		_, err := svc.UpdateConfig(context.Background(), admindto.UpdateRevenueRecognitionConfigRequest{
			TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID, Backfill: true,
		})
		require.True(t, ierr.IsValidation(err))
	})

	t.Run("an environment of another tenant is rejected", func(t *testing.T) {
		svc, _, d := newRevenueRecognitionTestService(t)
		require.NoError(t, d.params.EnvironmentRepo.Create(types.SetTenantID(context.Background(), "tenant_other"), &environment.Environment{
			ID: "env_other", BaseModel: types.BaseModel{TenantID: "tenant_other", Status: types.StatusPublished}}))
		_, err := svc.UpdateConfig(context.Background(), admindto.UpdateRevenueRecognitionConfigRequest{
			TenantID: settingsTenantID, EnvironmentID: "env_other", Enabled: true,
		})
		require.True(t, ierr.IsNotFound(err))
	})
}

func TestRevenueRecognitionStartBackfill(t *testing.T) {
	req := admindto.StartRevenueRecognitionBackfillRequest{TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID}

	t.Run("refused while revenue recognition is off", func(t *testing.T) {
		svc, runs, _ := newRevenueRecognitionTestService(t)
		_, err := svc.StartBackfill(context.Background(), req)
		require.True(t, ierr.IsValidation(err))
		require.Nil(t, runs.started)
	})

	t.Run("starts once enabled; a second start while running is rejected", func(t *testing.T) {
		svc, runs, _ := newRevenueRecognitionTestService(t)
		_, err := svc.UpdateConfig(context.Background(), admindto.UpdateRevenueRecognitionConfigRequest{
			TenantID: settingsTenantID, EnvironmentID: revrecEnvironmentID, Enabled: true,
		})
		require.NoError(t, err)

		run, err := svc.StartBackfill(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, "run-1", run.RunID)

		runs.startErr = serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "")
		_, err = svc.StartBackfill(context.Background(), req)
		require.True(t, ierr.IsAlreadyExists(err))
	})
}

func TestRevenueRecognitionGetBackfill(t *testing.T) {
	svc, runs, _ := newRevenueRecognitionTestService(t)
	_, err := svc.GetBackfill(context.Background(), "missing", "")
	require.True(t, ierr.IsNotFound(err))

	runs.state = &replayRunState{runID: "run-1", status: enums.WORKFLOW_EXECUTION_STATUS_RUNNING, activityStarted: true, attempt: 2, lastFailure: "db down"}
	run, err := svc.GetBackfill(context.Background(), "revenue-recognition-backfill-"+revrecEnvironmentID, "")
	require.NoError(t, err)
	require.Equal(t, admindto.ReplayStatusRunning, run.Status)
	require.Equal(t, int32(2), run.Attempt)
	require.Equal(t, "db down", run.LastError)
}
