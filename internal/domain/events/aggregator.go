package events

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Aggregator interface {
	// GetQuery returns the query for this aggregation, along with the
	// positional args to bind against the returned query's `?` placeholders
	// (in the exact order the placeholders appear in the query string).
	GetQuery(ctx context.Context, params *UsageParams) (string, []interface{})

	// GetType returns the aggregation type
	GetType() types.AggregationType
}
