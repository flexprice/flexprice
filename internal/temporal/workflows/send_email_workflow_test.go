package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestSendEmailWorkflow(t *testing.T) {
	tests := []struct {
		name string
		// deliver answers attempt n (from 1) with or without attachments allowed to be missing.
		deliver        func(n int, allowMissing bool) error
		wantStrict     int
		wantLenient    int
		wantMarkFailed bool
		wantErr        bool
	}{
		{
			name:       "sent on the first try",
			deliver:    func(int, bool) error { return nil },
			wantStrict: 1,
		},
		{
			name: "pdf never ready: sent without it",
			deliver: func(_ int, allowMissing bool) error {
				if allowMissing {
					return nil
				}
				return errors.New("pdf not ready")
			},
			wantStrict:  3,
			wantLenient: 1,
		},
		{
			name:       "rejected by the provider: no more attempts",
			deliver:    func(int, bool) error { return temporal.NewNonRetryableApplicationError("bad address", "EmailRejected", nil) },
			wantStrict: 1,
			wantErr:    true,
		},
		{
			name:           "provider down throughout: marked failed",
			deliver:        func(int, bool) error { return errors.New("503") },
			wantStrict:     3,
			wantLenient:    6,
			wantMarkFailed: true,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()

			strict, lenient, markFailed := 0, 0, false
			env.RegisterActivityWithOptions(func(_ context.Context, in models.DeliverEmailActivityInput) error {
				if in.AllowMissingAttachments {
					lenient++
					return tt.deliver(lenient, true)
				}
				strict++
				return tt.deliver(strict, false)
			}, activity.RegisterOptions{Name: ActivityDeliverEmail})
			env.RegisterActivityWithOptions(func(context.Context, models.SendEmailWorkflowInput) error {
				markFailed = true
				return nil
			}, activity.RegisterOptions{Name: ActivityMarkEmailFailed})

			env.ExecuteWorkflow(SendEmailWorkflow, models.SendEmailWorkflowInput{
				EmailLogID: "email_1", TenantID: "tenant_1", EnvironmentID: "env_1",
			})

			require.True(t, env.IsWorkflowCompleted())
			require.Equal(t, tt.wantErr, env.GetWorkflowError() != nil)
			require.Equal(t, tt.wantStrict, strict)
			require.Equal(t, tt.wantLenient, lenient)
			require.Equal(t, tt.wantMarkFailed, markFailed)
		})
	}
}
