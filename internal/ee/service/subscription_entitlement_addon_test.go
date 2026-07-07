package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionEntitlementAddonSuite tests entitlement resolution/overrides and addon
// removal flows in subscription.go.
type SubscriptionEntitlementAddonSuite struct {
	testutil.BaseServiceTestSuite
	svc      SubscriptionService
	testData struct {
		customer       *customer.Customer
		plan           *plan.Plan
		sub            *subscription.Subscription
		meter          *meter.Meter
		meteredFeature *feature.Feature
		staticFeature  *feature.Feature
		configFeature  *feature.Feature
		meteredEnt     *entitlement.Entitlement
		staticEnt      *entitlement.Entitlement
		configEnt      *entitlement.Entitlement
		now            time.Time
	}
}

func TestSubscriptionEntitlementAddon(t *testing.T) {
	suite.Run(t, new(SubscriptionEntitlementAddonSuite))
}

func (s *SubscriptionEntitlementAddonSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *SubscriptionEntitlementAddonSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionEntitlementAddonSuite) internalService() *subscriptionService {
	return s.svc.(*subscriptionService)
}

func (s *SubscriptionEntitlementAddonSuite) createFeature(name string, featureType types.FeatureType, meterID string) *feature.Feature {
	ctx := s.GetContext()
	f := &feature.Feature{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:      name,
		LookupKey: name,
		Type:      featureType,
		MeterID:   meterID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))
	return f
}

func (s *SubscriptionEntitlementAddonSuite) createEntitlement(entityType types.EntitlementEntityType, entityID string, f *feature.Feature, mutate func(*entitlement.Entitlement)) *entitlement.Entitlement {
	ctx := s.GetContext()
	ent := &entitlement.Entitlement{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:  entityType,
		EntityID:    entityID,
		FeatureID:   f.ID,
		FeatureType: f.Type,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	switch f.Type {
	case types.FeatureTypeMetered:
		ent.UsageLimit = lo.ToPtr(int64(1000))
		ent.UsageResetPeriod = types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY
	case types.FeatureTypeStatic:
		ent.StaticValue = "gold"
	case types.FeatureTypeConfig:
		ent.ConfigValue = map[string]interface{}{"key": "value"}
	}
	if mutate != nil {
		mutate(ent)
	}
	created, err := s.GetStores().EntitlementRepo.Create(ctx, ent)
	s.Require().NoError(err)
	return created
}

func (s *SubscriptionEntitlementAddonSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_ent_addon",
		Name:       "Entitlement Addon Customer",
		Email:      "ent-addon@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Entitlement Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.meter = &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Entitlement API Calls",
		EventName: "ent_api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.testData.meter))

	s.testData.meteredFeature = s.createFeature("ent_metered_feature", types.FeatureTypeMetered, s.testData.meter.ID)
	s.testData.staticFeature = s.createFeature("ent_static_feature", types.FeatureTypeStatic, "")
	s.testData.configFeature = s.createFeature("ent_config_feature", types.FeatureTypeConfig, "")

	s.testData.meteredEnt = s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, s.testData.meteredFeature, nil)
	s.testData.staticEnt = s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, s.testData.staticFeature, nil)
	s.testData.configEnt = s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_PLAN, s.testData.plan.ID, s.testData.configFeature, nil)

	s.testData.sub = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.sub, nil))
}

// createAddonWithPrice creates a published addon with a fixed monthly price.
func (s *SubscriptionEntitlementAddonSuite) createAddonWithPrice(name string, amount decimal.Decimal) *addon.Addon {
	ctx := s.GetContext()
	a := &addon.Addon{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ADDON),
		LookupKey: name,
		Name:      name,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().AddonRepo.Create(ctx, a))

	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           a.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return a
}

func (s *SubscriptionEntitlementAddonSuite) TestGetSubscriptionEntitlements() {
	ctx := s.GetContext()

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.svc.GetSubscriptionEntitlements(ctx, "sub_missing")
		s.Error(err)
	})

	s.Run("returns_plan_entitlements_when_no_overrides", func() {
		ents, err := s.svc.GetSubscriptionEntitlements(ctx, s.testData.sub.ID)
		s.Require().NoError(err)
		s.Len(ents, 3)
		featureIDs := lo.Map(ents, func(e *dto.EntitlementResponse, _ int) string { return e.FeatureID })
		s.ElementsMatch(featureIDs, []string{
			s.testData.meteredFeature.ID,
			s.testData.staticFeature.ID,
			s.testData.configFeature.ID,
		})
	})

	s.Run("active_subscription_override_replaces_parent", func() {
		override := s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, s.testData.sub.ID, s.testData.meteredFeature, func(e *entitlement.Entitlement) {
			e.ParentEntitlementID = &s.testData.meteredEnt.ID
			e.UsageLimit = lo.ToPtr(int64(5000))
			e.StartDate = lo.ToPtr(s.testData.now.Add(-24 * time.Hour))
		})

		ents, err := s.svc.GetSubscriptionEntitlements(ctx, s.testData.sub.ID)
		s.Require().NoError(err)
		s.Len(ents, 3)

		ids := lo.Map(ents, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.Contains(ids, override.ID)
		s.NotContains(ids, s.testData.meteredEnt.ID, "overridden plan entitlement must be filtered out")
	})

	s.Run("includes_entitlements_from_active_addons", func() {
		a := s.createAddonWithPrice("entitlement_source_addon", decimal.NewFromInt(3))
		addonEnt := s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, a.ID, s.testData.meteredFeature, nil)
		s.addAddon(a, types.AddonCadenceRecurring)

		ents, err := s.svc.GetSubscriptionEntitlements(ctx, s.testData.sub.ID)
		s.Require().NoError(err)

		ids := lo.Map(ents, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.Contains(ids, addonEnt.ID, "addon entitlement must be included for active association")
	})

	s.Run("expired_subscription_override_keeps_parent", func() {
		expired := s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, s.testData.sub.ID, s.testData.staticFeature, func(e *entitlement.Entitlement) {
			e.ParentEntitlementID = &s.testData.staticEnt.ID
			e.StaticValue = "platinum"
			e.StartDate = lo.ToPtr(s.testData.now.Add(-48 * time.Hour))
			e.EndDate = lo.ToPtr(s.testData.now.Add(-24 * time.Hour))
		})

		ents, err := s.svc.GetSubscriptionEntitlements(ctx, s.testData.sub.ID)
		s.Require().NoError(err)

		ids := lo.Map(ents, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.NotContains(ids, expired.ID, "expired override must not be returned")
		s.Contains(ids, s.testData.staticEnt.ID, "parent must be kept when override is inactive")
	})
}

func (s *SubscriptionEntitlementAddonSuite) TestGetAggregatedSubscriptionEntitlements() {
	ctx := s.GetContext()

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.svc.GetAggregatedSubscriptionEntitlements(ctx, "sub_missing", nil)
		s.Error(err)
	})

	s.Run("nil_request_aggregates_all_features", func() {
		resp, err := s.svc.GetAggregatedSubscriptionEntitlements(ctx, s.testData.sub.ID, nil)
		s.Require().NoError(err)
		s.Equal(s.testData.sub.ID, resp.SubscriptionID)
		s.Equal(s.testData.plan.ID, resp.PlanID)
		s.Len(resp.Features, 3)
		for _, f := range resp.Features {
			for _, src := range f.Sources {
				s.Equal(s.testData.sub.ID, src.SubscriptionID)
			}
		}
	})

	s.Run("feature_ids_filter_limits_result", func() {
		resp, err := s.svc.GetAggregatedSubscriptionEntitlements(ctx, s.testData.sub.ID, &dto.GetSubscriptionEntitlementsRequest{
			FeatureIDs: []string{s.testData.meteredFeature.ID},
		})
		s.Require().NoError(err)
		s.Require().Len(resp.Features, 1)
		s.Equal(s.testData.meteredFeature.ID, resp.Features[0].Feature.Feature.ID)
	})
}

func (s *SubscriptionEntitlementAddonSuite) TestListByCustomerID() {
	ctx := s.GetContext()

	s.Run("empty_customer_id_returns_validation_error", func() {
		_, err := s.svc.ListByCustomerID(ctx, "")
		s.Error(err)
	})

	s.Run("returns_active_subscriptions_for_customer", func() {
		subs, err := s.svc.ListByCustomerID(ctx, s.testData.customer.ID)
		s.Require().NoError(err)
		s.Require().Len(subs, 1)
		s.Equal(s.testData.sub.ID, subs[0].ID)
	})
}

func (s *SubscriptionEntitlementAddonSuite) TestFilterOverriddenEntitlements() {
	mkResp := func(id string, parentID *string, start, end *time.Time) *dto.EntitlementResponse {
		return &dto.EntitlementResponse{
			Entitlement: &entitlement.Entitlement{
				ID:                  id,
				ParentEntitlementID: parentID,
				StartDate:           start,
				EndDate:             end,
			},
		}
	}
	now := time.Now().UTC()
	svc := s.internalService()

	s.Run("no_overrides_combines_all_sources", func() {
		got := svc.filterOverriddenEntitlements(
			[]*dto.EntitlementResponse{mkResp("plan_1", nil, nil, nil)},
			[]*dto.EntitlementResponse{mkResp("addon_1", nil, nil, nil)},
			[]*dto.EntitlementResponse{mkResp("sub_1", nil, nil, nil)},
			"sub_x",
		)
		s.Len(got, 3)
	})

	s.Run("active_override_removes_parent_plan_and_addon_entitlements", func() {
		got := svc.filterOverriddenEntitlements(
			[]*dto.EntitlementResponse{mkResp("plan_1", nil, nil, nil), mkResp("plan_2", nil, nil, nil)},
			[]*dto.EntitlementResponse{mkResp("addon_1", nil, nil, nil)},
			[]*dto.EntitlementResponse{
				mkResp("sub_1", lo.ToPtr("plan_1"), nil, nil),
				mkResp("sub_2", lo.ToPtr("addon_1"), nil, nil),
			},
			"sub_x",
		)
		ids := lo.Map(got, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.ElementsMatch(ids, []string{"plan_2", "sub_1", "sub_2"})
	})

	s.Run("future_override_is_ignored", func() {
		futureStart := now.Add(24 * time.Hour)
		got := svc.filterOverriddenEntitlements(
			[]*dto.EntitlementResponse{mkResp("plan_1", nil, nil, nil)},
			nil,
			[]*dto.EntitlementResponse{mkResp("sub_1", lo.ToPtr("plan_1"), &futureStart, nil)},
			"sub_x",
		)
		ids := lo.Map(got, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.ElementsMatch(ids, []string{"plan_1"})
	})

	s.Run("expired_override_is_ignored", func() {
		pastStart := now.Add(-48 * time.Hour)
		pastEnd := now.Add(-24 * time.Hour)
		got := svc.filterOverriddenEntitlements(
			[]*dto.EntitlementResponse{mkResp("plan_1", nil, nil, nil)},
			nil,
			[]*dto.EntitlementResponse{mkResp("sub_1", lo.ToPtr("plan_1"), &pastStart, &pastEnd)},
			"sub_x",
		)
		ids := lo.Map(got, func(e *dto.EntitlementResponse, _ int) string { return e.ID })
		s.ElementsMatch(ids, []string{"plan_1"})
	})
}

func (s *SubscriptionEntitlementAddonSuite) TestProcessSubscriptionEntitlementOverrides() {
	ctx := s.GetContext()
	svc := s.internalService()

	listSubOverrides := func() []*entitlement.Entitlement {
		filter := types.NewNoLimitEntitlementFilter().
			WithEntityIDs([]string{s.testData.sub.ID}).
			WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION)
		ents, err := s.GetStores().EntitlementRepo.List(ctx, filter)
		s.Require().NoError(err)
		return ents
	}

	s.Run("empty_overrides_is_noop", func() {
		s.NoError(svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, nil))
		s.Empty(listSubOverrides())
	})

	s.Run("missing_entitlement_id_returns_validation_error", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{{}})
		s.Error(err)
	})

	s.Run("unknown_entitlement_returns_not_found", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: "ent_missing"},
		})
		s.Error(err)
	})

	s.Run("addon_entitlement_cannot_be_overridden", func() {
		a := s.createAddonWithPrice("override_addon", decimal.NewFromInt(5))
		addonEnt := s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, a.ID, s.testData.meteredFeature, nil)

		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: addonEnt.ID},
		})
		s.Error(err)
	})

	s.Run("metered_override_with_static_value_is_rejected", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: s.testData.meteredEnt.ID, StaticValue: lo.ToPtr("nope")},
		})
		s.Error(err)
	})

	s.Run("metered_override_with_config_value_is_rejected", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: s.testData.meteredEnt.ID, ConfigValue: map[string]interface{}{"a": 1}},
		})
		s.Error(err)
	})

	s.Run("static_override_with_usage_limit_is_rejected", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: s.testData.staticEnt.ID, UsageLimit: lo.ToPtr(int64(10))},
		})
		s.Error(err)
	})

	s.Run("config_override_with_is_enabled_is_rejected", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: s.testData.configEnt.ID, IsEnabled: lo.ToPtr(false)},
		})
		s.Error(err)
	})

	s.Run("creates_metered_static_and_config_overrides", func() {
		err := svc.ProcessSubscriptionEntitlementOverrides(ctx, s.testData.sub, []dto.OverrideEntitlementRequest{
			{EntitlementID: s.testData.meteredEnt.ID, UsageLimit: lo.ToPtr(int64(9999)), IsEnabled: lo.ToPtr(true)},
			{EntitlementID: s.testData.staticEnt.ID, StaticValue: lo.ToPtr("platinum")},
			{EntitlementID: s.testData.configEnt.ID, ConfigValue: map[string]interface{}{"key": "override"}},
		})
		s.Require().NoError(err)

		overrides := listSubOverrides()
		s.Require().Len(overrides, 3)

		byFeature := lo.KeyBy(overrides, func(e *entitlement.Entitlement) string { return e.FeatureID })

		metered := byFeature[s.testData.meteredFeature.ID]
		s.Require().NotNil(metered)
		s.Equal(int64(9999), lo.FromPtr(metered.UsageLimit))
		s.Equal(s.testData.meteredEnt.ID, lo.FromPtr(metered.ParentEntitlementID))
		s.Equal(types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, metered.UsageResetPeriod)
		s.Require().NotNil(metered.StartDate)
		s.True(metered.StartDate.Equal(s.testData.sub.StartDate))
		s.Nil(metered.EndDate)

		static := byFeature[s.testData.staticFeature.ID]
		s.Require().NotNil(static)
		s.Equal("platinum", static.StaticValue)

		config := byFeature[s.testData.configFeature.ID]
		s.Require().NotNil(config)
		s.Equal("override", config.ConfigValue["key"])
	})
}

func (s *SubscriptionEntitlementAddonSuite) TestValidateEntitlementCompatibility() {
	ctx := s.GetContext()
	svc := s.internalService()

	s.Run("addon_without_metered_entitlements_is_compatible", func() {
		a := s.createAddonWithPrice("compat_static_addon", decimal.NewFromInt(5))
		s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, a.ID, s.testData.staticFeature, nil)

		s.NoError(svc.validateEntitlementCompatibility(ctx, s.testData.sub.ID, a.ID))
	})

	s.Run("matching_usage_reset_period_is_compatible", func() {
		a := s.createAddonWithPrice("compat_matching_addon", decimal.NewFromInt(5))
		s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, a.ID, s.testData.meteredFeature, nil)

		s.NoError(svc.validateEntitlementCompatibility(ctx, s.testData.sub.ID, a.ID))
	})

	s.Run("conflicting_usage_reset_period_returns_error", func() {
		a := s.createAddonWithPrice("compat_conflicting_addon", decimal.NewFromInt(5))
		s.createEntitlement(types.ENTITLEMENT_ENTITY_TYPE_ADDON, a.ID, s.testData.meteredFeature, func(e *entitlement.Entitlement) {
			e.UsageResetPeriod = types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY
		})

		err := svc.validateEntitlementCompatibility(ctx, s.testData.sub.ID, a.ID)
		s.Error(err)
	})
}

func (s *SubscriptionEntitlementAddonSuite) addAddon(a *addon.Addon, cadence types.AddonCadence) string {
	ctx := s.GetContext()
	assoc, err := s.svc.AddAddonToSubscription(ctx, s.testData.sub.ID, &dto.AddAddonToSubscriptionRequest{
		AddonID: a.ID,
		Cadence: cadence,
	})
	s.Require().NoError(err)
	return assoc.ID
}

func (s *SubscriptionEntitlementAddonSuite) TestRemoveAddonFromSubscription() {
	ctx := s.GetContext()

	s.Run("missing_association_id_returns_validation_error", func() {
		s.Error(s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{}))
	})

	s.Run("association_not_found_returns_error", func() {
		s.Error(s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: "aa_missing",
		}))
	})

	s.Run("removes_recurring_addon_at_period_end", func() {
		a := s.createAddonWithPrice("remove_recurring_addon", decimal.NewFromInt(7))
		assocID := s.addAddon(a, types.AddonCadenceRecurring)

		s.NoError(s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: assocID,
			Reason:             "no longer needed",
		}))

		assoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assocID)
		s.Require().NoError(err)
		s.Equal(types.AddonStatusCancelled, assoc.AddonStatus)
		s.Equal("no longer needed", assoc.CancellationReason)
		s.Require().NotNil(assoc.EndDate)
		s.True(assoc.EndDate.Equal(s.testData.sub.CurrentPeriodEnd))

		// Line items are terminated at the effective end date.
		liFilter := types.NewNoLimitSubscriptionLineItemFilter()
		liFilter.SubscriptionIDs = []string{s.testData.sub.ID}
		liFilter.AddonAssociationIDs = []string{assocID}
		items, err := s.GetStores().SubscriptionLineItemRepo.List(ctx, liFilter)
		s.Require().NoError(err)
		s.Require().NotEmpty(items)
		for _, li := range items {
			s.False(li.EndDate.IsZero(), "line item must be scheduled to end")
		}

		// Idempotency: removing again reports "already scheduled to be removed".
		err = s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: assocID,
		})
		s.Error(err)
	})

	s.Run("effective_date_outside_period_is_rejected", func() {
		a := s.createAddonWithPrice("remove_bad_date_addon", decimal.NewFromInt(7))
		assocID := s.addAddon(a, types.AddonCadenceRecurring)

		badDate := s.testData.sub.CurrentPeriodEnd.Add(24 * time.Hour)
		err := s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: assocID,
			EffectiveDate:      &badDate,
		})
		s.Error(err)

		// The association must be untouched.
		assoc, gerr := s.GetStores().AddonAssociationRepo.GetByID(ctx, assocID)
		s.Require().NoError(gerr)
		s.Nil(assoc.EndDate)
	})

	s.Run("onetime_addon_already_scheduled_to_end_is_rejected", func() {
		a := s.createAddonWithPrice("remove_onetime_addon", decimal.NewFromInt(7))
		assocID := s.addAddon(a, types.AddonCadenceOnetime)

		err := s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: assocID,
		})
		s.Error(err)
	})

	s.Run("mid_period_removal_with_proration_behavior_succeeds", func() {
		a := s.createAddonWithPrice("remove_prorated_addon", decimal.NewFromInt(7))
		assocID := s.addAddon(a, types.AddonCadenceRecurring)

		effective := s.testData.now.Add(24 * time.Hour)
		s.NoError(s.svc.RemoveAddonFromSubscription(ctx, &dto.RemoveAddonRequest{
			AddonAssociationID: assocID,
			EffectiveDate:      &effective,
			ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		}))

		assoc, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, assocID)
		s.Require().NoError(err)
		s.Equal(types.AddonStatusCancelled, assoc.AddonStatus)
		s.Require().NotNil(assoc.EndDate)
		s.True(assoc.EndDate.Equal(effective))
	})
}
