package dto

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreatePlanRequest struct {
	Name         string                         `json:"name" validate:"required"`
	LookupKey    string                         `json:"lookup_key"`
	Description  string                         `json:"description"`
	Prices       []CreatePlanPriceRequest       `json:"prices"`
	Entitlements []CreatePlanEntitlementRequest `json:"entitlements"`
	CreditGrants []CreateCreditGrantRequest     `json:"credit_grants"`
}

type CreatePlanPriceRequest struct {
	*CreatePriceRequest
}

// Validate validates the CreatePlanPriceRequest, skipping plan_id/addon_id validation
// since these will be set after the plan is created
func (r *CreatePlanPriceRequest) Validate() error {
	if r.CreatePriceRequest == nil {
		return errors.NewError("price request is required").
			WithHint("Please provide a valid price request").
			Mark(errors.ErrValidation)
	}

	// Validate all fields except plan_id/addon_id
	req := r.CreatePriceRequest

	// Validate amount
	if req.Amount == "" {
		return errors.NewError("amount is required").
			WithHint("Please provide a valid amount").
			Mark(errors.ErrValidation)
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return errors.NewError("invalid amount format").
			WithHint("Please provide a valid decimal amount").
			WithReportableDetails(map[string]interface{}{
				"amount": req.Amount,
			}).
			Mark(errors.ErrValidation)
	}

	if amount.LessThan(decimal.Zero) {
		return errors.NewError("amount must be greater than 0").
			WithHint("Amount cannot be negative").
			Mark(errors.ErrValidation)
	}

	// Ensure currency is lowercase
	req.Currency = strings.ToLower(req.Currency)

	// Skip plan_id/addon_id validation since they will be set after plan creation

	// Billing model validations
	err = validator.ValidateRequest(req)
	if err != nil {
		return err
	}

	// Validate input field types
	err = req.Type.Validate()
	if err != nil {
		return err
	}

	err = req.BillingCadence.Validate()
	if err != nil {
		return err
	}

	err = req.BillingModel.Validate()
	if err != nil {
		return err
	}

	err = req.BillingPeriod.Validate()
	if err != nil {
		return err
	}

	err = req.InvoiceCadence.Validate()
	if err != nil {
		return err
	}

	switch req.BillingModel {
	case types.BILLING_MODEL_TIERED:
		if len(req.Tiers) == 0 {
			return errors.NewError("tiers are required when billing model is TIERED").
				WithHint("Price Tiers are required to set up tiered pricing").
				Mark(errors.ErrValidation)
		}
		if req.TierMode == "" {
			return errors.NewError("tier_mode is required when billing model is TIERED").
				WithHint("Price Tier mode is required to set up tiered pricing").
				Mark(errors.ErrValidation)
		}
		err = req.TierMode.Validate()
		if err != nil {
			return err
		}

	case types.BILLING_MODEL_PACKAGE:
		if req.TransformQuantity == nil {
			return errors.NewError("transform_quantity is required when billing model is PACKAGE").
				WithHint("Please provide the number of units to set up package pricing").
				Mark(errors.ErrValidation)
		}

		if req.TransformQuantity.DivideBy <= 0 {
			return errors.NewError("transform_quantity.divide_by must be greater than 0 when billing model is PACKAGE").
				WithHint("Please provide a valid number of units to set up package pricing").
				Mark(errors.ErrValidation)
		}

		// Validate round type
		if req.TransformQuantity.Round == "" {
			req.TransformQuantity.Round = types.ROUND_UP // Default to rounding up
		} else if req.TransformQuantity.Round != types.ROUND_UP && req.TransformQuantity.Round != types.ROUND_DOWN {
			return errors.NewError("invalid rounding type- allowed values are up and down").
				WithHint("Please provide a valid rounding type for package pricing").
				WithReportableDetails(map[string]interface{}{
					"round":   req.TransformQuantity.Round,
					"allowed": []string{types.ROUND_UP, types.ROUND_DOWN},
				}).
				Mark(errors.ErrValidation)
		}
	}

	switch req.Type {
	case types.PRICE_TYPE_USAGE:
		if req.MeterID == "" {
			return errors.NewError("meter_id is required for usage-based pricing").
				WithHint("Please provide a valid meter ID for usage-based pricing").
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

type CreatePlanEntitlementRequest struct {
	*CreateEntitlementRequest
}

// Validate validates the CreatePlanEntitlementRequest, skipping plan_id/addon_id validation
// since these will be set after the plan is created
func (r *CreatePlanEntitlementRequest) Validate() error {
	if r.CreateEntitlementRequest == nil {
		return errors.NewError("entitlement request is required").
			WithHint("Please provide a valid entitlement request").
			Mark(errors.ErrValidation)
	}

	// Validate all fields except plan_id/addon_id
	req := r.CreateEntitlementRequest

	// Validate feature_id
	if req.FeatureID == "" {
		return errors.NewError("feature_id is required").
			WithHint("Please provide a valid feature ID").
			Mark(errors.ErrValidation)
	}

	// Skip plan_id/addon_id validation since they will be set after plan creation

	// Validate feature_type
	err := req.FeatureType.Validate()
	if err != nil {
		return err
	}

	// Validate based on feature type
	switch req.FeatureType {
	case types.FeatureTypeMetered:
		if req.UsageResetPeriod != "" {
			if err := req.UsageResetPeriod.Validate(); err != nil {
				return err
			}
		}
	case types.FeatureTypeStatic:
		if req.StaticValue == "" {
			return errors.NewError("static_value is required for static features").
				WithHint("Static value is required for static features").
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

func (r *CreatePlanRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
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

	for _, cg := range r.CreditGrants {
		if err := r.validateCreditGrantForPlan(cg); err != nil {
			return err
		}
	}

	return nil
}

// validateCreditGrantForPlan validates a credit grant for plan creation
// This is similar to CreditGrant.Validate() but skips plan_id validation since
// the plan ID will be set after the plan is created
func (r *CreatePlanRequest) validateCreditGrantForPlan(cg CreateCreditGrantRequest) error {
	if cg.Name == "" {
		return errors.NewError("name is required").
			WithHint("Please provide a name for the credit grant").
			Mark(errors.ErrValidation)
	}

	if err := cg.Scope.Validate(); err != nil {
		return err
	}

	// For plan creation, we only validate PLAN scope (subscription scope not allowed)
	if cg.Scope != types.CreditGrantScopePlan {
		return errors.NewError("only PLAN scope is allowed for credit grants in plan creation").
			WithHint("Credit grants in plan creation must have PLAN scope").
			WithReportableDetails(map[string]interface{}{
				"scope": cg.Scope,
			}).
			Mark(errors.ErrValidation)
	}

	// Ensure subscription_id is not provided for plan-scoped grants
	if cg.SubscriptionID != nil && *cg.SubscriptionID != "" {
		return errors.NewError("subscription_id should not be provided for plan-scoped credit grants").
			WithHint("Credit grants in plan creation should not include subscription_id").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": *cg.SubscriptionID,
			}).
			Mark(errors.ErrValidation)
	}

	// Ensure plan_id is not provided in the request (it will be set automatically)
	if cg.PlanID != nil && *cg.PlanID != "" {
		return errors.NewError("plan_id should not be provided for credit grants in plan creation").
			WithHint("The plan_id will be set automatically when creating the plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": *cg.PlanID,
			}).
			Mark(errors.ErrValidation)
	}

	if cg.Credits.LessThanOrEqual(decimal.Zero) {
		return errors.NewError("credits must be greater than zero").
			WithHint("Please provide a positive credits").
			WithReportableDetails(map[string]interface{}{
				"credits": cg.Credits,
			}).
			Mark(errors.ErrValidation)
	}

	if err := cg.Cadence.Validate(); err != nil {
		return err
	}

	if err := cg.ExpirationType.Validate(); err != nil {
		return err
	}

	// Validate based on cadence
	if cg.Cadence == types.CreditGrantCadenceRecurring {
		if cg.Period == nil || lo.FromPtr(cg.Period) == "" {
			return errors.NewError("period is required for RECURRING cadence").
				WithHint("Please provide a valid period (e.g., MONTHLY, YEARLY)").
				WithReportableDetails(map[string]interface{}{
					"cadence": cg.Cadence,
				}).
				Mark(errors.ErrValidation)
		}

		if err := cg.Period.Validate(); err != nil {
			return err
		}

		if cg.PeriodCount == nil || lo.FromPtr(cg.PeriodCount) <= 0 {
			return errors.NewError("period_count is required for RECURRING cadence").
				WithHint("Please provide a valid period_count").
				WithReportableDetails(map[string]interface{}{
					"period_count": lo.FromPtr(cg.PeriodCount),
				}).
				Mark(errors.ErrValidation)
		}
	}

	if cg.ExpirationType == types.CreditGrantExpiryTypeDuration {
		if cg.ExpirationDurationUnit == nil {
			return errors.NewError("expiration_duration_unit is required for DURATION expiration type").
				WithHint("Please provide a valid expiration duration unit").
				WithReportableDetails(map[string]interface{}{
					"expiration_type": cg.ExpirationType,
				}).
				Mark(errors.ErrValidation)
		}

		if err := cg.ExpirationDurationUnit.Validate(); err != nil {
			return err
		}

		if cg.ExpirationDuration == nil || lo.FromPtr(cg.ExpirationDuration) <= 0 {
			return errors.NewError("expiration_duration is required for DURATION expiration type").
				WithHint("Please provide a valid expiration duration").
				WithReportableDetails(map[string]interface{}{
					"expiration_type": cg.ExpirationType,
				}).
				Mark(errors.ErrValidation)
		}
	}

	return nil
}

func (r *CreatePlanRequest) ToPlan(ctx context.Context) *plan.Plan {
	plan := &plan.Plan{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		LookupKey:     r.LookupKey,
		Name:          r.Name,
		Description:   r.Description,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	return plan
}

func (r *CreatePlanEntitlementRequest) ToEntitlement(ctx context.Context, planID *string, addonID *string) *entitlement.Entitlement {
	ent := r.CreateEntitlementRequest.ToEntitlement(ctx)
	ent.PlanID = planID
	ent.AddonID = addonID
	return ent
}

func (r *CreatePlanRequest) ToCreditGrant(ctx context.Context, planID string, creditGrantReq CreateCreditGrantRequest) *creditgrant.CreditGrant {
	cg := creditGrantReq.ToCreditGrant(ctx)
	cg.PlanID = &planID
	cg.Scope = types.CreditGrantScopePlan
	return cg
}

type CreatePlanResponse struct {
	*plan.Plan
}

type PlanResponse struct {
	*plan.Plan
	Prices       []*PriceResponse       `json:"prices,omitempty"`
	Entitlements []*EntitlementResponse `json:"entitlements,omitempty"`
	CreditGrants []*CreditGrantResponse `json:"credit_grants,omitempty"`
}

type UpdatePlanRequest struct {
	Name         *string                        `json:"name,omitempty"`
	LookupKey    *string                        `json:"lookup_key,omitempty"`
	Description  *string                        `json:"description,omitempty"`
	Prices       []UpdatePlanPriceRequest       `json:"prices,omitempty"`
	Entitlements []UpdatePlanEntitlementRequest `json:"entitlements,omitempty"`
	CreditGrants []UpdatePlanCreditGrantRequest `json:"credit_grants,omitempty"`
}

type UpdatePlanPriceRequest struct {
	// The ID of the price to update (present if the price is being updated)
	ID string `json:"id,omitempty"`
	// The price request to update existing price or create new price
	*CreatePriceRequest
}

type UpdatePlanEntitlementRequest struct {
	// The ID of the entitlement to update (present if the entitlement is being updated)
	ID string `json:"id,omitempty"`
	// The entitlement request to update existing entitlement or create new entitlement
	*CreatePlanEntitlementRequest
}

type UpdatePlanCreditGrantRequest struct {
	// The ID of the credit grant to update (present if the credit grant is being updated)
	ID string `json:"id,omitempty"`
	// The credit grant request to update existing credit grant or create new credit grant
	*CreateCreditGrantRequest
}

// ListPlansResponse represents the response for listing plans with prices, entitlements, and credit grants
type ListPlansResponse = types.ListResponse[*PlanResponse]
