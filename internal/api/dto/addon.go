package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateAddonRequest represents the request to create an addon
type CreateAddonRequest struct {
	Name        string                 `json:"name" validate:"required"`
	LookupKey   string                 `json:"lookup_key" validate:"required"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func (r *CreateAddonRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
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
	CreditGrants []*CreditGrantResponse `json:"credit_grants,omitempty"`
	// PriceSyncStatus reports how many associations are still behind this addon's
	// current usage-price sequence. Only populated on GetAddons when
	// `expand=price_sync_status` is requested.
	PriceSyncStatus *AddonPriceSyncStatusResponse `json:"price_sync_status,omitempty"`
}

// CreateAddonResponse represents the response after creating an addon
type CreateAddonResponse struct {
	*AddonResponse
}

// ListAddonsResponse represents the response for listing addons
type ListAddonsResponse = types.ListResponse[*AddonResponse] // @name ListAddonsResponse

// AddAddonToSubscriptionRequest represents the request to add an addon to a subscription
type AddAddonToSubscriptionRequest struct {
	AddonID           string                  `json:"addon_id" validate:"required"`
	Cadence           types.AddonCadence      `json:"cadence"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
	ProrationSettings ProrationSettings       `json:"-"`
	StartDate         *time.Time              `json:"start_date,omitempty"`
	// ChangeAt names when the attach applies without computing a date. Mutually exclusive
	// with StartDate; omit both to attach now.
	ChangeAt *types.ScheduleType    `json:"change_at,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`

	// LineItemCommitments allows setting commitment configuration per addon line item (keyed by price_id)
	LineItemCommitments map[string]*LineItemCommitmentConfig `json:"line_item_commitments,omitempty" validate:"omitempty,dive"`

	// OverrideLineItems allows overriding price/quantity/billing model for specific addon prices
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`

	// Coupons apply to this addon's line items; price_id targets one price, omitted targets all.
	Coupons []SubscriptionCouponInput `json:"coupons,omitempty"`

	// CouponRefs are the coupons resolved at quote time. Server-set on replay.
	CouponRefs []types.CouponRef `json:"-"`

	// PreviewOnly quotes the attach without writing anything. Server-set: callers reach it
	// through the preview endpoint, never by sending it.
	PreviewOnly bool `json:"-"`
}

func (a *AddAddonToSubscriptionRequest) ToAddonAssociation(ctx context.Context, enitiyId string, enitityType types.AddonAssociationEntityType) *addonassociation.AddonAssociation {

	now := clock.Now()
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
		Metadata:      a.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

func (r *AddAddonToSubscriptionRequest) ApplyDefaults() {
	if r == nil {
		return
	}
	applyLineItemCommitmentDefaults(r.LineItemCommitments)
}

func (r *AddAddonToSubscriptionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if r.ChangeAt != nil {
		if err := r.ChangeAt.Validate(); err != nil {
			return err
		}
		if r.StartDate != nil {
			return ierr.NewError("change_at and start_date are mutually exclusive").
				WithHint("Provide change_at for immediate or end_of_period, or start_date for any other date").
				WithReportableDetails(map[string]any{"addon_id": r.AddonID}).
				Mark(ierr.ErrValidation)
		}
	}

	// Default to recurring when not provided for backward compatibility.
	if r.Cadence == "" {
		r.Cadence = types.AddonCadenceRecurring
	}

	if err := r.Cadence.Validate(); err != nil {
		return err
	}

	if r.ProrationBehavior != "" {
		if err := r.ProrationBehavior.Validate(); err != nil {
			return err
		}
	}

	if err := validateLineItemCommitments(r.LineItemCommitments); err != nil {
		return err
	}

	if err := validateNoDuplicateOverridePriceIDs(r.OverrideLineItems); err != nil {
		return err
	}

	return nil
}

// AddonAssociationResponse represents the response for an addon association
type AddonAssociationResponse struct {
	*addonassociation.AddonAssociation
	Addon        *AddonResponse        `json:"addon,omitempty"`
	Subscription *SubscriptionResponse `json:"subscription,omitempty"`
}

// ListAddonAssociationsResponse represents the response for listing addon associations
type ListAddonAssociationsResponse = types.ListResponse[*AddonAssociationResponse] // @name ListAddonAssociationsResponse

type AddAddonToSubscriptionResponse struct {
	*addonassociation.AddonAssociation
	CheckoutSession *CheckoutSessionResponse `json:"checkout_session,omitempty"`
	Invoice         *InvoiceResponse         `json:"invoice,omitempty"`
}

// GetActiveAddonAssociationRequest represents the request to get active addon associations
type GetActiveAddonAssociationRequest struct {
	AddonIds   []string                         `json:"addon_ids,omitempty"`
	EntityID   string                           `json:"entity_id" validate:"required"`
	EntityType types.AddonAssociationEntityType `json:"entity_type" validate:"required"`
	StartDate  *time.Time                       `json:"start_date,omitempty"`
	EndDate    *time.Time                       `json:"end_date,omitempty"`

	AddonStatuses []types.AddonStatus `json:"addon_statuses,omitempty"`

	// ActiveAt narrows to associations live at that instant. See AddonAssociationFilter.
	ActiveAt *time.Time `json:"active_at,omitempty"`
}

func (r *GetActiveAddonAssociationRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	// Ensure end date is not before start date
	if r.StartDate != nil && r.EndDate != nil && r.EndDate.Before(*r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("Provide an end_date that is on or after start_date").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SyncAddonPricesResponse is returned by the addon usage-price sync. Uses the
// SyncPlanPricesResponse shape, the one the plan sync (including V2) actually
// returns.
type SyncAddonPricesResponse struct {
	AddonID string                 `json:"addon_id"`
	Message string                 `json:"message"`
	Summary SyncAddonPricesSummary `json:"summary"`
}

type SyncAddonPricesSummary struct {
	LineItemsFoundForCreation int `json:"line_items_found_for_creation"`
	LineItemsCreated          int `json:"line_items_created"`
	LineItemsTerminated       int `json:"line_items_terminated"`
}

// AddonPriceSyncStatusResponse reports how many of an addon's associations are
// still behind the addon's current usage-price sequence. UnsyncedAssociationCount
// == 0 means every active recurring association is caught up and a sync run would
// be a no-op.
type AddonPriceSyncStatusResponse struct {
	CurrentSequence          int64 `json:"current_sequence"`
	UnsyncedAssociationCount int64 `json:"unsynced_association_count"`
	Synced                   bool  `json:"synced"`
}
