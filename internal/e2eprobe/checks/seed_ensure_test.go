package checks

import (
	"context"
	"net/http"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestSeedEnsure(t *testing.T) {
	tests := []struct {
		name                    string
		setup                   func(fc *fakeClient)
		wantErr                 bool
		wantFeaturesCreated     int
		wantCustomersCreated    int
		wantPlansCreated        int
		wantPricesCreated       int
		wantSubsCreated         int
		wantWalletsCreated      int
		wantPersistentCustomers int
		wantPreFundedCustomers  int
		wantMeterIDs            int
		wantFeatureIDs          int
		wantPlanIDs             int
		wantSubIDs              int
	}{
		{
			name: "AllPresent: features pre-exist, customers present, plan pre-exists",
			setup: func(fc *fakeClient) {
				// Pre-populate 11 features with lookup keys and meter IDs.
				for _, spec := range seedFeatureSpecs {
					lk := spec.lookupKey
					mID := "meter_" + spec.eventName
					fc.features.features = append(fc.features.features, types.FeatureResponse{
						ID:        strPtr("feat_" + spec.lookupKey),
						LookupKey: &lk,
						MeterID:   &mID,
					})
				}
				// Pre-populate 10 customers.
				for i := 0; i < 10; i++ {
					ext := persistentExternalCustomerID(i)
					fc.customers.byExt[ext] = "cust_" + ext
				}
				// Pre-populate 1 plan.
				lk := e2eprobePlanLookupKey
				planID := "plan_existing"
				fc.plans.plans = append(fc.plans.plans, types.PlanResponse{
					ID:        &planID,
					LookupKey: &lk,
				})
				// subs and wallets are empty — they'll be created
			},
			wantErr:                 false,
			wantFeaturesCreated:     0,  // all 11 found via Query
			wantCustomersCreated:    1,  // 10 pre-populated; alert canary still needs creating
			wantPlansCreated:        0,  // plan found via Query
			wantPricesCreated:       12, // base + 11 usage prices
			wantSubsCreated:         11, // 10 persistent + 1 alert canary
			wantWalletsCreated:      4,  // 3 pre-funded + 1 alert canary
			wantPersistentCustomers: 11, // 10 persistent + 1 alert canary
			wantPreFundedCustomers:  3,
			wantMeterIDs:            11,
			wantFeatureIDs:          11,
			wantPlanIDs:             1,
			wantSubIDs:              11,
		},
		{
			name: "CreatesMissing: all empty, all entities created",
			setup: func(fc *fakeClient) {
				// Customers not found on initial lookup.
				fc.customers.getErr = errNotFound
			},
			wantErr:                 false,
			wantFeaturesCreated:     11,
			wantCustomersCreated:    11, // 10 persistent + 1 alert canary
			wantPlansCreated:        1,
			wantPricesCreated:       12, // base + 11 usage
			wantSubsCreated:         11, // 10 persistent + 1 alert canary
			wantWalletsCreated:      4,  // 3 pre-funded + 1 alert canary
			wantPersistentCustomers: 11, // 10 persistent + 1 alert canary
			wantPreFundedCustomers:  3,
			wantMeterIDs:            11,
			wantFeatureIDs:          11,
			wantPlanIDs:             1,
			wantSubIDs:              11,
		},
		{
			name: "PartialExisting: features exist but plan/subs/wallets don't",
			setup: func(fc *fakeClient) {
				// Pre-populate 11 features.
				for _, spec := range seedFeatureSpecs {
					lk := spec.lookupKey
					mID := "meter_" + spec.eventName
					fc.features.features = append(fc.features.features, types.FeatureResponse{
						ID:        strPtr("feat_" + spec.lookupKey),
						LookupKey: &lk,
						MeterID:   &mID,
					})
				}
				// Customers all exist.
				for i := 0; i < 10; i++ {
					ext := persistentExternalCustomerID(i)
					fc.customers.byExt[ext] = "cust_" + ext
				}
				// Plan and everything below are missing.
			},
			wantErr:                 false,
			wantFeaturesCreated:     0,
			wantCustomersCreated:    1,  // alert canary still needs creating
			wantPlansCreated:        1,
			wantPricesCreated:       12,
			wantSubsCreated:         11, // 10 persistent + 1 alert canary
			wantWalletsCreated:      4,  // 3 pre-funded + 1 alert canary
			wantPersistentCustomers: 11, // 10 persistent + 1 alert canary
			wantPreFundedCustomers:  3,
			wantMeterIDs:            11,
			wantFeatureIDs:          11,
			wantPlanIDs:             1,
			wantSubIDs:              11,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClient()
			reg := e2eprobe.NewRegistry()
			tc.setup(fc)
			s := NewSeedEnsure(fc, reg, "run-1", nil)
			err := s.Run(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}

			if len(fc.features.created) != tc.wantFeaturesCreated {
				t.Errorf("features.created = %d, want %d", len(fc.features.created), tc.wantFeaturesCreated)
			}
			if len(fc.customers.created) != tc.wantCustomersCreated {
				t.Errorf("customers.created = %d, want %d", len(fc.customers.created), tc.wantCustomersCreated)
			}
			if len(fc.plans.created) != tc.wantPlansCreated {
				t.Errorf("plans.created = %d, want %d", len(fc.plans.created), tc.wantPlansCreated)
			}
			if len(fc.prices.created) != tc.wantPricesCreated {
				t.Errorf("prices.created = %d, want %d", len(fc.prices.created), tc.wantPricesCreated)
			}
			if len(fc.subs.created) != tc.wantSubsCreated {
				t.Errorf("subs.created = %d, want %d", len(fc.subs.created), tc.wantSubsCreated)
			}
			if len(fc.wallets.created) != tc.wantWalletsCreated {
				t.Errorf("wallets.created = %d, want %d", len(fc.wallets.created), tc.wantWalletsCreated)
			}

			got := reg.Seeds()
			if len(got.PersistentCustomerIDs) != tc.wantPersistentCustomers {
				t.Errorf("PersistentCustomerIDs = %d, want %d", len(got.PersistentCustomerIDs), tc.wantPersistentCustomers)
			}
			if len(got.PreFundedCustomerIDs) != tc.wantPreFundedCustomers {
				t.Errorf("PreFundedCustomerIDs = %d, want %d", len(got.PreFundedCustomerIDs), tc.wantPreFundedCustomers)
			}
			if len(got.MeterIDs) != tc.wantMeterIDs {
				t.Errorf("MeterIDs = %d, want %d", len(got.MeterIDs), tc.wantMeterIDs)
			}
			if len(got.FeatureIDs) != tc.wantFeatureIDs {
				t.Errorf("FeatureIDs = %d, want %d", len(got.FeatureIDs), tc.wantFeatureIDs)
			}
			if len(got.PlanIDs) != tc.wantPlanIDs {
				t.Errorf("PlanIDs = %d, want %d", len(got.PlanIDs), tc.wantPlanIDs)
			}
			if len(got.PersistentSubIDs) != tc.wantSubIDs {
				t.Errorf("PersistentSubIDs = %d, want %d", len(got.PersistentSubIDs), tc.wantSubIDs)
			}
		})
	}
}

func TestSeedEnsure_BucketedFeaturesProvisioned(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	seeds := reg.Seeds()
	wantKeys := []string{"e2eprobe_max_15min_feature", "e2eprobe_sum_hour_feature", "e2eprobe_max_day_feature"}
	for _, k := range wantKeys {
		if _, ok := seeds.BucketedFeatureIDs[k]; !ok {
			t.Errorf("BucketedFeatureIDs missing key %q; got keys %v", k, keysOf(seeds.BucketedFeatureIDs))
		}
	}
}

func keysOf(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestSeedEnsure_CouponProvisioning(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("first Run() unexpected error: %v", err)
	}
	seeds := reg.Seeds()
	if seeds.SharedCouponID == "" {
		t.Fatalf("SharedCouponID empty after seed run")
	}
	if seeds.SharedCouponCode != "E2EPROBE_COUPON_10PCT" {
		t.Errorf("SharedCouponCode = %q, want E2EPROBE_COUPON_10PCT", seeds.SharedCouponCode)
	}
	if len(fc.coupons.created) != 1 {
		t.Fatalf("coupon created %d times on first run; want 1", len(fc.coupons.created))
	}
	if got := fc.coupons.created[0]; got.Type != types.CouponTypePercentage || got.Cadence != types.CouponCadenceOnce {
		t.Errorf("coupon Type/Cadence = %v/%v; want percentage/once", got.Type, got.Cadence)
	}
	if got := fc.coupons.created[0]; got.PercentageOff == nil || *got.PercentageOff != "10" {
		t.Errorf("coupon PercentageOff = %v, want \"10\"", got.PercentageOff)
	}

	// Second run must not create another (idempotent).
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("second Run() unexpected error: %v", err)
	}
	if len(fc.coupons.created) != 1 {
		t.Errorf("coupon created %d times across 2 runs; want 1 (idempotency broken)", len(fc.coupons.created))
	}
}

func TestSeedEnsure_TaxRateProvisioning(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("first Run() unexpected error: %v", err)
	}
	seeds := reg.Seeds()
	if seeds.SharedTaxRateID == "" {
		t.Fatalf("SharedTaxRateID empty after seed run (Create succeeded — should carry the id)")
	}
	if seeds.SharedTaxRateCode != "E2EPROBE_TAX_10PCT" {
		t.Errorf("SharedTaxRateCode = %q, want E2EPROBE_TAX_10PCT", seeds.SharedTaxRateCode)
	}
	if len(fc.taxRates.created) != 1 {
		t.Fatalf("tax rate created %d times on first run; want 1", len(fc.taxRates.created))
	}
	got := fc.taxRates.created[0]
	if got.Code != "E2EPROBE_TAX_10PCT" {
		t.Errorf("tax rate Code = %q, want E2EPROBE_TAX_10PCT", got.Code)
	}
	if got.PercentageValue == nil || *got.PercentageValue != "10" {
		t.Errorf("tax rate PercentageValue = %v, want \"10\"", got.PercentageValue)
	}
	if got.Scope == nil || *got.Scope != types.TaxRateScopeExternal {
		t.Errorf("tax rate Scope = %v, want EXTERNAL", got.Scope)
	}
}

// TestSeedEnsure_TaxRateAlreadyExistsSwallowed simulates the second-run
// scenario where CreateTaxRate returns "already exists" — the seed treats
// this as success, populates only the Code, and leaves the ID empty (the
// SDK v2.0.24 workaround for the broken TaxRates.List response shape).
func TestSeedEnsure_TaxRateAlreadyExistsSwallowed(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	code := types.ErrorCodeAlreadyExists
	status := int64(http.StatusConflict)
	msg := "A taxrate with this code E2EPROBE_TAX_10PCT already exists"
	fc.taxRates.createErr = &sdkerrors.ErrorResponse{
		Code:           &code,
		HTTPStatusCode: &status,
		Message:        &msg,
	}

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() must swallow ErrAlreadyExists on tax rate; got %v", err)
	}
	seeds := reg.Seeds()
	if seeds.SharedTaxRateCode != "E2EPROBE_TAX_10PCT" {
		t.Errorf("SharedTaxRateCode = %q, want E2EPROBE_TAX_10PCT (must be set even on duplicate path)", seeds.SharedTaxRateCode)
	}
	// SharedTaxRateID may end up populated by ensurePersistentTaxAssociation's
	// backfill from the fake CreateTaxAssociation response — that's fine.
	// The key invariant is that Run() did not fail.
}

func TestSeedEnsure_PlanEntitlementsProvisioned(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	seeds := reg.Seeds()
	// 11 total specs − 4 bucketed (e2eprobe_max/max_15min/sum_hour/max_day)
	// − 1 grant-only (e2eprobe_sum_multiplier) = 6 soft-limit entitlements.
	if len(seeds.PlanEntitlementIDs) != 6 {
		t.Errorf("PlanEntitlementIDs = %d, want 6 (non-bucketed features MINUS the reserved additive-grant feature); got %v", len(seeds.PlanEntitlementIDs), seeds.PlanEntitlementIDs)
	}
	// Every created entitlement carries usage_limit=100, is_soft_limit=true,
	// reset_period=MONTHLY, is_enabled=true.
	if len(fc.entitlements.created) != 6 {
		t.Errorf("entitlements Create called %d times, want 6 (grant-only feature excluded from soft-limit seeding)", len(fc.entitlements.created))
	}
	for i, req := range fc.entitlements.created {
		if req.UsageLimit == nil || *req.UsageLimit != 100 {
			t.Errorf("entitlement[%d] UsageLimit = %v, want 100", i, req.UsageLimit)
		}
		if req.IsSoftLimit == nil || !*req.IsSoftLimit {
			t.Errorf("entitlement[%d] IsSoftLimit = %v, want true", i, req.IsSoftLimit)
		}
		if req.IsEnabled == nil || !*req.IsEnabled {
			t.Errorf("entitlement[%d] IsEnabled = %v, want true", i, req.IsEnabled)
		}
		if req.UsageResetPeriod == nil || *req.UsageResetPeriod != types.EntitlementUsageResetPeriodMonthly {
			t.Errorf("entitlement[%d] UsageResetPeriod = %v, want Monthly", i, req.UsageResetPeriod)
		}
		if req.FeatureType != types.FeatureTypeMetered {
			t.Errorf("entitlement[%d] FeatureType = %v, want Metered", i, req.FeatureType)
		}
	}
}

func TestSeedEnsure_CommitmentAndCouponOnPersistentSubs(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if len(fc.subs.created) == 0 {
		t.Fatalf("no subs created")
	}
	// Every persistent sub must carry commitment fields.
	for i, req := range fc.subs.created {
		if req.CommitmentAmount == nil || *req.CommitmentAmount != "5.00" {
			t.Errorf("sub[%d] CommitmentAmount = %v, want \"5.00\"", i, req.CommitmentAmount)
		}
		if req.CommitmentDuration == nil || *req.CommitmentDuration != types.BillingPeriodMonthly {
			t.Errorf("sub[%d] CommitmentDuration = %v, want MONTHLY", i, req.CommitmentDuration)
		}
		if req.OverageFactor == nil || *req.OverageFactor != "1.5" {
			t.Errorf("sub[%d] OverageFactor = %v, want \"1.5\"", i, req.OverageFactor)
		}
	}

	// Persistent cust #1's sub must carry the shared coupon at sub-create.
	found := false
	for _, req := range fc.subs.created {
		if req.ExternalCustomerID != nil && *req.ExternalCustomerID == "e2eprobe-cust-persistent-1" {
			if len(req.SubscriptionCoupons) != 1 || req.SubscriptionCoupons[0].CouponCode != "E2EPROBE_COUPON_10PCT" {
				t.Errorf("cust-persistent-1 sub SubscriptionCoupons = %+v, want [{CouponCode: E2EPROBE_COUPON_10PCT}]", req.SubscriptionCoupons)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no sub found for e2eprobe-cust-persistent-1")
	}

	// Other persistent subs must NOT carry the coupon (only cust #1).
	for _, req := range fc.subs.created {
		if req.ExternalCustomerID == nil || *req.ExternalCustomerID == "e2eprobe-cust-persistent-1" {
			continue
		}
		if len(req.SubscriptionCoupons) != 0 {
			t.Errorf("sub for %s carries SubscriptionCoupons = %+v, want none", *req.ExternalCustomerID, req.SubscriptionCoupons)
		}
	}
}

func TestSeedEnsure_PersistentTaxAssociation(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fc.taxAssociations.created) != 1 {
		t.Fatalf("tax associations created = %d, want 1", len(fc.taxAssociations.created))
	}
	req := fc.taxAssociations.created[0]
	if req.TaxRateCode != "E2EPROBE_TAX_10PCT" {
		t.Errorf("TaxRateCode = %q, want E2EPROBE_TAX_10PCT", req.TaxRateCode)
	}
	if req.EntityID == nil || *req.EntityID == "" {
		t.Errorf("EntityID missing on tax association create")
	}
	if req.EntityType == nil || *req.EntityType != types.TaxRateEntityTypeSubscription {
		t.Errorf("EntityType = %v, want SUBSCRIPTION", req.EntityType)
	}

	// Idempotency on second Run:
	//   1. ensureTaxRates: fake CreateTaxRate returns "already exists" (the
	//      real server behaviour when the tax rate was created by Run #1).
	//   2. ensurePersistentTaxAssociation: fake TaxAssociations.List returns
	//      the existing association we created in Run #1, so create is
	//      skipped.
	trID := reg.Seeds().SharedTaxRateID
	dupCode := types.ErrorCodeAlreadyExists
	dupStatus := int64(http.StatusConflict)
	fc.taxRates.createErr = &sdkerrors.ErrorResponse{
		Code:           &dupCode,
		HTTPStatusCode: &dupStatus,
	}
	fc.taxAssociations.listResp = &dtos.ListTaxAssociationsResponse{
		ListTaxAssociationsResponse: &types.ListTaxAssociationsResponse{
			Items: []types.TaxAssociationResponse{{TaxRateID: &trID}},
		},
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("second Run() unexpected error: %v", err)
	}
	if len(fc.taxAssociations.created) != 1 {
		t.Errorf("tax association created %d times across 2 runs; want 1 (idempotency broken)", len(fc.taxAssociations.created))
	}
}

// TestSeedEnsure_PlanEntitlements_SkipsGrantFeature verifies that
// ensurePlanEntitlements no longer seeds a soft-limit entitlement on
// the reserved additive-grant feature. Task 5's ensureEntitlementGrants
// then creates the grant entitlement on the (plan, feature) slot the
// skip vacated.
func TestSeedEnsure_PlanEntitlements_SkipsGrantFeature(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	// The additive-grant feature must NOT get a soft-limit entitlement.
	// 11 total specs − 4 bucketed features − 1 grant-only = 6 soft-limit
	// entitlements. The SDK typed Create is invoked once per soft-limit
	// entitlement, so a strict count check is sufficient — the seed
	// queries features by lookup key and skips the grant feature at
	// that lookup step, so no create call for the grant feature ever
	// reaches the fake.
	if len(fc.entitlements.created) != 6 {
		t.Errorf("plan-level soft-limit entitlements created = %d, want 6 (non-bucketed features minus the reserved grant feature)", len(fc.entitlements.created))
	}
}

func TestSeedEnsure_AdditiveGrantEntitlementProvisioned(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	seeds := reg.Seeds()

	id, ok := seeds.GrantEntitlementIDs["e2eprobe_sum_multiplier_feature"]
	if !ok || id == "" {
		t.Fatalf("GrantEntitlementIDs missing e2eprobe_sum_multiplier_feature; got %v", seeds.GrantEntitlementIDs)
	}
	if len(fc.entitlements.createdWithGrant) != 1 {
		t.Fatalf("CreateWithGrant called %d times, want 1", len(fc.entitlements.createdWithGrant))
	}
	req := fc.entitlements.createdWithGrant[0]
	if req.GrantMeasure != "quantity" {
		t.Errorf("GrantMeasure = %q, want quantity", req.GrantMeasure)
	}
	if req.GrantQuota != "1000" {
		t.Errorf("GrantQuota = %q, want 1000", req.GrantQuota)
	}
	if req.GrantDurationValue != 1 {
		t.Errorf("GrantDurationValue = %d, want 1", req.GrantDurationValue)
	}
	if req.GrantDurationUnit != "hour" {
		t.Errorf("GrantDurationUnit = %q, want hour", req.GrantDurationUnit)
	}
	if req.AggregationMode != "additive" {
		t.Errorf("AggregationMode = %q, want additive", req.AggregationMode)
	}
	if req.FeatureType != "metered" {
		t.Errorf("FeatureType = %q, want metered", req.FeatureType)
	}
	if !req.IsEnabled {
		t.Errorf("IsEnabled = false, want true")
	}
	if len(fc.entitlements.getRawCalls) < 1 {
		t.Errorf("GetRaw was not called for config-echo verification")
	}
}

// TestSeedEnsure_AdditiveGrantEchoMismatchLoggedNotFatal: config-echo
// drift (server accepted the create but returned a different config) is
// surfaced via a Warn log AND leaves GrantEntitlementIDs empty so the
// grant probe soft-skips — but does NOT fail seed-ensure Run(). Prior
// to the 2026-08-13 "non-fatal grant provisioning" change the seed
// returned this error, which poisoned LoadSeeds and broke every
// ephemeral-creating probe. Alerting on the drift now happens via the
// structured warn log (SigNoz / Grafana pattern-match), not Slack.
func TestSeedEnsure_AdditiveGrantEchoMismatchLoggedNotFatal(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	dv := 1
	fc.entitlements.getRawResp = &e2eprobe.GrantEntitlementResponse{
		ID:                 "ent_grant_1",
		GrantMeasure:       "quantity",
		GrantQuota:         "1000",
		GrantDurationValue: &dv,
		GrantDurationUnit:  "hour",
		AggregationMode:    "parallel", // ← the divergence
		IsEnabled:          true,
	}

	s := NewSeedEnsure(fc, reg, "test-run", lg)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() must not fail hard on grant echo mismatch; got %v", err)
	}
	seeds := reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		t.Errorf("PlanIDs empty — non-fatal contract violated: downstream state was not preserved")
	}
	if len(seeds.GrantEntitlementIDs) != 0 {
		t.Errorf("GrantEntitlementIDs = %v; expected empty so probe soft-skips on drift", seeds.GrantEntitlementIDs)
	}
}

func TestSeedEnsure_AdditiveGrantIdempotent(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})
	s := NewSeedEnsure(fc, reg, "test-run", lg)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("first Run() unexpected error: %v", err)
	}
	if len(fc.entitlements.createdWithGrant) != 1 {
		t.Fatalf("first Run should have created 1 grant, got %d", len(fc.entitlements.createdWithGrant))
	}
	grantID := reg.Seeds().GrantEntitlementIDs["e2eprobe_sum_multiplier_feature"]

	existingID := grantID
	fc.entitlements.queryResp = &dtos.QueryEntitlementResponse{
		ListEntitlementsResponse: &types.ListEntitlementsResponse{
			Items: []types.EntitlementResponse{
				{ID: &existingID},
			},
		},
	}
	dv := 1
	fc.entitlements.getRawResp = &e2eprobe.GrantEntitlementResponse{
		ID:                 existingID,
		GrantMeasure:       "quantity",
		GrantQuota:         "1000",
		GrantDurationValue: &dv,
		GrantDurationUnit:  "hour",
		AggregationMode:    "additive",
		IsEnabled:          true,
	}

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("second Run() unexpected error: %v", err)
	}
	if len(fc.entitlements.createdWithGrant) != 1 {
		t.Errorf("grant CreateWithGrant called %d times across 2 runs; want 1 (idempotency broken)", len(fc.entitlements.createdWithGrant))
	}
}

func TestSeedEnsure_LegacySoftLimitReplacedByGrant(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	legacyID := "ent_legacy_soft"
	fc.entitlements.queryResp = &dtos.QueryEntitlementResponse{
		ListEntitlementsResponse: &types.ListEntitlementsResponse{
			Items: []types.EntitlementResponse{
				{ID: &legacyID},
			},
		},
	}
	// Per-id: legacyID's GetRaw returns empty GrantMeasure → classified as
	// legacy soft-limit → deleted. The newly-created grant's GetRaw (any
	// other id) falls through to the fake's default well-formed response.
	fc.entitlements.getRawRespByID = map[string]*e2eprobe.GrantEntitlementResponse{
		legacyID: {ID: legacyID, GrantMeasure: ""},
	}

	s := NewSeedEnsure(fc, reg, "test-run", lg)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	found := false
	for _, id := range fc.entitlements.deleted {
		if id == legacyID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("legacy soft-limit entitlement was not deleted; deleted = %v", fc.entitlements.deleted)
	}
	if len(fc.entitlements.createdWithGrant) != 1 {
		t.Errorf("CreateWithGrant called %d times, want 1", len(fc.entitlements.createdWithGrant))
	}
}

func TestSeedEnsure_AdditiveGrantAlreadyExistsSwallowed(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	code := types.ErrorCodeAlreadyExists
	status := int64(http.StatusConflict)
	fc.entitlements.createWithGrantErr = &sdkerrors.ErrorResponse{
		Code:           &code,
		HTTPStatusCode: &status,
	}

	s := NewSeedEnsure(fc, reg, "test-run", lg)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() must swallow ErrAlreadyExists on grant CreateWithGrant; got %v", err)
	}
}

// TestSeedEnsure_GrantsFailureDoesNotBlockDownstream is the regression
// guard for the "no ephemeral customers on staging" report (2026-08-13).
// If ensureEntitlementGrants fails for any reason (server rejects the
// grant config, response echo drifts, transient 5xx), the rest of the
// seed — including ensureSubscriptions which populates PersistentSubIDs —
// MUST still run to completion. Otherwise every ephemeral-creating probe
// soft-skips on empty PlanIDs and no customers are ever created.
func TestSeedEnsure_GrantsFailureDoesNotBlockDownstream(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// Inject a hard failure on CreateWithGrant that is NOT "already exists".
	fc.entitlements.createWithGrantErr = &sdkerrors.APIError{
		Message:    "server rejected grant config",
		StatusCode: http.StatusBadRequest,
		Body:       `{"code":"validation_error","message":"grant_measure not supported"}`,
	}

	s := NewSeedEnsure(fc, reg, "test-run", lg)
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run() must not fail hard when grant provisioning fails; got %v", err)
	}

	// Verify downstream state IS populated — this is the whole point.
	seeds := reg.Seeds()
	if len(seeds.PlanIDs) == 0 {
		t.Errorf("PlanIDs empty after grants failed — seed cascaded and broke everything")
	}
	if len(seeds.PersistentCustomerIDs) == 0 {
		t.Errorf("PersistentCustomerIDs empty after grants failed — ephemeral probes will soft-skip")
	}
	if len(seeds.PersistentSubIDs) == 0 {
		t.Errorf("PersistentSubIDs empty after grants failed — probes that depend on subs will soft-skip")
	}
	// Grant coverage is expected to be missing (the whole point of the failure).
	if len(seeds.GrantEntitlementIDs) != 0 {
		t.Errorf("GrantEntitlementIDs = %v; expected empty when grant provisioning failed", seeds.GrantEntitlementIDs)
	}
}
