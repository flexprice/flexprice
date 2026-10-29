package service

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_GetSkipsFeatureLookup() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("api-get")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)

	got, err := s.entService.GetEntitlement(s.GetContext(), created.ID)
	s.Require().NoError(err)
	s.Nil(got.Feature)
	s.Equal(types.EntitlementGrantScopeSubscription, got.GrantScope)
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_ReadSectionShowsLedger() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("api-read")
	created, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	window := s.seedEvaluatedSubGrantWindow("eg-api-read", fx, fx.sub.CurrentPeriodStart, fx.sub.CurrentPeriodEnd, 50, 20)

	view, err := (&entitlementGrantService{ServiceParams: s.buildServiceParams()}).subscriptionEntitlementGrantsView(s.GetContext(), fx.sub)
	s.Require().NoError(err)
	s.Require().NotNil(view)
	s.Require().Len(view.Entitlements, 1)
	s.Equal(created.ID, view.Entitlements[0].ID)
	s.Require().NotNil(view.GrantState)
	s.Require().Len(view.GrantState.Allowances, 1)
	allowance := view.GrantState.Allowances[0]
	s.Equal(window.ID, allowance.GrantID)
	s.True(decimal.NewFromInt(30).Equal(lo.FromPtr(allowance.Remaining)), "remaining: got %s", lo.FromPtr(allowance.Remaining))
	s.True(allowance.IsActive)

	plain := s.newSubGrantSubscription("api-read-plain")
	none, err := (&entitlementGrantService{ServiceParams: s.buildServiceParams()}).subscriptionEntitlementGrantsView(s.GetContext(), plain.sub)
	s.Require().NoError(err)
	s.Nil(none, "no section without a subscription entitlement grant")
}

func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrant_RefusesPlanChangesThatBreakIt() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("api-plan")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)
	ctx := s.GetContext()

	// A feature entitlement grant on the plan reaches the subscription.
	f := s.simpleFeature("feat-api-plan", fx.callsMeter)
	_, err = s.entService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
		PlanID:      fx.sub.PlanID,
		FeatureID:   f.ID,
		FeatureType: types.FeatureTypeMetered,
		UsageLimit:  lo.ToPtr(int64(100)),
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "feature entitlement grants")

	planPrice := func(id string, model types.BillingModel, period types.BillingPeriod) *price.Price {
		return &price.Price{
			ID: id, Amount: decimal.NewFromInt(1), Currency: "usd", Type: types.PRICE_TYPE_USAGE,
			BillingModel: model, BillingPeriod: period, BillingPeriodCount: 1, MeterID: fx.callsMeter,
			EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: fx.sub.PlanID,
		}
	}
	guard := &entitlementGrantService{ServiceParams: s.buildServiceParams()}

	// A tiered usage price on the plan lands on the subscription.
	tiered := planPrice("price-api-plan-tiered", types.BILLING_MODEL_TIERED, fx.sub.BillingPeriod)
	err = guard.validateCatalogChange(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, fx.sub.PlanID, changeForNewPrice(tiered))
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")

	// A price on another billing period never lands on this subscription.
	other := lo.Ternary(fx.sub.BillingPeriod == types.BILLING_PERIOD_ANNUAL, types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_ANNUAL)
	s.NoError(guard.validateCatalogChange(ctx, types.ENTITLEMENT_ENTITY_TYPE_PLAN, fx.sub.PlanID,
		changeForNewPrice(planPrice("price-api-plan-other", types.BILLING_MODEL_TIERED, other))))
}

// Bulk price create runs the same guard as a single create.
func (s *EntitlementGrantSuite) TestSubscriptionEntitlementGrantGuard_BulkPriceCreate() {
	s.enableEntitlementAlerts()
	fx := s.newSubGrantSubscription("api-bulk-price")
	_, err := s.entService.CreateEntitlement(s.GetContext(), subscriptionEntitlementGrantRequest(fx.sub.ID, 50, ""))
	s.Require().NoError(err)

	_, err = NewPriceService(s.buildServiceParams()).CreateBulkPrice(s.GetContext(), dto.CreateBulkPriceRequest{
		Items: []dto.CreatePriceRequest{{
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           fx.sub.PlanID,
			Type:               types.PRICE_TYPE_USAGE,
			MeterID:            fx.callsMeter,
			BillingPeriod:      fx.sub.BillingPeriod,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_TIERED,
			TierMode:           types.BILLING_TIER_SLAB,
			InvoiceCadence:     types.InvoiceCadenceArrear,
			Tiers: []dto.CreatePriceTier{
				{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(2)},
				{UnitAmount: decimal.NewFromInt(1)},
			},
		}},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")
}

// The test DB has no real transactions, so this checks the refusal; Postgres rolls the subscription back.
func (s *SubscriptionServiceSuite) TestCreateSubscription_RefusesIncompatibleSubscriptionEntitlementGrant() {
	ctx := s.GetContext()
	cfg := s.GetConfig()
	prev := cfg.UsageAlerts
	cfg.UsageAlerts.Enabled = true
	cfg.UsageAlerts.EntitlementAlertsEnabled = true
	s.T().Cleanup(func() { cfg.UsageAlerts = prev })
	s.Require().NoError(s.GetStores().SettingsRepo.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyEntitlementAlertConfig,
		Value:         map[string]interface{}{"alert_enabled": true},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	// The test plan carries a tiered usage price, which a subscription entitlement grant cannot cover.
	_, err := s.service.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(s.testData.now),
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		SubscriptionEntitlementGrants: []dto.CreateSubscriptionEntitlementGrantRequest{{
			GrantQuota:        lo.ToPtr(decimal.NewFromInt(50)),
			GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		}},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "tiered")
}

func (s *SubscriptionServiceSuite) TestCreateSubscription_RefusesGrantsWithCommitments() {
	_, err := s.service.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		StartDate:          lo.ToPtr(s.testData.now),
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		SubscriptionEntitlementGrants: []dto.CreateSubscriptionEntitlementGrantRequest{{
			GrantQuota:        lo.ToPtr(decimal.NewFromInt(50)),
			GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		}},
		Commitments: []dto.CreateCommitmentRequest{{}},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "cannot be combined with commitments")
}
