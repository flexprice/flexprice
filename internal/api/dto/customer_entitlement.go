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
	CustomerID    string                  `json:"customer_id"`
	Subscriptions []*SubscriptionResponse `json:"subscriptions"`
	Features      []*AggregatedFeature    `json:"features"`
}

// AggregatedFeature represents a feature with its aggregated entitlements
type AggregatedFeature struct {
	Feature     *FeatureResponse       `json:"feature"`
	Entitlement *AggregatedEntitlement `json:"entitlement"`
	Sources     []*EntitlementSource   `json:"sources"`
}

// GrantState is the runtime half of a grant-backed entitlement: what the
// customer has left right now, and what the cycle has accumulated.
type GrantState struct {
	// Windows is every window overlapping the current billing period, closed ones
	// included, oldest first — the per-window ledger behind CycleTotals. The
	// live balance is the entry (or entries, for parallel features) with
	// IsActive=true; there is at most one per bucket, and none between windows.
	Windows []*GrantWindowState `json:"windows"`
	// CycleTotals covers every window overlapping the current billing period,
	// closed ones included, so the overage figure matches what billing will fold.
	CycleTotals *GrantCycleTotals `json:"cycle_totals,omitempty"`
}

// GrantWindowState is one materialized grant window.
type GrantWindowState struct {
	GrantID       string `json:"grant_id"`
	EntitlementID string `json:"entitlement_id"`
	// Measure is quantity (meter units) or amount (currency) — the unit for
	// every figure below.
	Measure types.EntitlementGrantMeasure `json:"measure"`
	// Unlimited windows track usage but have no ceiling: quota and remaining are
	// meaningless and a client must render "unlimited", not a number.
	Unlimited bool                         `json:"unlimited"`
	Quota     decimal.Decimal              `json:"quota" swaggertype:"string"`
	Usage     decimal.Decimal              `json:"usage" swaggertype:"string"`
	Remaining decimal.Decimal              `json:"remaining" swaggertype:"string"`
	ValidFrom time.Time                    `json:"valid_from"`
	ValidTo   time.Time                    `json:"valid_to"`
	Status    types.EntitlementGrantStatus `json:"status"`
	// IsActive means the window is open right now — evaluated against the
	// server's clock so clients need not compare timestamps themselves.
	IsActive bool `json:"is_active"`
	// LastComputedAt is the freshness watermark. Usage is a snapshot refreshed
	// by a debounced background pass, so a client must render this rather than
	// implying the number is live.
	LastComputedAt *time.Time `json:"last_computed_at,omitempty"`
}

// GrantCycleTotals summarises every window in the current billing period.
type GrantCycleTotals struct {
	Windows      int             `json:"windows"`
	TotalQuota   decimal.Decimal `json:"total_quota" swaggertype:"string"`
	TotalUsage   decimal.Decimal `json:"total_usage" swaggertype:"string"`
	TotalOverage decimal.Decimal `json:"total_overage" swaggertype:"string"`
}

// AggregatedEntitlement contains the final calculated entitlement values.
//
// For parallel aggregation, Buckets carries the per-entitlement view — each
// entry is an independent budget. UsageLimit still reports the sum for legacy display.
type AggregatedEntitlement struct {
	IsEnabled        bool                              `json:"is_enabled"`
	UsageLimit       *int64                            `json:"usage_limit,omitempty"`
	IsSoftLimit      bool                              `json:"is_soft_limit"`
	UsageResetPeriod types.EntitlementUsageResetPeriod `json:"usage_reset_period,omitempty"`
	StaticValues     []string                          `json:"static_values,omitempty"`
	ConfigValues     []map[string]any                  `json:"config_values,omitempty"`
	AggregationMode  types.EntitlementAggregationMode  `json:"aggregation_mode,omitempty"`
	Buckets          []*AggregatedEntitlementBucket    `json:"buckets,omitempty"`

	// Grant config summary, so a client can render the promise ("1,000 calls per
	// day") before any window has opened. UsageLimit stays populated for legacy
	// display but is meaningless on a grant-backed feature.
	GrantMeasure       types.EntitlementGrantMeasure      `json:"grant_measure,omitempty"`
	GrantQuota         *decimal.Decimal                   `json:"grant_quota,omitempty" swaggertype:"string"`
	GrantDurationValue *int                               `json:"grant_duration_value,omitempty"`
	GrantDurationUnit  types.EntitlementGrantDurationUnit `json:"grant_duration_unit,omitempty"`
	// GrantUnlimited distinguishes "grant-based with no ceiling" from "no grant
	// config at all"; both leave GrantQuota nil.
	GrantUnlimited bool `json:"grant_unlimited,omitempty"`

	// GrantState is the runtime half of the config above: the windows this
	// allowance has materialized. Nil when the feature carries no grant config,
	// and its Windows are empty when none has opened yet — different facts that
	// a client must render differently.
	GrantState *GrantState `json:"grant_state,omitempty"`
}

// AggregatedEntitlementBucket is one independent budget within a parallel feature.
type AggregatedEntitlementBucket struct {
	EntitlementID      string                             `json:"entitlement_id"`
	SourceEntityID     string                             `json:"source_entity_id"`
	UsageLimit         *int64                             `json:"usage_limit,omitempty"`
	GrantMeasure       types.EntitlementGrantMeasure      `json:"grant_measure,omitempty"`
	GrantQuota         *decimal.Decimal                   `json:"grant_quota,omitempty" swaggertype:"string"`
	GrantDurationValue *int                               `json:"grant_duration_value,omitempty"`
	GrantDurationUnit  types.EntitlementGrantDurationUnit `json:"grant_duration_unit,omitempty"`
}

// EntitlementSourceType defines the type of entitlement source
type EntitlementSourceEntityType string

const (
	EntitlementSourceEntityTypePlan         EntitlementSourceEntityType = "plan"
	EntitlementSourceEntityTypeAddon        EntitlementSourceEntityType = "addon"
	EntitlementSourceEntityTypeSubscription EntitlementSourceEntityType = "subscription"
)

func (e EntitlementSourceEntityType) Validate() error {

	allowedValues := []string{
		string(EntitlementSourceEntityTypePlan),
		string(EntitlementSourceEntityTypeAddon),
		string(EntitlementSourceEntityTypeSubscription),
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
	EntityName       string                      `json:"entity_name"`
	EntitlementID    string                      `json:"entitlement_id"`
	IsEnabled        bool                        `json:"is_enabled"`
	UsageLimit       *int64                      `json:"usage_limit,omitempty"`
	StaticValue      string                      `json:"static_value,omitempty"`
	UsageResetPeriod types.BillingPeriod         `json:"usage_reset_period,omitempty"`
	ConfigValue      map[string]interface{}      `json:"config_value,omitempty"`
}

// GetCustomerUsageSummaryRequest represents the request for getting customer usage summary
type GetCustomerUsageSummaryRequest struct {
	CustomerID        string   `json:"customer_id,omitempty" form:"customer_id"`
	CustomerLookupKey string   `json:"customer_lookup_key,omitempty" form:"customer_lookup_key"`
	FeatureIDs        []string `json:"feature_ids,omitempty" form:"feature_ids"`
	FeatureLookupKeys []string `json:"feature_lookup_keys,omitempty" form:"feature_lookup_keys"`
	SubscriptionIDs   []string `json:"subscription_ids,omitempty" form:"subscription_ids"`
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
	Feature          *FeatureResponse     `json:"feature"`
	TotalLimit       *int64               `json:"total_limit"`
	IsUnlimited      bool                 `json:"is_unlimited"`
	CurrentUsage     decimal.Decimal      `json:"current_usage" swaggertype:"string"`
	UsagePercent     decimal.Decimal      `json:"usage_percent" swaggertype:"string"`
	IsEnabled        bool                 `json:"is_enabled"`
	IsSoftLimit      bool                 `json:"is_soft_limit"`
	NextUsageResetAt *time.Time           `json:"next_usage_reset_at"`
	Sources          []*EntitlementSource `json:"sources"`
	// GrantState carries the per-window ledger for grant-backed features, so a
	// client can break a cycle total down into the windows that produced it.
	GrantState *GrantState `json:"grant_state,omitempty"`
}
