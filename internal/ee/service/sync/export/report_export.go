package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ViewDeletedMessage is safe to show users.
const ViewDeletedMessage = "The saved view used by this export was deleted"

// ErrViewDeleted marks runs whose saved view is gone.
var ErrViewDeleted = errors.New(ViewDeletedMessage)

type ReportRunner interface {
	RunReportForExport(ctx context.Context, key string, params *types.ReportParams, rng string) (*dto.AnalyticsQueryResult, error)
	RunViewForExport(ctx context.Context, viewID, rng string) (*dto.AnalyticsQueryResult, time.Time, error)
	ReportColumnsForExport(ctx context.Context, key string, params *types.ReportParams, rng string) ([]string, error)
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
			header, err := e.runner.ReportColumnsForExport(ctx, request.SourceConfig.ReportKey, reportParams, rng)
			if err != nil {
				return nil, 0, err
			}
			return writeReportCSV(header, nil, nil)
		}
		reportParams = withCustomerFilter(reportParams, refIDs(refs))
	}
	if e.entity == types.ScheduledTaskEntityTypeAnalyticsView {
		var viewUpdatedAt time.Time
		result, viewUpdatedAt, err = e.runner.RunViewForExport(ctx, request.SourceConfig.ViewID, rng)
		if err == nil {
			e.metadata = map[string]string{"view_updated_at": viewUpdatedAt.UTC().Format(time.RFC3339)}
		}
		if ierr.IsNotFound(err) {
			return nil, 0, fmt.Errorf("%w: %w", ErrViewDeleted, err)
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
	textCols := make([]bool, len(result.Columns))
	for i, col := range result.Columns {
		header = append(header, col.Name)
		textCols[i] = col.Type != types.ColumnTypeDecimal && col.Role != types.ColumnRoleMetric
	}

	return writeReportCSV(header, textCols, result.Rows)
}

func writeReportCSV(header []string, textCols []bool, rows [][]string) ([]byte, int, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV header").Mark(ierr.ErrSystem)
	}
	out := make([]string, 0, len(header))
	for _, row := range rows {
		out = out[:0]
		for i, cell := range row {
			if i < len(textCols) && textCols[i] {
				cell = neutralizeFormula(cell)
			}
			out = append(out, cell)
		}
		if err := w.Write(out); err != nil {
			return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV rows").Mark(ierr.ErrSystem)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV rows").Mark(ierr.ErrSystem)
	}
	return buf.Bytes(), len(rows), nil
}

// Stops spreadsheets evaluating event-supplied text.
func neutralizeFormula(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}
