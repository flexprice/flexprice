package entitlementgrant

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
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

	// The 1h minimum is config-level and best-effort at the boundary: short
	// instantiated windows (forced cycle tails) are legal rows.
	g = baseGrant()
	g.ValidTo = g.ValidFrom.Add(30 * time.Minute)
	if err := g.Validate(); err != nil {
		t.Fatalf("short boundary window should validate, got %v", err)
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

func TestFromEnt_CarriesMetadata(t *testing.T) {
	md := types.Metadata{"proration_coefficient": "0.5", "proration_addon_assoc_a1": "0.5"}
	got := FromEnt(&ent.EntitlementGrant{
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
		Metadata:            md,
	})
	if got == nil {
		t.Fatal("FromEnt returned nil")
	}
	if len(got.Metadata) != 2 {
		t.Fatalf("expected 2 metadata keys, got %d", len(got.Metadata))
	}
	if got.Metadata["proration_addon_assoc_a1"] != "0.5" {
		t.Fatalf("idempotency marker did not survive FromEnt: %v", got.Metadata)
	}
}

func TestFromEnt_NilMetadataStaysNil(t *testing.T) {
	got := FromEnt(&ent.EntitlementGrant{ID: "eg_1"})
	if got.Metadata != nil {
		t.Fatalf("expected nil metadata to stay nil, got %v", got.Metadata)
	}
}

func TestBuilder_WithMetadataReplaces(t *testing.T) {
	g := baseGrant()
	g.Metadata = types.Metadata{"old": "1"}

	got := NewEntitlementGrantBuilder(g).
		WithMetadata(types.Metadata{"new": "2"}).
		Build()

	if _, ok := got.Metadata["old"]; ok {
		t.Fatalf("WithMetadata must replace wholesale, got %v", got.Metadata)
	}
	if got.Metadata["new"] != "2" {
		t.Fatalf("expected new key, got %v", got.Metadata)
	}
}

// The builder copies the source grant, so a merge must not mutate the original —
// the attach path builds off a grant it also still reads.
// The builder copies the source grant, so mutating what it produced must not
// reach back into the original — the attach path builds off a grant it still reads.
func TestBuilder_MetadataIsDeepCopied(t *testing.T) {
	g := baseGrant()
	g.Metadata = types.Metadata{"keep": "1"}

	got := NewEntitlementGrantBuilder(g).Build()
	got.Metadata["added"] = "2"

	if _, leaked := g.Metadata["added"]; leaked {
		t.Fatalf("builder shares the source grant's metadata map: %v", g.Metadata)
	}
}
