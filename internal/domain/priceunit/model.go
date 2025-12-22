package priceunit

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// PriceUnit is the model entity for the PriceUnit schema.
type PriceUnit struct {
	ID             string          `json:"id,omitempty"`
	Name           string          `json:"name,omitempty"`
	Code           string          `json:"code,omitempty"`
	Symbol         string          `json:"symbol,omitempty"`
	BaseCurrency   string          `json:"base_currency,omitempty"`
	ConversionRate decimal.Decimal `json:"conversion_rate,omitempty"`
	EnvironmentID  string          `json:"environment_id,omitempty"`
	Metadata       types.Metadata  `json:"metadata,omitempty"`
	types.BaseModel
}

func FromEnt(ent *ent.PriceUnit) *PriceUnit {
	if ent == nil {
		return nil
	}

	return &PriceUnit{
		ID:             ent.ID,
		Name:           ent.Name,
		Code:           ent.Code,
		Symbol:         ent.Symbol,
		BaseCurrency:   ent.BaseCurrency,
		ConversionRate: ent.ConversionRate,
		EnvironmentID:  ent.EnvironmentID,
		Metadata:       ent.Metadata,
		BaseModel: types.BaseModel{
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
			Status:    types.Status(ent.Status),
			TenantID:  ent.TenantID,
		},
	}
}

func FromEntList(ents []*ent.PriceUnit) []*PriceUnit {
	return lo.Map(ents, func(ent *ent.PriceUnit, _ int) *PriceUnit {
		return FromEnt(ent)
	})
}

// validateConversionRate validates that the conversion rate is positive and non-zero.
// Returns an error if validation fails, nil otherwise.
func validateConversionRate(conversionRate decimal.Decimal) error {
	if conversionRate.IsZero() || !conversionRate.IsPositive() {
		return ierr.NewError("conversion rate must be positive and non-zero").
			WithHint("Conversion rate must be positive and non-zero").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": conversionRate.String(),
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ConvertToFiatCurrencyAmount converts pricing unit amount to fiat currency.
// The result is rounded to the precision of the base currency (e.g., 2 decimal places for USD, 0 for JPY).
//
// Formula: fiat_amount = price_unit_amount * conversion_rate
//
// Example: 100 FP tokens * 0.01 USD/token = 1.00 USD
//
// Rounding strategy: Uses base currency precision to ensure consistent precision in stored values
// and match industry standards (Stripe, Lago use currency precision).
func ConvertToFiatCurrencyAmount(ctx context.Context, priceUnitAmount decimal.Decimal, conversionRate decimal.Decimal, baseCurrency string) (decimal.Decimal, error) {
	// Validate conversion rate
	if err := validateConversionRate(conversionRate); err != nil {
		return decimal.Zero, err
	}

	// Convert and round to base currency precision
	result := priceUnitAmount.Mul(conversionRate)
	return result.Round(int32(types.GetCurrencyPrecision(baseCurrency))), nil
}

// ConvertToPriceUnitAmount converts fiat currency amount to pricing unit.
// The result is rounded to the precision of the base currency (e.g., 2 decimal places for USD, 0 for JPY).
//
// Formula: price_unit_amount = fiat_amount / conversion_rate
//
// Example: 1.00 USD / 0.01 USD/FP = 100 FP tokens
//
// Rounding strategy: Uses base currency precision to ensure consistent precision in stored values.
// Price units inherit precision from their base currency, simplifying the model.
func ConvertToPriceUnitAmount(ctx context.Context, fiatAmount decimal.Decimal, conversionRate decimal.Decimal, baseCurrency string) (decimal.Decimal, error) {
	// Validate conversion rate
	if err := validateConversionRate(conversionRate); err != nil {
		return decimal.Zero, err
	}

	// Convert and round to base currency precision
	result := fiatAmount.Div(conversionRate)
	return result.Round(int32(types.GetCurrencyPrecision(baseCurrency))), nil
}
