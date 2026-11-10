package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// enableEntitlementAlerts turns grant evaluation on for the test: deployment flags and the env setting.
func (s *EntitlementGrantSuite) enableEntitlementAlerts() {
	cfg := s.GetConfig()
	prev := cfg.UsageAlerts
	cfg.UsageAlerts.Enabled = true
	cfg.UsageAlerts.EntitlementAlertsEnabled = true
	s.T().Cleanup(func() { cfg.UsageAlerts = prev })

	ctx := s.GetContext()
	if _, err := s.GetStores().SettingsRepo.GetByKey(ctx, types.SettingKeyEntitlementAlertConfig); err == nil {
		s.Require().NoError(s.GetStores().SettingsRepo.DeleteByKey(ctx, types.SettingKeyEntitlementAlertConfig))
	}
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyEntitlementAlertConfig,
		Value:         map[string]interface{}{"alert_enabled": true},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))
}

func subscriptionEntitlementGrantRequest(subID string, quota int64, mode types.EntitlementAggregationMode) dto.CreateEntitlementRequest {
	return dto.CreateEntitlementRequest{
		EntityType:        types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:          subID,
		GrantMeasure:      types.EntitlementGrantMeasureAmount,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(quota)),
		AggregationMode:   mode,
	}
}

// addUsageLine adds a usage line to the fixture subscription; edit shapes its price and line first.
func (s *EntitlementGrantSuite) addUsageLine(fx subGrantFixture, id string, edit func(*price.Price, *subscription.SubscriptionLineItem)) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	m := s.simpleMeter("meter-" + id)
	pr := &price.Price{
		ID:           "price-" + id,
		Amount:       decimal.NewFromInt(1),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
		MeterID:      m.ID,
		BaseModel:    types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: fx.sub.ID,
		CustomerID:     fx.sub.CustomerID,
		EntityID:       fx.sub.PlanID,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
		PriceID:        pr.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        m.ID,
		Currency:       "usd",
		BillingPeriod:  fx.sub.BillingPeriod,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      fx.sub.StartDate,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if edit != nil {
		edit(pr, li)
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_CreatesFeaturelessGrant() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-ok")

	resp, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantScopeSubscription, resp.GrantScope)
	s.Empty(resp.FeatureID)
	s.Equal(fx.sub.ID, resp.EntityID)
	s.True(decimal.NewFromInt(50).Equal(lo.FromPtr(resp.GrantQuota)))
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_RefusedWhenAlertsOff() {
	fx := s.newSubGrantSubscription("create-alerts-off")

	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().Error(err)
	s.Contains(err.Error(), "entitlement alerts")
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_RefusesIncompatibleSubscriptions() {
	tests := []struct {
		name   string
		reason string
		setup  func(fx subGrantFixture)
	}{
		{"minimum spend", "minimum spend", func(fx subGrantFixture) {
			fx.sub.CommitmentAmount = lo.ToPtr(decimal.NewFromInt(100))
			s.Require().NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), fx.sub))
		}},
		{"line on another cadence", "cadence", func(fx subGrantFixture) {
			s.addUsageLine(fx, "sli-weekly-"+fx.sub.ID, func(_ *price.Price, li *subscription.SubscriptionLineItem) {
				li.BillingPeriod = types.BILLING_PERIOD_WEEKLY
			})
		}},
		{"tiered price", "tiered", func(fx subGrantFixture) {
			s.addUsageLine(fx, "sli-tiered-"+fx.sub.ID, func(p *price.Price, _ *subscription.SubscriptionLineItem) {
				p.BillingModel = types.BILLING_MODEL_TIERED
			})
		}},
		{"tiered line scheduled next period", "tiered", func(fx subGrantFixture) {
			s.addUsageLine(fx, "sli-next-tiered-"+fx.sub.ID, func(p *price.Price, li *subscription.SubscriptionLineItem) {
				p.BillingModel = types.BILLING_MODEL_TIERED
				li.StartDate = fx.sub.CurrentPeriodEnd.Add(time.Hour)
			})
		}},
		{"meter that never resets", "never resets", func(fx subGrantFixture) {
			m := &meter.Meter{
				ID: "meter-never-" + fx.sub.ID, Name: "never", EventName: "never-" + fx.sub.ID,
				Aggregation: meter.Aggregation{Type: types.AggregationSum},
				ResetUsage:  types.ResetUsageNever,
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			}
			s.Require().NoError(s.meterStore.CreateMeter(s.GetContext(), m))
			s.addUsageLine(fx, "sli-never-"+fx.sub.ID, func(p *price.Price, li *subscription.SubscriptionLineItem) {
				p.MeterID, li.MeterID = m.ID, m.ID
			})
		}},
		{"feature entitlement grant", "feature entitlement grants", func(fx subGrantFixture) {
			f := s.simpleFeature("feat-allow-"+fx.sub.ID, fx.callsMeter)
			_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), &entitlement.Entitlement{
				ID:          "ent-allow-" + fx.sub.ID,
				EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
				EntityID:    fx.sub.PlanID,
				FeatureID:   f.ID,
				FeatureType: types.FeatureTypeMetered,
				IsEnabled:   true,
				UsageLimit:  lo.ToPtr(int64(100)),
				BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
			})
			s.Require().NoError(err)
		}},
	}
	for i, tt := range tests {
		s.Run(tt.name, func() {
			s.enableEntitlementAlerts()
			fx := s.newSubGrantSubscription("create-refuse-" + string(rune('a'+i)))
			tt.setup(fx)

			_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Contains(err.Error(), tt.reason)
		})
	}
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_RefusesMixedModes() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-mixed")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, types.EntitlementAggregationModeAdditive))
	s.Require().NoError(err)

	_, err = s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 20, types.EntitlementAggregationModeParallel))
	s.Require().Error(err)
	s.Contains(err.Error(), "cannot be mixed")

	_, err = s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 25, types.EntitlementAggregationModeAdditive))
	s.Require().NoError(err, "additive grants pool")
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_RefusesQuantityAndUnlimited() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-shape")

	quantity := subscriptionEntitlementGrantRequest(fx.sub.ID, 50, "")
	quantity.GrantMeasure = types.EntitlementGrantMeasureQuantity
	_, err := s.entService.CreateEntitlement(s.GetContext(), quantity)
	s.Require().Error(err)
	s.Contains(err.Error(), "measured in amount")

	unlimited := subscriptionEntitlementGrantRequest(fx.sub.ID, 50, "")
	unlimited.GrantQuota = nil
	unlimited.GrantUnlimited = true
	_, err = s.entService.CreateEntitlement(s.GetContext(), unlimited)
	s.Require().Error(err)
	s.Contains(err.Error(), "requires grant_quota")
}

// subGrantWindowsOf lists the subscription entitlement grant windows on a subscription, oldest first.
func (s *EntitlementGrantSuite) subGrantWindowsOf(subID string) []*entitlementgrant.EntitlementGrant {
	rows, err := s.GetStores().EntitlementGrantRepo.List(s.GetContext(), types.NewNoLimitEntitlementGrantFilter().
		WithSubscriptionIDs(subID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription))
	s.Require().NoError(err)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ValidFrom.Before(rows[j].ValidFrom) })
	return rows
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionEntitlementGrant_QuotaEditCarriesUsage() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-quota")
	s.backdate(fx)
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	window := s.seedEvaluatedSubGrantWindow("eg-edit-quota", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 20)
	s.seedMeterUsage(fx.extID, fx.storeMeter, window.ValidFrom.Add(time.Hour), 20) // $20

	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(50)),
	})
	s.Require().NoError(err)

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 2)
	closed, successor := windows[0], windows[1]
	s.Equal(window.ID, closed.ID)
	s.True(closed.ValidTo.Before(window.ValidTo), "the edited window closes now")
	s.True(successor.ValidFrom.Equal(closed.ValidTo), "the successor starts where it closed")
	s.True(successor.ValidTo.Equal(window.ValidTo))
	s.True(decimal.NewFromInt(30).Equal(successor.Quota), "$10 left + $20 more, not a fresh $50: got %s", successor.Quota)
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionEntitlementGrant_OnlyQuotaCanChange() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-other")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)

	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, dto.UpdateEntitlementRequest{
		AggregationMode: lo.ToPtr(types.EntitlementAggregationModeParallel),
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "only grant_quota")
}

func (s *EntitlementGrantSuite) TestDeleteSubscriptionEntitlementGrant_AdditiveSurvivorKeepsTheRest() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("delete-pool")
	s.backdate(fx)
	s.seedMeterUsage(fx.extID, fx.storeMeter, fx.sub.CurrentPeriodStart.Add(time.Hour), 10) // $10
	kept, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	removed, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 25, ""))
	s.Require().NoError(err)
	s.seedEvaluatedSubGrantWindow("eg-delete-pool", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 75, 10)

	s.Require().NoError(s.entService.DeleteEntitlement(s.GetContext(), removed.ID))

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 2)
	successor := windows[1]
	s.True(decimal.NewFromInt(40).Equal(successor.Quota), "$65 left less the $25 removed: got %s", successor.Quota)
	s.Equal(kept.ID, successor.EntitlementConfigID, "the surviving grant holds the slot")
}

func (s *EntitlementGrantSuite) TestDeleteSubscriptionEntitlementGrant_LastGrantClosesTheWindow() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("delete-last")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	window := s.seedEvaluatedSubGrantWindow("eg-delete-last", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 10)

	s.Require().NoError(s.entService.DeleteEntitlement(s.GetContext(), created.ID))

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 1, "no successor")
	s.True(windows[0].ValidTo.Before(window.ValidTo), "the window closes now")
}

// seedEvaluatedSubGrantWindow is seedSubGrantWindow for a window the evaluator has already measured.
func (s *EntitlementGrantSuite) seedEvaluatedSubGrantWindow(id string, fx subGrantFixture, from, to time.Time, quota, usage int64) *entitlementgrant.EntitlementGrant {
	g := s.seedSubGrantWindow(id, fx, from, to, quota, usage, nil)
	g.LastComputedAt = lo.ToPtr(time.Now().UTC())
	_, err := s.GetStores().EntitlementGrantRepo.Update(s.GetContext(), g)
	s.Require().NoError(err)
	return g
}

func meteredOverrideRequest(subID, featureID string) dto.CreateEntitlementRequest {
	return dto.CreateEntitlementRequest{
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:    subID,
		FeatureID:   featureID,
		FeatureType: types.FeatureTypeMetered,
		UsageLimit:  lo.ToPtr(int64(100)),
	}
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_RefusesFeatureEntitlementGrantOnGrantedSubscription() {
	s.enableEntitlementAlerts()
	granted := s.newSubGrantSubscription("reverse-granted")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(granted.sub.ID, 50, ""))
	s.Require().NoError(err)
	f := s.simpleFeature("feat-reverse-granted", granted.callsMeter)

	_, err = s.entService.CreateEntitlement(s.GetContext(), meteredOverrideRequest(granted.sub.ID, f.ID))
	s.Require().Error(err)
	s.Contains(err.Error(), "feature entitlement grants")

	plain := s.newSubGrantSubscription("reverse-plain")
	other := s.simpleFeature("feat-reverse-plain", plain.callsMeter)
	_, err = s.entService.CreateEntitlement(s.GetContext(), meteredOverrideRequest(plain.sub.ID, other.ID))
	s.Require().NoError(err, "subscriptions without a subscription entitlement grant are unaffected")
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrantGuard_ChecksLinesOnlyWhenGranted() {
	s.enableEntitlementAlerts()
	tiered := func(fx subGrantFixture) []*subscription.SubscriptionLineItem {
		li := s.addUsageLine(fx, "sli-new-tiered-"+fx.sub.ID, func(p *price.Price, _ *subscription.SubscriptionLineItem) {
			p.BillingModel = types.BILLING_MODEL_TIERED
		})
		return []*subscription.SubscriptionLineItem{li}
	}
	guard := &entitlementGrantService{ServiceParams: s.buildServiceParams()}

	granted := s.newSubGrantSubscription("guard-granted")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(granted.sub.ID, 50, ""))
	s.Require().NoError(err)
	err = guard.validateWithSubscriptionScopedEntitlement(s.GetContext(), granted.sub, subscriptionEntitlementGrantChange{items: tiered(granted)})
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")

	plain := s.newSubGrantSubscription("guard-plain")
	s.NoError(guard.validateWithSubscriptionScopedEntitlement(s.GetContext(), plain.sub, subscriptionEntitlementGrantChange{items: tiered(plain)}))

	// A grant that has ended no longer blocks changes.
	ended := s.newSubGrantSubscription("guard-ended")
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), &entitlement.Entitlement{
		ID: "ent-ended-" + ended.sub.ID, EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: ended.sub.ID,
		GrantScope: types.EntitlementGrantScopeSubscription, GrantMeasure: types.EntitlementGrantMeasureAmount,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod, GrantQuota: lo.ToPtr(decimal.NewFromInt(50)),
		IsEnabled: true, EndDate: lo.ToPtr(time.Now().UTC().Add(-time.Hour)), BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	})
	s.Require().NoError(err)
	s.NoError(guard.validateWithSubscriptionScopedEntitlement(s.GetContext(), ended.sub, subscriptionEntitlementGrantChange{items: tiered(ended)}))
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionEntitlementGrant_ParallelEditReopensOnlyItsWindow() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-parallel")
	s.backdate(fx)
	s.seedMeterUsage(fx.extID, fx.storeMeter, fx.sub.CurrentPeriodStart.Add(time.Hour), 20) // $20
	edited, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, types.EntitlementAggregationModeParallel))
	s.Require().NoError(err)
	other, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 100, types.EntitlementAggregationModeParallel))
	s.Require().NoError(err)

	window := func(id, ecID string, quota int64) *entitlementgrant.EntitlementGrant {
		g := s.seedEvaluatedSubGrantWindow(id, fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, quota, 20)
		g.EntitlementConfigID = ecID
		_, err := s.GetStores().EntitlementGrantRepo.Update(s.GetContext(), g)
		s.Require().NoError(err)
		return g
	}
	window("eg-edit-par-a", edited.ID, 30)
	untouched := window("eg-edit-par-b", other.ID, 100)

	_, err = s.entService.UpdateEntitlement(s.GetContext(), edited.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(50)),
	})
	s.Require().NoError(err)

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 3, "two original windows plus one successor")
	successor := lo.MaxBy(windows, func(a, b *entitlementgrant.EntitlementGrant) bool { return a.ValidFrom.After(b.ValidFrom) })
	s.Equal(edited.ID, successor.EntitlementConfigID)
	s.True(decimal.NewFromInt(30).Equal(successor.Quota), "$10 left + $20 more: got %s", successor.Quota)

	stillLive, err := s.GetStores().EntitlementGrantRepo.Get(s.GetContext(), untouched.ID)
	s.Require().NoError(err)
	s.True(stillLive.ValidTo.Equal(untouched.ValidTo), "the other parallel grant's window is not cut")
}

// backdate moves the fixture subscription and its lines a day into the past, so usage can land in it.
func (s *EntitlementGrantSuite) backdate(fx subGrantFixture) {
	ctx := s.GetContext()
	start := fx.sub.StartDate.Add(-24 * time.Hour)
	fx.sub.StartDate, fx.sub.CurrentPeriodStart = start, start
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, fx.sub))
	items, err := s.GetStores().SubscriptionLineItemRepo.ListBySubscription(ctx, fx.sub)
	s.Require().NoError(err)
	for _, li := range items {
		li.StartDate = start
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, li))
	}
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionEntitlementGrant_QuotaEditMeasuresUsageFirst() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-stale")
	s.backdate(fx)
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	// Last evaluated at $10 used, but $25 has been spent since the window opened.
	window := s.seedEvaluatedSubGrantWindow("eg-edit-stale", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 10)
	s.seedMeterUsage(fx.extID, fx.storeMeter, window.ValidFrom.Add(time.Hour), 25)

	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, dto.UpdateEntitlementRequest{
		GrantQuota: lo.ToPtr(decimal.NewFromInt(50)),
	})
	s.Require().NoError(err)

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 2)
	s.True(decimal.NewFromInt(25).Equal(windows[1].Quota), "$5 really left + $20 more, not $40: got %s", windows[1].Quota)
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_StartsNowUnlessDated() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-start")
	before := time.Now().UTC()
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	s.Require().NotNil(created.StartDate)
	s.False(created.StartDate.Before(before), "an undated grant starts when it is created")
	s.Nil(created.EndDate)

	dated := s.newSubGrantSubscription("create-start-dated")
	req := subscriptionEntitlementGrantRequest(dated.sub.ID, 30, "")
	req.StartDate = lo.ToPtr(dated.sub.CurrentPeriodStart)
	req.EndDate = lo.ToPtr(dated.sub.CurrentPeriodStart.Add(10 * 24 * time.Hour))
	got, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.Require().NoError(err)
	s.True(got.StartDate.Equal(*req.StartDate))
	s.True(got.EndDate.Equal(*req.EndDate))
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_PlanIDWithoutEntityIDIsRefused() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-plan-id")
	req := subscriptionEntitlementGrantRequest("", 30, "")
	req.PlanID = fx.sub.PlanID
	s.NotPanics(func() {
		_, err := s.entService.CreateEntitlement(s.GetContext(), req)
		s.Require().Error(err)
		s.True(ierr.IsValidation(err), "got %v", err)
	})
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrantGuard_ChecksLinesStartingNextPeriod() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("guard-future")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)

	future := &subscription.SubscriptionLineItem{
		ID: "sli-future-tiered", SubscriptionID: fx.sub.ID, PriceID: "price-future-tiered",
		PriceType: types.PRICE_TYPE_USAGE, MeterID: fx.callsMeter,
		BillingPeriod: fx.sub.BillingPeriod, StartDate: fx.sub.CurrentPeriodEnd.Add(time.Hour),
	}
	tiered := &price.Price{ID: "price-future-tiered", Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_TIERED, MeterID: fx.callsMeter}
	err = (&entitlementGrantService{ServiceParams: s.buildServiceParams()}).validateWithSubscriptionScopedEntitlement(s.GetContext(), fx.sub, subscriptionEntitlementGrantChange{
		items: []*subscription.SubscriptionLineItem{future}, prices: []*price.Price{tiered},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_RefusesCommitmentAddedLater() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("commit-later")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)

	_, err = NewCommitmentService(s.buildServiceParams()).Create(s.GetContext(), dto.CreateCommitmentRequest{SubscriptionID: fx.sub.ID})
	s.Require().Error(err)
	s.Contains(err.Error(), "subscription has a subscription entitlement grant")
}

// A terminated open-ended commitment matches the active-window query forever; only a live term blocks the grant.
func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_IgnoresTerminatedOpenEndedCommitment() {
	s.enableEntitlementAlerts()
	ctx := s.GetContext()
	setCommitmentsV2(s.T(), ctx, s.GetStores(), true)
	fx := s.newSubGrantSubscription("commit-terminated-open")
	dead := &commitment.Commitment{
		ID:               "cmt_dead_open",
		SubscriptionID:   fx.sub.ID,
		CustomerID:       fx.sub.CustomerID,
		Name:             "dead",
		InvoiceLabel:     "dead",
		Currency:         fx.sub.Currency,
		ScopeEntityType:  types.CommitmentScopeSubscription,
		ScopeEntityID:    fx.sub.ID,
		Type:             types.COMMITMENT_TYPE_AMOUNT,
		Value:            decimal.NewFromInt(10),
		Measurement:      types.CommitmentMeasurementBillingPeriod,
		Timezone:         "UTC",
		Priority:         1,
		StartDate:        fx.sub.CurrentPeriodStart.AddDate(0, -2, 0),
		CommitmentStatus: types.CommitmentStatusTerminated,
		TerminatedAt:     lo.ToPtr(fx.sub.CurrentPeriodStart.AddDate(0, -1, 0)),
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	created, err := s.GetStores().CommitmentRepo.Create(ctx, dead)
	s.Require().NoError(err)
	s.Require().NotNil(created)

	_, err = s.entService.CreateEntitlement(ctx, subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err, "a commitment that ended before this period must not block the grant")
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionEntitlementGrant_RepeatedEditAddsNothing() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-repeat")
	s.backdate(fx)
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	s.seedEvaluatedSubGrantWindow("eg-edit-repeat", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 0)

	edit := dto.UpdateEntitlementRequest{GrantQuota: lo.ToPtr(decimal.NewFromInt(50))}
	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, edit)
	s.Require().NoError(err)
	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, edit)
	s.Require().NoError(err)

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 2, "the second edit to the same quota re-cuts nothing")
	s.True(decimal.NewFromInt(50).Equal(windows[1].Quota), "got %s", windows[1].Quota)
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionEntitlementGrant_RefusesWindowAsLongAsThePeriod() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-long-window")
	req := subscriptionEntitlementGrantRequest(fx.sub.ID, 30, "")
	req.GrantDurationUnit = types.EntitlementGrantDurationUnitDay
	req.GrantDurationValue = lo.ToPtr(int(fx.sub.CurrentPeriodEnd.Sub(fx.sub.CurrentPeriodStart).Hours()/24) + 1)
	_, err := s.entService.CreateEntitlement(s.GetContext(), req)
	s.Require().Error(err)
	s.Contains(err.Error(), "as long as the billing period")
}
