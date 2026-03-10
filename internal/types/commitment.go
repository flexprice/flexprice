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

	// BaseUsageCost is the original usage cost before any commitment adjustment was applied.
	// Used by subscription-level cumulative commitment to reconstruct actual usage from invoices.
	BaseUsageCost decimal.Decimal `json:"base_usage_cost,omitempty" swaggertype:"string"`

	// Cumulative tracking fields (populated only for cross-period commitments, e.g. ANNUAL commitment on MONTHLY subscription)
	IsCumulative                bool            `json:"is_cumulative,omitempty"`
	CumulativeUsageCost         decimal.Decimal `json:"cumulative_usage_cost,omitempty" swaggertype:"string"`
	PreviousCumulativeUsageCost decimal.Decimal `json:"previous_cumulative_usage_cost,omitempty" swaggertype:"string"`
}
