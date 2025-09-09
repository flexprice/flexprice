package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Customer Entitlement and Usage DTOs
//
// These DTOs are used for the customer entitlement and usage APIs. They define the
// request and response structures for retrieving aggregated feature entitlements
// and usage summaries for a customer across all their subscriptions.
//
// These APIs are implemented in the BillingService:
// - GetCustomerEntitlements: Returns aggregated entitlements for a customer across all subscriptions
// - GetCustomerUsageSummary: Returns usage summaries for a customer's metered features
//
// The entitlement aggregation logic handles various feature types (metered, boolean, static)
// and provides a unified view of a customer's entitlements.

// GetCustomerEntitlementsRequest represents the request for getting customer entitlements
type GetCustomerEntitlementsRequest struct {
	FeatureIDs      []string `json:"feature_ids,omitempty" form:"feature_ids"`
	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
}

func (r *GetCustomerEntitlementsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// CustomerEntitlementsResponse represents the response for customer entitlements
type CustomerEntitlementsResponse struct {
	CustomerID string               `json:"customer_id"`
	Features   []*AggregatedFeature `json:"features"`
}

// AggregatedFeature represents a feature with its aggregated entitlements
type AggregatedFeature struct {
	Feature     *FeatureResponse       `json:"feature"`
	Entitlement *AggregatedEntitlement `json:"entitlement"`
	Sources     []*EntitlementSource   `json:"sources"`
}

// AggregatedEntitlement contains the final calculated entitlement values
type AggregatedEntitlement struct {
	IsEnabled        bool                              `json:"is_enabled"`
	UsageLimit       *int64                            `json:"usage_limit,omitempty"`
	IsSoftLimit      bool                              `json:"is_soft_limit"`
	UsageResetPeriod types.EntitlementUsageResetPeriod `json:"usage_reset_period,omitempty"`
	StaticValues     []string                          `json:"static_values,omitempty"` // For static/SLA features
}

// EntitlementSourceType defines the type of entitlement source
type EntitlementSourceEntityType string

const (
	EntitlementSourceEntityTypePlan  EntitlementSourceEntityType = "plan"
	EntitlementSourceEntityTypeAddon EntitlementSourceEntityType = "addon"
)

func (e EntitlementSourceEntityType) Validate() error {

	allowedValues := []string{
		string(EntitlementSourceEntityTypePlan),
		string(EntitlementSourceEntityTypeAddon),
	}

	if !lo.Contains(allowedValues, string(e)) {
		return ierr.NewError("invalid entitlement source entity type").
			WithHint("Please provide a valid entitlement source entity type").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// EntitlementSource tracks which subscription provided the entitlement
type EntitlementSource struct {
	SubscriptionID   string                      `json:"subscription_id"`
	EntityID         string                      `json:"entity_id"`
	EntityType       EntitlementSourceEntityType `json:"entity_type"`
	Quantity         int64                       `json:"quantity"`
	EntitiyName      string                      `json:"entity_name"`
	EntitlementID    string                      `json:"entitlement_id"`
	IsEnabled        bool                        `json:"is_enabled"`
	UsageLimit       *int64                      `json:"usage_limit,omitempty"`
	StaticValue      string                      `json:"static_value,omitempty"`
	UsageResetPeriod types.BillingPeriod         `json:"usage_reset_period,omitempty"`
}

// GetCustomerUsageSummaryRequest represents the request for getting customer usage summary
type GetCustomerUsageSummaryRequest struct {
	FeatureIDs      []string `json:"feature_ids,omitempty" form:"feature_ids"`
	SubscriptionIDs []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
}

func (r *GetCustomerUsageSummaryRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// BillingPeriodInfo represents information about a billing period
type BillingPeriodInfo struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Period    string    `json:"period"` // e.g., "monthly", "yearly"
}

// CustomerUsageSummaryResponse represents the response for customer usage summary
type CustomerUsageSummaryResponse struct {
	CustomerID string                    `json:"customer_id"`
	Features   []*FeatureUsageSummary    `json:"features"`
	Period     *BillingPeriodInfo        `json:"period"`
	Pagination *types.PaginationResponse `json:"pagination,omitempty"`
}

// FeatureUsageSummary represents usage for a single feature
type FeatureUsageSummary struct {
	Feature      *FeatureResponse     `json:"feature"`
	TotalLimit   *int64               `json:"total_limit"`
	CurrentUsage decimal.Decimal      `json:"current_usage"`
	UsagePercent decimal.Decimal      `json:"usage_percent"`
	IsEnabled    bool                 `json:"is_enabled"`
	IsSoftLimit  bool                 `json:"is_soft_limit"`
	Sources      []*EntitlementSource `json:"sources"`
}
