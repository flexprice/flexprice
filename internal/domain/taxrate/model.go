package taxrate

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type TaxRate struct {
	ID            string    `db:"id" json:"id"`
	Name          string    `db:"name" json:"name"`
	Code          string    `db:"code" json:"code"`
	Description   string    `db:"description" json:"description"`
	EnvironmentID string    `db:"environment_id" json:"environment_id"`
	Percentage    float64   `db:"percentage" json:"percentage"`
	FixedValue    float64   `db:"fixed_value" json:"fixed_value"`
	IsCompound    bool      `db:"is_compound" json:"is_compound"`
	ValidFrom     time.Time `db:"valid_from" json:"valid_from"`
	ValidTo       time.Time `db:"valid_to" json:"valid_to"`
	types.BaseModel
}

// FromEnt converts an Ent TaxRate to a domain TaxRate
func FromEnt(e *ent.TaxRate) *TaxRate {
	if e == nil {
		return nil
	}
	return &TaxRate{
		ID:            e.ID,
		Name:          e.Name,
		Description:   e.Description,
		Code:          e.Code,
		Percentage:    e.Percentage,
		FixedValue:    e.FixedValue,
		IsCompound:    e.IsCompound,
		ValidFrom:     lo.FromPtr(e.ValidFrom),
		ValidTo:       lo.FromPtr(e.ValidTo),
		EnvironmentID: e.EnvironmentID,
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
