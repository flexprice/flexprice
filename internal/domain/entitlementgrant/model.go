// Package entitlementgrant is the domain layer for the entitlement_grants
// table introduced in FLE-959. A grant is a concrete, time-boxed usage bucket
// instantiated by the alert workflow from an "entitlement config" (an
// entitlement row with grant_type=time_boxed). See ERD §7.2 for the full model.
package entitlementgrant

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// EntitlementGrant is one row of entitlement_grants.
//
// Field notes worth calling out (rest are self-explanatory):
//   - Usage is refreshed every workflow tick from ClickHouse — the row is a
//     snapshot, not the source of truth. adjustMeterUsageGrants (ERD §8.6)
//     reads this snapshot when computing billing overage.
//   - GrantStatus is the lifecycle enum (active → exhausted → expired), NOT the
//     BaseModel.Status which is row-level (published/archived/...).
//   - ScopeEntityType + ScopeEntityID identify what the grant meters. Phase 1
//     only opens feature-scoped grants; use FeatureID() when a caller only
//     cares about feature grants and wants a typed accessor.
//   - LastAlertPct is a fast filter to skip GetLatestAlert when nothing moved;
//     the source of truth for what has fired is alert_logs.
type EntitlementGrant struct {
	ID                  string                                `json:"id"`
	EntitlementConfigID string                                `json:"entitlement_config_id"`
	CustomerID          string                                `json:"customer_id"`
	SubscriptionID      string                                `json:"subscription_id"`
	ScopeEntityType     types.EntitlementGrantScopeEntityType `json:"scope_entity_type"`
	ScopeEntityID       string                                `json:"scope_entity_id"`
	Measure             types.EntitlementGrantMeasure         `json:"measure"`
	Quota               decimal.Decimal                       `json:"quota"`
	Usage               decimal.Decimal                       `json:"usage"`
	ValidFrom           time.Time                             `json:"valid_from"`
	ValidTo             time.Time                             `json:"valid_to"`
	GrantStatus         types.EntitlementGrantStatus          `json:"grant_status"`
	LastAlertPct        *int                                  `json:"last_alert_pct,omitempty"`
	LastAlertAt         *time.Time                            `json:"last_alert_at,omitempty"`
	LastComputedAt      *time.Time                            `json:"last_computed_at,omitempty"`
	EnvironmentID       string                                `json:"environment_id"`
	types.BaseModel
}

// IsFeatureScoped reports whether this grant targets a feature. Convenience
// for the Phase 1 hot path where every caller wants a feature ID.
func (g *EntitlementGrant) IsFeatureScoped() bool {
	if g == nil {
		return false
	}
	return g.ScopeEntityType == types.EntitlementGrantScopeFeature
}

// FeatureID returns the target feature id when the grant is feature-scoped,
// empty otherwise. Callers that only handle feature grants can guard on
// IsFeatureScoped() and use this instead of poking at ScopeEntityID directly.
func (g *EntitlementGrant) FeatureID() string {
	if !g.IsFeatureScoped() {
		return ""
	}
	return g.ScopeEntityID
}

// Window returns the grant's [valid_from, valid_to) as a pair. Handy when
// composing CH query bounds so the caller doesn't have to remember the
// half-open convention.
func (g *EntitlementGrant) Window() (time.Time, time.Time) {
	return g.ValidFrom, g.ValidTo
}

// IsLive reports whether the grant is still occupying the (config, customer)
// unique slot — i.e. active or exhausted. Same predicate as the partial
// unique index in the schema.
func (g *EntitlementGrant) IsLive() bool {
	return g.GrantStatus.IsLive()
}

// IsExhausted reports whether usage has reached quota. Convenience helper for
// the workflow's "should we mark exhausted?" branch.
func (g *EntitlementGrant) IsExhausted() bool {
	return g.Usage.GreaterThanOrEqual(g.Quota)
}

// Overage is the non-negative excess of usage over quota. Zero when the grant
// hasn't crossed. Used by adjustMeterUsageGrants (ERD §8.6).
func (g *EntitlementGrant) Overage() decimal.Decimal {
	over := g.Usage.Sub(g.Quota)
	if over.IsNegative() {
		return decimal.Zero
	}
	return over
}

// Validate enforces invariants at the domain boundary. The service layer adds
// meter-shape and pricing-shape checks that need external context (meter,
// price) and can't live on the row.
func (g *EntitlementGrant) Validate() error {
	if g.EntitlementConfigID == "" {
		return ierr.NewError("entitlement_config_id is required").
			WithHint("Grant must reference the entitlement config it was instantiated from").
			Mark(ierr.ErrValidation)
	}
	if g.CustomerID == "" {
		return ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	if g.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation)
	}
	if err := g.ScopeEntityType.Validate(); err != nil {
		return err
	}
	if g.ScopeEntityType == "" {
		return ierr.NewError("scope_entity_type is required").
			WithHint("Set scope_entity_type to feature, subscription, or group").
			Mark(ierr.ErrValidation)
	}
	if g.ScopeEntityID == "" {
		return ierr.NewError("scope_entity_id is required").
			WithHint("scope_entity_id identifies the feature/subscription/group this grant meters").
			Mark(ierr.ErrValidation)
	}
	if err := g.Measure.Validate(); err != nil {
		return err
	}
	if g.Measure == "" {
		return ierr.NewError("measure is required").
			WithHint("Set measure to quantity or amount").
			Mark(ierr.ErrValidation)
	}
	if !g.Quota.IsPositive() {
		return ierr.NewError("quota must be positive").
			WithReportableDetails(map[string]interface{}{"quota": g.Quota.String()}).
			Mark(ierr.ErrValidation)
	}
	if g.Usage.IsNegative() {
		return ierr.NewError("usage cannot be negative").
			WithReportableDetails(map[string]interface{}{"usage": g.Usage.String()}).
			Mark(ierr.ErrValidation)
	}
	if !g.ValidTo.After(g.ValidFrom) {
		return ierr.NewError("valid_to must be strictly after valid_from").
			WithReportableDetails(map[string]interface{}{
				"valid_from": g.ValidFrom,
				"valid_to":   g.ValidTo,
			}).
			Mark(ierr.ErrValidation)
	}
	if g.ValidTo.Sub(g.ValidFrom) < types.EntitlementGrantMinDuration {
		return ierr.NewError("grant window must be at least 1 hour").
			WithHint("Product rule: no grants shorter than 1 hour. See ERD §8.4.").
			WithReportableDetails(map[string]interface{}{
				"valid_from": g.ValidFrom,
				"valid_to":   g.ValidTo,
			}).
			Mark(ierr.ErrValidation)
	}
	if err := g.GrantStatus.Validate(); err != nil {
		return err
	}
	return nil
}

// FromEnt converts an ent row into the domain shape. Returns nil on nil input
// so callers can chain it into a List without a preflight nil check.
func FromEnt(e *ent.EntitlementGrant) *EntitlementGrant {
	if e == nil {
		return nil
	}
	return &EntitlementGrant{
		ID:                  e.ID,
		EntitlementConfigID: e.EntitlementConfigID,
		CustomerID:          e.CustomerID,
		SubscriptionID:      e.SubscriptionID,
		ScopeEntityType:     e.ScopeEntityType,
		ScopeEntityID:       e.ScopeEntityID,
		Measure:             e.Measure,
		Quota:               e.Quota,
		Usage:               e.Usage,
		ValidFrom:           e.ValidFrom,
		ValidTo:             e.ValidTo,
		GrantStatus:         e.GrantStatus,
		LastAlertPct:        e.LastAlertPct,
		LastAlertAt:         e.LastAlertAt,
		LastComputedAt:      e.LastComputedAt,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList is FromEnt lifted to a slice.
func FromEntList(list []*ent.EntitlementGrant) []*EntitlementGrant {
	out := make([]*EntitlementGrant, 0, len(list))
	for _, e := range list {
		if g := FromEnt(e); g != nil {
			out = append(out, g)
		}
	}
	return out
}
