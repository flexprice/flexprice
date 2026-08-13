package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

// entTestFake seeds fake client state matching the entitlement-enforcement-probe's
// happy-path expectations: e2eprobe_count feature present + one entitlement on
// the plan with limit=100/soft/enabled.
func entTestFake(t *testing.T) (*fakeClient, *e2eprobe.Registry) {
	t.Helper()
	fc := newFakeClient()

	// Pre-seed a feature so Features().Query returns e2eprobe_count with id=feat_1.
	// The existing fakeFeatures.Query only returns features previously registered
	// via Create; call Create to populate.
	featLK := "e2eprobe_count_feature"
	if _, err := fc.features.Create(context.Background(), sdktypes.CreateFeatureRequest{
		LookupKey: &featLK,
		Name:      "E2EProbe Count",
	}); err != nil {
		t.Fatalf("seed feature: %v", err)
	}
	featID := "feat_1"
	limit := int64(100)
	softLimit := true
	enabled := true
	// Pre-seed an entitlement: fakeEntitlements.Query returns queryResp when set.
	fc.entitlements.queryResp = &sdkdtos.QueryEntitlementResponse{
		ListEntitlementsResponse: &sdktypes.ListEntitlementsResponse{
			Items: []sdktypes.EntitlementResponse{{
				ID:          strPtr("ent_1"),
				FeatureID:   &featID,
				UsageLimit:  &limit,
				IsSoftLimit: &softLimit,
				IsEnabled:   &enabled,
			}},
		},
	}
	// Non-nil usage summary so the poll returns success on first iteration.
	fc.customers.usageSummary = &sdkdtos.GetCustomerUsageSummaryResponse{
		CustomerUsageSummaryResponse: &sdktypes.CustomerUsageSummaryResponse{},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{PlanIDs: []string{"plan_1"}})
	return fc, &reg
}

func TestEntitlementEnforcementProbe_HappyPath(t *testing.T) {
	fc, regPtr := entTestFake(t)
	reg := *regPtr
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewEntitlementEnforcementProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	// Probe ingests exactly 150 events past the 100 soft limit.
	if len(fc.events.ingested) != 150 {
		t.Errorf("ingested = %d, want 150", len(fc.events.ingested))
	}
}

func TestEntitlementEnforcementProbe_MissingEntitlementSoftSkip(t *testing.T) {
	fc, regPtr := entTestFake(t)
	reg := *regPtr
	// Drop the entitlement response — an absent entitlement means the seed
	// step hasn't landed yet; the probe must soft-skip, not page on-call.
	fc.entitlements.queryResp = nil
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewEntitlementEnforcementProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("absent entitlement must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created when entitlement prerequisite unmet")
	}
}

func TestEntitlementEnforcementProbe_WrongLimitFails(t *testing.T) {
	fc, regPtr := entTestFake(t)
	reg := *regPtr
	// Corrupt the limit to 999.
	badLimit := int64(999)
	fc.entitlements.queryResp.ListEntitlementsResponse.Items[0].UsageLimit = &badLimit
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewEntitlementEnforcementProbe(fc, reg, "test-run", lg)
	err := p.Run(context.Background())
	if err == nil {
		t.Fatalf("expected error when entitlement usage_limit != 100, got nil")
	}
}

func TestEntitlementEnforcementProbe_MissingSeedsSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	p := NewEntitlementEnforcementProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty seeds must soft-skip; got %v", err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("no customer should be created on empty seeds")
	}
}

// Ensure `errors` import is used somewhere (guarding future refactors).
var _ = errors.New
