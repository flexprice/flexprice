package dto

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type CreatePriceRequest struct {
	Amount             string                   `json:"amount"`
	Currency           string                   `json:"currency" validate:"required,len=3"`
	PlanID             string                   `json:"plan_id,omitempty"`
	Type               types.PriceType          `json:"type" validate:"required"`
	BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`
	BillingPeriodCount int                      `json:"billing_period_count" validate:"required,min=1"`
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
}

type CreatePriceTier struct {
	UpTo       *uint64 `json:"up_to"`
	UnitAmount string  `json:"unit_amount" validate:"required"`
	FlatAmount *string `json:"flat_amount" validate:"omitempty"`
}

// TODO : add all price validations
func (r *CreatePriceRequest) Validate() error {
	var err error
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

	if amount.LessThan(decimal.Zero) {
		return ierr.NewError("amount must be greater than 0").
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
		if len(r.Tiers) == 0 {
			return ierr.NewError("tiers are required when billing model is TIERED").
				WithHint("Price Tiers are required to set up tiered pricing").
				Mark(ierr.ErrValidation)
		}
		if r.TierMode == "" {
			return ierr.NewError("tier_mode is required when billing model is TIERED").
				WithHint("Price Tier mode is required to set up tiered pricing").
				Mark(ierr.ErrValidation)
		}
		err = r.TierMode.Validate()
		if err != nil {
			return err
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
					WithHint("Unit amount must be greater than 0").
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
						WithHint("Flat amount must be greater than 0").
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

	return nil
}

func (r *CreatePriceRequest) ToPrice(ctx context.Context) (*price.Price, error) {
	amount := decimal.Zero
	if r.Amount != "" {
		var err error
		amount, err = decimal.NewFromString(r.Amount)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Amount must be a valid decimal number").
				WithReportableDetails(map[string]interface{}{
					"amount": r.Amount,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	metadata := make(price.JSONBMetadata)
	if r.Metadata != nil {
		metadata = price.JSONBMetadata(r.Metadata)
	}

	var transformQuantity price.JSONBTransformQuantity
	if r.TransformQuantity != nil {
		transformQuantity = price.JSONBTransformQuantity(*r.TransformQuantity)
	}

	var tiers price.JSONBTiers
	if r.Tiers != nil {
		priceTiers := make([]price.PriceTier, len(r.Tiers))
		for i, tier := range r.Tiers {
			unitAmount, err := decimal.NewFromString(tier.UnitAmount)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Unit amount must be a valid decimal number").
					WithReportableDetails(map[string]interface{}{
						"unit_amount": tier.UnitAmount,
					}).
					Mark(ierr.ErrValidation)
			}

			var flatAmount *decimal.Decimal
			if tier.FlatAmount != nil {
				parsed, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Unit amount must be a valid decimal number").
						WithReportableDetails(map[string]interface{}{
							"flat_amount": tier.FlatAmount,
						}).
						Mark(ierr.ErrValidation)
				}
				flatAmount = &parsed
			}

			priceTiers[i] = price.PriceTier{
				UpTo:       tier.UpTo,
				UnitAmount: unitAmount,
				FlatAmount: flatAmount,
			}
		}

		tiers = price.JSONBTiers(priceTiers)
	}

	price := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             amount,
		Currency:           r.Currency,
		PlanID:             r.PlanID,
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
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	price.DisplayAmount = price.GetDisplayAmount()
	return price, nil
}

type UpdatePriceRequest struct {
	LookupKey   string            `json:"lookup_key"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type PriceResponse struct {
	*price.Price
	Meter *MeterResponse `json:"meter,omitempty"`
}

// ListPricesResponse represents the response for listing prices
type ListPricesResponse = types.ListResponse[*PriceResponse]

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

// CreatePriceWithUnitConfigRequest is the request for creating a price with a price unit
type CreatePriceWithUnitConfigRequest struct {
	PriceUnit          string                   `json:"price_unit" validate:"required,len=3"`
	Amount             string                   `json:"amount"`
	Currency           string                   `json:"currency" validate:"required,len=3"`
	PlanID             string                   `json:"plan_id,omitempty"`
	Type               types.PriceType          `json:"type" validate:"required"`
	BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`
	BillingPeriodCount int                      `json:"billing_period_count" validate:"required,min=1"`
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
}

func (r *CreatePriceWithUnitConfigRequest) Validate() error {
	// Validate required fields
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}
	// Ensure currency and price unit are lowercase
	r.Currency = strings.ToLower(r.Currency)
	r.PriceUnit = strings.ToLower(r.PriceUnit)
	return nil
}
