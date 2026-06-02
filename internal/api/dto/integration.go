package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
)

type IntegrationSyncMethod string

const (
	IntegrationSyncMethodPull IntegrationSyncMethod = "pull"
	IntegrationSyncMethodPush IntegrationSyncMethod = "push"
)

func (i IntegrationSyncMethod) Validate() error {
	allowed := []IntegrationSyncMethod{
		IntegrationSyncMethodPull,
		IntegrationSyncMethodPush,
	}
	if !lo.Contains(allowed, i) {
		return ierr.NewError("invalid entity type").
			WithHint("Entity type must be one of: customer, plan, invoice, subscription, payment, credit_note, addon, item, item_price, price").
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
	Method     IntegrationSyncMethod       `json:"method" default:"push"`
}

func (r *IntegrationSyncRequest) Validate() error {
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
