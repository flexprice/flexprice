package dto

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
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
			return fmt.Errorf("invalid amount format: %w", err)
		}
	}

	if amount.LessThan(decimal.Zero) {
		return fmt.Errorf("amount must be greater than 0")
	}

	// Ensure currency is lowercase
	r.Currency = strings.ToLower(r.Currency)

	// Billing model validations
	err = validator.New().Struct(r)
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

	switch r.BillingModel {
	case types.BILLING_MODEL_TIERED:
		if len(r.Tiers) == 0 {
			return fmt.Errorf("tiers are required when billing model is TIERED")
		}
		if r.TierMode == "" {
			return fmt.Errorf("tier_mode is required when billing model is TIERED")
		}
		err = r.TierMode.Validate()
		if err != nil {
			return err
		}

	case types.BILLING_MODEL_PACKAGE:
		if r.TransformQuantity == nil {
			return fmt.Errorf("transform_quantity is required when billing model is PACKAGE")
		}

		if r.TransformQuantity.DivideBy <= 0 {
			return fmt.Errorf("transform_quantity.divide_by must be greater than 0 when billing model is PACKAGE")
		}
	}

	switch r.Type {
	case types.PRICE_TYPE_USAGE:
		if r.MeterID == "" {
			return fmt.Errorf("meter_id is required when type is USAGE")
		}
	}

	switch r.BillingCadence {
	case types.BILLING_CADENCE_RECURRING:
		if r.BillingPeriod == "" {
			return fmt.Errorf("billing_period is required when billing_cadence is RECURRING")
		}
	}

	// Validate tiers if present
	if len(r.Tiers) > 0 && r.BillingModel == types.BILLING_MODEL_TIERED {
		for i, tier := range r.Tiers {
			tierAmount, err := decimal.NewFromString(tier.UnitAmount)
			if err != nil {
				return fmt.Errorf("invalid unit amount in tier %d: %w", i+1, err)
			}

			if tierAmount.LessThan(decimal.Zero) {
				return fmt.Errorf("tier %d: unit amount must be greater than 0", i+1)
			}

			if tier.FlatAmount != nil {
				flatAmount, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return fmt.Errorf("invalid flat amount in tier %d: %w", i+1, err)
				}

				if flatAmount.LessThan(decimal.Zero) {
					return fmt.Errorf("tier %d: flat amount must be greater than 0", i+1)
				}
			}
		}
	}
	return nil
}

func (r *CreatePriceRequest) ToPrice(ctx context.Context) (*price.Price, error) {
	amount := decimal.Zero
	if r.Amount != "" {
		var err error
		amount, err = decimal.NewFromString(r.Amount)
		if err != nil {
			return nil, fmt.Errorf("invalid amount format: %w", err)
		}
	}

	// Initialize empty JSONB fields with proper zero values
	filterValues := make(price.JSONBFilters)
	if r.FilterValues != nil {
		filterValues = price.JSONBFilters(r.FilterValues)
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
				return nil, fmt.Errorf("invalid unit amount in tier %d: %w", i, err)
			}

			var flatAmount *decimal.Decimal
			if tier.FlatAmount != nil {
				parsed, err := decimal.NewFromString(*tier.FlatAmount)
				if err != nil {
					return nil, fmt.Errorf("invalid flat amount in tier %d: %w", i, err)
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
		MeterID:            r.MeterID,
		FilterValues:       filterValues,
		LookupKey:          r.LookupKey,
		Description:        r.Description,
		Metadata:           metadata,
		TierMode:           r.TierMode,
		Tiers:              tiers,
		TransformQuantity:  transformQuantity,
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

type ListPricesResponse struct {
	Prices []PriceResponse `json:"prices"`
	Total  int             `json:"total"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
}
