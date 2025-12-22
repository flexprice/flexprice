package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type CreatePlanRequest struct {
	Name         string         `json:"name" validate:"required"`
	LookupKey    string         `json:"lookup_key"`
	Description  string         `json:"description"`
	DisplayOrder *int           `json:"display_order,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

type GetPricesByPlanRequest struct {
	PlanID       string `json:"plan_id" validate:"required"`
	AllowExpired bool   `json:"allow_expired,omitempty"`
}

func (r *GetPricesByPlanRequest) Validate() error {
	return validator.ValidateRequest(r)
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

	return nil
}

func (r *CreatePlanRequest) ToPlan(ctx context.Context) *plan.Plan {
	plan := &plan.Plan{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		LookupKey:     r.LookupKey,
		Name:          r.Name,
		Description:   r.Description,
		EnvironmentID: types.GetEnvironmentID(ctx),
		Metadata:      r.Metadata,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if r.DisplayOrder != nil {
		plan.DisplayOrder = r.DisplayOrder
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
	Name         *string        `json:"name,omitempty"`
	LookupKey    *string        `json:"lookup_key,omitempty"`
	Description  *string        `json:"description,omitempty"`
	DisplayOrder *int           `json:"display_order,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty"`
}

// ListPlansResponse represents the response for listing plans with prices, entitlements, and credit grants
type ListPlansResponse = types.ListResponse[*PlanResponse]

type SyncPlanPricesResponse struct {
	Message                string                 `json:"message"`
	PlanID                 string                 `json:"plan_id"`
	PlanName               string                 `json:"plan_name"`
	SynchronizationSummary SynchronizationSummary `json:"synchronization_summary"`
}

type SynchronizationSummary struct {
	// Basic counts
	SubscriptionsProcessed int `json:"subscriptions_processed"`
	PricesProcessed        int `json:"prices_processed"`
	LineItemsCreated       int `json:"line_items_created"`
	LineItemsTerminated    int `json:"line_items_terminated"`
	LineItemsSkipped       int `json:"line_items_skipped"`
	LineItemsFailed        int `json:"line_items_failed"`

	// Detailed breakdown by category
	SkippedAlreadyTerminated int `json:"skipped_already_terminated"`
	SkippedOverridden        int `json:"skipped_overridden"`
	SkippedIncompatible      int `json:"skipped_incompatible"`

	// Price analysis
	TotalPrices   int `json:"total_prices"`
	ActivePrices  int `json:"active_prices"`
	ExpiredPrices int `json:"expired_prices"`
}

// SubscriptionSyncParams contains all parameters needed for syncing a subscription with plan prices
type SubscriptionSyncParams struct {
	Context              context.Context
	Subscription         *subscription.Subscription
	PlanPriceMap         map[string]*price.Price
	LineItems            []*subscription.SubscriptionLineItem
	SubscriptionPriceMap map[string]*PriceResponse
}

// SubscriptionSyncResult contains the results of syncing a subscription with plan prices
type SubscriptionSyncResult struct {
	PricesProcessed                   int
	LineItemsCreated                  int
	LineItemsTerminated               int
	LineItemsSkippedAlreadyTerminated int
	LineItemsSkippedOverridden        int
	LineItemsSkippedIncompatible      int
	LineItemsFailed                   int
}
