// Package aggregatedusage holds the hourly per-customer meter quantities kept in Redis
// for real-time usage reads, and the rules for which usage lands there.
package aggregatedusage

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

const (
	// LateWindow is how far behind now usage stays in Redis before the checkpoint job finalizes it.
	LateWindow = 3 * time.Hour

	// CutoffGrace delays the cutoff so events stamped just before an hour boundary
	// land and replicate in ClickHouse before the checkpoint job reads that hour.
	CutoffGrace = 10 * time.Minute

	// BucketTTL is how long an hourly bucket lives; an hour is read for at most ~4h.
	BucketTTL = 6 * time.Hour

	// ActiveSetTTL outlives the last checkpoint run that reads an hour's active set: that run's cutoff
	// is the hour plus LateWindow, and it fires LateWindow + CutoffGrace later. One hour of slack.
	ActiveSetTTL = 2*LateWindow + CutoffGrace + time.Hour
)

// Cutoff is the start of the oldest hour still kept in Redis. Usage stamped before it
// is finalized (or about to be) by the checkpoint job and must not touch Redis.
func Cutoff(now time.Time) time.Time {
	return now.UTC().Add(-LateWindow - CutoffGrace).Truncate(time.Hour)
}

// Supports reports whether a meter's usage can be kept as an hourly bucket.
func Supports(aggregation types.AggregationType) bool {
	switch aggregation {
	case types.AggregationSum, types.AggregationCount, types.AggregationMax:
		return true
	default:
		return false
	}
}

// Bucket is one customer's quantity for one meter in one UTC hour.
type Bucket struct {
	ExternalCustomerID string
	Hour               time.Time
	MeterID            string
	Aggregation        types.AggregationType
	Quantity           decimal.Decimal
}

// Store writes hourly buckets for the tenant and environment in ctx.
type Store interface {
	// Increment merges buckets into Redis: SUM and COUNT add, MAX keeps the higher value.
	Increment(ctx context.Context, buckets []Bucket) error
}
