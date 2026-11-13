package usageanalytics

import "context"

// QueryParams is what the repository needs to price events: the query, plus the priced line windows
// that decide which events count and at what price.
type QueryParams struct {
	TenantID      string
	EnvironmentID string
	Query         *Query
	Lines         []LineWindow
}

// Repository runs the usage query over meter usage.
type Repository interface {
	// GetBreakdown prices every event inside a line window, then groups, sorts and pages.
	// With no lines it returns an empty result without querying.
	GetBreakdown(ctx context.Context, params *QueryParams) (*Result, error)
}
