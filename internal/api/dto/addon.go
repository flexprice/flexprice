package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateAddonRequest represents the request to create an addon
type CreateAddonRequest struct {
	Name        string                 `json:"name" validate:"required"`
	LookupKey   string                 `json:"lookup_key" validate:"required"`
	Description string                 `json:"description"`
	Type        types.AddonType        `json:"type" validate:"required"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func (r *CreateAddonRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.Type.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *CreateAddonRequest) ToAddon(ctx context.Context) *addon.Addon {
	return &addon.Addon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ADDON),
		Name:          r.Name,
		LookupKey:     r.LookupKey,
		Description:   r.Description,
		Type:          r.Type,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// UpdateAddonRequest represents the request to update an addon
type UpdateAddonRequest struct {
	Name        *string                `json:"name,omitempty"`
	Description *string                `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func (r *UpdateAddonRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	return nil
}

// AddonResponse represents the addon response
type AddonResponse struct {
	*addon.Addon

	// Optional expanded fields
	Prices       []*PriceResponse       `json:"prices,omitempty"`
	Entitlements []*EntitlementResponse `json:"entitlements,omitempty"`
}

// CreateAddonResponse represents the response after creating an addon
type CreateAddonResponse struct {
	*AddonResponse
}

// ListAddonsResponse represents the response for listing addons
type ListAddonsResponse = types.ListResponse[*AddonResponse]

// AddAddonToSubscriptionRequest represents the request to add an addon to a subscription
type AddAddonToSubscriptionRequest struct {
	AddonID   string                 `json:"addon_id" validate:"required"`
	StartDate *time.Time             `json:"start_date,omitempty"`
	EndDate   *time.Time             `json:"end_date,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
}

func (a *AddAddonToSubscriptionRequest) ToAddonAssociation(ctx context.Context, enitiyId string, enitityType types.AddonAssociationEntityType) *addonassociation.AddonAssociation {

	now := time.Now()
	startDate := now
	if a.StartDate != nil {
		startDate = *a.StartDate
	}
	return &addonassociation.AddonAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ADDON_ASSOCIATION),
		EntityID:      enitiyId,
		EntityType:    enitityType,
		AddonID:       a.AddonID,
		AddonStatus:   types.AddonStatusActive,
		StartDate:     &startDate,
		EndDate:       a.EndDate,
		Metadata:      a.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

func (r *AddAddonToSubscriptionRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	return nil
}

// AddonAssociationResponse represents the response for an addon association
type AddonAssociationResponse struct {
	*addonassociation.AddonAssociation
}
