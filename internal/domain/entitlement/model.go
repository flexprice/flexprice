package entitlement

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Entitlement represents the benefits a customer gets from a subscription plan
type Entitlement struct {
	ID                  string                            `json:"id"`
	EntityType          types.EntitlementEntityType       `json:"entity_type"`
	EntityID            string                            `json:"entity_id"`
	FeatureID           string                            `json:"feature_id"`
	FeatureType         types.FeatureType                 `json:"feature_type"`
	IsEnabled           bool                              `json:"is_enabled"`
	UsageLimit          *int64                            `json:"usage_limit"`
	UsageResetPeriod    types.EntitlementUsageResetPeriod `json:"usage_reset_period"`
	IsSoftLimit         bool                              `json:"is_soft_limit"`
	StaticValue         string                            `json:"static_value"`
	EnvironmentID       string                            `json:"environment_id"`
	DisplayOrder        int                               `json:"display_order"`
	ConfigValue         map[string]interface{}            `json:"config_value,omitempty"`
	ParentEntitlementID *string                           `json:"parent_entitlement_id,omitempty"`
	StartDate           *time.Time                        `json:"start_date,omitempty"`
	EndDate             *time.Time                        `json:"end_date,omitempty"`

	// Grant fields — only meaningful when GrantType == time_boxed. A legacy
	// entitlement leaves GrantType == NONE (default) and behaves exactly as
	// before. See ERD FLE-959 §7.1.
	GrantType          types.EntitlementGrantType         `json:"grant_type,omitempty"`
	GrantMeasure       types.EntitlementGrantMeasure      `json:"grant_measure,omitempty"`
	GrantDurationValue *int                               `json:"grant_duration_value,omitempty"`
	GrantDurationUnit  types.EntitlementGrantDurationUnit `json:"grant_duration_unit,omitempty"`
	GrantQuota         *decimal.Decimal                   `json:"grant_quota,omitempty"`
	Parallel           bool                               `json:"parallel"`

	types.BaseModel
}

// GrantDuration returns the configured grant window as a wall-clock
// time.Duration, or a zero duration with an error if the fields are missing /
// invalid. Only meaningful when GrantType == time_boxed.
func (e *Entitlement) GrantDuration() (time.Duration, error) {
	if e == nil || e.GrantDurationValue == nil {
		return 0, ierr.NewError("grant_duration_value is required for time-boxed grants").
			WithHint("Set grant_duration_value and grant_duration_unit on the entitlement").
			Mark(ierr.ErrValidation)
	}
	return types.EntitlementGrantDurationOf(*e.GrantDurationValue, e.GrantDurationUnit)
}

// EntitlementCloneOverrides holds optional overrides for CopyWith. Nil fields mean "keep existing value".
type EntitlementCloneOverrides struct {
	ID            *string
	EntityType    *types.EntitlementEntityType
	EntityID      *string
	FeatureID     *string // nil = keep existing; non-nil = remap (e.g. cross-env clone)
	EnvironmentID *string // nil = derive from ctx; non-nil = use explicit value
	BaseModel     *types.BaseModel
}

// CopyWith returns a shallow copy of the entitlement with optional overrides applied.
// Pointer fields on the original (UsageLimit, ParentEntitlementID, StartDate, EndDate) are shallow-copied.
// If BaseModel is not in overrides, uses types.GetDefaultBaseModel(ctx).
func (e *Entitlement) CopyWith(ctx context.Context, overrides *EntitlementCloneOverrides) *Entitlement {
	if e == nil {
		return nil
	}
	out := lo.FromPtr(e)
	if overrides == nil {
		return lo.ToPtr(out)
	}
	if overrides.ID != nil {
		out.ID = lo.FromPtr(overrides.ID)
	}
	if overrides.EntityType != nil {
		out.EntityType = lo.FromPtr(overrides.EntityType)
	}
	if overrides.EntityID != nil {
		out.EntityID = lo.FromPtr(overrides.EntityID)
	}
	if overrides.FeatureID != nil {
		out.FeatureID = lo.FromPtr(overrides.FeatureID)
	}
	if overrides.BaseModel != nil {
		out.BaseModel = lo.FromPtr(overrides.BaseModel)
	} else {
		out.BaseModel = types.GetDefaultBaseModel(ctx)
	}
	// EnvironmentID is NOT part of BaseModel — set explicitly or fall back to context
	if overrides.EnvironmentID != nil {
		out.EnvironmentID = lo.FromPtr(overrides.EnvironmentID)
	} else {
		out.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return lo.ToPtr(out)
}

// Validate performs validation on the entitlement
func (e *Entitlement) Validate() error {
	if e.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Please provide a valid entity type").
			Mark(ierr.ErrValidation)
	}
	if err := e.EntityType.Validate(); err != nil {
		return ierr.WithError(err).
			WithHint("Invalid entity type").
			Mark(ierr.ErrValidation)
	}
	if e.FeatureID == "" {
		return ierr.NewError("feature_id is required").
			WithHint("Please provide a valid feature ID").
			Mark(ierr.ErrValidation)
	}
	if e.FeatureType == "" {
		return ierr.NewError("feature_type is required").
			WithHint("Please specify the feature type").
			Mark(ierr.ErrValidation)
	}

	// Validate based on feature type
	switch e.FeatureType {
	case types.FeatureTypeMetered:
		if e.UsageResetPeriod != "" {
			if err := e.UsageResetPeriod.Validate(); err != nil {
				return ierr.WithError(err).
					WithHint("Invalid usage reset period").
					WithReportableDetails(map[string]interface{}{
						"usage_reset_period": e.UsageResetPeriod,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	case types.FeatureTypeStatic:
		if e.StaticValue == "" {
			return ierr.NewError("static_value is required for static features").
				WithHint("Please provide a static value for this feature").
				WithReportableDetails(map[string]interface{}{
					"feature_type": e.FeatureType,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if err := e.validateGrantConfig(); err != nil {
		return err
	}

	return nil
}

// validateGrantConfig enforces the grant-config invariants documented in ERD
// FLE-959 §7.1:
//   - grant_type defaults to NONE; NONE means "legacy behavior, grant fields
//     must be blank" so we can't accidentally act on partial config.
//   - time_boxed requires measure, duration (value + unit), and quota. Duration
//     must resolve to at least the product-mandated 1-hour floor.
//   - Grants only make sense on metered features; static features can't have a
//     time-boxed quota.
//
// This does NOT check the meter-shape restrictions (no MAX/bucketed, amount
// only on linear pricing) — those live in the service layer where meter/price
// info is available.
func (e *Entitlement) validateGrantConfig() error {
	// Empty grant_type is treated as NONE (default). Anything else must round-
	// trip through the enum validator.
	if e.GrantType != "" {
		if err := e.GrantType.Validate(); err != nil {
			return err
		}
	}

	grantType := e.GrantType
	if grantType == "" {
		grantType = types.EntitlementGrantTypeNone
	}

	if grantType == types.EntitlementGrantTypeNone {
		// Reject partial grant config on a NONE row — otherwise the DB carries
		// half-configured grants that nothing acts on but that surprise the
		// next reader.
		if e.GrantMeasure != "" || e.GrantDurationValue != nil || e.GrantDurationUnit != "" || e.GrantQuota != nil {
			return ierr.NewError("grant fields must be empty when grant_type is none").
				WithHint("Set grant_type=time_boxed to opt in, or clear grant_measure/duration/quota").
				Mark(ierr.ErrValidation)
		}
		return nil
	}

	// time_boxed — from here every grant field is required.
	if e.FeatureType != types.FeatureTypeMetered {
		return ierr.NewError("time-boxed grants require a metered feature").
			WithHint("Grants track usage against a meter — static features cannot be time-boxed").
			WithReportableDetails(map[string]interface{}{"feature_type": e.FeatureType}).
			Mark(ierr.ErrValidation)
	}

	if err := e.GrantMeasure.Validate(); err != nil {
		return err
	}
	if e.GrantMeasure == "" {
		return ierr.NewError("grant_measure is required for time-boxed grants").
			WithHint("Set grant_measure to quantity or amount").
			Mark(ierr.ErrValidation)
	}

	if err := e.GrantDurationUnit.Validate(); err != nil {
		return err
	}
	dur, err := e.GrantDuration()
	if err != nil {
		return err
	}
	if dur < types.EntitlementGrantMinDuration {
		return ierr.NewError("grant_duration must be at least 1 hour").
			WithHint("Product rule: no grants shorter than 1 hour").
			WithReportableDetails(map[string]interface{}{
				"grant_duration_value": e.GrantDurationValue,
				"grant_duration_unit":  e.GrantDurationUnit,
			}).
			Mark(ierr.ErrValidation)
	}

	if e.GrantQuota == nil || !e.GrantQuota.IsPositive() {
		return ierr.NewError("grant_quota must be positive for time-boxed grants").
			WithHint("Provide a positive decimal for grant_quota").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// FromEnt converts ent.Entitlement to domain Entitlement
func FromEnt(e *ent.Entitlement) *Entitlement {
	if e == nil {
		return nil
	}

	return &Entitlement{
		ID:                  e.ID,
		EntityType:          types.EntitlementEntityType(e.EntityType),
		EntityID:            e.EntityID,
		FeatureID:           e.FeatureID,
		FeatureType:         types.FeatureType(e.FeatureType),
		IsEnabled:           e.IsEnabled,
		UsageLimit:          e.UsageLimit,
		UsageResetPeriod:    types.EntitlementUsageResetPeriod(e.UsageResetPeriod),
		IsSoftLimit:         e.IsSoftLimit,
		StaticValue:         e.StaticValue,
		EnvironmentID:       e.EnvironmentID,
		DisplayOrder:        e.DisplayOrder,
		ConfigValue:         e.ConfigValue,
		ParentEntitlementID: e.ParentEntitlementID,
		StartDate:           e.StartDate,
		EndDate:             e.EndDate,
		GrantType:           e.GrantType,
		GrantMeasure:        e.GrantMeasure,
		GrantDurationValue:  e.GrantDurationValue,
		GrantDurationUnit:   e.GrantDurationUnit,
		GrantQuota:          e.GrantQuota,
		Parallel:            e.Parallel,
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

// FromEntList converts []*ent.Entitlement to []*Entitlement
func FromEntList(list []*ent.Entitlement) []*Entitlement {
	result := make([]*Entitlement, len(list))
	for i, e := range list {
		result[i] = FromEnt(e)
	}
	return result
}
