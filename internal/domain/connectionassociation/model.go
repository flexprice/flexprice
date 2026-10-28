package connectionassociation

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// ConnectionAssociation links a billing entity (owner) or a customer (preference) to a connection.
type ConnectionAssociation struct {
	ID            string                                `json:"id"`
	EntityType    types.ConnectionAssociationEntityType `json:"entity_type"`
	EntityID      string                                `json:"entity_id"`
	ConnectionID  string                                `json:"connection_id"`
	ProviderType  types.SecretProvider                  `json:"provider_type"`
	EnvironmentID string                                `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent ConnectionAssociation to a domain ConnectionAssociation
func FromEnt(e *ent.ConnectionAssociation) *ConnectionAssociation {
	if e == nil {
		return nil
	}
	return &ConnectionAssociation{
		ID:            e.ID,
		EntityType:    types.ConnectionAssociationEntityType(e.EntityType),
		EntityID:      e.EntityID,
		ConnectionID:  e.ConnectionID,
		ProviderType:  types.SecretProvider(e.ProviderType),
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

// FromEntList converts a list of ent ConnectionAssociations to domain ConnectionAssociations
func FromEntList(ents []*ent.ConnectionAssociation) []*ConnectionAssociation {
	result := make([]*ConnectionAssociation, 0, len(ents))
	for _, e := range ents {
		result = append(result, FromEnt(e))
	}
	return result
}
