package fxrate

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// FXRate is a tenant-configured fixed exchange rate. to = from × rate.
type FXRate struct {
	ID            string             `json:"id,omitempty"`
	EnvironmentID string             `json:"environment_id,omitempty"`
	Scope         types.FXRateScope  `json:"scope,omitempty"`
	ScopeID       string             `json:"scope_id,omitempty"`
	FromCurrency  string             `json:"from_currency,omitempty"`
	ToCurrency    string             `json:"to_currency,omitempty"`
	Rate          decimal.Decimal    `json:"rate" swaggertype:"string"`
	Source        types.FXRateSource `json:"source,omitempty"`
	StartDate     *time.Time         `json:"start_date,omitempty"`
	EndDate       *time.Time         `json:"end_date,omitempty"`
	Metadata      map[string]string  `json:"metadata,omitempty"`
	types.BaseModel
}

// FromEnt converts an Ent FXRate to a domain FXRate.
func FromEnt(e *ent.FXRate) *FXRate {
	if e == nil {
		return nil
	}
	return &FXRate{
		ID:            e.ID,
		EnvironmentID: e.EnvironmentID,
		Scope:         e.Scope,
		ScopeID:       e.ScopeID,
		FromCurrency:  e.FromCurrency,
		ToCurrency:    e.ToCurrency,
		Rate:          e.Rate,
		Source:        e.Source,
		StartDate:     e.StartDate,
		EndDate:       e.EndDate,
		Metadata:      e.Metadata,
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

// FromEntList converts a list of Ent FXRates to domain FXRates.
func FromEntList(list []*ent.FXRate) []*FXRate {
	if list == nil {
		return nil
	}
	return lo.Map(list, func(item *ent.FXRate, _ int) *FXRate {
		return FromEnt(item)
	})
}
