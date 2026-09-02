package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
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
