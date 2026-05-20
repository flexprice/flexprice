package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type IntegrationSyncRequest struct {
	EntityType types.IntegrationEntityType `json:"entity_type" validate:"required"`
	EntityID   string                      `json:"entity_id" validate:"required"`
	// ProviderType, when set, runs a synchronous sync for that provider only (paddle + invoice today).
	// Omit to trigger the standard multi-vendor async dispatch (Temporal workflows).
	ProviderType string `json:"provider_type,omitempty" validate:"omitempty,max=50"`
}

func (r *IntegrationSyncRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if r.ProviderType == "" {
		return nil
	}
	p := types.SecretProvider(r.ProviderType)
	if err := p.Validate(); err != nil {
		return err
	}
	if p != types.SecretProviderPaddle {
		return ierr.NewError("unsupported provider_type for synchronous integration sync").
			WithHint("Only paddle is supported via provider_type; omit provider_type to run the standard multi-vendor sync").
			Mark(ierr.ErrValidation)
	}
	if r.EntityType != types.IntegrationEntityTypeInvoice {
		return ierr.NewError("provider-specific sync requires entity_type invoice").
			Mark(ierr.ErrValidation)
	}
	return nil
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
