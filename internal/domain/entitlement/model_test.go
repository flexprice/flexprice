package entitlement

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// baseEntitlement returns a minimal, well-formed entitlement so each test can
// mutate just the grant fields under test.
func baseEntitlement() *Entitlement {
	return &Entitlement{
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    "plan_1",
		FeatureID:   "feat_1",
		FeatureType: types.FeatureTypeMetered,
	}
}

// fullGrant returns a fully-configured grant-based entitlement.
func fullGrant() *Entitlement {
	e := baseEntitlement()
	e.GrantMeasure = types.EntitlementGrantMeasureQuantity
	v := 5
	e.GrantDurationValue = &v
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	q := decimal.NewFromInt(100)
	e.GrantQuota = &q
	return e
}

func TestEntitlement_Validate_LegacyStillWorks(t *testing.T) {
	e := baseEntitlement()
	if err := e.Validate(); err != nil {
		t.Fatalf("legacy entitlement should validate, got %v", err)
	}
	if e.HasGrantConfig() {
		t.Fatalf("legacy entitlement must not report a grant config")
	}
}

func TestEntitlement_Validate_FullGrantConfigPasses(t *testing.T) {
	e := fullGrant()
	if err := e.Validate(); err != nil {
		t.Fatalf("fully configured grant should validate, got %v", err)
	}
	if !e.HasGrantConfig() {
		t.Fatalf("grant-configured entitlement must report HasGrantConfig")
	}
}

func TestEntitlement_Validate_PartialGrantConfigRejected(t *testing.T) {
	// Grant config is all-or-nothing: stripping any field from a full config
	// must fail validation.
	strips := []struct {
		name string
		mut  func(e *Entitlement)
	}{
		{"measure required", func(e *Entitlement) { e.GrantMeasure = "" }},
		{"duration_value required", func(e *Entitlement) { e.GrantDurationValue = nil }},
		{"duration_unit required", func(e *Entitlement) { e.GrantDurationUnit = "" }},
		{"quota required", func(e *Entitlement) { e.GrantQuota = nil }},
	}
	for _, tc := range strips {
		t.Run(tc.name, func(t *testing.T) {
			e := fullGrant()
			tc.mut(e)
			if err := e.Validate(); err == nil {
				t.Fatalf("expected error when %s", tc.name)
			}
		})
	}
}

func TestEntitlement_Validate_ParallelRequiresGrantConfig(t *testing.T) {
	e := baseEntitlement()
	e.AggregationMode = types.EntitlementAggregationModeParallel
	err := e.Validate()
	if err == nil {
		t.Fatalf("parallel without a grant config must be rejected")
	}

	p := fullGrant()
	p.AggregationMode = types.EntitlementAggregationModeParallel
	if err := p.Validate(); err != nil {
		t.Fatalf("parallel with a grant config should validate, got %v", err)
	}
}

func TestEntitlement_Validate_OneHourIsLegalBoundary(t *testing.T) {
	// Product rule: no grants shorter than 1 hour; 1 hour is the smallest legal window.
	e := fullGrant()
	v := 1
	e.GrantDurationValue = &v
	if err := e.Validate(); err != nil {
		t.Fatalf("1 hour should be the legal boundary, got %v", err)
	}
}

func TestEntitlement_Validate_GrantRejectsStaticFeature(t *testing.T) {
	e := fullGrant()
	e.FeatureType = types.FeatureTypeStatic
	e.StaticValue = "unlimited"
	err := e.Validate()
	if err == nil {
		t.Fatalf("expected static feature to be rejected for grant configs")
	}
	if !strings.Contains(err.Error(), "metered feature") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestEntitlement_Validate_GrantRejectsZeroOrNegativeQuota(t *testing.T) {
	for _, q := range []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-1)} {
		e := fullGrant()
		e.GrantQuota = &q
		if err := e.Validate(); err == nil {
			t.Fatalf("expected quota=%s to be rejected", q)
		}
	}
}

func TestEntitlement_Validate_DayUnitStartAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	e.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart
	if err := e.Validate(); err != nil {
		t.Fatalf("day + unit_start should validate, got %v", err)
	}
}

func TestEntitlement_Validate_DayFirstUsageAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	e.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorFirstUsage
	if err := e.Validate(); err != nil {
		t.Fatalf("day + first_usage should validate, got %v", err)
	}
}

func TestEntitlement_Validate_DayEmptyBehaviorAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	// GrantAllocationBehavior left as zero value
	if err := e.Validate(); err != nil {
		t.Fatalf("day + empty behavior should validate, got %v", err)
	}
}

func TestEntitlement_Validate_WeekUnitStartAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitWeek
	e.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart
	if err := e.Validate(); err != nil {
		t.Fatalf("week + unit_start should validate, got %v", err)
	}
}

func TestEntitlement_Validate_HourUnitStartAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	e.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart
	if err := e.Validate(); err != nil {
		t.Fatalf("hour + unit_start should validate, got %v", err)
	}
}

func TestEntitlement_Validate_SubscriptionPeriodAccepted(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	e.GrantDurationValue = nil
	e.GrantAllocationBehavior = ""
	if err := e.Validate(); err != nil {
		t.Fatalf("subscription_period should validate, got %v", err)
	}
}

func TestEntitlement_Validate_SubscriptionPeriodWithDurationValueRejected(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	// GrantDurationValue kept from fullGrant() (non-nil) — must be rejected
	if err := e.Validate(); err == nil {
		t.Fatalf("subscription_period with duration_value must be rejected")
	}
}

func TestEntitlement_Validate_SubscriptionPeriodWithBehaviorRejected(t *testing.T) {
	e := fullGrant()
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitSubscriptionPeriod
	e.GrantDurationValue = nil
	e.GrantAllocationBehavior = types.EntitlementGrantAllocationBehaviorUnitStart
	if err := e.Validate(); err == nil {
		t.Fatalf("subscription_period with allocation_behavior must be rejected")
	}
}
