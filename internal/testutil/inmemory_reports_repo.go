package testutil

import (
	"context"

	domainAnalytics "github.com/flexprice/flexprice/internal/domain/analytics"
)

var _ domainAnalytics.ReportsRepository = (*InMemoryReportsRepo)(nil)

// InMemoryReportsRepo returns canned report rows.
type InMemoryReportsRepo struct {
	On                bool
	Max               int
	Columns           []domainAnalytics.ReportColumn
	Rows              [][]string
	Err               error
	Last              *domainAnalytics.ResolvedView
	LastTenantID      string
	LastEnvironmentID string
	LastDataset       *domainAnalytics.DatasetQuery
}

func (r *InMemoryReportsRepo) RunDataset(_ context.Context, q *domainAnalytics.DatasetQuery, tenantID, environmentID string) (*domainAnalytics.ReportResult, error) {
	r.LastDataset, r.LastTenantID, r.LastEnvironmentID = q, tenantID, environmentID
	if r.Err != nil {
		return nil, r.Err
	}
	return &domainAnalytics.ReportResult{Columns: r.Columns, Rows: r.Rows}, nil
}

func (r *InMemoryReportsRepo) Enabled() bool { return r.On }

func (r *InMemoryReportsRepo) MaxRows() int { return r.Max }

func (r *InMemoryReportsRepo) RunReport(_ context.Context, rv *domainAnalytics.ResolvedView, tenantID, environmentID string) (*domainAnalytics.ReportResult, error) {
	r.Last, r.LastTenantID, r.LastEnvironmentID = rv, tenantID, environmentID
	if r.Err != nil {
		return nil, r.Err
	}
	return &domainAnalytics.ReportResult{Columns: r.Columns, Rows: r.Rows}, nil
}
