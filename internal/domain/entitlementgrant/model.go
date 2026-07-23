// Package entitlementgrant is the domain layer for time-boxed usage buckets
// instantiated from an entitlement config.
package entitlementgrant

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// EntitlementGrant is one row of entitlement_grants.
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
	LastComputedAt      *time.Time                            `json:"last_computed_at,omitempty"`
	EnvironmentID       string                                `json:"environment_id"`
	types.BaseModel
}

func (g *EntitlementGrant) IsFeatureScoped() bool {
	if g == nil {
		return false
	}
	return g.ScopeEntityType == types.EntitlementGrantScopeFeature
}

// FeatureID returns the target feature id when feature-scoped, "" otherwise.
func (g *EntitlementGrant) FeatureID() string {
	if !g.IsFeatureScoped() {
		return ""
	}
	return g.ScopeEntityID
}

// Window returns the half-open [valid_from, valid_to) grant window.
func (g *EntitlementGrant) Window() (time.Time, time.Time) {
	if g == nil {
		return time.Time{}, time.Time{}
	}
	return g.ValidFrom, g.ValidTo
}

func (g *EntitlementGrant) IsExhausted() bool {
	if g == nil {
		return false
	}
	return g.Usage.GreaterThanOrEqual(g.Quota)
}

// Overage is the non-negative excess of usage over quota.
func (g *EntitlementGrant) Overage() decimal.Decimal {
	if g == nil {
		return decimal.Zero
	}
	over := g.Usage.Sub(g.Quota)
	if over.IsNegative() {
		return decimal.Zero
	}
	return over
}

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

func FromEntList(list []*ent.EntitlementGrant) []*EntitlementGrant {
	out := make([]*EntitlementGrant, 0, len(list))
	for _, e := range list {
		if g := FromEnt(e); g != nil {
			out = append(out, g)
		}
	}
	return out
}
