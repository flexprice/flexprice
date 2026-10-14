package analytics

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// ReportColumn describes one output column.
type ReportColumn struct {
	Name string
	Type types.ColumnType
	Role types.ColumnRole
}

// ReportResult is report columns and rows.
type ReportResult struct {
	Columns []ReportColumn
	Rows    [][]string
}

// DatasetQuery is a validated dataset request.
type DatasetQuery struct {
	Def     *ViewDefinition
	From    time.Time
	To      time.Time
	Compare string
}

// ReportsRepository runs billing-family and dataset views.
type ReportsRepository interface {
	Enabled() bool
	MaxRows() int
	RunReport(ctx context.Context, rv *ResolvedView, tenantID, environmentID string) (*ReportResult, error)
	RunDataset(ctx context.Context, q *DatasetQuery, tenantID, environmentID string) (*ReportResult, error)
}
