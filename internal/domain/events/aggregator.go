package events

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Aggregator interface {
	// GetQuery returns the query and its parameterized arguments for this aggregation.
	// Time values are passed as parameterized args to prevent SQL injection.
	GetQuery(ctx context.Context, params *UsageParams) (string, []interface{})

	// GetType returns the aggregation type
	GetType() types.AggregationType
}
