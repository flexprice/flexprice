package types

import "github.com/shopspring/decimal"

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

// CommitmentDuration defines the time frame of a commitment.
// For example, a monthly subscription may have an annual or quarterly commitment.
// Uses the same values as BillingPeriod (MONTHLY, ANNUAL, QUARTERLY, etc.)
type CommitmentDuration string

const (
	COMMITMENT_DURATION_MONTHLY   CommitmentDuration = "MONTHLY"
	COMMITMENT_DURATION_ANNUAL    CommitmentDuration = "ANNUAL"
	COMMITMENT_DURATION_WEEKLY    CommitmentDuration = "WEEKLY"
	COMMITMENT_DURATION_DAILY     CommitmentDuration = "DAILY"
	COMMITMENT_DURATION_QUARTERLY CommitmentDuration = "QUARTERLY"
	COMMITMENT_DURATION_HALF_YEAR CommitmentDuration = "HALF_YEARLY"
)

// Validate checks if the commitment duration is valid
func (cd CommitmentDuration) Validate() bool {
	switch cd {
	case COMMITMENT_DURATION_MONTHLY, COMMITMENT_DURATION_ANNUAL,
		COMMITMENT_DURATION_WEEKLY, COMMITMENT_DURATION_DAILY,
		COMMITMENT_DURATION_QUARTERLY, COMMITMENT_DURATION_HALF_YEAR:
		return true
	default:
		return false
	}
}

// String returns the string representation of the commitment duration
func (cd CommitmentDuration) String() string {
	return string(cd)
}

// CommitmentInfo holds information about a commitment
type CommitmentInfo struct {
	Type          CommitmentType     `json:"type"`
	Amount        decimal.Decimal    `json:"amount" swaggertype:"string"`
	Quantity      decimal.Decimal    `json:"quantity,omitempty" swaggertype:"string"` // Only used for quantity-based commitments
	Duration      CommitmentDuration `json:"duration,omitempty"`
	OverageFactor *decimal.Decimal   `json:"overage_factor,omitempty" swaggertype:"string"`
	TrueUpEnabled bool               `json:"true_up_enabled"`
	IsWindowed    bool               `json:"is_windowed"`
	// total_cost = computed_commitment_utilized_amount + computed_overage_amount + computed_true_up_amount
	ComputedTrueUpAmount             decimal.Decimal `json:"computed_true_up_amount" swaggertype:"string"`
	ComputedOverageAmount            decimal.Decimal `json:"computed_overage_amount" swaggertype:"string"`
	ComputedCommitmentUtilizedAmount decimal.Decimal `json:"computed_commitment_utilized_amount" swaggertype:"string"`
}
