package addon

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type SubscriptionAddon struct {
	ID                 string                 `json:"id,omitempty"`
	EnvironmentID      string                 `json:"environment_id,omitempty"`
	SubscriptionID     string                 `json:"subscription_id,omitempty"`
	AddonID            string                 `json:"addon_id,omitempty"`
	StartDate          *time.Time             `json:"start_date,omitempty"`
	EndDate            *time.Time             `json:"end_date,omitempty"`
	AddonStatus        types.AddonStatus      `json:"addon_status,omitempty"`
	CancellationReason string                 `json:"cancellation_reason,omitempty"`
	CancelledAt        *time.Time             `json:"cancelled_at,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	types.BaseModel
}

func (s *SubscriptionAddon) FromEnt(entAddon *ent.SubscriptionAddon) *SubscriptionAddon {
	return &SubscriptionAddon{
		ID:                 entAddon.ID,
		EnvironmentID:      entAddon.EnvironmentID,
		SubscriptionID:     entAddon.SubscriptionID,
		AddonID:            entAddon.AddonID,
		StartDate:          entAddon.StartDate,
		EndDate:            entAddon.EndDate,
		AddonStatus:        types.AddonStatus(entAddon.AddonStatus),
		CancellationReason: entAddon.CancellationReason,
		CancelledAt:        entAddon.CancelledAt,
		Metadata:           entAddon.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  entAddon.TenantID,
			Status:    types.Status(entAddon.Status),
			CreatedAt: entAddon.CreatedAt,
			UpdatedAt: entAddon.UpdatedAt,
			CreatedBy: entAddon.CreatedBy,
			UpdatedBy: entAddon.UpdatedBy,
		},
	}
}

func (s *SubscriptionAddon) FromEntList(entAddons []*ent.SubscriptionAddon) []*SubscriptionAddon {
	return lo.Map(entAddons, func(entAddon *ent.SubscriptionAddon, _ int) *SubscriptionAddon {
		return s.FromEnt(entAddon)
	})
}
