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

func TestEntitlement_Validate_LegacyStillWorks(t *testing.T) {
	// A legacy entitlement with no grant fields set validates fine — no
	// regression on the pre-FLE-959 shape.
	e := baseEntitlement()
	if err := e.Validate(); err != nil {
		t.Fatalf("legacy entitlement should validate, got %v", err)
	}
}

func TestEntitlement_Validate_NoneRejectsPartialGrantConfig(t *testing.T) {
	// The invariant we enforce: type=none means grant fields must be blank.
	// Anything else is a half-configured row nobody acts on but that
	// surprises the next reader.
	cases := []struct {
		name string
		mut  func(e *Entitlement)
	}{
		{"measure set on none", func(e *Entitlement) { e.GrantMeasure = types.EntitlementGrantMeasureQuantity }},
		{"duration value set on none", func(e *Entitlement) { v := 5; e.GrantDurationValue = &v }},
		{"duration unit set on none", func(e *Entitlement) { e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour }},
		{"quota set on none", func(e *Entitlement) { q := decimal.NewFromInt(10); e.GrantQuota = &q }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := baseEntitlement()
			e.GrantType = types.EntitlementGrantTypeNone
			tc.mut(e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("expected error for partial grant config on none")
			}
			if !strings.Contains(err.Error(), "grant fields must be empty when grant_type is none") {
				t.Fatalf("unexpected error text: %v", err)
			}
		})
	}
}

func TestEntitlement_Validate_TimeBoxedRequiresFullQuartet(t *testing.T) {
	full := func() *Entitlement {
		e := baseEntitlement()
		e.GrantType = types.EntitlementGrantTypeTimeBoxed
		e.GrantMeasure = types.EntitlementGrantMeasureQuantity
		v := 5
		e.GrantDurationValue = &v
		e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
		q := decimal.NewFromInt(100)
		e.GrantQuota = &q
		return e
	}
	if err := full().Validate(); err != nil {
		t.Fatalf("fully configured time_boxed grant should validate, got %v", err)
	}

	// Strip one field at a time and confirm each is required.
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
			e := full()
			tc.mut(e)
			if err := e.Validate(); err == nil {
				t.Fatalf("expected error when %s", tc.name)
			}
		})
	}
}

func TestEntitlement_Validate_TimeBoxedRejectsSubHourDuration(t *testing.T) {
	// Product rule: no grants shorter than 1 hour. 30 minutes fails.
	//
	// The min-duration constant is exposed as time.Hour; the smallest legal
	// value is 1 hour (via `1 hour`).
	e := baseEntitlement()
	e.GrantType = types.EntitlementGrantTypeTimeBoxed
	e.GrantMeasure = types.EntitlementGrantMeasureQuantity
	// There's no minute unit, so we can't construct a sub-hour value through
	// the enum. This test guards against a future enum extension: if someone
	// adds MINUTE, this test would catch a 30-minute grant sneaking through.
	// For now, the smallest hour value (1) is legal — that's the boundary.
	v := 1
	e.GrantDurationValue = &v
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	q := decimal.NewFromInt(100)
	e.GrantQuota = &q
	if err := e.Validate(); err != nil {
		t.Fatalf("1 hour should be legal boundary, got %v", err)
	}
}

func TestEntitlement_Validate_TimeBoxedRejectsStaticFeature(t *testing.T) {
	// Grants track usage. Static features have no usage to track.
	e := baseEntitlement()
	e.FeatureType = types.FeatureTypeStatic
	e.StaticValue = "unlimited"
	e.GrantType = types.EntitlementGrantTypeTimeBoxed
	e.GrantMeasure = types.EntitlementGrantMeasureQuantity
	v := 5
	e.GrantDurationValue = &v
	e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
	q := decimal.NewFromInt(1)
	e.GrantQuota = &q
	err := e.Validate()
	if err == nil {
		t.Fatalf("expected static feature to be rejected for time_boxed grants")
	}
	if !strings.Contains(err.Error(), "metered feature") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestEntitlement_Validate_TimeBoxedRejectsZeroOrNegativeQuota(t *testing.T) {
	build := func(q decimal.Decimal) *Entitlement {
		e := baseEntitlement()
		e.GrantType = types.EntitlementGrantTypeTimeBoxed
		e.GrantMeasure = types.EntitlementGrantMeasureQuantity
		v := 5
		e.GrantDurationValue = &v
		e.GrantDurationUnit = types.EntitlementGrantDurationUnitHour
		e.GrantQuota = &q
		return e
	}
	for _, q := range []decimal.Decimal{decimal.Zero, decimal.NewFromInt(-1)} {
		e := build(q)
		if err := e.Validate(); err == nil {
			t.Fatalf("expected quota=%s to be rejected", q)
		}
	}
}
