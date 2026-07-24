package entitlementgrant

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func baseGrant() *EntitlementGrant {
	return &EntitlementGrant{
		ID:                  "eg_1",
		EntitlementConfigID: "ent_1",
		CustomerID:          "cust_1",
		SubscriptionID:      "sub_1",
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       "feat_1",
		Measure:             types.EntitlementGrantMeasureQuantity,
		Quota:               decimal.NewFromInt(100),
		ValidFrom:           time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		ValidTo:             time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC),
		GrantStatus:         types.EntitlementGrantStatusActive,
	}
}

func TestValidate_HappyPath(t *testing.T) {
	if err := baseGrant().Validate(); err != nil {
		t.Fatalf("baseline grant should validate, got %v", err)
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(g *EntitlementGrant)
	}{
		{"entitlement_config_id", func(g *EntitlementGrant) { g.EntitlementConfigID = "" }},
		{"customer_id", func(g *EntitlementGrant) { g.CustomerID = "" }},
		{"subscription_id", func(g *EntitlementGrant) { g.SubscriptionID = "" }},
		{"scope_entity_type", func(g *EntitlementGrant) { g.ScopeEntityType = "" }},
		{"scope_entity_id", func(g *EntitlementGrant) { g.ScopeEntityID = "" }},
		{"measure", func(g *EntitlementGrant) { g.Measure = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := baseGrant()
			tc.mut(g)
			if err := g.Validate(); err == nil {
				t.Fatalf("expected error when %s missing", tc.name)
			}
		})
	}
}

func TestValidate_QuotaAndUsageSigns(t *testing.T) {
	g := baseGrant()
	g.Quota = decimal.NewFromInt(-1)
	if err := g.Validate(); err == nil {
		t.Fatalf("negative quota should be rejected")
	}

	g = baseGrant()
	g.Quota = decimal.Zero
	if err := g.Validate(); err == nil {
		t.Fatalf("zero quota should be rejected — grants must be positive")
	}

	g = baseGrant()
	g.Usage = decimal.NewFromInt(-1)
	if err := g.Validate(); err == nil {
		t.Fatalf("negative usage should be rejected")
	}
}

func TestValidate_WindowShape(t *testing.T) {
	g := baseGrant()
	g.ValidTo = g.ValidFrom
	if err := g.Validate(); err == nil {
		t.Fatalf("valid_to must be strictly after valid_from")
	}

	g = baseGrant()
	g.ValidTo = g.ValidFrom.Add(30 * time.Minute)
	if err := g.Validate(); err == nil {
		t.Fatalf("sub-1h window must be rejected")
	}

	g = baseGrant()
	g.ValidTo = g.ValidFrom.Add(time.Hour)
	if err := g.Validate(); err != nil {
		t.Fatalf("exactly-1h window should validate, got %v", err)
	}
}

func TestOverage(t *testing.T) {
	cases := []struct {
		name  string
		quota int64
		usage int64
		want  int64
	}{
		{"under quota", 100, 40, 0},
		{"exactly at quota", 100, 100, 0},
		{"over quota", 100, 250, 150},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := baseGrant()
			g.Quota = decimal.NewFromInt(tc.quota)
			g.Usage = decimal.NewFromInt(tc.usage)
			if got := g.Overage(); !got.Equal(decimal.NewFromInt(tc.want)) {
				t.Fatalf("want %d, got %s", tc.want, got)
			}
		})
	}
}

func TestIsExhausted(t *testing.T) {
	g := baseGrant()
	g.Usage = decimal.NewFromInt(99)
	if g.IsExhausted() {
		t.Fatalf("99/100 should not be exhausted")
	}
	g.Usage = decimal.NewFromInt(100)
	if !g.IsExhausted() {
		t.Fatalf("100/100 should be exhausted")
	}
	g.Usage = decimal.NewFromInt(101)
	if !g.IsExhausted() {
		t.Fatalf("101/100 should be exhausted")
	}
}

func TestIsFeatureScoped_AndFeatureID(t *testing.T) {
	g := baseGrant()
	if !g.IsFeatureScoped() {
		t.Fatalf("default scope=feature should report IsFeatureScoped")
	}
	if got := g.FeatureID(); got != "feat_1" {
		t.Fatalf("FeatureID = %q; want feat_1", got)
	}

	g.ScopeEntityType = types.EntitlementGrantScopeSubscription
	if g.IsFeatureScoped() {
		t.Fatalf("subscription scope should not report IsFeatureScoped")
	}
	if got := g.FeatureID(); got != "" {
		t.Fatalf("FeatureID for non-feature scope = %q; want empty", got)
	}
}

func TestBuilder_CopiesAndUpdates(t *testing.T) {
	orig := baseGrant()
	at := orig.ValidTo.Add(time.Minute)

	updated := NewEntitlementGrantBuilder(orig).
		WithUsage(decimal.NewFromInt(150)).
		WithGrantStatus(types.EntitlementGrantStatusExhausted).
		WithLastComputedAt(&at).
		Build()

	if !updated.Usage.Equal(decimal.NewFromInt(150)) ||
		updated.GrantStatus != types.EntitlementGrantStatusExhausted ||
		updated.LastComputedAt == nil || !updated.LastComputedAt.Equal(at) {
		t.Fatalf("builder did not apply updates: %+v", updated)
	}
	if !orig.Usage.IsZero() || orig.GrantStatus != types.EntitlementGrantStatusActive || orig.LastComputedAt != nil {
		t.Fatalf("builder must not mutate the original: %+v", orig)
	}
	if updated.ID != orig.ID || !updated.ValidFrom.Equal(orig.ValidFrom) {
		t.Fatalf("builder must carry over untouched fields")
	}

	if NewEntitlementGrantBuilder(nil).WithID("eg_x").Build().ID != "eg_x" {
		t.Fatalf("nil-seeded builder should construct from scratch")
	}
}
