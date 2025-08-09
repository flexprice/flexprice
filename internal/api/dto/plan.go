package dto

import (
	"context"

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

type CreatePlanEntitlementRequest struct {
	*CreateEntitlementRequest
}

// Validate validates the entitlement when provided inline within a plan creation request.
func (r *CreatePlanEntitlementRequest) Validate() error {
	if r.CreateEntitlementRequest == nil {
		return errors.NewError("entitlement request cannot be nil").
			WithHint("Please provide valid entitlement configuration").
			Mark(errors.ErrValidation)
	}

	if err := validator.ValidateRequest(r.CreateEntitlementRequest); err != nil {
		return err
	}

	if r.CreateEntitlementRequest.FeatureID == "" {
		return errors.NewError("feature_id is required").
			WithHint("Feature ID is required").
			Mark(errors.ErrValidation)
	}

	if err := r.CreateEntitlementRequest.FeatureType.Validate(); err != nil {
		return err
	}

	// Type-specific validations
	switch r.CreateEntitlementRequest.FeatureType {
	case types.FeatureTypeMetered:
		if r.CreateEntitlementRequest.UsageResetPeriod != "" {
			if err := r.CreateEntitlementRequest.UsageResetPeriod.Validate(); err != nil {
				return err
			}
		}
	case types.FeatureTypeStatic:
		if r.CreateEntitlementRequest.StaticValue == "" {
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
		if price.CreatePriceRequest == nil {
			return errors.NewError("price request cannot be nil").
				WithHint("Please provide valid price configuration").
				Mark(errors.ErrValidation)
		}

		// Ensure price_unit_type is set, default to FIAT if not provided
		if price.PriceUnitType == "" {
			price.PriceUnitType = types.PRICE_UNIT_TYPE_FIAT
		}

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

func (r *CreatePlanEntitlementRequest) ToEntitlement(ctx context.Context, planID string) *entitlement.Entitlement {
	ent := r.CreateEntitlementRequest.ToEntitlement(ctx)
	ent.EntityType = types.ENTITLEMENT_ENTITY_TYPE_PLAN
	ent.EntityID = planID
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
	// TODO: Add inline addons
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
