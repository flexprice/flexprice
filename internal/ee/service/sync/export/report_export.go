package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ReportRunner runs saved reports and views for export.
type ReportRunner interface {
	RunReportForExport(ctx context.Context, key string, params *types.ReportParams, rng string) (*dto.AnalyticsQueryResult, error)
	RunViewForExport(ctx context.Context, viewID, rng string) (*dto.AnalyticsQueryResult, time.Time, error)
}

// RunMetadataExporter exposes metadata gathered while preparing data.
type RunMetadataExporter interface {
	RunMetadata() map[string]string
}

// ReportExporter exports a report or analytics view as CSV.
type ReportExporter struct {
	runner   ReportRunner
	entity   types.ScheduledTaskEntityType
	metadata map[string]string
}

// NewReportExporter creates an exporter for a report or analytics view source.
func NewReportExporter(runner ReportRunner, entity types.ScheduledTaskEntityType) *ReportExporter {
	return &ReportExporter{runner: runner, entity: entity}
}

func (e *ReportExporter) GetFilenamePrefix() string {
	return string(e.entity)
}

// RunMetadata returns metadata recorded by the last PrepareData.
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
	if e.entity == types.ScheduledTaskEntityTypeAnalyticsView {
		var viewUpdatedAt time.Time
		result, viewUpdatedAt, err = e.runner.RunViewForExport(ctx, request.SourceConfig.ViewID, rng)
		if err == nil {
			e.metadata = map[string]string{"view_updated_at": viewUpdatedAt.UTC().Format(time.RFC3339)}
		}
	} else {
		result, err = e.runner.RunReportForExport(ctx, request.SourceConfig.ReportKey, request.SourceConfig.ReportParams, rng)
	}
	if err != nil {
		return nil, 0, err
	}

	header := make([]string, 0, len(result.Columns))
	for _, col := range result.Columns {
		header = append(header, col.Name)
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(header); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV header").Mark(ierr.ErrSystem)
	}
	if err := w.WriteAll(result.Rows); err != nil {
		return nil, 0, ierr.WithError(err).WithHint("Failed to write CSV rows").Mark(ierr.ErrSystem)
	}
	return buf.Bytes(), len(result.Rows), nil
}
