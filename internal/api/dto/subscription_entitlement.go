package dto

import (
	"github.com/flexprice/flexprice/internal/validator"
)

// Subscription Entitlement DTOs
//
// These DTOs are used for the subscription entitlement APIs. They define the
// request and response structures for retrieving aggregated feature entitlements
// for a subscription, including entitlements from the plan and any associated addons.

// GetSubscriptionEntitlementsRequest represents the request for getting subscription entitlements
type GetSubscriptionEntitlementsRequest struct {
	FeatureIDs []string `json:"feature_ids,omitempty" form:"feature_ids"`
}

func (r *GetSubscriptionEntitlementsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// SubscriptionEntitlementsResponse represents the response for subscription entitlements
type SubscriptionEntitlementsResponse struct {
	SubscriptionID string               `json:"subscription_id"`
	PlanID         string               `json:"plan_id"`
	Features       []*AggregatedFeature `json:"features"`
}
