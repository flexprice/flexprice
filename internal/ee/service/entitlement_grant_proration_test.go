package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
		// A live mid-cycle row has always been evaluated: the tick that opens a
		// subscription_period grant evaluates it in the same pass. That timestamp is
		// where a mid-cycle change closes the window, so the fixture must carry one.
		WithLastComputedAt(lo.ToPtr(s.testData.now.Add(-time.Hour))).
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

// End-to-end through the real attach path: a PARALLEL addon owns its own slot, so
// the attach writes nothing and the tick opens it. The tick must anchor that slot at
// the association's start, not at the cycle start — otherwise the addon's first
// window bills for usage recorded before the customer had the entitlement.
func (s *SubscriptionServiceSuite) TestAddonParallelGrant_TickAnchorsAtAssociationStart() {
	featureID := s.seedGrantFeature("feat_eg_par_anchor")
	s.seedGrantEC("ent_aaa_plan_par", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		s.testData.plan.ID, 500, types.EntitlementAggregationModeParallel)
	s.seedGrantAddon("addon_eg_par_anchor", "ent_zzz_addon_par", featureID, 400,
		types.EntitlementAggregationModeParallel)

	sub := s.testData.subscription
	attachedAt := s.testData.now
	s.Require().True(attachedAt.After(sub.CurrentPeriodStart), "fixture must attach mid-cycle")
	s.Require().NoError(s.attachAddon("addon_eg_par_anchor", attachedAt, types.ProrationBehaviorCreateProrations))

	// The tick, exactly as the evaluator runs it.
	cust, err := s.GetStores().CustomerRepo.Get(s.GetContext(), sub.CustomerID)
	s.Require().NoError(err)
	grantSvc := NewEntitlementGrantService(s.service.(*subscriptionService).ServiceParams)
	_, _, err = grantSvc.EnsureGrantsForSubscriptions(s.GetContext(), cust,
		[]*subscription.Subscription{sub}, attachedAt.Add(time.Minute))
	s.Require().NoError(err)

	rows := s.sortedGrantsForFeature(featureID)
	byEC := lo.SliceToMap(rows, func(g *entitlementgrant.EntitlementGrant) (string, *entitlementgrant.EntitlementGrant) {
		return g.EntitlementConfigID, g
	})

	plan := byEC["ent_aaa_plan_par"]
	s.Require().NotNil(plan, "the plan's own parallel slot must still exist")
	s.True(plan.ValidFrom.Equal(sub.CurrentPeriodStart),
		"the plan's slot is untouched by the attach: got %s want %s", plan.ValidFrom, sub.CurrentPeriodStart)

	addonRow := byEC["ent_zzz_addon_par"]
	s.Require().NotNil(addonRow, "the addon's parallel slot must be opened by the tick")
	s.True(addonRow.Quota.Equal(decimal.NewFromInt(400)),
		"parallel carries its own quota, not the pool: got %s", addonRow.Quota)
	s.True(addonRow.ValidFrom.Equal(attachedAt),
		"the addon's slot must start when it was attached, not at the cycle start: got %s want %s",
		addonRow.ValidFrom, attachedAt)
	s.False(addonRow.ValidFrom.Equal(sub.CurrentPeriodStart), "must not backdate to the cycle start")
}

// sortedGrantsForFeature returns the feature's rows oldest window first, so a
// segmented cycle reads as the sequence it is.
func (s *SubscriptionServiceSuite) sortedGrantsForFeature(featureID string) []*entitlementgrant.EntitlementGrant {
	rows := s.grantsForFeature(featureID)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ValidFrom.Before(rows[j].ValidFrom) })
	return rows
}

// The feature already has this cycle's pooled row. Grants are immutable, so the
// attach closes that window and opens a successor beside it carrying the remaining
// balance plus the addon's prorated delta. The two segments tile the cycle.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_ExistingFeature_ClosesAndOpensSuccessor() {
	featureID := s.seedGrantFeature("feat_eg_topup")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_topup", "ent_zzz_addon", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_topup", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "the cycle must be two tiling segments, got %d", len(rows))
	closed, live := rows[0], rows[1]

	s.Equal(existing.ID, closed.ID, "the first segment is the row that already existed")
	s.True(closed.Quota.Equal(decimal.NewFromInt(1000)), "a closed window keeps the quota it was opened with")
	s.True(closed.ValidFrom.Equal(s.testData.subscription.CurrentPeriodStart))

	s.Equal("ent_aaa_plan", live.EntitlementConfigID, "the successor stays on the incumbent slot")
	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600))
	s.True(live.Quota.Equal(want), "expected quota %s, got %s", want, live.Quota)
	s.True(live.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd))

	s.True(closed.ValidTo.Equal(live.ValidFrom),
		"segments must tile with no gap or overlap: closed ends %s, successor starts %s",
		closed.ValidTo, live.ValidFrom)

	coefficient, err := decimal.NewFromString(live.Metadata["proration_coefficient"])
	s.Require().NoError(err)
	s.True(coefficient.LessThan(decimal.NewFromInt(1)),
		"a mid-cycle attach is scaled, got coefficient %s", coefficient)
	s.Equal(grantProrationSourceAddonAttach.String(), live.Metadata["proration_source"],
		"a segment must name the change that cut it")
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

// Each attach segments the cycle again, and the live segment carries the pooled
// balance forward, so the allowance accumulates without any row being mutated.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_MultipleAddonsAccumulate() {
	featureID := s.seedGrantFeature("feat_eg_multi")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_m1", "ent_m1", featureID, 600, "")
	s.seedGrantAddon("addon_eg_m2", "ent_m2", featureID, 300, "")
	s.Require().NoError(s.attachAddon("addon_eg_m1", s.testData.now, types.ProrationBehaviorCreateProrations))
	s.Require().NoError(s.attachAddon("addon_eg_m2", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.sortedGrantsForFeature(featureID)

	// The first attach segments the cycle. The second finds a successor the evaluator
	// has not seen yet — a window with no measurable life — so it replaces that row
	// over the same span rather than segmenting again. Either way the balance carries.
	s.Require().Len(rows, 2, "expected the closed segment plus one live row, got %d", len(rows))
	live := rows[len(rows)-1]

	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600)).Add(s.expectedProrated(300))
	s.True(live.Quota.Equal(want), "both deltas must accumulate, expected %s, got %s", want, live.Quota)
	s.True(live.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd))

	for i := 1; i < len(rows); i++ {
		s.True(rows[i-1].ValidTo.Equal(rows[i].ValidFrom),
			"segment %d must start where %d ended", i, i-1)
	}
}

// A row the evaluator has never seen has no measurable life — usage over a window
// with no elapsed measured time is zero — so the attach replaces it over its own
// window instead of closing it into a window Validate would reject. Nothing is lost:
// the replacement spans the same range, so usage inside it still counts against the
// full pool.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_UnevaluatedRow_ReplacedInPlace() {
	featureID := s.seedGrantFeature("feat_eg_unevaluated")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")

	existing := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)
	// Undo the fixture's snapshot: this row has never been through a tick.
	existing.LastComputedAt = nil
	_, err := s.GetStores().EntitlementGrantRepo.Update(s.GetContext(), existing)
	s.Require().NoError(err)

	s.seedGrantAddon("addon_eg_unevaluated", "ent_unevaluated", featureID, 600, "")
	s.Require().NoError(s.attachAddon("addon_eg_unevaluated", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "the un-lived row is replaced, not segmented, got %d rows", len(rows))
	got := rows[0]

	s.NotEqual(existing.ID, got.ID, "the replacement is a new row")
	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600))
	s.True(got.Quota.Equal(want), "expected %s, got %s", want, got.Quota)
	s.True(got.ValidFrom.Equal(s.testData.subscription.CurrentPeriodStart),
		"the replacement must span the original window so no usage escapes it")
	s.True(got.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd))
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

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2)
	closed, live := rows[0], rows[1]
	s.Equal(existing.ID, closed.ID)

	s.True(live.Quota.Equal(decimal.NewFromInt(1600)),
		"unprorated attach carries the full balance plus the full quota, expected 1600, got %s", live.Quota)

	s.Equal("1", live.Metadata["proration_coefficient"], "the delta was not scaled")
	s.True(live.ValidTo.Equal(s.testData.subscription.CurrentPeriodEnd))
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

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2)
	closed, live := rows[0], rows[1]
	s.Equal(existing.ID, closed.ID)

	s.True(live.Quota.Equal(decimal.NewFromInt(1600)),
		"a full-period attach adds the full quota, expected 1600, got %s", live.Quota)
	s.Equal("1", live.Metadata["proration_coefficient"])
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

// A parallel EC owns its own slot, and the evaluator opens that slot with the EC's full
// quota — a standalone budget, not a top-up of a pool. So the attach writes no row of its
// own; it only ends the feature's live windows at the change, which hands every slot back
// to the evaluator to reissue. The allowance is replenished rather than prorated: an
// immutable window cannot be topped up in place, so a mid-cycle addon resets the feature.
func (s *SubscriptionServiceSuite) TestAddonEntitlementProration_Parallel_ClosedForEvaluatorToReissue() {
	featureID := s.seedGrantFeature("feat_eg_par")
	s.seedGrantEC("ent_aaa_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID,
		1000, types.EntitlementAggregationModeParallel)
	planRow := s.seedCycleGrant("ent_aaa_plan", featureID, 1000)

	s.seedGrantAddon("addon_eg_par", "ent_par_addon", featureID, 600, types.EntitlementAggregationModeParallel)
	s.Require().NoError(s.attachAddon("addon_eg_par", s.testData.now, types.ProrationBehaviorCreateProrations))

	rows := s.grantsForFeature(featureID)
	s.Require().Len(rows, 1, "the attach must not write parallel rows itself, got %d", len(rows))

	got := rows[0]
	s.Equal(planRow.ID, got.ID, "the plan's row is closed in place, not replaced")
	s.Equal("ent_aaa_plan", got.EntitlementConfigID)
	s.True(got.Quota.Equal(decimal.NewFromInt(1000)), "the closed window keeps the quota it ran with")
	s.True(got.ValidFrom.Equal(s.testData.subscription.CurrentPeriodStart), "the window's start never moves")
	s.False(got.ValidTo.Before(s.testData.now),
		"the window must end at the change so the evaluator can reissue the slot: got %s, change at %s",
		got.ValidTo, s.testData.now)
	s.True(got.ValidTo.Before(s.testData.subscription.CurrentPeriodEnd), "it must no longer run to the cycle end")
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

// =============================================================================
// Batch grant passes: states a single-source change cannot reach
// =============================================================================

// -----------------------------------------------------------------------------
// batch fixtures
//
// The multi-addon entry point does not exist yet (D1/E2), but the merged pass already
// takes []GrantSource — so these drive it directly and pin the behaviour the batch
// must have before anything is wired to it.
// -----------------------------------------------------------------------------

func (s *SubscriptionServiceSuite) grantService() *subscriptionGrantService {
	return newSubscriptionGrantService(s.service.(*subscriptionService).ServiceParams)
}

func (s *SubscriptionServiceSuite) runGrantPass(incoming, removed []GrantSource) error {
	svc := s.grantService()
	cfg, err := svc.Resolve(s.GetContext(), GrantChangeRequest{
		Sub:      s.testData.subscription,
		Incoming: incoming,
		Removed:  removed,
	})
	if err != nil {
		return err
	}
	return svc.Apply(s.GetContext(), cfg)
}

func (s *SubscriptionServiceSuite) source(
	at time.Time,
	origin grantProrationSource,
	ecs ...*entitlement.Entitlement,
) GrantSource {
	return GrantSource{
		ChangeType:    grantChangeTypeFor(s.testData.subscription, at),
		EffectiveDate: at,
		Behavior:      types.ProrationBehaviorCreateProrations,
		Origin:        origin,
		AddonID:       ecs[0].EntityID,
	}
}

func (s *SubscriptionServiceSuite) periodEnd() time.Time {
	return s.testData.subscription.CurrentPeriodEnd
}

// expectedProratedAt scales a quota over the part of the cycle left at `at`.
func (s *SubscriptionServiceSuite) expectedProratedAt(quota int64, at time.Time) decimal.Decimal {
	sub := s.testData.subscription
	total := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart).Seconds()
	remaining := sub.CurrentPeriodEnd.Sub(at).Seconds()
	return decimal.NewFromInt(quota).
		Mul(decimal.NewFromFloat(remaining).Div(decimal.NewFromFloat(total))).
		Round(15)
}

func (s *SubscriptionServiceSuite) liveRow(featureID string) *entitlementgrant.EntitlementGrant {
	rows := s.sortedGrantsForFeature(featureID)
	if len(rows) == 0 {
		return nil
	}
	return rows[len(rows)-1]
}

// assertCutAtChange asserts a window was ended by the change rather than running to its
// natural end. The pass closes at LatestOf(effectiveDate, now), a few ms past the
// fixture's `now`, so the boundary can only be bracketed.
func (s *SubscriptionServiceSuite) assertCutAtChange(g *entitlementgrant.EntitlementGrant) {
	s.True(!g.ValidTo.Before(s.testData.now) && g.ValidTo.Before(s.periodEnd()),
		"expected a window cut between %s and %s, got %s", s.testData.now, s.periodEnd(), g.ValidTo)
}

func (s *SubscriptionServiceSuite) assertTiled(featureID string) {
	rows := s.sortedGrantsForFeature(featureID)
	for i := 1; i < len(rows); i++ {
		s.True(rows[i-1].ValidTo.Equal(rows[i].ValidFrom),
			"%s: segment %d must start where %d ended", featureID, i, i-1)
	}
}

func (s *SubscriptionServiceSuite) ecByID(ecID string) *entitlement.Entitlement {
	ec, err := s.GetStores().EntitlementRepo.Get(s.GetContext(), ecID)
	s.Require().NoError(err)
	return ec
}

// seedActiveAddonAssociation puts an addon on the subscription without going through the
// attach path, so the feature keeps its cold-start state (no live grant row).
func (s *SubscriptionServiceSuite) seedActiveAddonAssociation(associationID, addonID string) *addonassociation.AddonAssociation {
	start := s.testData.now
	assoc := &addonassociation.AddonAssociation{
		ID:          associationID,
		EntityID:    s.testData.subscription.ID,
		EntityType:  types.AddonAssociationEntityTypeSubscription,
		AddonID:     addonID,
		StartDate:   &start,
		AddonStatus: types.AddonStatusActive,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().AddonAssociationRepo.Create(s.GetContext(), assoc))
	return assoc
}

// -----------------------------------------------------------------------------
// 1. multiple additions
// -----------------------------------------------------------------------------

// Two addons landing on one additive feature must pool into ONE successor. Looping the
// single-addon path would let the second close the successor the first just opened.
func (s *SubscriptionServiceSuite) TestGrantBatch_Add_OverlappingAdditiveFeature_PoolsIntoOneSuccessor() {
	featureID := s.seedGrantFeature("feat_b_add_overlap")
	s.seedGrantEC("ent_b_plan_ov", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_plan_ov", featureID, 1000)

	a := s.seedGrantEC("ent_b_add_a", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_a", 600, "")
	b := s.seedGrantEC("ent_b_add_b", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_b", 300, "")

	s.Require().NoError(s.runGrantPass([]GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonAttach, a),
		s.source(s.testData.now, grantProrationSourceAddonAttach, b),
	}, nil))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "one closed segment plus one live successor, got %d", len(rows))

	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600)).Add(s.expectedProrated(300))
	s.True(rows[1].Quota.Equal(want), "both deltas pool, expected %s got %s", want, rows[1].Quota)
	s.True(rows[1].ValidTo.Equal(s.periodEnd()))
	s.assertTiled(featureID)
}

// Independent features must not interfere: each closes its own predecessor and opens
// its own successor.
func (s *SubscriptionServiceSuite) TestGrantBatch_Add_NonOverlappingFeatures_SegmentIndependently() {
	f1 := s.seedGrantFeature("feat_b_add_f1")
	f2 := s.seedGrantFeature("feat_b_add_f2")
	s.seedGrantEC("ent_b_plan_f1", f1, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedGrantEC("ent_b_plan_f2", f2, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 500, "")
	s.seedCycleGrant("ent_b_plan_f1", f1, 1000)
	s.seedCycleGrant("ent_b_plan_f2", f2, 500)

	a := s.seedGrantEC("ent_b_f1_addon", f1, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_f1", 600, "")
	b := s.seedGrantEC("ent_b_f2_addon", f2, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_f2", 200, "")

	s.Require().NoError(s.runGrantPass([]GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonAttach, a),
		s.source(s.testData.now, grantProrationSourceAddonAttach, b),
	}, nil))

	s.Require().Len(s.sortedGrantsForFeature(f1), 2)
	s.Require().Len(s.sortedGrantsForFeature(f2), 2)
	s.True(s.liveRow(f1).Quota.Equal(decimal.NewFromInt(1000).Add(s.expectedProrated(600))))
	s.True(s.liveRow(f2).Quota.Equal(decimal.NewFromInt(500).Add(s.expectedProrated(200))))
	s.assertTiled(f1)
	s.assertTiled(f2)
}

// An add dated at period end belongs to the NEXT cycle: resolveGrantProration skips it
// and the tick opens it at full quota at renewal. It must not alter this cycle.
func (s *SubscriptionServiceSuite) TestGrantBatch_Add_NowPlusPeriodEnd_OnlyNowAffectsThisCycle() {
	featureID := s.seedGrantFeature("feat_b_add_mixed_date")
	s.seedGrantEC("ent_b_plan_md", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_plan_md", featureID, 1000)

	now := s.seedGrantEC("ent_b_md_now", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_md_now", 600, "")
	later := s.seedGrantEC("ent_b_md_later", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_md_later", 300, "")

	s.Require().NoError(s.runGrantPass([]GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonAttach, now),
		s.source(s.periodEnd(), grantProrationSourceAddonAttach, later),
	}, nil))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "the period-end add must not segment this cycle, got %d rows", len(rows))

	want := decimal.NewFromInt(1000).Add(s.expectedProrated(600))
	s.True(rows[1].Quota.Equal(want),
		"only the immediate add contributes this cycle, expected %s got %s", want, rows[1].Quota)
}

// A parallel EC owns its slot outright, so the pass opens nothing for it — the tick
// reissues it. The additive feature in the same batch is still segmented.
func (s *SubscriptionServiceSuite) TestGrantBatch_Add_ParallelFeature_ClosedForTickNotReopened() {
	par := s.seedGrantFeature("feat_b_add_par")
	add := s.seedGrantFeature("feat_b_add_additive")
	s.seedGrantEC("ent_b_par_plan", par, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 400,
		types.EntitlementAggregationModeParallel)
	s.seedGrantEC("ent_b_add_plan", add, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_par_plan", par, 400)
	s.seedCycleGrant("ent_b_add_plan", add, 1000)

	parEC := s.seedGrantEC("ent_b_par_addon", par, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_par", 100,
		types.EntitlementAggregationModeParallel)
	addEC := s.seedGrantEC("ent_b_add_addon", add, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_add", 600, "")

	s.Require().NoError(s.runGrantPass([]GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonAttach, parEC),
		s.source(s.testData.now, grantProrationSourceAddonAttach, addEC),
	}, nil))

	parRows := s.sortedGrantsForFeature(par)
	s.Require().Len(parRows, 1, "the parallel slot is closed and left to the tick, got %d rows", len(parRows))
	s.assertCutAtChange(parRows[0])

	s.Require().Len(s.sortedGrantsForFeature(add), 2)
	s.True(s.liveRow(add).Quota.Equal(decimal.NewFromInt(1000).Add(s.expectedProrated(600))))
}

// -----------------------------------------------------------------------------
// 2. multiple removals
// -----------------------------------------------------------------------------

// Two removals on one additive feature share one pooled row: it must be closed once,
// and one successor carries the remaining balance for the survivors.
func (s *SubscriptionServiceSuite) TestGrantBatch_Remove_OverlappingAdditiveFeature_ClosesPooledRowOnce() {
	featureID := s.seedGrantFeature("feat_b_rm_overlap")
	s.seedGrantEC("ent_b_rm_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_rm_plan", featureID, 1000)

	a := s.seedGrantEC("ent_b_rm_a", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rm_a", 600, "")
	b := s.seedGrantEC("ent_b_rm_b", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rm_b", 300, "")

	s.Require().NoError(s.runGrantPass(nil, []GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonDetach, a),
		s.source(s.testData.now, grantProrationSourceAddonDetach, b),
	}))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "closed pool plus one carry-forward, got %d", len(rows))
	s.True(rows[1].Quota.Equal(decimal.NewFromInt(1000)), "the carry-forward keeps the unspent balance")
	s.Equal(rows[0].ID, rows[1].Metadata["carry_forward_from"])
	s.assertTiled(featureID)
}

// A removal dated at period end must leave this cycle alone: the window expires on its own.
// What keeps the EC out of the NEXT cycle is the association window, pinned separately by
// TestGrantBatch_RemoveAtPeriodEnd_AssociationStopsNextCycle.
func (s *SubscriptionServiceSuite) TestGrantBatch_Remove_NowPlusPeriodEnd_PeriodEndLeavesWindowIntact() {
	f1 := s.seedGrantFeature("feat_b_rm_now")
	f2 := s.seedGrantFeature("feat_b_rm_later")
	s.seedGrantEC("ent_b_rmnow_plan", f1, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedGrantEC("ent_b_rmlater_plan", f2, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 500, "")
	s.seedCycleGrant("ent_b_rmnow_plan", f1, 1000)
	untouched := s.seedCycleGrant("ent_b_rmlater_plan", f2, 500)

	a := s.seedGrantEC("ent_b_rmnow_addon", f1, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rmnow", 600, "")
	b := s.seedGrantEC("ent_b_rmlater_addon", f2, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rmlater", 200, "")

	s.Require().NoError(s.runGrantPass(nil, []GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonDetach, a),
		s.source(s.periodEnd(), grantProrationSourceAddonDetach, b),
	}))

	s.Len(s.sortedGrantsForFeature(f1), 2, "the immediate removal segments its feature")

	later := s.sortedGrantsForFeature(f2)
	s.Require().Len(later, 1, "the period-end removal must not segment this cycle, got %d rows", len(later))
	s.True(later[0].ValidTo.Equal(untouched.ValidTo),
		"the window must still run to period end, got %s", later[0].ValidTo)
}

// Parallel slots are owned per EC: removing one addon closes only its own row.
func (s *SubscriptionServiceSuite) TestGrantBatch_Remove_ParallelFeature_ClosesOnlyItsOwnSlot() {
	featureID := s.seedGrantFeature("feat_b_rm_par")
	s.seedGrantEC("ent_b_rmpar_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 400,
		types.EntitlementAggregationModeParallel)
	removed := s.seedGrantEC("ent_b_rmpar_addon", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rmpar", 100,
		types.EntitlementAggregationModeParallel)

	planSlot := s.seedCycleGrant("ent_b_rmpar_plan", featureID, 400)
	addonSlot := s.seedCycleGrant("ent_b_rmpar_addon", featureID, 100)

	s.Require().NoError(s.runGrantPass(nil, []GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonDetach, removed),
	}))

	rows := s.grantsForFeature(featureID)
	byID := lo.SliceToMap(rows, func(g *entitlementgrant.EntitlementGrant) (string, *entitlementgrant.EntitlementGrant) {
		return g.ID, g
	})
	s.True(byID[planSlot.ID].ValidTo.Equal(planSlot.ValidTo), "the surviving slot is untouched")
	s.assertCutAtChange(byID[addonSlot.ID])
}

// Nothing survives the removal, so the window closes with no successor: reopening it
// would hand back quota from configs that no longer feed the feature.
func (s *SubscriptionServiceSuite) TestGrantBatch_Remove_LastECOnFeature_ClosesWithoutSuccessor() {
	featureID := s.seedGrantFeature("feat_b_rm_last")
	only := s.seedGrantEC("ent_b_rmlast_addon", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_rmlast", 600, "")
	s.seedCycleGrant("ent_b_rmlast_addon", featureID, 600)

	s.Require().NoError(s.runGrantPass(nil, []GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonDetach, only),
	}))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "no successor when nothing survives, got %d rows", len(rows))
	s.assertCutAtChange(rows[0])
}

// -----------------------------------------------------------------------------
// 3. mixed additions and removals
// -----------------------------------------------------------------------------

// A swap on one feature is ONE decision: the window closes once and a single successor
// carries the survivors plus the incoming quota.
func (s *SubscriptionServiceSuite) TestGrantBatch_Swap_SameAdditiveFeature_OneCloseOneSuccessor() {
	featureID := s.seedGrantFeature("feat_b_swap")
	s.seedGrantEC("ent_b_swap_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_swap_plan", featureID, 1000)

	out := s.seedGrantEC("ent_b_swap_out", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_swap_out", 600, "")
	in := s.seedGrantEC("ent_b_swap_in", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_swap_in", 300, "")

	s.Require().NoError(s.runGrantPass(
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonAttach, in)},
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonDetach, out)},
	))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "a swap segments once, got %d rows", len(rows))

	want := decimal.NewFromInt(1000).Add(s.expectedProrated(300))
	s.True(rows[1].Quota.Equal(want), "expected %s got %s", want, rows[1].Quota)
	s.Empty(rows[1].Metadata["carry_forward_from"], "the incoming open owns the successor, not a carry-forward")
	s.assertTiled(featureID)
}

// A removal only changes the successor's quota on a COLD-START feature, where the open
// sums the surviving ECs instead of inheriting a predecessor's balance. These two pin the
// one thing a removal's date decides: whether the leaving EC is still a live config.
//
// Removed now: the EC is gone, so it must not contribute.
func (s *SubscriptionServiceSuite) TestGrantBatch_ColdStart_RemoveNow_LeavingECDropsOut() {
	featureID := s.seedGrantFeature("feat_b_cold_now")
	s.seedGrantEC("ent_b_cold_now_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedGrantAddon("addon_b_cold_now_out", "ent_b_cold_now_out", featureID, 600, "")
	s.seedActiveAddonAssociation("assoc_b_cold_now", "addon_b_cold_now_out")

	out := s.ecByID("ent_b_cold_now_out")
	in := s.seedGrantEC("ent_b_cold_now_in", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_cold_now_in", 300, "")

	s.Require().NoError(s.runGrantPass(
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonAttach, in)},
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonDetach, out)},
	))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "cold start opens one window, got %d rows", len(rows))

	want := s.expectedProrated(300).Add(decimal.NewFromInt(1000))
	s.True(rows[0].Quota.Equal(want),
		"the leaving addon must not contribute, expected %s got %s", want, rows[0].Quota)
}

// Removed at period end: the addon is paid for through the cycle, so its EC still counts.
func (s *SubscriptionServiceSuite) TestGrantBatch_ColdStart_RemoveAtPeriodEnd_LeavingECStillCounts() {
	featureID := s.seedGrantFeature("feat_b_cold_pe")
	s.seedGrantEC("ent_b_cold_pe_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedGrantAddon("addon_b_cold_pe_out", "ent_b_cold_pe_out", featureID, 600, "")
	s.seedActiveAddonAssociation("assoc_b_cold_pe", "addon_b_cold_pe_out")

	out := s.ecByID("ent_b_cold_pe_out")
	in := s.seedGrantEC("ent_b_cold_pe_in", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_cold_pe_in", 300, "")

	s.Require().NoError(s.runGrantPass(
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonAttach, in)},
		[]GrantSource{s.source(s.periodEnd(), grantProrationSourceAddonDetach, out)},
	))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "cold start opens one window, got %d rows", len(rows))

	want := s.expectedProrated(300).Add(decimal.NewFromInt(1000)).Add(decimal.NewFromInt(600))
	s.True(rows[0].Quota.Equal(want),
		"the addon leaves at period end, so it still feeds this cycle, expected %s got %s", want, rows[0].Quota)
}

// The only EC feeding a feature leaves as another arrives. The successor must start where
// the closed window ended — two live rows over one instant would double the quota — but must
// NOT inherit its balance: removing that EC on its own drops the balance, and an unrelated
// addon sharing the batch cannot be what rescues it.
func (s *SubscriptionServiceSuite) TestGrantBatch_Swap_LastECOnFeature_TilesWithoutCarryingQuota() {
	featureID := s.seedGrantFeature("feat_b_lastswap")
	s.seedGrantAddon("addon_b_lastswap_out", "ent_b_lastswap_out", featureID, 600, "")
	s.seedActiveAddonAssociation("assoc_b_lastswap", "addon_b_lastswap_out")
	s.seedCycleGrant("ent_b_lastswap_out", featureID, 600)

	out := s.ecByID("ent_b_lastswap_out")
	in := s.seedGrantEC("ent_b_lastswap_in", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_lastswap_in", 300, "")

	s.Require().NoError(s.runGrantPass(
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonAttach, in)},
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonDetach, out)},
	))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 2, "one closed window plus the incoming addon's own, got %d", len(rows))

	s.assertCutAtChange(rows[0])
	s.assertTiled(featureID)

	want := s.expectedProrated(300)
	s.True(rows[1].Quota.Equal(want),
		"the leaving addon's unspent quota must not carry, expected %s got %s", want, rows[1].Quota)
}

// Add and remove on unrelated features must not leak into each other.
func (s *SubscriptionServiceSuite) TestGrantBatch_AddAndRemove_DifferentFeatures_Independent() {
	added := s.seedGrantFeature("feat_b_mix_added")
	removed := s.seedGrantFeature("feat_b_mix_removed")
	s.seedGrantEC("ent_b_mixadd_plan", added, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedGrantEC("ent_b_mixrm_plan", removed, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 500, "")
	s.seedCycleGrant("ent_b_mixadd_plan", added, 1000)
	s.seedCycleGrant("ent_b_mixrm_plan", removed, 500)

	in := s.seedGrantEC("ent_b_mixadd_addon", added, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_mixadd", 600, "")
	out := s.seedGrantEC("ent_b_mixrm_addon", removed, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_mixrm", 200, "")

	s.Require().NoError(s.runGrantPass(
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonAttach, in)},
		[]GrantSource{s.source(s.testData.now, grantProrationSourceAddonDetach, out)},
	))

	s.Require().Len(s.sortedGrantsForFeature(added), 2)
	s.True(s.liveRow(added).Quota.Equal(decimal.NewFromInt(1000).Add(s.expectedProrated(600))))

	rmRows := s.sortedGrantsForFeature(removed)
	s.Require().Len(rmRows, 2)
	s.True(rmRows[1].Quota.Equal(decimal.NewFromInt(500)), "the carry-forward keeps the survivors' balance")
	s.assertTiled(added)
	s.assertTiled(removed)
}

// A spent pool has nothing to hand forward, and a zero-quota successor is rejected by
// the model — so the window stays open and keeps its slot covered.
func (s *SubscriptionServiceSuite) TestGrantBatch_Remove_SpentPool_LeavesWindowOpen() {
	featureID := s.seedGrantFeature("feat_b_rm_spent")
	s.seedGrantEC("ent_b_spent_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	out := s.seedGrantEC("ent_b_spent_addon", featureID, types.ENTITLEMENT_ENTITY_TYPE_ADDON, "addon_b_spent", 600, "")

	spent := s.seedCycleGrant("ent_b_spent_plan", featureID, 1000)
	spent.Usage = decimal.NewFromInt(1000)
	_, err := s.GetStores().EntitlementGrantRepo.Update(s.GetContext(), spent)
	s.Require().NoError(err)

	s.Require().NoError(s.runGrantPass(nil, []GrantSource{
		s.source(s.testData.now, grantProrationSourceAddonDetach, out),
	}))

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "the spent window is left alone, got %d rows", len(rows))
	s.True(rows[0].ValidTo.Equal(spent.ValidTo), "it must keep its original window")
}

// What actually stops next cycle's window is the association, not the grant pass: a
// cancelled association drops out of GetActiveAddonAssociation, so the EC no longer
// feeds the feature. This pins WHEN that happens for a period-end removal.
func (s *SubscriptionServiceSuite) TestGrantBatch_RemoveAtPeriodEnd_AssociationStopsNextCycle() {
	ctx := s.GetContext()
	featureID := s.seedGrantFeature("feat_b_assoc_end")
	s.seedGrantEC("ent_b_assoc_plan", featureID, types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, 1000, "")
	s.seedCycleGrant("ent_b_assoc_plan", featureID, 1000)
	s.seedGrantAddon("addon_b_assoc", "ent_b_assoc_addon", featureID, 600, "")

	s.Require().NoError(s.attachAddon("addon_b_assoc", s.testData.now, types.ProrationBehaviorCreateProrations))

	ecsBefore, err := s.grantService().GetSubscriptionGrantECsByFeature(ctx, s.testData.subscription)
	s.Require().NoError(err)
	s.Len(ecsBefore[featureID], 2, "the attached addon feeds the feature")

	assocs, err := s.GetStores().AddonAssociationRepo.List(ctx, types.NewNoLimitAddonAssociationFilter())
	s.Require().NoError(err)
	assoc, found := lo.Find(assocs, func(a *addonassociation.AddonAssociation) bool {
		return a.AddonID == "addon_b_assoc"
	})
	s.Require().True(found)

	s.Require().NoError(s.service.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
		AddonAssociationID: assoc.ID,
		EffectiveDate:      lo.ToPtr(s.periodEnd()),
	}))

	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assoc.ID)
	s.Require().NoError(err)
	s.True(lo.FromPtr(stored.EndDate).Equal(s.periodEnd()), "the association ends at period end")

	// Cancelled is stamped immediately even for a future-dated removal, so status alone
	// cannot gate the entitlement — the window has to.
	s.Equal(types.AddonStatusCancelled, stored.AddonStatus)

	ecsAfter, err := s.grantService().GetSubscriptionGrantECsByFeature(ctx, s.testData.subscription)
	s.Require().NoError(err)
	s.Len(ecsAfter[featureID], 2,
		"the addon is paid for until period end, so its EC must still feed this cycle")

	s.True(s.liveRow(featureID).ValidTo.Equal(s.periodEnd()),
		"and its grant window must still run to period end")
}
