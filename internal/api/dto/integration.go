package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
)

// IntegrationSyncMethod defined the type of sync to run
type IntegrationSyncMethod string

const (
	// IntegrationSyncMethodPull fetches data from integration and updates entities in flexprice
	IntegrationSyncMethodPull IntegrationSyncMethod = "pull"
	// IntegrationSyncMethodPush updates entities in integration from current entity state in flexprice
	IntegrationSyncMethodPush IntegrationSyncMethod = "push"
)

func (i IntegrationSyncMethod) Validate() error {
	allowed := []IntegrationSyncMethod{
		IntegrationSyncMethodPull,
		IntegrationSyncMethodPush,
	}
	if !lo.Contains(allowed, i) {
		return ierr.NewError("invalid sync method").
			WithHint("Sync method must be one of: pull, push").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (i IntegrationSyncMethod) String() string {
	return string(i)
}

type IntegrationSyncRequest struct {
	EntityType types.IntegrationEntityType `json:"entity_type" validate:"required"`
	EntityID   string                      `json:"entity_id" validate:"required"`
	// Method controls the direction of sync. "push" (default) writes the local
	// entity to the external provider. "pull" fetches the latest state from the
	// provider and updates the local record. Omitting this field defaults to "push".
	Method IntegrationSyncMethod `json:"method"`
}

func (r *IntegrationSyncRequest) Validate() error {
	if r.Method == "" {
		r.Method = IntegrationSyncMethodPush
	}
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}
	err = r.Method.Validate()
	if err != nil {
		return err
	}
	return r.EntityType.Validate()
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

type IntegrationConfigEntry struct {
	Provider      types.SecretProvider `json:"provider"`
	BaseConfig    *types.SyncConfig    `json:"base_config"`
	CurrentConfig *types.SyncConfig    `json:"current_config"`
}

type IntegrationConfigResponse struct {
	Integrations []IntegrationConfigEntry `json:"integrations"`
}

func EntityOnlySyncConfig(sc *types.SyncConfig) *types.SyncConfig {
	if sc == nil {
		return types.DefaultSyncConfig()
	}
	return &types.SyncConfig{
		Plan:         sc.Plan,
		Subscription: sc.Subscription,
		Invoice:      sc.Invoice,
		Customer:     sc.Customer,
		Payment:      sc.Payment,
		Deal:         sc.Deal,
		Quote:        sc.Quote,
	}
}
