package usagerollup

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type bucketKey struct {
	externalCustomerID string
	hour               time.Time
	meterID            string
	aggregation        types.AggregationType
}

// Aggregator folds a batch of usage rows into hourly buckets so a batch becomes
// a handful of Redis writes instead of one per row.
type Aggregator struct {
	cutoff     time.Time
	quantities map[bucketKey]decimal.Decimal
	tooOld     int
	rowCount   int
}

// NewAggregator returns an Aggregator that drops rows stamped before cutoff.
func NewAggregator(cutoff time.Time) *Aggregator {
	return &Aggregator{
		cutoff:     cutoff,
		quantities: make(map[bucketKey]decimal.Decimal),
	}
}

// Add folds one usage row in. Rows of unsupported meters are ignored.
// COUNT adds one per row, matching ClickHouse's COUNT(DISTINCT id).
func (a *Aggregator) Add(
	externalCustomerID string,
	timestamp time.Time,
	meterID string,
	aggregation types.AggregationType,
	bucketSize types.WindowSize,
	qty decimal.Decimal,
) {
	if a == nil || externalCustomerID == "" || !Supports(aggregation, bucketSize) {
		return
	}
	if timestamp.Before(a.cutoff) {
		a.tooOld++
		return
	}

	key := bucketKey{
		externalCustomerID: externalCustomerID,
		hour:               timestamp.UTC().Truncate(time.Hour),
		meterID:            meterID,
		aggregation:        aggregation,
	}
	current, seen := a.quantities[key]
	a.quantities[key] = merge(aggregation, current, seen, qty)
	a.rowCount++
}

func merge(aggregation types.AggregationType, current decimal.Decimal, seen bool, qty decimal.Decimal) decimal.Decimal {
	switch aggregation {
	case types.AggregationCount:
		return current.Add(decimal.NewFromInt(1))
	case types.AggregationMax:
		if !seen || qty.GreaterThan(current) {
			return qty
		}
		return current
	default:
		return current.Add(qty)
	}
}

// Buckets returns the folded buckets in no particular order.
func (a *Aggregator) Buckets() []Bucket {
	if a == nil {
		return nil
	}
	buckets := make([]Bucket, 0, len(a.quantities))
	for key, quantity := range a.quantities {
		buckets = append(buckets, Bucket{
			ExternalCustomerID: key.externalCustomerID,
			Hour:               key.hour,
			MeterID:            key.meterID,
			Aggregation:        key.aggregation,
			Quantity:           quantity,
		})
	}
	return buckets
}

// RowCount is the number of rows folded into buckets.
func (a *Aggregator) RowCount() int {
	if a == nil {
		return 0
	}
	return a.rowCount
}

// TooOld is the number of supported rows dropped for being stamped before the cutoff.
func (a *Aggregator) TooOld() int {
	if a == nil {
		return 0
	}
	return a.tooOld
}
