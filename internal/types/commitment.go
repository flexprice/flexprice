package types

import (
	"fmt"
	"sort"
	"strings"

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

// TimeOfDayBucket defines a [StartHour, EndHour) half-open hour range within a UTC day.
// Hours are integers in [0, 24].
// When StartHour < EndHour: normal range (e.g. {0, 12} = midnight to noon).
// When StartHour >= EndHour: wraps midnight (e.g. {22, 6} = 10pm to 6am next day).
type TimeOfDayBucket struct {
	StartHour int `json:"start_hour"`
	EndHour   int `json:"end_hour"`
}

// ContainsHour reports whether the given UTC hour (0–23) falls within this bucket.
func (b TimeOfDayBucket) ContainsHour(hour int) bool {
	if b.StartHour < b.EndHour {
		return hour >= b.StartHour && hour < b.EndHour
	}
	// Midnight-wrapping: e.g. {22, 6} covers hours 22, 23, 0, 1, 2, 3, 4, 5
	return hour >= b.StartHour || hour < b.EndHour
}

// TimeOfDayBuckets is a slice of TimeOfDayBucket.
type TimeOfDayBuckets []TimeOfDayBucket

// ContainsHour reports whether the given UTC hour falls within any configured bucket.
func (bs TimeOfDayBuckets) ContainsHour(hour int) bool {
	for _, b := range bs {
		if b.ContainsHour(hour) {
			return true
		}
	}
	return false
}

// ToString returns a deterministic string suitable for use as a Go map key.
// Returns "" for an empty slice. Example: "0-12", "0-2,8-14".
func (bs TimeOfDayBuckets) ToString() string {
	if len(bs) == 0 {
		return ""
	}
	sorted := make(TimeOfDayBuckets, len(bs))
	copy(sorted, bs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartHour < sorted[j].StartHour
	})
	parts := make([]string, len(sorted))
	for i, b := range sorted {
		parts[i] = fmt.Sprintf("%d-%d", b.StartHour, b.EndHour)
	}
	return strings.Join(parts, ",")
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
