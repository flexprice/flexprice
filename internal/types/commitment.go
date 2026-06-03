package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// CommitmentType defines how commitment is specified - either as an amount or quantity
type CommitmentType string

const (
	// COMMITMENT_TYPE_AMOUNT indicates commitment is specified as a monetary amount
	COMMITMENT_TYPE_AMOUNT CommitmentType = "amount"
	// COMMITMENT_TYPE_QUANTITY indicates commitment is specified as a usage quantity
	COMMITMENT_TYPE_QUANTITY CommitmentType = "quantity"
)

// Validate checks if the commitment type is valid
func (ct CommitmentType) Validate() bool {
	switch ct {
	case COMMITMENT_TYPE_AMOUNT, COMMITMENT_TYPE_QUANTITY:
		return true
	default:
		return false
	}
}

// String returns the string representation of the commitment type
func (ct CommitmentType) String() string {
	return string(ct)
}

// Bucket is a point in a UTC day expressed as Hour (0-24) and Minute (0-59).
// Hour=24 with Minute=0 is allowed so callers can express "end of day"
// (e.g. {Start: {0, 0}, End: {24, 0}} = the whole day).
type Bucket struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

// MinuteOfDay returns the bucket's position in the day as Hour*60 + Minute,
// in the range [0, 1440]. Used for ordering and overlap checks.
func (b Bucket) MinuteOfDay() int {
	return b.Hour*60 + b.Minute
}

// TimeOfDayBucket defines a [Start, End) half-open range within a UTC day.
// When Start.MinuteOfDay() < End.MinuteOfDay(): normal range (e.g. 09:00-17:00).
// When Start.MinuteOfDay() > End.MinuteOfDay(): wraps midnight (e.g. 22:00-06:00).
// When equal: empty range — matches nothing.
type TimeOfDayBucket struct {
	Start Bucket `json:"start"`
	End   Bucket `json:"end"`
}

// ContainsTime reports whether t falls within this bucket. The check uses the
// UTC hour and minute of t.
func (b TimeOfDayBucket) ContainsTime(t time.Time) bool {
	utc := t.UTC()
	cur := utc.Hour()*60 + utc.Minute()
	start := b.Start.MinuteOfDay()
	end := b.End.MinuteOfDay()
	if start == end {
		// Half-open [n, n) — empty range.
		return false
	}
	if start < end {
		return cur >= start && cur < end
	}
	// Midnight-wrapping: e.g. {22:00, 06:00} covers 22:00..23:59 and 00:00..05:59.
	return cur >= start || cur < end
}

// TimeOfDayBuckets is a slice of TimeOfDayBucket.
type TimeOfDayBuckets []TimeOfDayBucket

// ContainsTime reports whether t falls within any configured bucket.
func (bs TimeOfDayBuckets) ContainsTime(t time.Time) bool {
	for _, b := range bs {
		if b.ContainsTime(t) {
			return true
		}
	}
	return false
}

// CommitmentInfo holds information about a commitment
type CommitmentInfo struct {
	Type          CommitmentType   `json:"type"`
	Amount        decimal.Decimal  `json:"amount" swaggertype:"string"`
	Quantity      decimal.Decimal  `json:"quantity,omitempty" swaggertype:"string"` // Only used for quantity-based commitments
	Duration      BillingPeriod    `json:"duration,omitempty"`
	OverageFactor *decimal.Decimal `json:"overage_factor,omitempty" swaggertype:"string"`
	TrueUpEnabled bool             `json:"true_up_enabled"`
	IsWindowed    bool             `json:"is_windowed"`
	// total_cost = computed_commitment_utilized_amount + computed_overage_amount + computed_true_up_amount
	ComputedTrueUpAmount             decimal.Decimal `json:"computed_true_up_amount" swaggertype:"string"`
	ComputedOverageAmount            decimal.Decimal `json:"computed_overage_amount" swaggertype:"string"`
	ComputedCommitmentUtilizedAmount decimal.Decimal `json:"computed_commitment_utilized_amount" swaggertype:"string"`
}
