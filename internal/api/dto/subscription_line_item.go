package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionPriceCreateRequest struct {
	Type               types.PriceType          `json:"type" validate:"required"`
	PriceUnitType      types.PriceUnitType      `json:"price_unit_type" validate:"required"`
	BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`
	BillingPeriodCount int                      `json:"billing_period_count"`
	BillingModel       types.BillingModel       `json:"billing_model" validate:"required"`
	InvoiceCadence     types.InvoiceCadence     `json:"invoice_cadence" validate:"required"`
	Amount             *decimal.Decimal         `json:"amount,omitempty" swaggertype:"string"`
	MeterID            string                   `json:"meter_id,omitempty"`
	FilterValues       map[string][]string      `json:"filter_values,omitempty"`
	LookupKey          string                   `json:"lookup_key,omitempty"`
	TrialPeriodDays    int                      `json:"trial_period_days"`
	Description        string                   `json:"description,omitempty"`
	Metadata           map[string]string        `json:"metadata,omitempty"`
	TierMode           types.BillingTier        `json:"tier_mode,omitempty"`
	Tiers              []CreatePriceTier        `json:"tiers,omitempty"`
	TransformQuantity  *price.TransformQuantity `json:"transform_quantity,omitempty"`
	PriceUnitConfig    *PriceUnitConfig         `json:"price_unit_config,omitempty"`
	StartDate          *time.Time               `json:"start_date,omitempty"`
	EndDate            *time.Time               `json:"end_date,omitempty"`
	DisplayName        string                   `json:"display_name,omitempty"`
	MinQuantity        *int64                   `json:"min_quantity,omitempty"`
}

// ToCreatePriceRequest builds a CreatePriceRequest for subscription-scoped price creation.
// Currency, EntityType, and EntityID are set from the subscription; if StartDate is not set on the request, it defaults to the subscription's start date.
func (p *SubscriptionPriceCreateRequest) ToCreatePriceRequest(sub *subscription.Subscription) CreatePriceRequest {
	startDate := p.StartDate
	if startDate == nil {
		subStart := sub.StartDate.UTC()
		startDate = &subStart
	}
	req := CreatePriceRequest{
		Type:                 p.Type,
		PriceUnitType:        p.PriceUnitType,
		BillingPeriod:        p.BillingPeriod,
		BillingPeriodCount:   p.BillingPeriodCount,
		BillingModel:         p.BillingModel,
		InvoiceCadence:       p.InvoiceCadence,
		Amount:               p.Amount,
		MeterID:              p.MeterID,
		FilterValues:         p.FilterValues,
		LookupKey:            p.LookupKey,
		TrialPeriodDays:      p.TrialPeriodDays,
		Description:          p.Description,
		Metadata:             p.Metadata,
		TierMode:             p.TierMode,
		Tiers:                p.Tiers,
		TransformQuantity:    p.TransformQuantity,
		PriceUnitConfig:      p.PriceUnitConfig,
		StartDate:            startDate,
		EndDate:              p.EndDate,
		DisplayName:          p.DisplayName,
		MinQuantity:          p.MinQuantity,
		Currency:             sub.Currency,
		EntityType:           types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:             sub.ID,
		SkipEntityValidation: true,
	}
	if req.BillingPeriodCount < 1 {
		req.BillingPeriodCount = 1
	}
	return req
}

// CreateSubscriptionLineItemRequest represents the request to create a subscription line item.
// For prices with billing_period ONETIME, request end_date is ignored: the line item end_date is always start_date + 1 calendar day (UTC), clamped to the subscription end when present.
type CreateSubscriptionLineItemRequest struct {
	// PriceID references an existing price (plan, addon, or subscription-scoped). Exactly one of price_id or price must be set.
	PriceID string `json:"price_id,omitempty"`
	// Price defines a new price inline; server creates a subscription-scoped price and adds the line item. Exactly one of price_id or price must be set. Entity/currency are set from the subscription.
	Price                *SubscriptionPriceCreateRequest `json:"price,omitempty"`
	Quantity             decimal.Decimal                 `json:"quantity,omitempty"`
	StartDate            *time.Time                      `json:"start_date,omitempty"`
	EndDate              *time.Time                      `json:"end_date,omitempty"`
	Metadata             map[string]string               `json:"metadata,omitempty"`
	DisplayName          string                          `json:"display_name,omitempty"`
	SubscriptionPhaseID  *string                         `json:"subscription_phase_id,omitempty"`
	SkipEntitlementCheck bool                            `json:"-"` // This is used to skip entitlement check when creating a subscription line item

	// ProrationBehavior controls mid-period charge creation. Defaults to none.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// Commitment fields
	CommitmentAmount        *decimal.Decimal          `json:"commitment_amount,omitempty"`
	CommitmentQuantity      *decimal.Decimal          `json:"commitment_quantity,omitempty"`
	CommitmentType          types.CommitmentType      `json:"commitment_type,omitempty"`
	CommitmentOverageFactor *decimal.Decimal          `json:"commitment_overage_factor,omitempty"`
	CommitmentTrueUpEnabled bool                      `json:"commitment_true_up_enabled,omitempty"`
	CommitmentWindowed      bool                      `json:"commitment_windowed,omitempty"`
	CommitmentDuration      *types.BillingPeriod      `json:"commitment_duration,omitempty"`
	CommitmentTimeBuckets   []CommitmentBucketRequest `json:"commitment_time_buckets,omitempty"`
}

// DeleteSubscriptionLineItemRequest represents the request to delete a subscription line item
type DeleteSubscriptionLineItemRequest struct {
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`

	// ProrationBehavior controls mid-period credit issuance. Defaults to none.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
}

type UpdateSubscriptionLineItemRequest struct {
	// EffectiveFrom for the existing line item (if not provided, defaults to now)
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price
	Amount *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`

	// Metadata for the new line item
	Metadata map[string]string `json:"metadata,omitempty"`

	// Commitment fields
	CommitmentAmount        *decimal.Decimal     `json:"commitment_amount,omitempty"`
	CommitmentQuantity      *decimal.Decimal     `json:"commitment_quantity,omitempty"`
	CommitmentType          types.CommitmentType `json:"commitment_type,omitempty"`
	CommitmentOverageFactor *decimal.Decimal     `json:"commitment_overage_factor,omitempty"`
	CommitmentTrueUpEnabled *bool                `json:"commitment_true_up_enabled,omitempty"`
	CommitmentWindowed      *bool                `json:"commitment_windowed,omitempty"`
	CommitmentDuration      *types.BillingPeriod `json:"commitment_duration,omitempty"`
	// Pointer so an explicit empty array can clear existing buckets (omission keeps them).
	CommitmentTimeBuckets *[]CommitmentBucketRequest `json:"commitment_time_buckets,omitempty"`
}

// LineItemParams contains all necessary parameters for creating a line item
type LineItemParams struct {
	Subscription *SubscriptionResponse
	Price        *PriceResponse
	Plan         *PlanResponse  // Optional, for plan-based line items
	Addon        *AddonResponse // Optional, for addon-based line items
	EntityType   types.SubscriptionLineItemEntityType
}

// HasCommitment returns true if the request has commitment configured
func (r *CreateSubscriptionLineItemRequest) HasCommitment() bool {
	hasAmountCommitment := r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := r.CommitmentQuantity != nil && r.CommitmentQuantity.GreaterThan(decimal.Zero)
	return hasAmountCommitment || hasQuantityCommitment
}

// HasCommitment returns true if the request has commitment configured
func (r *UpdateSubscriptionLineItemRequest) HasCommitment() bool {
	hasAmountCommitment := r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := r.CommitmentQuantity != nil && r.CommitmentQuantity.GreaterThan(decimal.Zero)
	return hasAmountCommitment || hasQuantityCommitment
}

// Validate validates the create subscription line item request.
// linePrice is optional and can be provided for MinQuantity validation when using price_id.
// sub is optional; when provided, line item and inline price start/end dates are validated to fall within subscription bounds.
func (r *CreateSubscriptionLineItemRequest) Validate(linePrice *price.Price, sub *subscription.Subscription) error {
	// Exactly one of price_id or price must be set
	hasPriceID := r.PriceID != ""
	hasPrice := r.Price != nil
	if hasPriceID && hasPrice {
		return ierr.NewError("cannot provide both price_id and price").
			WithHint("Provide either price_id (existing price) or price (inline price), not both.").
			Mark(ierr.ErrValidation)
	}
	if !hasPriceID && !hasPrice {
		return ierr.NewError("either price_id or price is required").
			WithHint("Provide either price_id (existing price) or price (inline price).").
			Mark(ierr.ErrValidation)
	}

	onetimeIgnoresRequestEndDate := (linePrice != nil && linePrice.BillingPeriod == types.BILLING_PERIOD_ONETIME) ||
		(r.Price != nil && r.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME)

	// Validate start date is not after end date if both are provided
	if !onetimeIgnoresRequestEndDate && r.StartDate != nil && r.EndDate != nil {
		if r.StartDate.After(lo.FromPtr(r.EndDate)) {
			return ierr.NewError("start_date cannot be after end_date").
				WithHint("Start date cannot be after end date").
				Mark(ierr.ErrValidation)
		}
	}

	// When subscription is provided, validate line item and inline price dates fall within subscription bounds
	if sub != nil {
		subStartUTC := sub.StartDate.UTC()
		if r.StartDate != nil {
			startUTC := lo.FromPtr(r.StartDate).UTC()
			if startUTC.Before(subStartUTC) {
				return ierr.NewError("line item start_date cannot be before subscription start date").
					WithHint("start_date must be on or after the subscription's start date.").
					WithReportableDetails(map[string]interface{}{
						"start_date":         r.StartDate,
						"subscription_start": sub.StartDate,
					}).
					Mark(ierr.ErrValidation)
			}
		}
		if !onetimeIgnoresRequestEndDate && sub.EndDate != nil && r.EndDate != nil {
			subEndUTC := lo.FromPtr(sub.EndDate).UTC()
			endUTC := lo.FromPtr(r.EndDate).UTC()
			if endUTC.After(subEndUTC) {
				return ierr.NewError("line item end_date cannot be after subscription end date").
					WithHint("end_date must be on or before the subscription's end date when the subscription has an end date.").
					WithReportableDetails(map[string]interface{}{
						"end_date":         r.EndDate,
						"subscription_end": sub.EndDate,
					}).
					Mark(ierr.ErrValidation)
			}
		}
		if r.Price != nil {
			if r.Price.StartDate != nil {
				startUTC := lo.FromPtr(r.Price.StartDate).UTC()
				if startUTC.Before(subStartUTC) {
					return ierr.NewError("price start_date cannot be before subscription start date").
						WithHint("price start_date must be on or after the subscription's start date.").
						WithReportableDetails(map[string]interface{}{
							"price_start_date":   r.Price.StartDate,
							"subscription_start": sub.StartDate,
						}).
						Mark(ierr.ErrValidation)
				}
			}
			if sub.EndDate != nil && r.Price.EndDate != nil {
				subEndUTC := lo.FromPtr(sub.EndDate).UTC()
				endUTC := lo.FromPtr(r.Price.EndDate).UTC()
				if endUTC.After(subEndUTC) {
					return ierr.NewError("price end_date cannot be after subscription end date").
						WithHint("price end_date must be on or before the subscription's end date when the subscription has an end date.").
						WithReportableDetails(map[string]interface{}{
							"price_end_date":   r.Price.EndDate,
							"subscription_end": sub.EndDate,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}
	}

	// Note: inline price path (r.Price) is nil here; ONETIME billing period is validated
	// downstream in CreatePriceRequest.Validate() for that path.
	// ONETIME charges must use ADVANCE invoice cadence
	if linePrice != nil && linePrice.BillingPeriod == types.BILLING_PERIOD_ONETIME {
		if linePrice.InvoiceCadence != "" && linePrice.InvoiceCadence != types.InvoiceCadenceAdvance {
			return ierr.NewError("ONETIME charges must have invoice_cadence ADVANCE").
				WithHint("One-time charges are always billed in advance").
				Mark(ierr.ErrValidation)
		}
	}

	// Reject negative quantity; zero defaults to min_quantity downstream.
	if r.Quantity.IsNegative() {
		return ierr.NewError("quantity must be non-negative").
			WithHint("Quantity cannot be negative").
			Mark(ierr.ErrValidation)
	}

	// Validate commitment fields if provided
	if err := r.validateCommitmentFields(); err != nil {
		return err
	}

	// When using price (inline), full price validation is done in service after injecting subscription context

	if hasPrice {
		if err := validator.ValidateRequest(r.Price); err != nil {
			return err
		}
	}

	return nil
}

// validateCommitmentFieldsCommon contains shared commitment validation logic for both Create and Update requests
// isCreateRequest determines whether auto-setting of commitment type is allowed
func validateCommitmentFieldsCommon(
	commitmentAmount *decimal.Decimal,
	commitmentQuantity *decimal.Decimal,
	commitmentType types.CommitmentType,
	commitmentOverageFactor *decimal.Decimal,
	isCreateRequest bool,
) error {
	hasAmountCommitment := commitmentAmount != nil && commitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := commitmentQuantity != nil && commitmentQuantity.GreaterThan(decimal.Zero)
	hasCommitment := hasAmountCommitment || hasQuantityCommitment

	if !hasCommitment {
		// No commitment configured, nothing to validate
		return nil
	}

	// Rule 1: Cannot set both commitment_amount and commitment_quantity
	if hasAmountCommitment && hasQuantityCommitment {
		return ierr.NewError("cannot set both commitment_amount and commitment_quantity").
			WithHint("Specify either commitment_amount or commitment_quantity, not both").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount":   commitmentAmount,
				"commitment_quantity": commitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: Commitment type must be valid
	if commitmentType != "" && !commitmentType.Validate() {
		return ierr.NewError("invalid commitment_type").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": commitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 3: For update requests, commitment type is required when commitment is set
	// For create requests, it will be auto-set in normalization
	if !isCreateRequest && hasCommitment && commitmentType == "" {
		return ierr.NewError("commitment_type is required").
			WithHint("Commitment type must be either 'amount' or 'quantity'").
			WithReportableDetails(map[string]interface{}{
				"commitment_type": commitmentType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 4: Validate commitment type matches the provided field (if type is specified)
	if commitmentType != "" {
		if hasAmountCommitment && commitmentType != types.COMMITMENT_TYPE_AMOUNT {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_amount is set, commitment_type must be 'amount'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":   commitmentType,
					"commitment_amount": commitmentAmount,
				}).
				Mark(ierr.ErrValidation)
		}

		if hasQuantityCommitment && commitmentType != types.COMMITMENT_TYPE_QUANTITY {
			return ierr.NewError("commitment_type mismatch").
				WithHint("When commitment_quantity is set, commitment_type must be 'quantity'").
				WithReportableDetails(map[string]interface{}{
					"commitment_type":     commitmentType,
					"commitment_quantity": commitmentQuantity,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Rule 5: Overage factor is required and must be greater than 1.0
	if commitmentOverageFactor == nil {
		return ierr.NewError("commitment_overage_factor is required when commitment is set").
			WithHint("Specify a commitment_overage_factor greater than 1.0").
			Mark(ierr.ErrValidation)
	}

	if commitmentOverageFactor.LessThanOrEqual(decimal.NewFromFloat(1)) {
		return ierr.NewError("commitment_overage_factor must be greater than 1.0").
			WithHint("Overage factor determines the multiplier for usage beyond commitment").
			WithReportableDetails(map[string]interface{}{
				"commitment_overage_factor": commitmentOverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 6: Validate commitment values are positive
	if hasAmountCommitment && commitmentAmount.IsNegative() {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": commitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if hasQuantityCommitment && commitmentQuantity.IsNegative() {
		return ierr.NewError("commitment_quantity must be non-negative").
			WithHint("Commitment quantity cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"commitment_quantity": commitmentQuantity,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// validateCommitmentFields validates commitment-related fields for create request
func (r *CreateSubscriptionLineItemRequest) validateCommitmentFields() error {
	// Use shared validation logic
	err := validateCommitmentFieldsCommon(
		r.CommitmentAmount,
		r.CommitmentQuantity,
		r.CommitmentType,
		r.CommitmentOverageFactor,
		true, // isCreateRequest
	)
	if err != nil {
		return err
	}

	// commitment_time_buckets only constrains the commitment-application window —
	// it has no meaning unless commitment_windowed is true.
	if len(r.CommitmentTimeBuckets) > 0 && !r.CommitmentWindowed {
		return ierr.NewError("commitment_time_buckets requires commitment_windowed=true").
			WithHint("Set commitment_windowed=true to apply commitment only during the configured hours").
			Mark(ierr.ErrValidation)
	}
	if err := validateTimeOfDayBuckets(r.CommitmentTimeBuckets); err != nil {
		return err
	}

	// Auto-set commitment type if not provided (only for create requests)
	if r.HasCommitment() && r.CommitmentType == "" {
		hasAmountCommitment := r.CommitmentAmount != nil && r.CommitmentAmount.GreaterThan(decimal.Zero)
		if hasAmountCommitment {
			r.CommitmentType = types.COMMITMENT_TYPE_AMOUNT
		} else {
			r.CommitmentType = types.COMMITMENT_TYPE_QUANTITY
		}
	}

	return nil
}

// ToSubscriptionLineItem converts the request to a domain subscription line item
func (r *CreateSubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context, params LineItemParams) *subscription.SubscriptionLineItem {
	// Resolve InvoiceCadence from price
	invoiceCadence := types.InvoiceCadenceAdvance
	if params.Price != nil {
		invoiceCadence = params.Price.InvoiceCadence
		// ONETIME charges default to ADVANCE invoice cadence if not explicitly set
		if params.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME && invoiceCadence == "" {
			invoiceCadence = types.InvoiceCadenceAdvance
		}
	}

	lineItem := &subscription.SubscriptionLineItem{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:      params.Subscription.ID,
		CustomerID:          params.Subscription.CustomerID,
		PriceID:             r.PriceID,
		PriceType:           params.Price.Type,
		Currency:            params.Subscription.Currency,
		BillingPeriod:       params.Price.BillingPeriod,
		InvoiceCadence:      invoiceCadence,
		EntityType:          params.EntityType,
		Metadata:            r.Metadata,
		SubscriptionPhaseID: r.SubscriptionPhaseID,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}

	if r.DisplayName != "" {
		lineItem.DisplayName = r.DisplayName
	} else if params.Price != nil {
		lineItem.DisplayName = params.Price.DisplayName
	}

	// Set price type specific fields
	if params.Price != nil {
		if params.Price.Type == types.PRICE_TYPE_USAGE {
			lineItem.MeterID = params.Price.MeterID
			if params.Price.Meter != nil {
				lineItem.MeterDisplayName = params.Price.Meter.Name
			}
			lineItem.Quantity = decimal.Zero
		} else {
			// Zero/omitted quantity defaults to MinQuantity, else the price's default.
			// Non-zero quantity is used as-is (no floor validation on the create path).
			lineItem.Quantity = price.ApplyQuantityDefault(r.Quantity, params.Price.Price)
		}

		// Copy price unit fields from price to line item
		lineItem.PriceUnitID = params.Price.PriceUnitID
		lineItem.PriceUnit = params.Price.PriceUnit
	} else {
		lineItem.Quantity = decimal.NewFromInt(1)
	}

	// Set entity-specific fields
	if params.EntityType == types.SubscriptionLineItemEntityTypePlan && params.Plan != nil {
		lineItem.EntityID = params.Plan.ID
		lineItem.PlanDisplayName = params.Plan.Name
	} else if params.EntityType == types.SubscriptionLineItemEntityTypeAddon && params.Addon != nil {
		lineItem.EntityID = params.Addon.ID
		if lineItem.Metadata == nil {
			lineItem.Metadata = make(map[string]string)
		}
		lineItem.Metadata["addon_id"] = params.Addon.ID
		lineItem.Metadata["subscription_id"] = params.Subscription.ID
		lineItem.Metadata["addon_quantity"] = "1"
		lineItem.Metadata["addon_status"] = string(types.AddonStatusActive)
	} else if params.EntityType == types.SubscriptionLineItemEntityTypeSubscription && params.Subscription != nil {
		lineItem.EntityID = params.Subscription.ID
		if params.Price != nil && params.Price.DisplayName != "" {
			lineItem.PlanDisplayName = params.Price.DisplayName
		}
	}

	// Effective start = latest of subscription start, price start, and request start_date (when provided).
	startDate := params.Subscription.StartDate
	if params.Price != nil && params.Price.StartDate != nil && params.Price.StartDate.After(startDate) {
		startDate = lo.FromPtr(params.Price.StartDate)
	}
	if r.StartDate != nil && r.StartDate.After(startDate) {
		startDate = lo.FromPtr(r.StartDate)
	}
	lineItem.StartDate = startDate.UTC()

	if params.Price != nil && params.Price.BillingPeriod == types.BILLING_PERIOD_ONETIME {
		end := lineItem.StartDate.Add(time.Second)
		if sub := params.Subscription; sub != nil && sub.EndDate != nil {
			if se := lo.FromPtr(sub.EndDate).UTC(); end.After(se) {
				end = se
			}
		}
		lineItem.EndDate = end
	} else if r.EndDate != nil {
		endDateVal := r.EndDate.UTC()
		if startDate.After(endDateVal) {
			endDateVal = startDate.UTC()
		}
		lineItem.EndDate = endDateVal
	}

	// Set commitment fields if provided
	if r.CommitmentAmount != nil {
		lineItem.CommitmentAmount = r.CommitmentAmount
	}
	if r.CommitmentQuantity != nil {
		lineItem.CommitmentQuantity = r.CommitmentQuantity
	}
	if r.CommitmentType != "" {
		lineItem.CommitmentType = r.CommitmentType
	}
	if r.CommitmentOverageFactor != nil {
		lineItem.CommitmentOverageFactor = r.CommitmentOverageFactor
	}
	lineItem.CommitmentTrueUpEnabled = r.CommitmentTrueUpEnabled
	lineItem.CommitmentWindowed = r.CommitmentWindowed
	if r.CommitmentDuration != nil {
		lineItem.CommitmentDuration = r.CommitmentDuration
	}
	if len(r.CommitmentTimeBuckets) > 0 {
		// Build the domain buckets here (IDs + commitment fields); the service
		// layer materializes a price per bucket and fills in the PriceIDs.
		lineItem.CommitmentTimeBuckets = bucketRequestsToDomain(r.CommitmentTimeBuckets)
	}

	return lineItem
}

// Validate validates the delete subscription line item request
func (r *DeleteSubscriptionLineItemRequest) Validate() error {
	return nil
}

// Validate validates the update subscription line item request
func (r *UpdateSubscriptionLineItemRequest) Validate() error {
	// If EffectiveFrom is provided, at least one critical field must be present
	if r.EffectiveFrom != nil && !r.ShouldCreateNewLineItem() {
		return ierr.NewError("effective_from requires at least one critical field").
			WithHint("When providing effective_from, you must also provide one of: amount, billing_model, tier_mode, tiers, transform_quantity, or commitment fields").
			Mark(ierr.ErrValidation)
	}

	// Validate commitment fields if provided
	if err := r.validateCommitmentFields(); err != nil {
		return err
	}

	return nil
}

// validateCommitmentFields validates commitment-related fields for update request
func (r *UpdateSubscriptionLineItemRequest) validateCommitmentFields() error {
	// Use shared validation logic (update requests require explicit commitment type)
	if err := validateCommitmentFieldsCommon(
		r.CommitmentAmount,
		r.CommitmentQuantity,
		r.CommitmentType,
		r.CommitmentOverageFactor,
		false, // isCreateRequest
	); err != nil {
		return err
	}

	// commitment_time_buckets only constrains the commitment-application window —
	// it has no meaning unless commitment_windowed is true. We can only enforce this
	// when both fields are explicitly provided in the update; cross-checks against
	// the existing line item's windowed flag happen at the service layer.
	if r.CommitmentTimeBuckets != nil && len(*r.CommitmentTimeBuckets) > 0 &&
		r.CommitmentWindowed != nil && !*r.CommitmentWindowed {
		return ierr.NewError("commitment_time_buckets requires commitment_windowed=true").
			WithHint("Set commitment_windowed=true to apply commitment only during the configured hours").
			Mark(ierr.ErrValidation)
	}
	if r.CommitmentTimeBuckets != nil {
		if err := validateTimeOfDayBuckets(*r.CommitmentTimeBuckets); err != nil {
			return err
		}
	}
	return nil
}

// CommitmentBucketRequest is the inline shape for one time-of-day commitment
// bucket on a subscription line item.
//
// New bucket: omit id and provide price — the service creates a
// SUBSCRIPTION-scoped Price and stores its id on the bucket.
//
// Existing bucket (update flows): provide the id previously returned by the
// API and omit price — the bucket keeps its existing price. Commitment fields
// always come from the request, so a commitment can change while the price is
// kept.
type CommitmentBucketRequest struct {
	ID              string               `json:"id,omitempty"`
	Start           types.Bucket         `json:"start"`
	End             types.Bucket         `json:"end"`
	Price           *CreatePriceRequest  `json:"price,omitempty"`
	CommitmentType  types.CommitmentType `json:"commitment_type"`
	CommitmentValue decimal.Decimal      `json:"commitment_value" swaggertype:"string"`
	OverageFactor   *decimal.Decimal     `json:"overage_factor,omitempty" swaggertype:"string"`
	TrueUpEnabled   bool                 `json:"true_up_enabled,omitempty"`
}

// Validate runs per-bucket field validation; idx is the bucket's position in
// the request array, surfaced in error details. Array invariants (overlap,
// window alignment) live on TimeOfDayBuckets and are applied by the service
// after prices are created (and after the meter is loaded so we know windowMin).
func (r CommitmentBucketRequest) Validate(idx int) error {
	if err := validateBucketPoint(r.Start, idx); err != nil {
		return err
	}
	if err := validateBucketPoint(r.End, idx); err != nil {
		return err
	}
	// Exactly one of id (reuse existing bucket + price) or price (create new)
	// must be provided.
	if r.ID == "" && r.Price == nil {
		return ierr.NewError("bucket price is required").
			WithHint("Provide price for a new bucket, or id to keep an existing bucket").
			WithReportableDetails(map[string]interface{}{"bucket_index": idx}).
			Mark(ierr.ErrValidation)
	}
	if r.ID != "" && r.Price != nil {
		return ierr.NewError("cannot provide both id and price on a bucket").
			WithHint("Provide id to keep the existing bucket price, or price (without id) to create a new bucket").
			WithReportableDetails(map[string]interface{}{"bucket_index": idx}).
			Mark(ierr.ErrValidation)
	}
	if r.Price != nil && r.Price.EntityType != "" && r.Price.EntityType != types.PRICE_ENTITY_TYPE_SUBSCRIPTION {
		return ierr.NewError("bucket price entity_type must be SUBSCRIPTION").
			WithHint("Use entity_type=SUBSCRIPTION on inline bucket prices").
			WithReportableDetails(map[string]interface{}{"bucket_index": idx}).
			Mark(ierr.ErrValidation)
	}
	tmp := types.TimeOfDayBucket{
		Start:           r.Start,
		End:             r.End,
		CommitmentType:  r.CommitmentType,
		CommitmentValue: r.CommitmentValue,
		OverageFactor:   r.OverageFactor,
		TrueUpEnabled:   r.TrueUpEnabled,
	}
	if err := tmp.Validate(); err != nil {
		// Type-level errors don't know the array position; annotate it here.
		return ierr.WithError(err).
			WithReportableDetails(map[string]interface{}{"bucket_index": idx}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToTimeOfDayBucket maps the request to a domain bucket with all commitment
// fields but no PriceID — the service creates the bucket's price (or resolves
// the existing one when id is provided) and fills in PriceID. A request without
// an id gets a fresh server-assigned ID.
func (r CommitmentBucketRequest) ToTimeOfDayBucket() types.TimeOfDayBucket {
	id := r.ID
	if id == "" {
		id = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COMMITMENT_BUCKET)
	}
	return types.TimeOfDayBucket{
		ID:              id,
		Start:           r.Start,
		End:             r.End,
		CommitmentType:  r.CommitmentType,
		CommitmentValue: r.CommitmentValue,
		OverageFactor:   r.OverageFactor,
		TrueUpEnabled:   r.TrueUpEnabled,
	}
}

// bucketRequestsToDomain maps a slice of bucket requests to domain buckets
// (preserving order) so the service can fill in PriceIDs positionally.
func bucketRequestsToDomain(reqs []CommitmentBucketRequest) types.TimeOfDayBuckets {
	if len(reqs) == 0 {
		return types.TimeOfDayBuckets{}
	}
	out := make(types.TimeOfDayBuckets, len(reqs))
	for i, r := range reqs {
		out[i] = r.ToTimeOfDayBucket()
	}
	return out
}

// validateTimeOfDayBuckets enforces per-bucket Hour ∈ [0, 24] and Minute ∈ [0, 59],
// rejecting Hour=24 combined with Minute>0 (only 24:00 is a meaningful end-of-day).
func validateTimeOfDayBuckets(buckets []CommitmentBucketRequest) error {
	for i, b := range buckets {
		if err := b.Validate(i); err != nil {
			return err
		}
	}
	return nil
}

func validateBucketPoint(b types.Bucket, idx int) error {
	if b.Hour < 0 || b.Hour > 24 {
		return ierr.NewError("commitment_time_buckets: hour out of range").
			WithHint("Hour must be in [0, 24]").
			WithReportableDetails(map[string]interface{}{
				"index": idx,
				"hour":  b.Hour,
			}).
			Mark(ierr.ErrValidation)
	}
	if b.Minute < 0 || b.Minute > 59 {
		return ierr.NewError("commitment_time_buckets: minute out of range").
			WithHint("Minute must be in [0, 59]").
			WithReportableDetails(map[string]interface{}{
				"index":  idx,
				"minute": b.Minute,
			}).
			Mark(ierr.ErrValidation)
	}
	if b.Hour == 24 && b.Minute > 0 {
		return ierr.NewError("commitment_time_buckets: 24:00 is the only allowed end-of-day value").
			WithHint("Hour=24 must have Minute=0").
			WithReportableDetails(map[string]interface{}{
				"index":  idx,
				"hour":   b.Hour,
				"minute": b.Minute,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ShouldCreateNewLineItem checks if the request contains any critical fields that require creating a new line item
func (r *UpdateSubscriptionLineItemRequest) ShouldCreateNewLineItem() bool {
	return (r.Amount != nil && !r.Amount.IsZero()) ||
		r.BillingModel != "" ||
		r.TierMode != "" ||
		len(r.Tiers) > 0 ||
		r.TransformQuantity != nil ||
		r.HasCommitment() ||
		r.CommitmentOverageFactor != nil ||
		r.CommitmentTrueUpEnabled != nil ||
		r.CommitmentWindowed != nil ||
		r.CommitmentDuration != nil ||
		r.CommitmentTimeBuckets != nil
}

// ToSubscriptionLineItem converts the update request to a domain subscription line item
// This method creates a new line item based on the existing one with updated parameters
func (r *UpdateSubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context, existingLineItem *subscription.SubscriptionLineItem, newPriceID string) *subscription.SubscriptionLineItem {
	// Start with the existing line item as base
	newLineItem := &subscription.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   existingLineItem.SubscriptionID,
		CustomerID:       existingLineItem.CustomerID,
		PriceID:          newPriceID,
		PriceType:        existingLineItem.PriceType,
		Currency:         existingLineItem.Currency,
		BillingPeriod:    existingLineItem.BillingPeriod,
		InvoiceCadence:   existingLineItem.InvoiceCadence,
		EntityType:       existingLineItem.EntityType,
		EntityID:         existingLineItem.EntityID,
		PlanDisplayName:  existingLineItem.PlanDisplayName,
		MeterID:          existingLineItem.MeterID,
		MeterDisplayName: existingLineItem.MeterDisplayName,
		DisplayName:      existingLineItem.DisplayName,
		Quantity:         existingLineItem.Quantity,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	// Set metadata - use provided metadata or keep existing
	if r.Metadata != nil {
		newLineItem.Metadata = r.Metadata
	} else {
		newLineItem.Metadata = existingLineItem.Metadata
	}

	// Set commitment fields - use provided values or keep existing
	if r.CommitmentAmount != nil {
		newLineItem.CommitmentAmount = r.CommitmentAmount
	} else {
		newLineItem.CommitmentAmount = existingLineItem.CommitmentAmount
	}

	if r.CommitmentQuantity != nil {
		newLineItem.CommitmentQuantity = r.CommitmentQuantity
	} else {
		newLineItem.CommitmentQuantity = existingLineItem.CommitmentQuantity
	}

	if r.CommitmentType != "" {
		newLineItem.CommitmentType = r.CommitmentType
	} else {
		newLineItem.CommitmentType = existingLineItem.CommitmentType
	}

	if r.CommitmentOverageFactor != nil {
		newLineItem.CommitmentOverageFactor = r.CommitmentOverageFactor
	} else {
		newLineItem.CommitmentOverageFactor = existingLineItem.CommitmentOverageFactor
	}

	if r.CommitmentTrueUpEnabled != nil {
		newLineItem.CommitmentTrueUpEnabled = *r.CommitmentTrueUpEnabled
	} else {
		newLineItem.CommitmentTrueUpEnabled = existingLineItem.CommitmentTrueUpEnabled
	}

	if r.CommitmentWindowed != nil {
		newLineItem.CommitmentWindowed = *r.CommitmentWindowed
	} else {
		newLineItem.CommitmentWindowed = existingLineItem.CommitmentWindowed
	}

	if r.CommitmentDuration != nil {
		newLineItem.CommitmentDuration = r.CommitmentDuration
	} else {
		newLineItem.CommitmentDuration = existingLineItem.CommitmentDuration
	}

	if r.CommitmentTimeBuckets != nil {
		// Replace-all: build fresh domain buckets (new IDs, empty PriceIDs); the
		// service layer materializes a price per bucket and fills in the PriceIDs.
		newLineItem.CommitmentTimeBuckets = bucketRequestsToDomain(*r.CommitmentTimeBuckets)
	} else {
		newLineItem.CommitmentTimeBuckets = existingLineItem.CommitmentTimeBuckets
	}

	return newLineItem
}
