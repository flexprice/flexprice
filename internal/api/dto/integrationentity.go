package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type CreateIntegrationEntityRequest struct {
	// ConnectionID is the ID of the connection in the flexprice system
	ConnectionID string `json:"connection_id" binding:"required" validate:"required"`
	// EntityType is the type of entity being connected (e.g., customer, payment)
	EntityType types.EntityType `json:"entity_type" binding:"required" validate:"required"`
	// EntityID is the ID of the FlexPrice entity
	EntityID string `json:"entity_id" binding:"required" validate:"required"`
	// ProviderType is the type of external provider (e.g., stripe, razorpay)
	ProviderType types.SecretProvider `json:"provider_type" binding:"required" validate:"required"`
	// ProviderID is the ID of the entity in the external system
	ProviderID string `json:"provider_id" binding:"required" validate:"required"`
}

func (r *CreateIntegrationEntityRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	if err := r.ProviderType.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *CreateIntegrationEntityRequest) ToIntegrationEntity(ctx context.Context) *integration.IntegrationEntity {
	return &integration.IntegrationEntity{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INTEGRATION_ENTITY),
		ConnectionID: r.ConnectionID,
		EntityType:   r.EntityType,
		EntityID:     r.EntityID,
		ProviderType: r.ProviderType,
		ProviderID:   r.ProviderID,
		BaseModel:    types.GetDefaultBaseModel(ctx),
	}
}
