package dto

import (
	"context"
	"fmt"
	"strings"
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
	Amount             string                   `json:"amount,omitempty"`
	Currency           string                   `json:"currency" validate:"required,len=3"`
	PlanID             string                   `json:"plan_id,omitempty"`
	EntityType         types.PriceEntityType    `json:"entity_type,omitempty"`
	EntityID           string                   `json:"entity_id,omitempty"`
	Type               types.PriceType          `json:"type" validate:"required"`
	PriceUnitType      types.PriceUnitType      `json:"price_unit_type" validate:"omitempty"`
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

type CreatePriceTier struct {
	// up_to is the quantity up to which this tier applies. It is null for the last tier.
	// IMPORTANT: Tier boundaries are INCLUSIVE.
	// - If up_to is 1000, then quantity less than or equal to 1000 belongs to this tier
	// - This behavior is consistent across both VOLUME and SLAB tier modes
	UpTo *uint64 `json:"up_to"`

	// unit_amount is the amount per unit for the given tier
	UnitAmount string `json:"unit_amount" validate:"required"`

	// flat_amount is the flat amount for the given tier (optional)
	// Applied on top of unit_amount*quantity. Useful for cases like "2.7$ + 5c"
	FlatAmount *string `json:"flat_amount" validate:"omitempty"`
}

type PriceUnitConfig struct {
	Amount         string            `json:"amount,omitempty"`
	PriceUnit      string            `json:"price_unit" validate:"required,len=3"`
	PriceUnitTiers []CreatePriceTier `json:"price_unit_tiers,omitempty"`
}

func (p *PriceUnitConfig) Validate() error {
	if err := validator.ValidateRequest(p); err != nil {
		return err
	}

	return nil
}

// Validate validates the create price request
func (r *CreatePriceRequest) Validate() error {
	var err error

	// Validate entity type
	err = r.EntityType.Validate()
	if err != nil {
		return err
	}

	if r.EntityID == "" {
		return ierr.NewError("entity_id is required when entity_type is provided").
			WithHint("Please provide an entity id").
			Mark(ierr.ErrValidation)
	}
	// Set default value to Billing Period Count if not provided
	if r.BillingPeriodCount == 0 {
		r.BillingPeriodCount = 1
	} else if r.BillingPeriodCount < 0 {
		return ierr.NewError("invalid billing period count").
			WithHint("Billing Period must be a valid positive number").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Base validations
	amount := decimal.Zero
	if r.Amount != "" {
		amount, err = decimal.NewFromString(r.Amount)
		if err != nil {
			return ierr.NewError("invalid amount format").
				WithHint("Amount must be a valid decimal number").
				WithReportableDetails(map[string]interface{}{
					"amount": r.Amount,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Amount must be non-negative
	if amount.LessThan(decimal.Zero) {
		return ierr.NewError("amount cannot be negative").
			WithHint("Amount cannot be negative").
			Mark(ierr.ErrValidation)
	}

	// Ensure currency is lowercase
	r.Currency = strings.ToLower(r.Currency)

	// Billing model validations
	err = validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	// valid input field types with available values

	err = r.Type.Validate()
	if err != nil {
		return err
	}

	err = r.BillingCadence.Validate()
	if err != nil {
		return err
	}

	err = r.BillingModel.Validate()
	if err != nil {
		return err
	}

	err = r.BillingPeriod.Validate()
	if err != nil {
		return err
	}

	err = r.InvoiceCadence.Validate()
	if err != nil {
		return err
	}

	switch r.BillingModel {
	case types.BILLING_MODEL_TIERED:
		// Check for tiers
		if len(r.Tiers) == 0 {
			return ierr.NewError("tiers are required when billing model is TIERED").
				WithHint("Price Tiers are required to set up tiered pricing").
				Mark(ierr.ErrValidation)
		}

	case types.BILLING_MODEL_PACKAGE:
		if r.TransformQuantity == nil {
			return ierr.NewError("transform_quantity is required when billing model is PACKAGE").
				WithHint("Please provide the number of units to set up package pricing").
				Mark(ierr.ErrValidation)
		}

		if r.TransformQuantity.DivideBy <= 0 {
			return ierr.NewError("transform_quantity.divide_by must be greater than 0 when billing model is PACKAGE").
				WithHint("Please provide a valid number of units to set up package pricing").
				Mark(ierr.ErrValidation)
		}

		// Validate round type
		if r.TransformQuantity.Round == "" {
			r.TransformQuantity.Round = types.ROUND_UP // Default to rounding up
		} else if r.TransformQuantity.Round != types.ROUND_UP && r.TransformQuantity.Round != types.ROUND_DOWN {
			return ierr.NewError("invalid rounding type- allowed values are up and down").
				WithHint("Please provide a valid rounding type for package pricing").
				WithReportableDetails(map[string]interface{}{
					"round":   r.TransformQuantity.Round,
					"allowed": []string{types.ROUND_UP, types.ROUND_DOWN},
				}).
				Mark(ierr.ErrValidation)
		}
	}

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

	switch r.BillingCadence {
	case types.BILLING_CADENCE_RECURRING:
		if r.BillingPeriod == "" {
			return ierr.NewError("billing_period is required when billing_cadence is RECURRING").
				WithHint("Please select a billing period to set up recurring pricing").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate tiers if present
	if len(r.Tiers) > 0 && r.BillingModel == types.BILLING_MODEL_TIERED {
		for _, tier := range r.Tiers {
			tierAmount, err := decimal.NewFromString(tier.UnitAmount)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Unit amount must be a valid decimal number").
					WithReportableDetails(map[string]interface{}{
						"unit_amount": tier.UnitAmount,
					}).
					Mark(ierr.ErrValidation)
			}

			if tierAmount.LessThan(decimal.Zero) {
				return ierr.WithError(err).
					WithHint("Unit amount cannot be negative").
					WithReportableDetails(map[string]interface{}{
						"unit_amount": tier.UnitAmount,
					}).
					Mark(ierr.ErrValidation)
			}

			if tier.FlatAmount != nil {
				flatAmount, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Flat amount must be a valid decimal number").
						WithReportableDetails(map[string]interface{}{
							"flat_amount": tier.FlatAmount,
						}).
						Mark(ierr.ErrValidation)
				}

				if flatAmount.LessThan(decimal.Zero) {
					return ierr.WithError(err).
						WithHint("Flat amount cannot be negative").
						WithReportableDetails(map[string]interface{}{
							"flat_amount": tier.FlatAmount,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}
	}

	// trial period validations
	// Trial period should be non-negative
	if r.TrialPeriod < 0 {
		return ierr.NewError("trial period must be non-negative").
			WithHint("Please provide a non-negative trial period").
			Mark(ierr.ErrValidation)
	}

	// Trial period should only be set for recurring fixed prices
	if r.TrialPeriod > 0 &&
		r.BillingCadence != types.BILLING_CADENCE_RECURRING &&
		r.Type != types.PRICE_TYPE_FIXED {
		return ierr.NewError("trial period can only be set for recurring fixed prices").
			WithHint("Trial period can only be set for recurring fixed prices").
			Mark(ierr.ErrValidation)
	}

	if r.EntityType != "" {

		if err := r.EntityType.Validate(); err != nil {
			return err
		}

		if r.EntityID == "" {
			return ierr.NewError("entity_id is required when entity_type is provided").
				WithHint("Please provide an entity id").
				Mark(ierr.ErrValidation)
		}
	}

	// validate the start and end date if present
	if r.StartDate != nil && r.EndDate != nil {
		if r.StartDate.After(*r.EndDate) {
			return ierr.NewError("start date must be before end date").
				WithHint("Start date must be before end date").
				Mark(ierr.ErrValidation)
		}
	}

	// if price type is usage and entity type is addon, throw an error
	if r.Type == types.PRICE_TYPE_USAGE && r.EntityType == types.PRICE_ENTITY_TYPE_ADDON {
		return ierr.NewError("Usage based price cannot be added to an addon").
			WithHint("Usage based price cannot be added to an addon").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreatePriceRequest) ToPrice(ctx context.Context) (*priceDomain.Price, error) {
	// Ensure price unit type is set to FIAT if not provided
	if r.PriceUnitType == "" {
		r.PriceUnitType = types.PRICE_UNIT_TYPE_FIAT
	}

	// Parse amount with optimized error handling
	amount, err := r.parseAmount()
	if err != nil {
		return nil, err
	}

	// Set start date with early return for nil case
	startDate := r.StartDate
	if startDate == nil {
		now := time.Now().UTC()
		startDate = &now
	}

	// Convert metadata efficiently
	metadata := priceDomain.JSONBMetadata(r.Metadata)
	if r.Metadata == nil {
		metadata = make(priceDomain.JSONBMetadata)
	}

	// Convert transform quantity efficiently
	var transformQuantity priceDomain.JSONBTransformQuantity
	if r.TransformQuantity != nil {
		transformQuantity = priceDomain.JSONBTransformQuantity(*r.TransformQuantity)
	}

	// Convert tiers with optimized helper function
	tiers, err := r.convertTiers(r.Tiers)
	if err != nil {
		return nil, err
	}

	// Convert price unit tiers with optimized helper function
	var priceUnitTiers priceDomain.JSONBTiers
	if r.PriceUnitConfig != nil && r.PriceUnitConfig.PriceUnitTiers != nil {
		priceUnitTiers, err = r.convertTiers(r.PriceUnitConfig.PriceUnitTiers)
		if err != nil {
			return nil, err
		}
	}

	var minQuantity *decimal.Decimal
	if r.MinQuantity != nil {
		minQuantityInt := int64(*r.MinQuantity)
		minQuantity = lo.ToPtr(decimal.NewFromInt(minQuantityInt))
	}

	// Create price struct with pre-allocated fields
	price := &priceDomain.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             amount,
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
		Tiers:              tiers,
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

	// Set price unit related fields if price unit config is provided
	if r.PriceUnitConfig != nil {
		price.PriceUnit = r.PriceUnitConfig.PriceUnit
		price.PriceUnitTiers = priceUnitTiers

		if r.PriceUnitConfig.Amount != "" {
			priceUnitAmount, err := decimal.NewFromString(r.PriceUnitConfig.Amount)
			if err != nil {
				return nil, r.createDecimalError("Price unit amount must be a valid decimal number", "amount", r.PriceUnitConfig.Amount)
			}
			price.PriceUnitAmount = &priceUnitAmount
		}
	}

	price.DisplayAmount = price.GetDisplayAmount()
	return price, nil
}

// parseAmount parses the amount string with optimized error handling
func (r *CreatePriceRequest) parseAmount() (decimal.Decimal, error) {
	if r.Amount == "" {
		return decimal.Zero, nil
	}

	amount, err := decimal.NewFromString(r.Amount)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Amount must be a valid decimal number").
			WithReportableDetails(map[string]interface{}{
				"amount": r.Amount,
			}).
			Mark(ierr.ErrValidation)
	}

	return amount, nil
}

// convertTiers converts CreatePriceTier slice to priceDomain.JSONBTiers with optimized error handling
func (r *CreatePriceRequest) convertTiers(tiers []CreatePriceTier) (priceDomain.JSONBTiers, error) {
	if len(tiers) == 0 {
		return nil, nil
	}

	priceTiers := make([]priceDomain.PriceTier, len(tiers))
	for i, tier := range tiers {
		unitAmount, err := decimal.NewFromString(tier.UnitAmount)
		if err != nil {
			return nil, r.createDecimalError("Unit amount must be a valid decimal number", "unit_amount", tier.UnitAmount)
		}

		var flatAmount *decimal.Decimal
		if tier.FlatAmount != nil {
			parsed, err := decimal.NewFromString(*tier.FlatAmount)
			if err != nil {
				return nil, r.createDecimalError("Flat amount must be a valid decimal number", "flat_amount", *tier.FlatAmount)
			}
			flatAmount = &parsed
		}

		priceTiers[i] = priceDomain.PriceTier{
			UpTo:       tier.UpTo,
			UnitAmount: unitAmount,
			FlatAmount: flatAmount,
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
			createReq.Amount = r.Amount.String()
		} else {
			createReq.Amount = existingPrice.Amount.String()
		}

	case types.BILLING_MODEL_PACKAGE:
		// For PACKAGE, amount and transform_quantity are relevant
		if r.Amount != nil {
			createReq.Amount = r.Amount.String()
		} else {
			createReq.Amount = existingPrice.Amount.String()
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
					UnitAmount: tier.UnitAmount.String(),
				}
				if tier.FlatAmount != nil {
					flatAmountStr := tier.FlatAmount.String()
					createReq.Tiers[i].FlatAmount = &flatAmountStr
				}
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

type PriceResponse struct {
	*price.Price
	Meter *MeterResponse `json:"meter,omitempty"`
	Plan  *PlanResponse  `json:"plan,omitempty"`
	Addon *AddonResponse `json:"addon,omitempty"`
	Group *GroupResponse `json:"group,omitempty"`

	// TODO: Remove this once we have a proper price entity type
	PlanID string `json:"plan_id,omitempty"`
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

func (r *DeletePriceRequest) Validate() error {
	if r.EndDate != nil && r.EndDate.Before(time.Now().UTC()) {
		return ierr.NewError("end date must be in the future").
			WithHint("End date must be in the future").
			Mark(ierr.ErrValidation)
	}

	return nil
}
