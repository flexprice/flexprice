package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

// TimeOfDayBucket defines a [Start, End) half-open range within a UTC day with
// its own commitment + base price. The bucket overrides the line item's
// price/commitment for any window whose start falls inside [Start, End).
//
// Every bucket must carry a commitment (type + value) and a price — filter-only
// buckets are not supported.
type TimeOfDayBucket struct {
	// ID is server-assigned. Stable for the lifetime of the line item;
	// invoice breakdown and analytics responses reference this ID.
	ID    string `json:"id,omitempty"`
	Start Bucket `json:"start"`
	End   Bucket `json:"end"`

	// PriceID is the SUBSCRIPTION-scoped price created at bucket-creation time.
	// Immutable post-create; changing pricing requires a successor line item.
	PriceID string `json:"price_id,omitempty"`

	CommitmentType  CommitmentType   `json:"commitment_type,omitempty"`
	CommitmentValue decimal.Decimal  `json:"commitment_value" swaggertype:"string"`
	OverageFactor   *decimal.Decimal `json:"overage_factor,omitempty"`
	TrueUpEnabled   bool             `json:"true_up_enabled,omitempty"`
}

// HasCommitment reports whether the bucket carries a valid commitment config.
// Valid buckets always do; this is a defensive guard for the billing path.
func (b TimeOfDayBucket) HasCommitment() bool {
	return b.CommitmentType != "" && b.CommitmentValue.GreaterThan(decimal.Zero)
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

// Validate reports per-field issues with the bucket. It does NOT enforce
// array-level invariants (overlap, window alignment) — those live in the
// commitment_bucket_validation file and require external context.
//
// Every bucket must carry a commitment (type + value > 0) and an overage factor
// greater than 1.0 — filter-only buckets are not supported.
func (b TimeOfDayBucket) Validate() error {
	// Start/End point sanity is enforced by the DTO layer (validateBucketPoint),
	// but we still need start != end at the type level.
	if b.Start.Hour == b.End.Hour && b.Start.Minute == b.End.Minute {
		return ierr.NewError("bucket start must differ from end").
			WithHint("Empty buckets are not allowed").
			Mark(ierr.ErrValidation)
	}
	if b.CommitmentType == "" || !b.CommitmentType.Validate() {
		return ierr.NewError("commitment_type is required").
			WithHint(`Set commitment_type to "amount" or "quantity" on every bucket`).
			Mark(ierr.ErrValidation)
	}
	if !b.CommitmentValue.GreaterThan(decimal.Zero) {
		return ierr.NewError("commitment_value must be > 0").
			WithHint("Provide a positive decimal value for commitment_value").
			Mark(ierr.ErrValidation)
	}
	// Overage factor is required and must be a premium over the committed rate,
	// matching the line-item rule.
	if b.OverageFactor == nil {
		return ierr.NewError("overage_factor is required").
			WithHint("Specify an overage_factor greater than 1.0 on every bucket").
			Mark(ierr.ErrValidation)
	}
	if b.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond the bucket commitment").
			Mark(ierr.ErrValidation)
	}
	return nil
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
