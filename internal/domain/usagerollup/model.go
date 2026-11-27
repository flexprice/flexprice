// Package usagerollup holds the hourly per-customer meter quantities kept in Redis
// for real-time usage reads, and the rules for which usage lands there.
package usagerollup

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

	// HourlyUsageTTL is how long an hour's usage lives in Redis; an hour is read for at most ~4h.
	HourlyUsageTTL = 6 * time.Hour
)

// Cutoff is the start of the oldest hour still kept in Redis. Usage stamped before it
// is finalized (or about to be) by the checkpoint job and must not touch Redis.
func Cutoff(now time.Time) time.Time {
	return now.UTC().Add(-LateWindow - CutoffGrace).Truncate(time.Hour)
}

// OldestRedisHour is the oldest hour whose usage is certain to still be in Redis: the TTL runs from
// the last write, which is no earlier than the hour's start. Older hours may have expired.
func OldestRedisHour(now time.Time) time.Time {
	return now.UTC().Truncate(time.Hour).Add(-(HourlyUsageTTL - time.Hour))
}

// SupportsAggregation reports whether usage with this aggregation can be kept as hourly usage in Redis.
func SupportsAggregation(aggregation types.AggregationType) bool {
	switch aggregation {
	case types.AggregationSum, types.AggregationCount, types.AggregationMax:
		return true
	default:
		return false
	}
}

// HourlyUsage is one customer's quantity for one meter in one UTC hour. For COUNT the quantity
// is the number of events.
type HourlyUsage struct {
	ExternalCustomerID string
	Hour               time.Time
	MeterID            string
	Aggregation        types.AggregationType
	Quantity           decimal.Decimal
}

// Repository keeps hourly usage in Redis for the tenant and environment in ctx.
type Repository interface {
	// Increment merges usage into Redis: SUM and COUNT add, MAX keeps the higher value. Entries for
	// the same customer, meter and hour may repeat.
	Increment(ctx context.Context, usage []HourlyUsage) error
	// ListCustomerHourlyUsage reads one customer's usage for the given hours in one round trip.
	ListCustomerHourlyUsage(ctx context.Context, externalCustomerID string, hours []time.Time) ([]HourlyUsage, error)
	// ListActiveCustomers returns the external customer IDs with usage in the UTC hour starting at hour.
	ListActiveCustomers(ctx context.Context, hour time.Time) ([]string, error)
}
