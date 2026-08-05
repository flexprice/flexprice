package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// Plan is the minimal webhook representation of a plan. Deliberately excludes
// dto.PlanResponse's Prices/Entitlements/CreditGrants — those were the actual
// source of size bloat when a plan is nested inside a subscription payload.
type Plan struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	LookupKey    string         `json:"lookup_key"`
	Description  string         `json:"description,omitempty"`
	DisplayOrder *int           `json:"display_order,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

// NewPlan returns nil if resp is nil, so callers can assign the result
// directly to an optional nested field.
func NewPlan(resp *dto.PlanResponse) *Plan {
	if resp == nil || resp.Plan == nil {
		return nil
	}
	return &Plan{
		ID:           resp.ID,
		Name:         resp.Name,
		LookupKey:    resp.LookupKey,
		Description:  resp.Description,
		DisplayOrder: resp.DisplayOrder,
		Metadata:     resp.Metadata,
	}
}
