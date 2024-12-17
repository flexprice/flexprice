package price

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// JSONB types for complex fields
type JSONBTiers []PriceTier
type JSONBTransformQuantity TransformQuantity
type JSONBMetadata map[string]string
type JSONBFilters map[string][]string

// Price model with JSONB tags
type Price struct {
	// ID uuid identifier for the price
	ID string `db:"id" json:"id"`

	// Amount stored in main currency units (e.g., dollars, not cents)
	// For USD: 12.50 means $12.50
	Amount decimal.Decimal `db:"amount" json:"-"`

	// DisplayAmount is the formatted amount with currency symbol
	// For USD: $12.50
	DisplayAmount string `db:"display_amount" json:"amount"`

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
	TransformQuantity JSONBTransformQuantity `db:"transform_quantity,jsonb" json:"transform_quantity"` // JSONB field

	// Metadata is a jsonb field for additional information
	Metadata JSONBMetadata `db:"metadata,jsonb" json:"metadata"` // JSONB field

	types.BaseModel
}

// GetCurrencySymbol returns the currency symbol for the price
func (p *Price) GetCurrencySymbol() string {
	return types.GetCurrencySymbol(p.Currency)
}

// ValidateAmount checks if amount is within valid range for price definition
func (p *Price) ValidateAmount() error {
	if p.Amount.LessThan(decimal.Zero) {
		return fmt.Errorf("amount must be greater than 0")
	}
	return nil
}

// FormatAmountToString formats the amount to string
// It rounds off the amount according to currency precision
func (p *Price) FormatAmountToString() string {
	config := types.GetCurrencyConfig(p.Currency)
	return p.Amount.Round(config.Precision).String()
}

// FormatAmountToFloat64 formats the amount to float64
// It rounds off the amount according to currency precision
func (p *Price) FormatAmountToFloat64() float64 {
	return p.Amount.Round(types.GetCurrencyPrecision(p.Currency)).InexactFloat64()
}

// GetDisplayAmount returns the amount in the currency ex $12.00
func (p *Price) GetDisplayAmount() string {
	amount := p.FormatAmountToString()
	return fmt.Sprintf("%s%s", p.GetCurrencySymbol(), amount)
}

// CalculateAmountWithPrecision performs calculation with proper rounding
func (p *Price) CalculateAmountWithPrecision(quantity decimal.Decimal) decimal.Decimal {
	// Calculate with full precision
	result := p.Amount.Mul(quantity)
	return result.Round(types.GetCurrencyPrecision(p.Currency))
}

// CalculateTierAmountWithPrecision performs calculation with proper rounding
func (pt *PriceTier) CalculateTierAmountWithPrecision(quantity decimal.Decimal, currency string) decimal.Decimal {
	// Calculate tier cost with proper rounding
	tierCost := pt.UnitAmount.Mul(quantity)
	if pt.FlatAmount != nil {
		tierCost = tierCost.Add(*pt.FlatAmount)
	}
	return tierCost.Round(types.GetCurrencyPrecision(currency))
}

// GetDisplayAmount returns the amount in the currency ex $12.00
func GetDisplayAmount(amount decimal.Decimal, currency string) string {
	p := &Price{Amount: amount, Currency: currency}
	return p.GetDisplayAmount()
}

// FormatAmountToString formats the amount to string
// It rounds off the amount according to currency precision
func FormatAmountToString(amount decimal.Decimal, currency string) string {
	config := types.GetCurrencyConfig(currency)
	return amount.Round(config.Precision).String()
}

// FormatAmountToFloat64 formats the amount to float64
// It rounds off the amount according to currency precision
func FormatAmountToFloat64(amount decimal.Decimal, currency string) float64 {
	return amount.Round(types.GetCurrencyPrecision(currency)).InexactFloat64()
}

// CalculateAmountWithPrecision performs calculation with proper rounding
func CalculateAmountWithPrecision(amount decimal.Decimal, currency string, quantity decimal.Decimal) decimal.Decimal {
	p := &Price{Amount: amount, Currency: currency}
	return p.CalculateAmountWithPrecision(quantity)
}

// PriceTransform is the quantity transformation in case of PACKAGE billing model
// NOTE: We need to apply this to the quantity before calculating the effective price
type TransformQuantity struct {
	DivideBy int    `json:"divide_by,omitempty"` // Divide quantity by this number
	Round    string `json:"round,omitempty"`     // up or down
}

type PriceTier struct {
	// Upto is the quantity up to which this tier applies. It is null for the last tier
	UpTo *uint64 `json:"up_to"`
	// UnitAmount is the amount per unit for the given tier
	UnitAmount decimal.Decimal `json:"unit_amount"`
	// FlatAmount is the flat amount for the given tier and it is applied
	// on top of the unit amount*quantity. It solves cases in banking like 2.7% + 5c
	FlatAmount *decimal.Decimal `json:"flat_amount,omitempty"`
}

// TODO : comeup with a better way to handle jsonb fields

// Scanner/Valuer implementations for JSONBTiers
func (j *JSONBTiers) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb tiers")
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONBTiers) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// GetTierUpTo returns the up_to value for the tier and treats null case as MaxUint64.
// NOTE: Only to be used for sorting of tiers to avoid any unexpected behaviour
func (t PriceTier) GetTierUpTo() uint64 {
	if t.UpTo != nil {
		return *t.UpTo
	}
	return math.MaxUint64
}

// Scanner/Valuer implementations for JSONBTransform
func (j *JSONBTransformQuantity) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb transform")
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONBTransformQuantity) Value() (driver.Value, error) {
	if j == (JSONBTransformQuantity{}) {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scanner/Valuer implementations for JSONBMetadata
func (j *JSONBMetadata) Scan(value interface{}) error {
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

func (j *JSONBFilters) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid type for jsonb filters")
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONBFilters) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}
