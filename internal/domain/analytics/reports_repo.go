package analytics

import (
	"context"

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

// ReportsRepository runs billing-family views.
type ReportsRepository interface {
	Enabled() bool
	MaxRows() int
	RunReport(ctx context.Context, rv *ResolvedView, tenantID, environmentID string) (*ReportResult, error)
}
