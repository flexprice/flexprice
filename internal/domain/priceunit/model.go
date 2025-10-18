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
	Precision      int             `json:"precision,omitempty"`
	EnvironmentID  string          `json:"environment_id,omitempty"`
	types.BaseModel
}

func FromEnt(ent *ent.PriceUnit) *PriceUnit {
	return &PriceUnit{
		ID:             ent.ID,
		Name:           ent.Name,
		Code:           ent.Code,
		Symbol:         ent.Symbol,
		BaseCurrency:   ent.BaseCurrency,
		ConversionRate: ent.ConversionRate,
		Precision:      ent.Precision,
		EnvironmentID:  ent.EnvironmentID,
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

// ConvertToFiatCurrencyAmount converts pricing unit amount to fiat currency
// Formula: fiat_amount = price_unit_amount * conversion_rate
// Example: 100 FP tokens * 0.01 USD/token = 1.00 USD
func ConvertToFiatCurrencyAmount(ctx context.Context, priceUnitAmount decimal.Decimal, conversionRate decimal.Decimal, precision int) (decimal.Decimal, error) {
	// Validate inputs
	if conversionRate.IsZero() || !conversionRate.IsPositive() {
		return decimal.Zero, ierr.NewError("conversion rate must be positive and non-zero").
			WithHint("Conversion rate must be positive and non-zero").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": conversionRate.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	if precision < 0 || precision > 8 {
		return decimal.Zero, ierr.NewError("precision must be between 0 and 8").
			WithHint("Precision must be between 0 and 8").
			WithReportableDetails(map[string]interface{}{
				"precision": precision,
			}).
			Mark(ierr.ErrValidation)
	}

	// Convert and round to specified precision
	result := priceUnitAmount.Mul(conversionRate)
	return result.Round(int32(precision)), nil
}

// ConvertToPriceUnitAmount converts fiat currency amount to pricing unit
// Formula: price_unit_amount = fiat_amount / conversion_rate
// Example: 1.00 USD / 0.01 USD/FP = 100 FP tokens
func ConvertToPriceUnitAmount(ctx context.Context, fiatAmount decimal.Decimal, conversionRate decimal.Decimal, precision int) (decimal.Decimal, error) {

	if conversionRate.IsZero() || !conversionRate.IsPositive() {
		return decimal.Zero, ierr.NewError("conversion rate must be positive and non-zero").
			WithHint("Conversion rate must be positive and non-zero").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": conversionRate.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	if precision < 0 || precision > 8 {
		return decimal.Zero, ierr.NewError("precision must be between 0 and 8").
			WithHint("Precision must be between 0 and 8").
			WithReportableDetails(map[string]interface{}{
				"precision": precision,
			}).
			Mark(ierr.ErrValidation)
	}

	// Convert and round to specified precision
	result := fiatAmount.Div(conversionRate)
	return result.Round(int32(precision)), nil
}
