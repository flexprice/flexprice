package export

import (
	"context"
	"errors"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

func TestExportFailure(t *testing.T) {
	notFound := ierr.NewError("view not found").Mark(ierr.ErrNotFound)
	tests := []struct {
		name         string
		err          error
		nonRetryable bool
	}{
		{"validation", ierr.NewError("bad").Mark(ierr.ErrValidation), true},
		{"not found", notFound, true},
		{"wrapped not found", ierr.WithError(notFound).WithHint("lookup failed").Mark(ierr.ErrDatabase), true},
		{"transient", errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var appErr *temporal.ApplicationError
			got := exportFailure(tt.err)
			if !tt.nonRetryable {
				require.False(t, errors.As(got, &appErr) && appErr.NonRetryable())
				return
			}
			require.ErrorAs(t, got, &appErr)
			require.True(t, appErr.NonRetryable())
		})
	}
}

func TestExportData_NilReportRunnerIsNonRetryable(t *testing.T) {
	a := &ExportActivity{logger: logger.NewNoopLogger()}
	_, err := a.ExportData(context.Background(), ExportDataInput{
		EntityType:   types.ScheduledTaskEntityTypeReport,
		TenantID:     "t",
		EnvID:        "e",
		ManagedCopy:  true,
		JobConfig:    &types.S3JobConfig{},
		SourceConfig: &types.ExportSourceConfig{ReportKey: "invoice_total"},
	})
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
}
