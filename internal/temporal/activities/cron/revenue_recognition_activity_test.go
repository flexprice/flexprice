package cron

import (
	"context"
	"testing"

	journalsvc "github.com/flexprice/flexprice/internal/ee/service/journal"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// recordingJournal captures each request and reports one finished contract.
type recordingJournal struct {
	journalsvc.JournalService
	got journalsvc.RecognitionRunRequest

	backfill                journalsvc.BackfillRequest
	tenantID, environmentID string
}

func (r *recordingJournal) RunRecognition(_ context.Context, req journalsvc.RecognitionRunRequest) error {
	r.got = req
	req.OnProgress(journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_2"})
	return nil
}

func (r *recordingJournal) Backfill(ctx context.Context, req journalsvc.BackfillRequest) error {
	r.backfill, r.tenantID, r.environmentID = req, types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	req.OnProgress(journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_2"})
	return nil
}

func TestBackfillRevenueActivity(t *testing.T) {
	journal := &recordingJournal{}
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	env.SetHeartbeatDetails(journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_1"})
	acts := NewRevenueRecognitionActivities(journal, nil)
	env.RegisterActivity(acts.BackfillRevenueActivity)

	_, err := env.ExecuteActivity(acts.BackfillRevenueActivity, cronModels.BackfillRevenueActivityInput{TenantID: "tenant_1", EnvironmentID: "env_1"})
	require.NoError(t, err)
	require.Equal(t, "tenant_1", journal.tenantID, "the environment is set on ctx")
	require.Equal(t, "env_1", journal.environmentID)
	require.Equal(t, &journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_1"}, journal.backfill.Resume)
}

func TestRecognizeRevenueActivity(t *testing.T) {
	t.Run("a first attempt starts from the top", func(t *testing.T) {
		journal := &recordingJournal{}
		env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		acts := NewRevenueRecognitionActivities(journal, nil)
		env.RegisterActivity(acts.RecognizeRevenueActivity)

		_, err := env.ExecuteActivity(acts.RecognizeRevenueActivity, cronModels.RecognizeRevenueActivityInput{})
		require.NoError(t, err)
		require.Nil(t, journal.got.Resume)
	})

	t.Run("a retry resumes from the heartbeat cursor", func(t *testing.T) {
		journal := &recordingJournal{}
		env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
		env.SetHeartbeatDetails(journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_1"})
		acts := NewRevenueRecognitionActivities(journal, nil)
		env.RegisterActivity(acts.RecognizeRevenueActivity)

		_, err := env.ExecuteActivity(acts.RecognizeRevenueActivity, cronModels.RecognizeRevenueActivityInput{})
		require.NoError(t, err)
		require.Equal(t, &journalsvc.RecognitionCursor{EnvironmentID: "env_1", ContractID: "contract_1"}, journal.got.Resume)
	})
}
