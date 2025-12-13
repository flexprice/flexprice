package taxrate

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type TaxRate struct {
	ID              string              `json:"id,omitempty"`
	EnvironmentID   string              `json:"environment_id,omitempty"`
	Name            string              `json:"name,omitempty"`
	Description     string              `json:"description,omitempty"`
	Code            string              `json:"code,omitempty"`
	TaxRateStatus   types.TaxRateStatus `json:"tax_rate_status,omitempty"`
	TaxRateType     types.TaxRateType   `json:"tax_rate_type,omitempty"`
	Scope           types.TaxRateScope  `json:"scope,omitempty"`
	PercentageValue *decimal.Decimal    `json:"percentage_value,omitempty" swaggertype:"string"`
	FixedValue      *decimal.Decimal    `json:"fixed_value,omitempty" swaggertype:"string"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
	types.BaseModel
}

// FromEnt converts an Ent TaxRate to a domain TaxRate
func FromEnt(e *ent.TaxRate) *TaxRate {
	if e == nil {
		return nil
	}
	return &TaxRate{
		ID:              e.ID,
		Name:            e.Name,
		Description:     e.Description,
		Code:            e.Code,
		TaxRateStatus:   types.TaxRateStatus(e.TaxRateStatus),
		TaxRateType:     types.TaxRateType(e.TaxRateType),
		Scope:           types.TaxRateScope(e.Scope),
		PercentageValue: e.PercentageValue,
		FixedValue:      e.FixedValue,
		EnvironmentID:   e.EnvironmentID,
		Metadata:        e.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of Ent TaxRates to a list of domain TaxRates
func FromEntList(list []*ent.TaxRate) []*TaxRate {
	if list == nil {
		return nil
	}

	return lo.Map(list, func(item *ent.TaxRate, _ int) *TaxRate {
		return FromEnt(item)
	})
}
