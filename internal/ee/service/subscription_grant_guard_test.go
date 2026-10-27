package service

import (
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
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

func subscriptionGrantRequest(subID string, quota int64, mode types.EntitlementAggregationMode) dto.CreateEntitlementRequest {
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

func (s *EntitlementGrantSuite) TestCreateSubscriptionGrant_CreatesFeaturelessGrant() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-ok")

	resp, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	s.Equal(types.EntitlementGrantScopeSubscription, resp.GrantScope)
	s.Empty(resp.FeatureID)
	s.Equal(fx.sub.ID, resp.EntityID)
	s.True(decimal.NewFromInt(50).Equal(lo.FromPtr(resp.GrantQuota)))
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionGrant_RefusedWhenAlertsOff() {
	fx := s.newSubGrantSubscription("create-alerts-off")

	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 50, ""))
	s.Require().Error(err)
	s.Contains(err.Error(), "entitlement alerts")
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionGrant_RefusesIncompatibleSubscriptions() {
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
		{"feature usage allowance", "feature usage allowances", func(fx subGrantFixture) {
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

			_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 50, ""))
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)
			s.Contains(err.Error(), tt.reason)
		})
	}
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionGrant_RefusesMixedModes() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-mixed")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 50, types.EntitlementAggregationModeAdditive))
	s.Require().NoError(err)

	_, err = s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 20, types.EntitlementAggregationModeParallel))
	s.Require().Error(err)
	s.Contains(err.Error(), "cannot be mixed")

	_, err = s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 25, types.EntitlementAggregationModeAdditive))
	s.Require().NoError(err, "additive grants pool")
}

func (s *EntitlementGrantSuite) TestCreateSubscriptionGrant_RefusesQuantityAndUnlimited() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("create-shape")

	quantity := subscriptionGrantRequest(fx.sub.ID, 50, "")
	quantity.GrantMeasure = types.EntitlementGrantMeasureQuantity
	_, err := s.entService.CreateEntitlement(s.GetContext(), quantity)
	s.Require().Error(err)
	s.Contains(err.Error(), "measured in amount")

	unlimited := subscriptionGrantRequest(fx.sub.ID, 50, "")
	unlimited.GrantQuota = nil
	unlimited.GrantUnlimited = true
	_, err = s.entService.CreateEntitlement(s.GetContext(), unlimited)
	s.Require().Error(err)
	s.Contains(err.Error(), "requires grant_quota")
}

// subGrantWindowsOf lists the subscription grant windows on a subscription, oldest first.
func (s *EntitlementGrantSuite) subGrantWindowsOf(subID string) []*entitlementgrant.EntitlementGrant {
	rows, err := s.GetStores().EntitlementGrantRepo.List(s.GetContext(), types.NewNoLimitEntitlementGrantFilter().
		WithSubscriptionIDs(subID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription))
	s.Require().NoError(err)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ValidFrom.Before(rows[j].ValidFrom) })
	return rows
}

func (s *EntitlementGrantSuite) TestUpdateSubscriptionGrant_QuotaEditCarriesUsage() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-quota")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)
	window := s.seedEvaluatedSubGrantWindow("eg-edit-quota", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 30, 20)

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

func (s *EntitlementGrantSuite) TestUpdateSubscriptionGrant_OnlyQuotaCanChange() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("edit-other")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 30, ""))
	s.Require().NoError(err)

	_, err = s.entService.UpdateEntitlement(s.GetContext(), created.ID, dto.UpdateEntitlementRequest{
		AggregationMode: lo.ToPtr(types.EntitlementAggregationModeParallel),
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "only grant_quota")
}

func (s *EntitlementGrantSuite) TestDeleteSubscriptionGrant_AdditiveSurvivorKeepsTheRest() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("delete-pool")
	kept, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	removed, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 25, ""))
	s.Require().NoError(err)
	s.seedEvaluatedSubGrantWindow("eg-delete-pool", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 75, 10)

	s.Require().NoError(s.entService.DeleteEntitlement(s.GetContext(), removed.ID))

	windows := s.subGrantWindowsOf(fx.sub.ID)
	s.Require().Len(windows, 2)
	successor := windows[1]
	s.True(decimal.NewFromInt(40).Equal(successor.Quota), "$65 left less the $25 removed: got %s", successor.Quota)
	s.Equal(kept.ID, successor.EntitlementConfigID, "the surviving grant holds the slot")
}

func (s *EntitlementGrantSuite) TestDeleteSubscriptionGrant_LastGrantClosesTheWindow() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("delete-last")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(fx.sub.ID, 30, ""))
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

func (s *EntitlementGrantSuite) TestSubscriptionGrant_RefusesFeatureAllowanceOnGrantedSubscription() {
	s.enableEntitlementAlerts()
	granted := s.newSubGrantSubscription("reverse-granted")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(granted.sub.ID, 50, ""))
	s.Require().NoError(err)
	f := s.simpleFeature("feat-reverse-granted", granted.callsMeter)

	_, err = s.entService.CreateEntitlement(s.GetContext(), meteredOverrideRequest(granted.sub.ID, f.ID))
	s.Require().Error(err)
	s.Contains(err.Error(), "feature usage allowances")

	plain := s.newSubGrantSubscription("reverse-plain")
	other := s.simpleFeature("feat-reverse-plain", plain.callsMeter)
	_, err = s.entService.CreateEntitlement(s.GetContext(), meteredOverrideRequest(plain.sub.ID, other.ID))
	s.Require().NoError(err, "subscriptions without a subscription grant are unaffected")
}

func (s *EntitlementGrantSuite) TestSubscriptionGrantGuard_ChecksLinesOnlyWhenGranted() {
	s.enableEntitlementAlerts()
	tiered := func(fx subGrantFixture) []*subscription.SubscriptionLineItem {
		li := s.addUsageLine(fx, "sli-new-tiered-"+fx.sub.ID, func(p *price.Price, _ *subscription.SubscriptionLineItem) {
			p.BillingModel = types.BILLING_MODEL_TIERED
		})
		return []*subscription.SubscriptionLineItem{li}
	}
	guard := newSubscriptionGrantGuard(s.buildServiceParams())

	granted := s.newSubGrantSubscription("guard-granted")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionGrantRequest(granted.sub.ID, 50, ""))
	s.Require().NoError(err)
	err = guard.checkIfGranted(s.GetContext(), granted.sub, subscriptionGrantChange{items: tiered(granted)})
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")

	plain := s.newSubGrantSubscription("guard-plain")
	s.NoError(guard.checkIfGranted(s.GetContext(), plain.sub, subscriptionGrantChange{items: tiered(plain)}))
}
