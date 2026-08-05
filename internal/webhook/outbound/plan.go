package outbound

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// PlanWebhookPayload is the minimal webhook representation of a plan. Deliberately
// excludes dto.PlanResponse's Prices/Entitlements/CreditGrants — those were the
// actual source of size bloat when a plan is nested inside a subscription payload.
type PlanWebhookPayload struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	LookupKey    string         `json:"lookup_key"`
	Description  string         `json:"description,omitempty"`
	DisplayOrder *int           `json:"display_order,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
	Status       string         `json:"status"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

// NewPlanWebhookPayload returns nil if resp is nil, so callers can assign the
// result directly to an optional nested field.
func NewPlanWebhookPayload(resp *dto.PlanResponse) *PlanWebhookPayload {
	if resp == nil || resp.Plan == nil {
		return nil
	}
	return &PlanWebhookPayload{
		ID:           resp.ID,
		Name:         resp.Name,
		LookupKey:    resp.LookupKey,
		Description:  resp.Description,
		DisplayOrder: resp.DisplayOrder,
		Metadata:     resp.Metadata,
		Status:       string(resp.Status),
		CreatedAt:    resp.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:    resp.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
