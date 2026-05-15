package plan

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type Plan struct {
	ID             string               `db:"id" json:"id"`
	Name           string               `db:"name" json:"name"`
	LookupKey      string               `db:"lookup_key" json:"lookup_key"`
	Description    string               `db:"description" json:"description"`
	EnvironmentID  string               `db:"environment_id" json:"environment_id"`
	Metadata       types.Metadata       `db:"metadata" json:"metadata"`
	InvoiceCadence types.InvoiceCadence `db:"invoice_cadence" json:"invoice_cadence"`
	TrialPeriod    int                  `db:"trial_period" json:"trial_period"`
	DisplayOrder   *int                 `db:"display_order" json:"display_order,omitempty"`
	types.BaseModel
}

// PlanCloneOverrides holds optional overrides for CopyWith. Nil fields mean "keep existing value".
type PlanCloneOverrides struct {
	ID            *string
	Name          *string
	LookupKey     *string
	Description   *string
	EnvironmentID *string // nil = derive from ctx; non-nil = use explicit value
	Metadata      types.Metadata
	DisplayOrder  *int
	BaseModel     *types.BaseModel
}

// CopyWith returns a shallow copy of the plan with optional overrides applied.
// If BaseModel is not in overrides, uses types.GetDefaultBaseModel(ctx).
func (p *Plan) CopyWith(ctx context.Context, overrides *PlanCloneOverrides) *Plan {
	if p == nil {
		return nil
	}
	out := lo.FromPtr(p)
	if overrides == nil {
		return lo.ToPtr(out)
	}
	if overrides.ID != nil {
		out.ID = lo.FromPtr(overrides.ID)
	}
	if overrides.Name != nil {
		out.Name = lo.FromPtr(overrides.Name)
	}
	if overrides.LookupKey != nil {
		out.LookupKey = lo.FromPtr(overrides.LookupKey)
	}
	if overrides.Description != nil {
		out.Description = lo.FromPtr(overrides.Description)
	}
	if overrides.Metadata != nil {
		out.Metadata = overrides.Metadata
	}
	if overrides.DisplayOrder != nil {
		out.DisplayOrder = overrides.DisplayOrder
	}
	if overrides.BaseModel != nil {
		out.BaseModel = lo.FromPtr(overrides.BaseModel)
	} else {
		out.BaseModel = types.GetDefaultBaseModel(ctx)
	}
	// EnvironmentID is NOT part of BaseModel — set explicitly or fall back to context
	if overrides.EnvironmentID != nil {
		out.EnvironmentID = lo.FromPtr(overrides.EnvironmentID)
	} else {
		out.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return lo.ToPtr(out)
}

// FromEnt converts an Ent Plan to a domain Plan
func FromEnt(e *ent.Plan) *Plan {
	if e == nil {
		return nil
	}
	return &Plan{
		ID:             e.ID,
		Name:           e.Name,
		LookupKey:      e.LookupKey,
		Description:    e.Description,
		EnvironmentID:  e.EnvironmentID,
		Metadata:       types.Metadata(e.Metadata),
		InvoiceCadence: e.InvoiceCadence,
		TrialPeriod:    e.TrialPeriod,
		DisplayOrder:   &e.DisplayOrder,
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

// FromEntList converts a list of Ent Plans to domain Plans
func FromEntList(list []*ent.Plan) []*Plan {
	if list == nil {
		return nil
	}
	plans := make([]*Plan, len(list))
	for i, item := range list {
		plans[i] = FromEnt(item)
	}
	return plans
}
