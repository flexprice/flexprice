package dto

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreatePriceRequest struct {
	Amount             *decimal.Decimal         `json:"amount,omitempty" swaggertype:"string"`
	Currency           string                   `json:"currency" validate:"required,len=3"`
	EntityType         types.PriceEntityType    `json:"entity_type" validate:"required"`
	EntityID           string                   `json:"entity_id" validate:"required"`
	Type               types.PriceType          `json:"type" validate:"required"`
	PriceUnitType      types.PriceUnitType      `json:"price_unit_type" validate:"required"`
	BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`
	BillingPeriodCount int                      `json:"billing_period_count" default:"1"`
	BillingModel       types.BillingModel       `json:"billing_model" validate:"required"`
	BillingCadence     types.BillingCadence     `json:"billing_cadence" validate:"required"`
	MeterID            string                   `json:"meter_id,omitempty"`
	FilterValues       map[string][]string      `json:"filter_values,omitempty"`
	LookupKey          string                   `json:"lookup_key,omitempty"`
	InvoiceCadence     types.InvoiceCadence     `json:"invoice_cadence" validate:"required"`
	TrialPeriod        int                      `json:"trial_period"`
	Description        string                   `json:"description,omitempty"`
	Metadata           map[string]string        `json:"metadata,omitempty"`
	TierMode           types.BillingTier        `json:"tier_mode,omitempty"`
	Tiers              []CreatePriceTier        `json:"tiers,omitempty"`
	TransformQuantity  *price.TransformQuantity `json:"transform_quantity,omitempty"`
	PriceUnitConfig    *PriceUnitConfig         `json:"price_unit_config,omitempty"`
	StartDate          *time.Time               `json:"start_date,omitempty"`
	EndDate            *time.Time               `json:"end_date,omitempty"`
	DisplayName        string                   `json:"display_name,omitempty"`

	// MinQuantity is the minimum quantity of the price
	MinQuantity *int64 `json:"min_quantity,omitempty"`

	// SkipEntityValidation is used to skip entity validation when creating a price from a subscription i.e. override price workflow
	// This is used when creating a subscription-scoped price
	// NOTE: This is not a public field and is used internally should be used with caution
	SkipEntityValidation bool `json:"-"`

	// ParentPriceID is the id of the parent price for this price
	ParentPriceID string `json:"-"`

	// GroupID is the id of the group to add the price to
	GroupID string `json:"group_id,omitempty"`
}

type PriceUnitConfig struct {
	Amount         *decimal.Decimal  `json:"amount,omitempty" swaggertype:"string"`
	PriceUnit      string            `json:"price_unit" validate:"required,len=3"`
	PriceUnitTiers []CreatePriceTier `json:"price_unit_tiers,omitempty"`
}

type CreatePriceTier struct {
	// up_to is the quantity up to which this tier applies. It is null for the last tier.
	// IMPORTANT: Tier boundaries are INCLUSIVE.
	// - If up_to is 1000, then quantity less than or equal to 1000 belongs to this tier
	// - This behavior is consistent across both VOLUME and SLAB tier modes
	UpTo *uint64 `json:"up_to"`

	// unit_amount is the amount per unit for the given tier
	UnitAmount decimal.Decimal `json:"unit_amount" validate:"required" swaggertype:"string"`

	// flat_amount is the flat amount for the given tier (optional)
	// Applied on top of unit_amount*quantity. Useful for cases like "2.7$ + 5c"
	FlatAmount *decimal.Decimal `json:"flat_amount,omitempty" swaggertype:"string"`
}

type UpdatePriceRequest struct {
	// All price fields that can be updated
	// Non-critical fields (can be updated directly)
	LookupKey     string            `json:"lookup_key,omitempty"`
	Description   string            `json:"description,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	EffectiveFrom *time.Time        `json:"effective_from,omitempty"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price (optional)
	Amount *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`

	// GroupID is the id of the group to update the price in
	GroupID string `json:"group_id,omitempty"`
}

type PriceResponse struct {
	*price.Price
	Meter *MeterResponse `json:"meter,omitempty"`
	Plan  *PlanResponse  `json:"plan,omitempty"`
	Addon *AddonResponse `json:"addon,omitempty"`
	Group *GroupResponse `json:"group,omitempty"`
}

// ListPricesResponse represents the response for listing prices
type ListPricesResponse = types.ListResponse[*PriceResponse]

// CreateBulkPriceRequest represents the request to create multiple prices in bulk
type CreateBulkPriceRequest struct {
	Items []CreatePriceRequest `json:"items" validate:"required,min=1,max=100"`
}

// CreateBulkPriceResponse represents the response for bulk price creation
type CreateBulkPriceResponse struct {
	Items []*PriceResponse `json:"items"`
}

// CostBreakup provides detailed information about cost calculation
// including which tier was applied and the effective per unit cost
type CostBreakup struct {
	// EffectiveUnitCost is the per-unit cost based on the applicable tier
	EffectiveUnitCost decimal.Decimal
	// SelectedTierIndex is the index of the tier that was applied (-1 if no tiers)
	SelectedTierIndex int
	// TierUnitAmount is the unit amount of the selected tier
	TierUnitAmount decimal.Decimal
	// FinalCost is the total cost for the quantity
	FinalCost decimal.Decimal
}

type DeletePriceRequest struct {
	EndDate *time.Time `json:"end_date,omitempty"`
}

// Validate validates the tier structure
func (t *CreatePriceTier) Validate() error {
	// Validate unit amount (allows zero)
	if t.UnitAmount.LessThan(decimal.Zero) {
		return ierr.NewError("unit amount cannot be negative").
			WithHint("Unit amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"unit_amount": t.UnitAmount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate flat amount if provided
	if t.FlatAmount != nil && t.FlatAmount.LessThan(decimal.Zero) {
		return ierr.NewError("flat amount cannot be negative").
			WithHint("Flat amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"flat_amount": t.FlatAmount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (p *PriceUnitConfig) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}

	// Validate price unit tiers if present
	if err := validateTiers(p.PriceUnitTiers, "price_unit_tiers"); err != nil {
		return err
	}

	return nil
}

// Validate validates the create price request
func (r *CreatePriceRequest) Validate() error {
	if r.PriceUnitType == "" {
		r.PriceUnitType = types.PRICE_UNIT_TYPE_FIAT
	}

	// 2. Basic field validations
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Set default billing period count
	if r.BillingPeriodCount < 1 {
		return ierr.NewError("billing period count must be greater than 0").
			WithHint("Billing period count must be greater than 0").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// 3. Validate enum types
	if err := r.Type.Validate(); err != nil {
		return err
	}
	if err := r.BillingModel.Validate(); err != nil {
		return err
	}
	if err := r.BillingCadence.Validate(); err != nil {
		return err
	}
	if err := r.BillingPeriod.Validate(); err != nil {
		return err
	}
	if err := r.InvoiceCadence.Validate(); err != nil {
		return err
	}

	// 4. Validate entity fields
	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	// 5. Validate price unit config (if CUSTOM)
	if r.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
		if r.PriceUnitConfig == nil {
			return ierr.NewError("price_unit_config is required when using custom pricing unit").
				WithHint("Price unit config must be provided when using custom pricing units").
				Mark(ierr.ErrValidation)
		}

		if err := r.PriceUnitConfig.Validate(); err != nil {
			return err
		}

		// Ensure PriceUnitType is set to CUSTOM when PriceUnitConfig is provided
		if r.PriceUnitType != types.PRICE_UNIT_TYPE_CUSTOM {
			return ierr.NewError("price_unit_type must be CUSTOM when price_unit_config is provided").
				WithHint("Set price_unit_type to CUSTOM when using price_unit_config").
				WithReportableDetails(map[string]interface{}{
					"price_unit_type": r.PriceUnitType,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate that tiers are not provided in both places
		if len(r.Tiers) > 0 && len(r.PriceUnitConfig.PriceUnitTiers) > 0 {
			return ierr.NewError("cannot provide both regular tiers and price unit tiers").
				WithHint("For custom pricing units, use price_unit_config.price_unit_tiers only, not regular tiers").
				WithReportableDetails(map[string]interface{}{
					"tiers":            len(r.Tiers),
					"price_unit_tiers": len(r.PriceUnitConfig.PriceUnitTiers),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// 6. Validate billing model specific requirements
	switch r.BillingModel {
	case types.BILLING_MODEL_TIERED:
		// Validate tiers based on price type
		if r.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
			// CUSTOM: require price unit tiers
			if len(r.PriceUnitConfig.PriceUnitTiers) == 0 {
				return ierr.NewError("price_unit_tiers are required when billing model is TIERED and using custom pricing unit").
					WithHint("Price unit tiers are required to set up tiered pricing with custom pricing units").
					Mark(ierr.ErrValidation)
			}
			// Validate price unit tiers
			if err := validateTiers(r.PriceUnitConfig.PriceUnitTiers, "price_unit_tiers"); err != nil {
				return err
			}
		} else {
			// FIAT: require regular tiers
			if len(r.Tiers) == 0 {
				return ierr.NewError("tiers are required when billing model is TIERED").
					WithHint("Price tiers are required to set up tiered pricing").
					Mark(ierr.ErrValidation)
			}
			// Validate regular tiers
			if err := validateTiers(r.Tiers, "tiers"); err != nil {
				return err
			}
		}

	case types.BILLING_MODEL_PACKAGE:
		if r.TransformQuantity == nil {
			return ierr.NewError("transform_quantity is required when billing model is PACKAGE").
				WithHint("Please provide the number of units to set up package pricing").
				Mark(ierr.ErrValidation)
		}
		if err := r.TransformQuantity.Validate(); err != nil {
			return err
		}

		if r.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
			if r.PriceUnitConfig.Amount == nil {
				return ierr.NewError("price_unit_config.amount is required when billing model is PACKAGE and using custom pricing unit").
					WithHint("Price unit amount is required to set up package pricing with custom pricing units").
					Mark(ierr.ErrValidation)
			}
		} else {
			if r.Amount == nil {
				return ierr.NewError("amount is required when billing model is PACKAGE and using fiat pricing unit").
					WithHint("Amount is required to set up package pricing with fiat pricing units").
					Mark(ierr.ErrValidation)
			}
		}
	case types.BILLING_MODEL_FLAT_FEE:

		if r.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {
			if r.PriceUnitConfig.Amount == nil {
				return ierr.NewError("price_unit_config.amount is required when billing model is FLAT_FEE and using custom pricing unit").
					WithHint("Price unit amount is required to set up flat fee pricing with custom pricing units").
					Mark(ierr.ErrValidation)
			}
		} else {
			if r.Amount == nil {
				return ierr.NewError("amount is required when billing model is FLAT_FEE and using fiat pricing unit").
					WithHint("Amount is required to set up flat fee pricing with fiat pricing units").
					Mark(ierr.ErrValidation)
			}
		}

	}

	// 8. Validate price type specific requirements
	switch r.Type {
	case types.PRICE_TYPE_USAGE:
		if r.MeterID == "" {
			return ierr.NewError("meter_id is required when type is USAGE").
				WithHint("Please select a metered feature to set up usage pricing").
				Mark(ierr.ErrValidation)
		}
		if r.MinQuantity != nil {
			return ierr.NewError("min_quantity cannot be set for usage pricing").
				WithHint("min_quantity cannot be set for usage pricing").
				Mark(ierr.ErrValidation)
		}
	}

	// 9. Validate billing cadence specific requirements
	switch r.BillingCadence {
	case types.BILLING_CADENCE_RECURRING:
		if r.BillingPeriod == "" {
			return ierr.NewError("billing_period is required when billing_cadence is RECURRING").
				WithHint("Please select a billing period to set up recurring pricing").
				Mark(ierr.ErrValidation)
		}
	}

	// 11. Validate trial period
	if r.TrialPeriod < 0 {
		return ierr.NewError("trial period must be non-negative").
			WithHint("Please provide a non-negative trial period").
			Mark(ierr.ErrValidation)
	}
	if r.TrialPeriod > 0 &&
		r.BillingCadence != types.BILLING_CADENCE_RECURRING &&
		r.Type != types.PRICE_TYPE_FIXED {
		return ierr.NewError("trial period can only be set for recurring fixed prices").
			WithHint("Trial period can only be set for recurring fixed prices").
			Mark(ierr.ErrValidation)
	}

	// 12. Validate dates
	if r.StartDate != nil && r.EndDate != nil {
		if r.StartDate.After(*r.EndDate) {
			return ierr.NewError("start date must be before end date").
				WithHint("Start date must be before end date").
				Mark(ierr.ErrValidation)
		}
	}

	// 13. Validate usage price cannot be added to addon
	if r.Type == types.PRICE_TYPE_USAGE && r.EntityType == types.PRICE_ENTITY_TYPE_ADDON {
		return ierr.NewError("Usage based price cannot be added to an addon").
			WithHint("Usage based price cannot be added to an addon").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToPrice converts the request to a Price domain object
// Service layer should handle price unit conversion BEFORE calling this method
func (r *CreatePriceRequest) ToPrice(ctx context.Context) (*priceDomain.Price, error) {

	if r.PriceUnitType == "" {
		r.PriceUnitType = types.PRICE_UNIT_TYPE_FIAT
	}

	startDate := r.StartDate
	if startDate == nil {
		now := time.Now().UTC()
		startDate = &now
	}

	metadata := priceDomain.JSONBMetadata(r.Metadata)
	if r.Metadata == nil {
		metadata = make(priceDomain.JSONBMetadata)
	}

	// Initialize transformQuantity with default divide_by=1 if not provided
	var transformQuantity priceDomain.JSONBTransformQuantity
	if r.TransformQuantity != nil {
		transformQuantity = priceDomain.JSONBTransformQuantity(*r.TransformQuantity)
	} else {
		// Default to divide_by=1 when TransformQuantity is not provided
		transformQuantity = priceDomain.JSONBTransformQuantity{
			DivideBy: 1,
		}
	}

	var minQuantity *decimal.Decimal
	if r.MinQuantity != nil {
		minQuantityInt := int64(*r.MinQuantity)
		minQuantity = lo.ToPtr(decimal.NewFromInt(minQuantityInt))
	}

	// Create price struct with common fields
	price := &priceDomain.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             lo.FromPtrOr(r.Amount, decimal.Zero),
		Currency:           r.Currency,
		PriceUnitType:      r.PriceUnitType,
		Type:               r.Type,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingModel:       r.BillingModel,
		BillingCadence:     r.BillingCadence,
		InvoiceCadence:     r.InvoiceCadence,
		TrialPeriod:        r.TrialPeriod,
		MeterID:            r.MeterID,
		LookupKey:          r.LookupKey,
		Description:        r.Description,
		Metadata:           metadata,
		TierMode:           r.TierMode,
		TransformQuantity:  transformQuantity,
		EntityType:         r.EntityType,
		DisplayName:        r.DisplayName,
		EntityID:           r.EntityID,
		MinQuantity:        minQuantity,
		StartDate:          startDate,
		ParentPriceID:      r.ParentPriceID,
		EndDate:            r.EndDate,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		GroupID:            r.GroupID,
	}

	// Set type-specific fields
	if r.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM {

		price.PriceUnit = lo.ToPtr(r.PriceUnitConfig.PriceUnit)

		// Convert and set price unit tiers (original tiers for display)
		if r.PriceUnitConfig.PriceUnitTiers != nil {
			priceUnitTiers, err := r.convertTiers(r.PriceUnitConfig.PriceUnitTiers)
			if err != nil {
				return nil, err
			}
			price.PriceUnitTiers = priceUnitTiers
		}

		// Set price unit amount
		if r.PriceUnitConfig.Amount != nil {
			price.PriceUnitAmount = r.PriceUnitConfig.Amount
		}
	} else {
		// FIAT-specific fields
		// Convert and set tiers (FIAT-specific)
		tiers, err := r.convertTiers(r.Tiers)
		if err != nil {
			return nil, err
		}
		price.Tiers = tiers
	}

	price.DisplayAmount = price.GetDisplayAmount()

	return price, nil
}

// convertTiers converts CreatePriceTier slice to priceDomain.JSONBTiers with optimized error handling
func (r *CreatePriceRequest) convertTiers(tiers []CreatePriceTier) (priceDomain.JSONBTiers, error) {
	if len(tiers) == 0 {
		return nil, nil
	}

	priceTiers := make([]priceDomain.PriceTier, len(tiers))
	for i, tier := range tiers {
		priceTiers[i] = priceDomain.PriceTier{
			UpTo:       tier.UpTo,
			UnitAmount: tier.UnitAmount,
			FlatAmount: tier.FlatAmount,
		}
	}

	return priceDomain.JSONBTiers(priceTiers), nil
}

// createDecimalError creates a standardized decimal parsing error
func (r *CreatePriceRequest) createDecimalError(hint, field, value string) error {
	return ierr.NewError("invalid decimal format").
		WithHint(hint).
		WithReportableDetails(map[string]interface{}{
			field: value,
		}).
		Mark(ierr.ErrValidation)
}

func (r *UpdatePriceRequest) Validate() error {
	// If EffectiveFrom is provided, at least one critical field must be present
	if r.EffectiveFrom != nil && !r.ShouldCreateNewPrice() {
		return ierr.NewError("effective_from requires at least one critical field").
			WithHint("When providing effective_from, you must also provide one of: amount, billing_model, tier_mode, tiers, or transform_quantity").
			Mark(ierr.ErrValidation)
	}

	if r.EffectiveFrom != nil && r.ShouldCreateNewPrice() && r.EffectiveFrom.Before(time.Now().UTC()) {
		return ierr.NewError("effective from date must be in the future when used as termination date").
			WithHint("Effective from date must be in the future when updating critical fields").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ShouldCreateNewPrice checks if the request contains any critical fields that require creating a new price
func (r *UpdatePriceRequest) ShouldCreateNewPrice() bool {
	return r.BillingModel != "" ||
		r.Amount != nil ||
		r.TierMode != "" ||
		len(r.Tiers) > 0 ||
		r.TransformQuantity != nil
}

// ToCreatePriceRequest converts the update request to a create request for the new price
func (r *UpdatePriceRequest) ToCreatePriceRequest(existingPrice *price.Price) CreatePriceRequest {
	// Start with existing price as base
	createReq := CreatePriceRequest{
		EntityType:           existingPrice.EntityType,
		EntityID:             existingPrice.EntityID,
		SkipEntityValidation: true, // Skip validation since we're updating an existing entity
	}

	// Copy all non-critical, non-billing-model-specific fields from existing price
	createReq.Currency = existingPrice.Currency
	createReq.Type = existingPrice.Type
	createReq.BillingPeriod = existingPrice.BillingPeriod
	createReq.BillingPeriodCount = existingPrice.BillingPeriodCount
	createReq.BillingCadence = existingPrice.BillingCadence
	createReq.InvoiceCadence = existingPrice.InvoiceCadence
	createReq.TrialPeriod = existingPrice.TrialPeriod
	createReq.MeterID = existingPrice.MeterID
	createReq.ParentPriceID = existingPrice.GetRootPriceID()
	createReq.DisplayName = existingPrice.DisplayName

	if existingPrice.MinQuantity != nil {
		createReq.MinQuantity = lo.ToPtr(existingPrice.MinQuantity.IntPart())
	}

	// GroupID is the id of the group to update the price in
	if r.GroupID != "" {
		createReq.GroupID = r.GroupID
	} else {
		createReq.GroupID = existingPrice.GroupID
	}

	// Determine target billing model (use request billing model if provided, otherwise existing)
	targetBillingModel := existingPrice.BillingModel
	if r.BillingModel != "" {
		targetBillingModel = r.BillingModel
	}
	createReq.BillingModel = targetBillingModel

	// Handle billing model-specific fields based on target billing model
	switch targetBillingModel {
	case types.BILLING_MODEL_FLAT_FEE:
		// For FLAT_FEE, only amount is relevant
		if r.Amount != nil {
			createReq.Amount = r.Amount
		} else {
			existingAmount := existingPrice.Amount
			createReq.Amount = &existingAmount
		}

	case types.BILLING_MODEL_PACKAGE:
		// For PACKAGE, amount and transform_quantity are relevant
		if r.Amount != nil {
			createReq.Amount = r.Amount
		} else {
			existingAmount := existingPrice.Amount
			createReq.Amount = &existingAmount
		}

		if r.TransformQuantity != nil {
			createReq.TransformQuantity = r.TransformQuantity
		} else if existingPrice.TransformQuantity != (price.JSONBTransformQuantity{}) {
			transformQuantity := price.TransformQuantity(existingPrice.TransformQuantity)
			createReq.TransformQuantity = &transformQuantity
		}

	case types.BILLING_MODEL_TIERED:
		// For TIERED, only tier_mode and tiers are relevant
		if r.TierMode != "" {
			createReq.TierMode = r.TierMode
		} else {
			createReq.TierMode = existingPrice.TierMode
		}

		if len(r.Tiers) > 0 {
			createReq.Tiers = r.Tiers
		} else if len(existingPrice.Tiers) > 0 {
			createReq.Tiers = make([]CreatePriceTier, len(existingPrice.Tiers))
			for i, tier := range existingPrice.Tiers {
				createReq.Tiers[i] = CreatePriceTier{
					UpTo:       tier.UpTo,
					UnitAmount: tier.UnitAmount,
				}
				createReq.Tiers[i].FlatAmount = tier.FlatAmount
			}
		}
	}

	// Apply non-critical field updates from request
	if r.LookupKey != "" {
		createReq.LookupKey = r.LookupKey
	} else {
		createReq.LookupKey = existingPrice.LookupKey
	}
	if r.Description != "" {
		createReq.Description = r.Description
	} else {
		createReq.Description = existingPrice.Description
	}
	if r.Metadata != nil {
		createReq.Metadata = r.Metadata
	} else {
		createReq.Metadata = existingPrice.Metadata
	}

	// Note: StartDate and EndDate are handled by the service layer:
	// - EffectiveFrom in the request is used as termination date for the old price
	// - New price starts exactly when the old price ends (terminationEndDate)
	// - New price will not have an end date unless explicitly set

	return createReq
}

// Validate validates the bulk price creation request
func (r *CreateBulkPriceRequest) Validate() error {
	if len(r.Items) == 0 {
		return ierr.NewError("at least one price is required").
			WithHint("Please provide at least one price to create").
			Mark(ierr.ErrValidation)
	}

	if len(r.Items) > 100 {
		return ierr.NewError("too many prices in bulk request").
			WithHint("Maximum 100 prices allowed per bulk request").
			Mark(ierr.ErrValidation)
	}

	// Validate each individual price
	for i, price := range r.Items {
		if err := price.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("Price at index %d is invalid", i)).
				WithReportableDetails(map[string]interface{}{
					"index": i,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func (r *DeletePriceRequest) Validate() error {
	if r.EndDate != nil && r.EndDate.Before(time.Now().UTC()) {
		return ierr.NewError("end date must be in the future").
			WithHint("End date must be in the future").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// validateTiers validates an array of tiers and returns an error if any tier is invalid
func validateTiers(tiers []CreatePriceTier, fieldName string) error {
	if len(tiers) == 0 {
		return nil
	}

	for i, tier := range tiers {
		if err := tier.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("%s at index %d is invalid", fieldName, i)).
				WithReportableDetails(map[string]interface{}{
					"tier_index": i,
					"field_name": fieldName,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}
