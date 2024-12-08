package dto

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type CreatePriceRequest struct {
	Amount             uint64                `json:"amount" validate:"required"`
	Currency           string                `json:"currency" validate:"required,len=3"`
	PlanID             string                `json:"plan_id,omitempty"`
	Type               types.PriceType       `json:"type" validate:"required"`
	BillingPeriod      types.BillingPeriod   `json:"billing_period" validate:"required"`
	BillingPeriodCount int                   `json:"billing_period_count" validate:"required,min=1"`
	BillingModel       types.BillingModel    `json:"billing_model" validate:"required"`
	BillingCadence     types.BillingCadence  `json:"billing_cadence" validate:"required"`
	MeterID            string                `json:"meter_id,omitempty"`
	FilterValues       map[string][]string   `json:"filter_values,omitempty"`
	LookupKey          string                `json:"lookup_key,omitempty"`
	Description        string                `json:"description,omitempty"`
	Metadata           map[string]string     `json:"metadata,omitempty"`
	TierMode           types.BillingTier     `json:"tier_mode,omitempty"`
	Tiers              []price.PriceTier     `json:"tiers,omitempty"`
	Transform          *price.PriceTransform `json:"transform,omitempty"`
}

// TODO : add all price validations
func (r *CreatePriceRequest) Validate() error {

	// Base validations
	if r.Amount <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}

	// Ensure currency is lowercase
	r.Currency = strings.ToLower(r.Currency)

	// Billing model validations
	err := validator.New().Struct(r)
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

	case types.BILLING_MODEL_PACKAGE:
		if r.Transform == nil {
			return fmt.Errorf("transform is required when billing model is PACKAGE")
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
	return nil
}

func (r *CreatePriceRequest) ToPrice(ctx context.Context) *price.Price {
	// Initialize empty JSONB fields with proper zero values
	filterValues := make(price.JSONBFilters)
	if r.FilterValues != nil {
		filterValues = price.JSONBFilters(r.FilterValues)
	}

	metadata := make(price.JSONBMetadata)
	if r.Metadata != nil {
		metadata = price.JSONBMetadata(r.Metadata)
	}

	var transform price.JSONBTransform
	if r.Transform != nil {
		transform = price.JSONBTransform(*r.Transform)
	}

	var tiers price.JSONBTiers
	if r.Tiers != nil {
		tiers = price.JSONBTiers(r.Tiers)
	}

	var tierMode types.BillingTier
	if r.TierMode != "" {
		tierMode = r.TierMode
	}

	price := &price.Price{
		ID:                 uuid.New().String(),
		Amount:             r.Amount,
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
		TierMode:           tierMode,
		Tiers:              tiers,
		Transform:          transform,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	price.DisplayAmount = price.GetDisplayAmount()
	return price
}

type UpdatePriceRequest struct {
	LookupKey   string            `json:"lookup_key"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type PriceResponse struct {
	*price.Price
}

type ListPricesResponse struct {
	Prices []PriceResponse `json:"prices"`
	Total  int             `json:"total"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
}
