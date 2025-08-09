package addonassociation

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// AddonAssociation is the model entity for the AddonAssociation schema.
type AddonAssociation struct {
	ID                 string                           `json:"id,omitempty"`
	EnvironmentID      string                           `json:"environment_id,omitempty"`
	EntityID           string                           `json:"entity_id,omitempty"`
	EntityType         types.AddonAssociationEntityType `json:"entity_type,omitempty"`
	AddonID            string                           `json:"addon_id,omitempty"`
	StartDate          *time.Time                       `json:"start_date,omitempty"`
	EndDate            *time.Time                       `json:"end_date,omitempty"`
	AddonStatus        types.AddonStatus                `json:"addon_status,omitempty"`
	CancellationReason string                           `json:"cancellation_reason,omitempty"`
	CancelledAt        *time.Time                       `json:"cancelled_at,omitempty"`
	Metadata           map[string]interface{}           `json:"metadata,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.AddonAssociation) *AddonAssociation {
	return &AddonAssociation{
		ID:                 ent.ID,
		EnvironmentID:      ent.EnvironmentID,
		EntityID:           ent.EntityID,
		EntityType:         types.AddonAssociationEntityType(ent.EntityType),
		AddonID:            ent.AddonID,
		StartDate:          ent.StartDate,
		EndDate:            ent.EndDate,
		AddonStatus:        types.AddonStatus(ent.AddonStatus),
		CancellationReason: ent.CancellationReason,
		CancelledAt:        ent.CancelledAt,
		Metadata:           ent.Metadata,
		BaseModel: types.BaseModel{
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
			TenantID:  ent.TenantID,
			Status:    types.Status(ent.Status),
		},
	}
}

func FromEntList(ents []*ent.AddonAssociation) []*AddonAssociation {
	return lo.Map(ents, func(ent *ent.AddonAssociation, _ int) *AddonAssociation {
		return FromEnt(ent)
	})
}
