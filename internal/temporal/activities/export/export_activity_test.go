package export

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	syncExport "github.com/flexprice/flexprice/internal/ee/service/sync/export"
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

func TestExportFailure_ViewDeletedKeepsSafeMessage(t *testing.T) {
	notFound := ierr.NewError("view not found").Mark(ierr.ErrNotFound)
	err := exportFailure(fmt.Errorf("%w: %w", syncExport.ErrViewDeleted, notFound))

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
	require.Equal(t, ErrTypeViewDeleted, appErr.Type())
	require.Equal(t, syncExport.ViewDeletedMessage, appErr.Message())
}

func TestExportFailure_DestinationWriteHidesProviderText(t *testing.T) {
	raw := errors.New("AccessDenied: acme-private-bucket")
	tagged := ierr.WithError(fmt.Errorf("%w: %w", syncExport.ErrDestinationWrite, raw)).
		WithHint("Failed to upload CSV export").
		Mark(ierr.ErrHTTPClient)

	var appErr *temporal.ApplicationError
	require.ErrorAs(t, exportFailure(tagged), &appErr)
	require.False(t, appErr.NonRetryable())
	require.Equal(t, ErrTypeDestinationWrite, appErr.Type())
	require.Equal(t, syncExport.DestinationWriteMessage, appErr.Message())
	require.NotContains(t, appErr.Message(), "acme")
}

func TestExportFailure_Timeouts(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("query: %w", context.DeadlineExceeded),
		ierr.WithError(&proto.Exception{Code: 159}).Mark(ierr.ErrValidation),
	} {
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, exportFailure(err), &appErr)
		require.Equal(t, ErrTypeTimedOut, appErr.Type())
	}
	require.Equal(t, "boom timeout", exportFailure(errors.New("boom timeout")).Error())
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
