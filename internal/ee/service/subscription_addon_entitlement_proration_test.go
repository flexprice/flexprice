package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// -----------------------------------------------------------------------------
// fixtures
// -----------------------------------------------------------------------------

func (s *SubscriptionServiceSuite) seedGrantFeature(featureID string) string {
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), &feature.Feature{
		ID:        featureID,
		Name:      featureID,
		Type:      types.FeatureTypeMetered,
		MeterID:   s.testData.meters.apiCalls.ID,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}))
	return featureID
}

// seedGrantEC writes a subscription_period grant EC with an explicit id so the
// tests control ULID ordering.
func (s *SubscriptionServiceSuite) seedGrantEC(
	id, featureID string,
	entityType types.EntitlementEntityType,
	entityID string,
	quota int64,
	mode types.EntitlementAggregationMode,
) *entitlement.Entitlement {
	dv := 1
	q := decimal.NewFromInt(quota)
	ec := &entitlement.Entitlement{
		ID:                 id,
		EntityType:         entityType,
		EntityID:           entityID,
		FeatureID:          featureID,
		FeatureType:        types.FeatureTypeMetered,
		IsEnabled:          true,
		GrantMeasure:       types.EntitlementGrantMeasureQuantity,
		GrantDurationValue: &dv,
		GrantDurationUnit:  types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:         &q,
		AggregationMode:    mode,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), ec)
	s.NoError(err)
	return ec
}

// seedGrantAddon registers an addon carrying one subscription_period grant EC.
func (s *SubscriptionServiceSuite) seedGrantAddon(
	addonID, ecID, featureID string,
	quota int64,
	mode types.EntitlementAggregationMode,
) {
	ctx := s.GetContext()
	s.NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        addonID,
		LookupKey: addonID,
		Name:      addonID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + addonID,
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meters.apiCalls.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
	s.seedGrantEC(ecID, featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, addonID, quota, mode)
}

// seedCycleGrant writes the row the evaluator would already have opened for this cycle.
func (s *SubscriptionServiceSuite) seedCycleGrant(ecID, featureID string, quota int64) *entitlementgrant.EntitlementGrant {
	sub := s.testData.subscription
	g := entitlementgrant.NewEntitlementGrantBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
		WithEntitlementConfigID(ecID).
		WithCustomerID(sub.CustomerID).
		WithSubscriptionID(sub.ID).
		WithScope(types.EntitlementGrantScopeFeature, featureID).
		WithMeasure(types.EntitlementGrantMeasureQuantity).
		WithQuota(decimal.NewFromInt(quota)).
		WithWindow(sub.CurrentPeriodStart, sub.CurrentPeriodEnd).
		WithGrantStatus(types.EntitlementGrantStatusActive).
		WithEnvironmentID(types.GetEnvironmentID(s.GetContext())).
		WithBaseModel(types.GetDefaultBaseModel(s.GetContext())).
		Build()
	created, err := s.GetStores().EntitlementGrantRepo.Create(s.GetContext(), g)
	s.Require().NoError(err)
	return created
}

func (s *SubscriptionServiceSuite) grantsForFeature(featureID string) []*entitlementgrant.EntitlementGrant {
	rows, err := s.GetStores().EntitlementGrantRepo.List(s.GetContext(), types.NewNoLimitEntitlementGrantFilter())
	s.Require().NoError(err)
	return lo.Filter(rows, func(g *entitlementgrant.EntitlementGrant, _ int) bool {
		return g.FeatureID() == featureID
	})
}

func (s *SubscriptionServiceSuite) associationIDFor(addonID string) string {
	filter := types.NewNoLimitAddonAssociationFilter()
	assocs, err := s.GetStores().AddonAssociationRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	for _, a := range assocs {
		if a != nil && a.AddonID == addonID {
			return a.ID
		}
	}
	s.Require().Fail("no association found for addon " + addonID)
	return ""
}

func (s *SubscriptionServiceSuite) attachAddon(addonID string, at time.Time, behavior types.ProrationBehavior) error {
	_, err := s.service.AddAddonToSubscription(s.GetContext(), &dto.AddAddonRequest{
		SubscriptionID: s.testData.subscription.ID,
		AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
			AddonID:           addonID,
			Cadence:           types.AddonCadenceRecurring,
			StartDate:         &at,
			ProrationBehavior: behavior,
		},
	})
	return err
}

// The suite's cycle is [now-24h, now+6d): attaching at `now` leaves 6 of 7 days.
func (s *SubscriptionServiceSuite) expectedProrated(quota int64) decimal.Decimal {
	sub := s.testData.subscription
	total := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart).Seconds()
	remaining := sub.CurrentPeriodEnd.Sub(s.testData.now).Seconds()
	return decimal.NewFromInt(quota).
		Mul(decimal.NewFromFloat(remaining).Div(decimal.NewFromFloat(total))).
		Round(15)
}

// -----------------------------------------------------------------------------
// tests
// -----------------------------------------------------------------------------

// The feature already has this cycle's pooled row: the attach tops it up in
// place and leaves the window alone, because the matching line item is the
// plan's full-cycle one and consumption must carry forward.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_ExistingFeature_TopsUpInPlace() {
	featureID := s.seedGrantFeature("feat_eg_topup")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_topup", "ent_zzz_addon", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_topup", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 1, "the pool must stay one row, got %d", len(rows))
	got := rows[0]

	s.Equal(existing.ID, got.ID, "the existing row must be topped up, not replaced")
	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600))
	s.True(got.Quota.Equal(want), "expected quota %s, got %s", want, got.Quota)
	s.True(got.ValidFrom.Equal(s.testData.subscription.CurrentPeriodStart), "window start must not move")
	s.True(got.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd), "window end must not move")
	s.Equal("true", got.Metadata["proration_applied"], "a mid-cycle attach is scaled")
	s.NotEmpty(got.Metadata["proration_coefficient"])
}

// The feature is new to the subscription: a row is created starting at the
// attach, so usage refresh counts only post-attach usage — the same window the
// addon's own prorated charge covers.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_NewFeature_CreatesRowFromAttach() {
	featureID := s.seedGrantFeature("feat_eg_new")
	s.seedGrantAddon("addon_eg_new", "ent_new_addon", featureID, 600, "")

	s.Require().NoError(s.attachAddon("addon_eg_new", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 1)
	got := rows[0]

	s.Equal("ent_new_addon", got.EntitlementConfigID)
	want := s.expectedProrated(600)
	s.True(got.Quota.Equal(want), "expected prorated quota %s, got %s", want, got.Quota)
	s.True(got.ValidFrom.Equal(s.testData.now), "a feature new to the sub starts at the attach, got %s", got.ValidFrom)
	s.True(got.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd))
}

// Two addons on one feature accumulate into the same pooled row, each under its
// own association marker.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_MultipleAddonsAccumulate() {
	featureID := s.seedGrantFeature("feat_eg_multi")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_m1", "ent_m1", featureID, 600, "")
	s.seedGrantAddon("addon_eg_m2", "ent_m2", featureID, 300, "")
	s.Require().NoError(s.attachAddon("addon_eg_m1", s.testData.now, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(s.attachAddon("addon_eg_m2", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 1, "both addons pool into one row, got %d", len(rows))

	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600)).Add(s.expectedProrated(300))
	s.True(rows[0].Quota.Equal(want), "expected %s, got %s", want, rows[0].Quota)

	markers := 0
	for k := range rows[0].Metadata {
		if len(k) > len("proration_addon_assoc_") && k[:len("proration_addon_assoc_")] == "proration_addon_assoc_" {
			markers++
		}
	}
	s.Equal(2, markers, "each attach must leave its own idempotency marker")
}

// persistAddonAttach also runs on the payment-gated replay, so the top-up must
// not apply twice.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_ReplayAppliesOnce() {
	featureID := s.seedGrantFeature("feat_eg_replay")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_replay", "ent_replay", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_replay", s.testData.now, types.ProrationBehaviorCreateProrations))

	afterFirst, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), existing.ID)
	s.Require().NoError(err)

	// The payment-gated replay re-runs the same association through the same
	// resolve-then-materialize pair. Re-applying it must be inert.
	subService := s.service.(*subscriptionService)
	assocID := s.associationIDFor("addon_eg_replay")
	entries := subService.addonEntitlementGrantProration(
		s.GetContext(), s.testData.subscription, s.testData.now,
		types.ProrationBehaviorCreateProrations, "addon_eg_replay", assocID)
	s.Require().NotEmpty(entries, "the replay must resolve the same entry it applied first")
	s.Require().NoError(subService.materializeAddonEntitlementGrantProration(
		s.GetContext(), s.testData.subscription, entries, assocID))

	afterReplay, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), existing.ID)
	s.Require().NoError(err)
	s.True(afterReplay.Quota.Equal(afterFirst.Quota),
		"replay must not top up twice: %s became %s", afterFirst.Quota, afterReplay.Quota)
}

// proration_behavior only decides whether the addon's allowance is SCALED, not
// whether it lands. Credit grants already hand over full credits here, so the
// quota must follow or the two disagree about the same attach.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_BehaviorNone_GrantsFullQuota() {
	featureID := s.seedGrantFeature("feat_eg_none")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_none", "ent_none", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_none", s.testData.now, types.ProrationBehaviorNone))

	got, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), existing.ID)
	s.Require().NoError(err)
	s.True(got.Quota.Equal(decimal.NewFromInt(1600)),
		"unprorated attach adds the addon's full quota, expected 1600, got %s", got.Quota)
	s.Equal("false", got.Metadata["proration_applied"], "the delta was not scaled")
	s.Equal("1", got.Metadata["proration_coefficient"])
	s.True(got.ValidFrom.Equal(s.testData.subscription.CurrentPeriodStart), "window must not move")
}

// An attach at the period start covers the whole cycle, so the coefficient is 1
// and the full quota lands — the row must not be left at the plan's allowance
// while the addon's credits are granted in full.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_AtPeriodStart_GrantsFullQuota() {
	featureID := s.seedGrantFeature("feat_eg_start")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_start", "ent_start", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_start",
		s.testData.subscription.CurrentPeriodStart, types.ProrationBehaviorCreateProrations))

	got, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), existing.ID)
	s.Require().NoError(err)
	s.True(got.Quota.Equal(decimal.NewFromInt(1600)),
		"a full-period attach adds the full quota, expected 1600, got %s", got.Quota)
	s.Equal("1", got.Metadata["proration_coefficient"])
}

// An attach in the last instant of a period prorates to a sliver. numeric(25,15)
// still holds it, so the row is written — the point is that the attach never
// fails over it, and the quota stays positive so Validate passes.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_NearZeroCoefficient_StillAttaches() {
	featureID := s.seedGrantFeature("feat_eg_sliver")
	s.seedGrantAddon("addon_eg_sliver", "ent_sliver", featureID, 1, "")

	at := s.testData.subscription.CurrentPeriodEnd.Add(-time.Nanosecond)
	s.Require().NoError(s.attachAddon("addon_eg_sliver", at, types.ProrationBehaviorCreateProrations),
		"a sliver must never fail the attach")

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 1)
	s.True(rows[0].Quota.IsPositive(), "a sliver must stay positive or Validate would reject it")
	s.True(rows[0].Quota.LessThan(decimal.NewFromFloat(0.001)), "expected a sliver, got %s", rows[0].Quota)
}

// A grant config carrying no quota prorates to nothing; skip it rather than
// fail the whole attach on EntitlementGrant.Validate.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_ZeroQuota_SkipsWithoutFailing() {
	featureID := s.seedGrantFeature("feat_eg_zero")
	s.seedGrantAddon("addon_eg_zero", "ent_zero", featureID, 0, "")

	s.Require().NoError(s.attachAddon("addon_eg_zero", s.testData.now, types.ProrationBehaviorCreateProrations),
		"a zero-quota grant config must never fail the attach")
	s.Empty(s.grantsForFeature(featureID), "a non-positive prorated quota writes no row")
}

// Parallel is not special-cased away: the addon's EC owns its own slot, so it
// gets its own prorated row alongside the plan's.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_Parallel_GetsOwnRow() {
	featureID := s.seedGrantFeature("feat_eg_par")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID,
		1000, types.EntitlementAggregationModeParallel)
	planRow := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_par", "ent_par_addon", featureID, 600, types.EntitlementAggregationModeParallel)
	s.Require().NoError(s.attachAddon("addon_eg_par", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 2, "parallel budgets are independent rows, got %d", len(rows))

	byEC := lo.SliceToMap(rows, func(g *entitlementgrant.EntitlementGrant) (string, *entitlementgrant.EntitlementGrant) {
		return g.EntitlementConfigID, g
	})
	s.True(byEC["ent_aaa_plan"].Quota.Equal(decimal.NewFromInt(1000)), "the plan's budget is untouched")
	s.Equal(planRow.ID, byEC["ent_aaa_plan"].ID)

	addonRow := byEC["ent_par_addon"]
	s.Require().NotNil(addonRow)
	want := s.expectedProrated(600)
	s.True(addonRow.Quota.Equal(want), "expected prorated %s, got %s", want, addonRow.Quota)
	s.True(addonRow.ValidFrom.Equal(s.testData.now), "a parallel budget meters only post-attach usage")
}

// hour/day/week grants are usage-anchored and open post-attach on their own, so
// they take no prorated top-up.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_NonPeriodDuration_Ignored() {
	featureID := s.seedGrantFeature("feat_eg_daily")
	ctx := s.GetContext()

	s.NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID: "addon_eg_daily", LookupKey: "addon_eg_daily", Name: "daily",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_addon_eg_daily",
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           "addon_eg_daily",
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meters.apiCalls.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
	dv := 1
	q := decimal.NewFromInt(600)
	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID:                 "ent_daily_addon",
		EntityType:         types.ENTITLEMENT_ENTITY_TYPE_ADDON,
		EntityID:           "addon_eg_daily",
		FeatureID:          featureID,
		FeatureType:        types.FeatureTypeMetered,
		IsEnabled:          true,
		GrantMeasure:       types.EntitlementGrantMeasureQuantity,
		GrantDurationValue: &dv,
		GrantDurationUnit:  types.EntitlementGrantDurationUnitDay,
		GrantQuota:         &q,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)

	s.Require().NoError(s.attachAddon("addon_eg_daily", s.testData.now, types.ProrationBehaviorCreateProrations))
	s.Empty(s.grantsForFeature(featureID), "a day-unit grant opens on its own from usage, not from the attach")
}
