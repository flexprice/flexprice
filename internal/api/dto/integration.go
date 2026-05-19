package dto

import (
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

var supportedSyncEntityTypes = []string{"invoice", "customer"}

type IntegrationSyncRequest struct {
	EntityType string `json:"entity_type" validate:"required"`
	EntityID   string `json:"entity_id" validate:"required,max=255"`
}

func (r *IntegrationSyncRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	for _, t := range supportedSyncEntityTypes {
		if r.EntityType == t {
			return nil
		}
	}

	return ierr.NewError(fmt.Sprintf("unsupported entity_type: %s", r.EntityType)).
		WithHintf("Supported entity types: %v", supportedSyncEntityTypes).
		Mark(ierr.ErrValidation)
}

type LinkIntegrationMappingRequest struct {
	EntityType       types.IntegrationEntityType `json:"entity_type" validate:"required"`
	EntityID         string                      `json:"entity_id" validate:"required,max=255"`
	ProviderType     string                      `json:"provider_type" validate:"required,max=50"`
	ProviderEntityID string                      `json:"provider_entity_id" validate:"required,max=255"`
	Metadata         map[string]interface{}      `json:"metadata,omitempty"`
}

type LinkIntegrationMappingResponse struct {
	Mapping *EntityIntegrationMappingResponse `json:"mapping"`
}

func (r *LinkIntegrationMappingRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if err := types.SecretProvider(r.ProviderType).Validate(); err != nil {
		return err
	}
	return nil
}
