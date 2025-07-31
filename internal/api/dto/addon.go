package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreateAddonRequest represents the request to create an addon
type CreateAddonRequest struct {
	Name         string                          `json:"name" validate:"required"`
	LookupKey    string                          `json:"lookup_key" validate:"required"`
	Description  string                          `json:"description"`
	Type         types.AddonType                 `json:"type" validate:"required"`
	Prices       []CreateAddonPriceRequest       `json:"prices"`
	Entitlements []CreateAddonEntitlementRequest `json:"entitlements"`
	Metadata     map[string]interface{}          `json:"metadata"`
}

type CreateAddonPriceRequest struct {
	*CreatePriceRequest
}

type CreateAddonEntitlementRequest struct {
	*CreateEntitlementRequest
}

func (r *CreateAddonEntitlementRequest) ToEntitlement(ctx context.Context, addonID *string) *entitlement.Entitlement {
	ent := r.CreateEntitlementRequest.ToEntitlement(ctx)
	ent.AddonID = addonID
	return ent
}

func (r *CreateAddonRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	// Validate addon type
	if r.Type != types.AddonTypeSingleInstance && r.Type != types.AddonTypeMultipleInstance {
		return errors.NewError("invalid addon type").
			WithHint("Addon type must be single_instance or multi_instance").
			Mark(errors.ErrValidation)
	}

	for _, price := range r.Prices {
		if err := price.Validate(); err != nil {
			return err
		}
	}

	for _, ent := range r.Entitlements {
		if err := ent.Validate(); err != nil {
			return err
		}
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
	Name         *string                         `json:"name,omitempty"`
	Description  *string                         `json:"description,omitempty"`
	Metadata     map[string]interface{}          `json:"metadata,omitempty"`
	Prices       []UpdateAddonPriceRequest       `json:"prices,omitempty"`
	Entitlements []UpdateAddonEntitlementRequest `json:"entitlements,omitempty"`
}

type UpdateAddonPriceRequest struct {
	// The ID of the price to update (present if the price is being updated)
	ID string `json:"id,omitempty"`
	// The price request to update existing price or create new price
	*CreatePriceRequest
}

type UpdateAddonEntitlementRequest struct {
	// The ID of the entitlement to update (present if the entitlement is being updated)
	ID string `json:"id,omitempty"`
	// The entitlement request to update existing entitlement or create new entitlement
	*CreateAddonEntitlementRequest
}

func (r *UpdateAddonRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	// Validate prices
	for _, price := range r.Prices {
		if err := price.Validate(); err != nil {
			return err
		}
	}

	// Validate entitlements
	for _, ent := range r.Entitlements {
		if err := ent.Validate(); err != nil {
			return err
		}
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
	AddonID           string                  `json:"addon_id" validate:"required"`
	PriceID           string                  `json:"price_id" validate:"required"`
	Quantity          int                     `json:"quantity" validate:"min=1"`
	StartDate         *time.Time              `json:"start_date,omitempty"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior"`
	Metadata          map[string]interface{}  `json:"metadata"`
}

func (r *AddAddonToSubscriptionRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if r.Quantity <= 0 {
		r.Quantity = 1
	}

	if r.ProrationBehavior == "" {
		r.ProrationBehavior = types.ProrationBehaviorCreateProrations
	}

	return nil
}

func (r *AddAddonToSubscriptionRequest) ToDomain(ctx context.Context, subscriptionID string) *addon.SubscriptionAddon {

	startDate := r.StartDate
	if startDate == nil {
		now := time.Now()
		startDate = &now
	}

	return &addon.SubscriptionAddon{
		ID:                types.GenerateShortIDWithPrefix(string(types.UUID_PREFIX_ADDON)),
		SubscriptionID:    subscriptionID,
		AddonID:           r.AddonID,
		PriceID:           r.PriceID,
		Quantity:          r.Quantity,
		StartDate:         startDate,
		AddonStatus:       types.AddonStatusActive,
		ProrationBehavior: r.ProrationBehavior,
		Metadata:          r.Metadata,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
}

// UpdateSubscriptionAddonRequest represents the request to update a subscription addon
type UpdateSubscriptionAddonRequest struct {
	Quantity          *int                     `json:"quantity" validate:"omitempty,min=1"`
	PriceID           *string                  `json:"price_id"`
	ProrationBehavior *types.ProrationBehavior `json:"proration_behavior"`
	Metadata          map[string]interface{}   `json:"metadata"`
}

func (r *UpdateSubscriptionAddonRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// SubscriptionAddonResponse represents the subscription addon response
type SubscriptionAddonResponse struct {
	ID                string                  `json:"id"`
	SubscriptionID    string                  `json:"subscription_id"`
	AddonID           string                  `json:"addon_id"`
	PriceID           string                  `json:"price_id"`
	Quantity          int                     `json:"quantity"`
	StartDate         *time.Time              `json:"start_date,omitempty"`
	EndDate           *time.Time              `json:"end_date,omitempty"`
	AddonStatus       types.AddonStatus       `json:"addon_status"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior"`
	ProratedAmount    *decimal.Decimal        `json:"prorated_amount,omitempty"`
	UsageLimit        *decimal.Decimal        `json:"usage_limit,omitempty"`
	UsageResetPeriod  string                  `json:"usage_reset_period,omitempty"`
	UsageResetDate    *time.Time              `json:"usage_reset_date,omitempty"`
	Metadata          map[string]interface{}  `json:"metadata,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`

	// Optional expanded fields
	Addon *AddonResponse `json:"addon,omitempty"`
	Price *PriceResponse `json:"price,omitempty"`
}

func (r *SubscriptionAddonResponse) FromDomain(sa *addon.SubscriptionAddon) *SubscriptionAddonResponse {
	if sa == nil {
		return nil
	}

	return &SubscriptionAddonResponse{
		ID:                sa.ID,
		SubscriptionID:    sa.SubscriptionID,
		AddonID:           sa.AddonID,
		PriceID:           sa.PriceID,
		Quantity:          sa.Quantity,
		StartDate:         sa.StartDate,
		EndDate:           sa.EndDate,
		AddonStatus:       sa.AddonStatus,
		ProrationBehavior: sa.ProrationBehavior,
		ProratedAmount:    sa.ProratedAmount,
		UsageLimit:        sa.UsageLimit,
		UsageResetPeriod:  sa.UsageResetPeriod,
		UsageResetDate:    sa.UsageResetDate,
		Metadata:          sa.Metadata,
		CreatedAt:         sa.CreatedAt,
		UpdatedAt:         sa.UpdatedAt,
	}
}

// ListSubscriptionAddonsResponse represents the response for listing subscription addons
type ListSubscriptionAddonsResponse = types.ListResponse[*SubscriptionAddonResponse]

// Helper functions for converting slices
func AddonResponsesToDomain(responses []*AddonResponse) []*addon.Addon {
	return lo.Map(responses, func(r *AddonResponse, _ int) *addon.Addon {
		return &addon.Addon{
			ID:          r.ID,
			Name:        r.Name,
			LookupKey:   r.LookupKey,
			Description: r.Description,
			Type:        r.Type,
			Metadata:    r.Metadata,
			BaseModel: types.BaseModel{
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			},
		}
	})
}
