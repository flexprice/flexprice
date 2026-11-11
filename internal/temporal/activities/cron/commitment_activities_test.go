package cron

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

// recordingCommitments counts BillDueInstalments calls and fails them with err.
type recordingCommitments struct {
	service.CommitmentService
	calls int
	err   error
}

func (r *recordingCommitments) BillDueInstalments(context.Context) error {
	r.calls++
	return r.err
}

func TestBillDueInstalmentsActivity(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{})
	require.NoError(t, err)

	tests := []struct {
		name    string
		billErr error
	}{
		{"succeeds when every instalment bills", nil},
		{"fails when an instalment fails", errors.New("billing failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitments := &recordingCommitments{err: tt.billErr}
			acts := NewCommitmentCronActivities(commitments, log)
			env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			env.RegisterActivity(acts.BillDueInstalmentsActivity)

			_, err := env.ExecuteActivity(acts.BillDueInstalmentsActivity)
			require.Equal(t, 1, commitments.calls)
			if tt.billErr == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
		})
	}
}
