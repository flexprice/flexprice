package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type Plan struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	LookupKey    string         `json:"lookup_key"`
	Description  string         `json:"description,omitempty"`
	DisplayOrder *int           `json:"display_order,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

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
