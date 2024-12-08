package price

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"

	"github.com/flexprice/flexprice/internal/types"
)

// JSONB types for complex fields
type JSONBTiers []PriceTier
type JSONBTransform PriceTransform
type JSONBMetadata map[string]string
type JSONBFilters map[string][]string

// Price model with JSONB tags
type Price struct {
	// ID uuid identifier for the price
	ID string `db:"id" json:"id"`

	// Amount in cents ex 1200 for $12
	Amount uint64 `db:"amount" json:"amount"`

	// DisplayAmount is the amount in the currency ex $12.00
	DisplayAmount string `db:"display_amount" json:"display_amount"`

	// Currency 3 digit ISO currency code in lowercase ex usd, eur, gbp
	Currency string `db:"currency" json:"currency"`

	// PlanID is the id of the plan for plan based pricing
	PlanID string `db:"plan_id" json:"plan_id"`

	// Type is the type of the price ex USAGE, FIXED
	Type types.PriceType `db:"type" json:"type"`

	// BillingPeriod is the billing period for the price ex month, year
	BillingPeriod types.BillingPeriod `db:"billing_period" json:"billing_period"`

	// BillingPeriodCount is the count of the billing period ex 1, 3, 6, 12
	BillingPeriodCount int `db:"billing_period_count" json:"billing_period_count"`

	// BillingModel is the billing model for the price ex FLAT_FEE, PACKAGE, TIERED
	BillingModel types.BillingModel `db:"billing_model" json:"billing_model"`

	// BillingCadence is the billing cadence for the price ex RECURRING, ONETIME
	BillingCadence types.BillingCadence `db:"billing_cadence" json:"billing_cadence"`

	// Tiered pricing fields when BillingModel is TIERED
	TierMode types.BillingTier `db:"tier_mode" json:"tier_mode"`

	// Tiers are the tiers for the price when BillingModel is TIERED
	Tiers JSONBTiers `db:"tiers,jsonb" json:"tiers"` // JSONB field

	// MeterID is the id of the meter for usage based pricing
	MeterID string `db:"meter_id" json:"meter_id"`

	// LookupKey used for looking up the price in the database
	LookupKey string `db:"lookup_key" json:"lookup_key"`

	// Description of the price
	Description string `db:"description" json:"description"`

	// FilterValues are the filter values for the price in case of usage based pricing
	FilterValues JSONBFilters `db:"filter_values,jsonb" json:"filter_values"`

	// Transform is the quantity transformation in case of PACKAGE billing model
	Transform JSONBTransform `db:"transform,jsonb" json:"transform"` // JSONB field

	// Metadata is a jsonb field for additional information
	Metadata JSONBMetadata `db:"metadata,jsonb" json:"metadata"` // JSONB field

	types.BaseModel
}

func (p *Price) GetCurrencySymbol() string {
	return types.GetCurrencySymbol(p.Currency)
}

func (p *Price) GetDisplayAmount() string {
	return fmt.Sprintf("%s%.2f", p.GetCurrencySymbol(), float64(p.Amount)/100.0)
}

func GetDisplayAmount(amount uint64, currency string) string {
	price := &Price{
		Amount:   amount,
		Currency: currency,
	}
	return price.GetDisplayAmount()
}

func GetAmountInDollars(amount uint64) float64 {
	return float64(amount) / 100.0
}

func GetAmountInCents(amount float64) uint64 {
	// round to 2 decimal places
	amountFloat := math.Round(amount*100) / 100
	return uint64(amountFloat)
}

type PriceTransform struct {
	DivideBy int    `json:"divide_by,omitempty"` // Divide quantity by this number
	Round    string `json:"round,omitempty"`     // up, down, or nearest
}

type PriceTier struct {
	UpTo       *int `json:"up_to"`                 // null means infinity
	UnitAmount int  `json:"unit_amount"`           // Amount per unit in cents
	FlatAmount *int `json:"flat_amount,omitempty"` // Optional flat fee for this tier
}

// TODO : comeup with a better way to handle jsonb fields

// Scanner/Valuer implementations for JSONBTiers
func (j JSONBTiers) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb tiers")
	}
	return json.Unmarshal(bytes, &j)
}

func (j JSONBTiers) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// func (j JSONBTiers) GetTierUpTo(i int) int {
// 	if i >= len(j) {
// 		return math.MaxInt
// 	}

// 	tier := j[i]
// 	if tier.UpTo != nil {
// 		return *tier.UpTo
// 	}
// 	return math.MaxInt
// }

func (t PriceTier) GetTierUpTo() int {
	if t.UpTo != nil {
		return *t.UpTo
	}
	return math.MaxInt
}

// Scanner/Valuer implementations for JSONBTransform
func (j *JSONBTransform) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb transform")
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONBTransform) Value() (driver.Value, error) {
	if j == (JSONBTransform{}) {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scanner/Valuer implementations for JSONBMetadata
func (j JSONBMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb metadata")
	}
	return json.Unmarshal(bytes, &j)
}

func (j JSONBMetadata) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j JSONBFilters) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb filters")
	}
	return json.Unmarshal(bytes, &j)
}

func (j JSONBFilters) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
