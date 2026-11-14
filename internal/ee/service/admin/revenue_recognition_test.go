package admin

import (
	"context"
	"testing"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
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
	d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)

	runs := &fakeBackfillRuns{}
	return &revenueRecognitionService{ServiceParams: d.params, settings: service.NewSettingsService(d.params), runs: runs}, runs, d
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
		svc, runs, d := newRevenueRecognitionTestService(t)
		ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), settingsTenantID), revrecEnvironmentID)
		require.NoError(t, d.settings.Create(ctx, &settings.Setting{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING), Key: types.SettingKeyRevenueRecognitionConfig, EnvironmentID: revrecEnvironmentID,
			Value: map[string]interface{}{"enabled": true}, BaseModel: types.BaseModel{TenantID: settingsTenantID, Status: types.StatusPublished},
		}))

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
