package costanalytics

import "context"

// QueryParams is what the repository needs to cost events: the query, plus the priced line windows
// that decide which events count and at what price.
type QueryParams struct {
	TenantID      string
	EnvironmentID string
	Query         *Query
	Lines         []LineWindow
}

// Repository runs the cost query over meter usage.
type Repository interface {
	// GetCostBreakdown costs every event inside a line window, then groups, sorts and pages.
	// With no lines it returns an empty result without querying.
	GetCostBreakdown(ctx context.Context, params *QueryParams) (*Result, error)
}
