package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type ReportRunner interface {
	RunReportForExport(ctx context.Context, key string, params *types.ReportParams, rng string) (*dto.AnalyticsQueryResult, error)
	RunViewForExport(ctx context.Context, viewID, rng string) (*dto.AnalyticsQueryResult, time.Time, error)
}

type RunMetadataExporter interface {
	RunMetadata() map[string]string
}

// Exports a report as CSV.
type ReportExporter struct {
	runner   ReportRunner
	entity   types.ScheduledTaskEntityType
	metadata map[string]string
	scope    customerScope
}

func NewReportExporter(runner ReportRunner, entity types.ScheduledTaskEntityType, customerRepo customer.Repository) *ReportExporter {
	return &ReportExporter{runner: runner, entity: entity, scope: newCustomerScope(customerRepo, maxScopedReportCustomers)}
}

// Copies params with a customer filter.
func withCustomerFilter(params *types.ReportParams, customerIDs []string) *types.ReportParams {
	scoped := &types.ReportParams{}
	if params != nil {
		*scoped = *params
	}
	scoped.Filters = append(append([]types.ReportParamFilter{}, scoped.Filters...),
		types.ReportParamFilter{Field: "customer_id", Op: "in", Value: customerIDs})
	return scoped
}

// Header when no customers match.
func emptyReportHeader(key string, params *types.ReportParams) []string {
	var header []string
	if params != nil && params.Shape == string(types.ShapeTimeseries) {
		header = append(header, "bucket")
	}
	if params != nil {
		header = append(header, params.Dimensions...)
	}
	return append(header, key)
}

func (e *ReportExporter) GetFilenamePrefix() string {
	return string(e.entity)
}

// Returns last run metadata.
func (e *ReportExporter) RunMetadata() map[string]string {
	return e.metadata
}

func (e *ReportExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	if request.SourceConfig == nil {
		return nil, 0, ierr.NewError("source config is required").
			WithHint("report exports need a source configuration").
			Mark(ierr.ErrValidation)
	}
	rng := scheduledtask.ReportRange(request.StartTime, request.EndTime)

	var result *dto.AnalyticsQueryResult
	var err error
	reportParams := request.SourceConfig.ReportParams
	if request.SourceConfig.GetCustomerFilter() != nil && e.entity != types.ScheduledTaskEntityTypeReport {
		return nil, 0, ierr.NewError("customer_filter not supported for this export").
			WithHint("This data can't be limited to customers yet.").
			Mark(ierr.ErrValidation)
	}
	refs, scoped, err := e.scope.resolve(ctx, request)
	if err != nil {
		return nil, 0, err
	}
	if scoped {
		if len(refs) == 0 {
			return writeReportCSV(emptyReportHeader(request.SourceConfig.ReportKey, reportParams), nil)
		}
		reportParams = withCustomerFilter(reportParams, refIDs(refs))
	}
	if e.entity == types.ScheduledTaskEntityTypeAnalyticsView {
		var viewUpdatedAt time.Time
		result, viewUpdatedAt, err = e.runner.RunViewForExport(ctx, request.SourceConfig.ViewID, rng)
		if err == nil {
			e.metadata = map[string]string{"view_updated_at": viewUpdatedAt.UTC().Format(time.RFC3339)}
		}
	} else {
		result, err = e.runner.RunReportForExport(ctx, request.SourceConfig.ReportKey, reportParams, rng)
	}
	if err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, ierr.NewError("report returned no result").Mark(ierr.ErrSystem)
	}

	header := make([]string, 0, len(result.Columns))
	for _, col := range result.Columns {
		header = append(header, col.Name)
	}

	return writeReportCSV(header, result.Rows)
}

func writeReportCSV(header []string, rows [][]string) ([]byte, int, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV header").Mark(ierr.ErrSystem)
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV rows").Mark(ierr.ErrSystem)
	}
	return buf.Bytes(), len(rows), nil
}
