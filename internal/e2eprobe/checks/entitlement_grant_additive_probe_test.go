package checks

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

// grantProbeFake seeds the fake with the minimum state needed for the
// probe's happy path.
func grantProbeFake(t *testing.T) (*fakeClient, e2eprobe.Registry) {
	t.Helper()
	fc := newFakeClient()

	featLK := "e2eprobe_sum_multiplier_feature"
	if _, err := fc.features.Create(context.Background(), sdktypes.CreateFeatureRequest{
		LookupKey: &featLK,
		Name:      "E2EProbe SumMul",
	}); err != nil {
		t.Fatalf("seed feature: %v", err)
	}
	featID := "feat_1" // fake assigns feat_<N> based on creation order

	fc.subs.getEntitlementsResp = &sdkdtos.GetSubscriptionEntitlementsResponse{
		SubscriptionEntitlementsResponse: &sdktypes.SubscriptionEntitlementsResponse{
			Features: []sdktypes.AggregatedFeature{
				{Feature: &sdktypes.FeatureResponse{ID: &featID}},
			},
		},
	}
	fc.customers.usageSummary = &sdkdtos.GetCustomerUsageSummaryResponse{
		CustomerUsageSummaryResponse: &sdktypes.CustomerUsageSummaryResponse{},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PlanIDs: []string{"plan_1"},
		GrantEntitlementIDs: map[string]string{
			"e2eprobe_sum_multiplier_feature": "ent_grant_1",
		},
	})
	return fc, reg
}

func TestEntitlementGrantAdditiveProbe_HappyPath(t *testing.T) {
	fc, reg := grantProbeFake(t)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewEntitlementGrantAdditiveProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fc.events.ingested) != 200 {
		t.Errorf("ingested = %d, want 200", len(fc.events.ingested))
	}
	if len(fc.customers.created) != 1 {
		t.Errorf("customers created = %d, want 1", len(fc.customers.created))
	}
	if len(fc.subs.created) != 1 {
		t.Errorf("subs created = %d, want 1", len(fc.subs.created))
	}
}

func TestEntitlementGrantAdditiveProbe_MissingSeedsSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewEntitlementGrantAdditiveProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty seeds must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created when seeds empty; got %d", len(fc.customers.created))
	}
}

func TestEntitlementGrantAdditiveProbe_MissingGrantEntitlementSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})

	p := NewEntitlementGrantAdditiveProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("missing grant entitlement id must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created when grant not seeded; got %d", len(fc.customers.created))
	}
}

func TestEntitlementGrantAdditiveProbe_SubEntitlementMissingFails(t *testing.T) {
	fc, reg := grantProbeFake(t)
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Empty feature list — sub isn't inheriting the grant.
	fc.subs.getEntitlementsResp = &sdkdtos.GetSubscriptionEntitlementsResponse{
		SubscriptionEntitlementsResponse: &sdktypes.SubscriptionEntitlementsResponse{
			Features: []sdktypes.AggregatedFeature{},
		},
	}

	p := NewEntitlementGrantAdditiveProbe(fc, reg, "test-run", lg)
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error when sub does not inherit the grant, got nil")
	}
}
