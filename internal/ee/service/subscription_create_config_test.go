package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupondomain "github.com/flexprice/flexprice/internal/domain/coupon"
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

// SubscriptionCreateConfigSuite tests the CreateSubscription helper flows in
// subscription.go: phases, phase/subscription coupons, extra phase line items,
// credit grants and entitlement proration.
type SubscriptionCreateConfigSuite struct {
	testutil.BaseServiceTestSuite
	svc      SubscriptionService
	testData struct {
		customer   *customer.Customer
		plan       *plan.Plan
		fixedPrice *price.Price
		sub        *subscription.Subscription
		coupon     *coupondomain.Coupon
		now        time.Time
	}
}

func TestSubscriptionCreateConfig(t *testing.T) {
	suite.Run(t, new(SubscriptionCreateConfigSuite))
}

func (s *SubscriptionCreateConfigSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *SubscriptionCreateConfigSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionCreateConfigSuite) internalService() *subscriptionService {
	return s.svc.(*subscriptionService)
}

func (s *SubscriptionCreateConfigSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_create_config",
		Name:       "Create Config Customer",
		Email:      "create-config@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Create Config Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.fixedPrice = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(20),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.fixedPrice))

	s.testData.sub = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          s.testData.now.Add(-15 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-15 * 24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(15 * 24 * time.Hour),
		BillingAnchor:      s.testData.now.Add(-15 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, s.testData.sub, nil))

	pct := decimal.NewFromInt(10)
	s.testData.coupon = &coupondomain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Create Config Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		CouponCode:    lo.ToPtr("CONFIG10"),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.testData.coupon.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, s.testData.coupon))
}

func (s *SubscriptionCreateConfigSuite) TestNormalizePhaseCoupons() {
	ctx := s.GetContext()
	svc := s.internalService()
	phaseID := "phase_test_1"
	phaseStart := s.testData.now
	phaseEnd := s.testData.now.Add(30 * 24 * time.Hour)
	priceToLineItem := map[string]string{s.testData.fixedPrice.ID: "li_1"}

	s.Run("subscription_level_coupons_carry_phase_dates", func() {
		got, err := svc.normalizePhaseCoupons(ctx, dto.SubscriptionPhaseCreateRequest{
			StartDate: phaseStart,
			EndDate:   &phaseEnd,
			Coupons:   []string{s.testData.coupon.ID, ""},
		}, phaseID, priceToLineItem)
		s.Require().NoError(err)
		s.Require().Len(got, 1)
		s.Equal(s.testData.coupon.ID, got[0].CouponID)
		s.Equal(phaseID, lo.FromPtr(got[0].SubscriptionPhaseID))
		s.True(got[0].StartDate.Equal(phaseStart))
		s.True(got[0].EndDate.Equal(phaseEnd))
		s.Nil(got[0].LineItemID)
	})

	s.Run("line_item_coupons_resolve_price_to_line_item", func() {
		got, err := svc.normalizePhaseCoupons(ctx, dto.SubscriptionPhaseCreateRequest{
			StartDate: phaseStart,
			LineItemCoupons: map[string][]string{
				s.testData.fixedPrice.ID: {s.testData.coupon.ID},
				"price_unknown":          {s.testData.coupon.ID}, // skipped with a log, no error
			},
		}, phaseID, priceToLineItem)
		s.Require().NoError(err)
		s.Require().Len(got, 1)
		s.Equal("li_1", lo.FromPtr(got[0].LineItemID))
	})

	s.Run("subscription_coupons_resolve_code_and_price", func() {
		customStart := phaseStart.Add(24 * time.Hour)
		got, err := svc.normalizePhaseCoupons(ctx, dto.SubscriptionPhaseCreateRequest{
			StartDate: phaseStart,
			EndDate:   &phaseEnd,
			SubscriptionCoupons: []dto.SubscriptionCouponInput{
				{CouponCode: "CONFIG10", StartDate: &customStart, PriceID: &s.testData.fixedPrice.ID},
				{CouponCode: ""}, // skipped
				{CouponCode: "CONFIG10", PriceID: lo.ToPtr("price_unknown")}, // subscription-level fallback
			},
		}, phaseID, priceToLineItem)
		s.Require().NoError(err)
		s.Require().Len(got, 2)
		s.Equal(s.testData.coupon.ID, got[0].CouponID)
		s.True(got[0].StartDate.Equal(customStart))
		s.Equal("li_1", lo.FromPtr(got[0].LineItemID))
		s.Nil(got[1].LineItemID)
	})

	s.Run("unknown_coupon_code_returns_error", func() {
		_, err := svc.normalizePhaseCoupons(ctx, dto.SubscriptionPhaseCreateRequest{
			StartDate: phaseStart,
			SubscriptionCoupons: []dto.SubscriptionCouponInput{
				{CouponCode: "DOES_NOT_EXIST"},
			},
		}, phaseID, priceToLineItem)
		s.Error(err)
	})
}

func (s *SubscriptionCreateConfigSuite) TestCreatePhaseExtraLineItems() {
	ctx := s.GetContext()
	svc := s.internalService()
	phase := &subscription.SubscriptionPhase{
		ID:             types.GenerateUUIDWithPrefix("phase"),
		SubscriptionID: s.testData.sub.ID,
		StartDate:      s.testData.now,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	phaseEnd := s.testData.now.Add(10 * 24 * time.Hour)

	s.Run("start_date_before_phase_start_is_rejected", func() {
		_, err := svc.createPhaseExtraLineItems(ctx, s.testData.sub, phase, dto.SubscriptionPhaseCreateRequest{
			StartDate: s.testData.now,
			LineItems: []dto.CreateSubscriptionLineItemRequest{
				{PriceID: s.testData.fixedPrice.ID, StartDate: lo.ToPtr(s.testData.now.Add(-24 * time.Hour))},
			},
		})
		s.Error(err)
	})

	s.Run("start_date_after_phase_end_is_rejected", func() {
		_, err := svc.createPhaseExtraLineItems(ctx, s.testData.sub, phase, dto.SubscriptionPhaseCreateRequest{
			StartDate: s.testData.now,
			EndDate:   &phaseEnd,
			LineItems: []dto.CreateSubscriptionLineItemRequest{
				{PriceID: s.testData.fixedPrice.ID, StartDate: lo.ToPtr(phaseEnd.Add(24 * time.Hour))},
			},
		})
		s.Error(err)
	})

	s.Run("creates_line_item_defaulting_to_phase_start", func() {
		created, err := svc.createPhaseExtraLineItems(ctx, s.testData.sub, phase, dto.SubscriptionPhaseCreateRequest{
			StartDate: s.testData.now,
			EndDate:   &phaseEnd,
			LineItems: []dto.CreateSubscriptionLineItemRequest{
				{PriceID: s.testData.fixedPrice.ID},
			},
		})
		s.Require().NoError(err)
		s.Require().Len(created, 1)
		s.Equal(s.testData.fixedPrice.ID, created[0].PriceID)
		s.Equal(phase.ID, lo.FromPtr(created[0].SubscriptionPhaseID))
		s.True(created[0].StartDate.Equal(s.testData.now))

		// Read back through the repo.
		stored, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, created[0].ID)
		s.Require().NoError(err)
		s.Equal(s.testData.sub.ID, stored.SubscriptionID)
	})
}

func (s *SubscriptionCreateConfigSuite) TestHandleSubscriptionPhases() {
	ctx := s.GetContext()
	svc := s.internalService()

	s.Run("empty_phases_is_noop", func() {
		s.NoError(svc.handleSubscriptionPhases(ctx, s.testData.sub, nil, nil, s.testData.plan, nil))
	})

	s.Run("creates_second_phase_with_line_items_and_coupons", func() {
		phase1Start := s.testData.sub.StartDate
		phase2Start := s.testData.now.Add(30 * 24 * time.Hour)
		phase2End := s.testData.now.Add(60 * 24 * time.Hour)

		phases := []*subscription.SubscriptionPhase{
			{
				ID:             types.GenerateUUIDWithPrefix("phase"),
				SubscriptionID: s.testData.sub.ID,
				StartDate:      phase1Start,
				EndDate:        &phase2Start,
				EnvironmentID:  types.GetEnvironmentID(ctx),
				BaseModel:      types.GetDefaultBaseModel(ctx),
			},
			{
				ID:             types.GenerateUUIDWithPrefix("phase"),
				SubscriptionID: s.testData.sub.ID,
				StartDate:      phase2Start,
				EndDate:        &phase2End,
				EnvironmentID:  types.GetEnvironmentID(ctx),
				BaseModel:      types.GetDefaultBaseModel(ctx),
			},
		}
		phaseRequests := []dto.SubscriptionPhaseCreateRequest{
			{StartDate: phase1Start, EndDate: &phase2Start},
			{
				StartDate: phase2Start,
				EndDate:   &phase2End,
				Coupons:   []string{s.testData.coupon.ID},
			},
		}
		validPrices := []*dto.PriceResponse{{Price: s.testData.fixedPrice}}

		s.Require().NoError(svc.handleSubscriptionPhases(ctx, s.testData.sub, phases, phaseRequests, s.testData.plan, validPrices))

		// Phase 0 is not persisted here (handled inside the create transaction);
		// phase 1 must exist.
		storedPhase, err := s.GetStores().SubscriptionPhaseRepo.Get(ctx, phases[1].ID)
		s.Require().NoError(err)
		s.True(storedPhase.StartDate.Equal(phase2Start))

		_, err = s.GetStores().SubscriptionPhaseRepo.Get(ctx, phases[0].ID)
		s.Error(err, "phase 0 must not be created by handleSubscriptionPhases")

		// Line items for phase 1 were created from plan prices.
		liFilter := types.NewNoLimitSubscriptionLineItemFilter()
		liFilter.SubscriptionIDs = []string{s.testData.sub.ID}
		items, err := s.GetStores().SubscriptionLineItemRepo.List(ctx, liFilter)
		s.Require().NoError(err)

		phaseItems := lo.Filter(items, func(li *subscription.SubscriptionLineItem, _ int) bool {
			return lo.FromPtr(li.SubscriptionPhaseID) == phases[1].ID
		})
		s.Require().Len(phaseItems, 1)
		s.Equal(s.testData.fixedPrice.ID, phaseItems[0].PriceID)
		s.True(phaseItems[0].StartDate.Equal(phase2Start))

		// Phase coupon was applied as a coupon association on the subscription.
		assocFilter := types.NewCouponAssociationFilter()
		assocFilter.SubscriptionIDs = []string{s.testData.sub.ID}
		associations, err := s.GetStores().CouponAssociationRepo.List(ctx, assocFilter)
		s.Require().NoError(err)
		s.Require().NotEmpty(associations)
	})
}

func (s *SubscriptionCreateConfigSuite) TestHandleSubCoupons() {
	ctx := s.GetContext()
	svc := s.internalService()

	countAssociations := func(subID string) int {
		filter := types.NewCouponAssociationFilter()
		filter.SubscriptionIDs = []string{subID}
		associations, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
		s.Require().NoError(err)
		return len(associations)
	}

	newSub := func() *subscription.Subscription {
		sub := &subscription.Subscription{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-15 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-15 * 24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(15 * 24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(-15 * 24 * time.Hour),
			Currency:           "usd",
			BillingCycle:       types.BillingCycleAnniversary,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusActive,
			SubscriptionType:   types.SubscriptionTypeStandalone,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, nil))
		return sub
	}

	s.Run("no_coupons_is_noop", func() {
		sub := newSub()
		s.NoError(svc.handleSubCoupons(ctx, sub, dto.CreateSubscriptionRequest{}, nil))
		s.Equal(0, countAssociations(sub.ID))
	})

	s.Run("deprecated_coupon_ids_create_subscription_level_associations", func() {
		sub := newSub()
		req := dto.CreateSubscriptionRequest{Coupons: []string{s.testData.coupon.ID}}
		s.Require().NoError(svc.handleSubCoupons(ctx, sub, req, nil))
		s.Equal(1, countAssociations(sub.ID))
	})

	s.Run("line_item_coupons_resolve_via_price_map_and_skip_unknown", func() {
		sub := newSub()
		li := &subscription.SubscriptionLineItem{
			ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID: sub.ID,
			CustomerID:     sub.CustomerID,
			EntityID:       s.testData.plan.ID,
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        s.testData.fixedPrice.ID,
			PriceType:      s.testData.fixedPrice.Type,
			Currency:       sub.Currency,
			BillingPeriod:  sub.BillingPeriod,
			Quantity:       decimal.NewFromInt(1),
			StartDate:      sub.StartDate,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

		req := dto.CreateSubscriptionRequest{
			LineItemCoupons: map[string][]string{
				s.testData.fixedPrice.ID: {s.testData.coupon.ID},
				"price_unknown":          {s.testData.coupon.ID}, // skipped
			},
		}
		priceMap := map[string]string{s.testData.fixedPrice.ID: li.ID}
		s.Require().NoError(svc.handleSubCoupons(ctx, sub, req, priceMap))
		s.Equal(1, countAssociations(sub.ID))
	})

	s.Run("subscription_coupons_resolve_code", func() {
		sub := newSub()
		req := dto.CreateSubscriptionRequest{
			SubscriptionCoupons: []dto.SubscriptionCouponInput{
				{CouponCode: "CONFIG10"},
			},
		}
		s.Require().NoError(svc.handleSubCoupons(ctx, sub, req, nil))
		s.Equal(1, countAssociations(sub.ID))
	})

	s.Run("unknown_coupon_code_returns_not_found", func() {
		sub := newSub()
		req := dto.CreateSubscriptionRequest{
			SubscriptionCoupons: []dto.SubscriptionCouponInput{
				{CouponCode: "MISSING_CODE"},
			},
		}
		s.Error(svc.handleSubCoupons(ctx, sub, req, nil))
		s.Equal(0, countAssociations(sub.ID))
	})
}

func (s *SubscriptionCreateConfigSuite) TestHandleCreditGrants() {
	ctx := s.GetContext()
	svc := s.internalService()

	baseGrantReq := func(name string) dto.CreateCreditGrantRequest {
		return dto.CreateCreditGrantRequest{
			Name:           name,
			Scope:          types.CreditGrantScopeSubscription,
			Credits:        decimal.NewFromInt(50),
			Cadence:        types.CreditGrantCadenceOneTime,
			ExpirationType: types.CreditGrantExpiryTypeNever,
		}
	}

	s.Run("empty_requests_is_noop", func() {
		s.NoError(svc.handleCreditGrants(ctx, s.testData.sub, nil))
	})

	s.Run("mismatched_conversion_rates_are_rejected", func() {
		g1 := baseGrantReq("grant_a")
		g1.ConversionRate = lo.ToPtr(decimal.NewFromInt(1))
		g2 := baseGrantReq("grant_b")
		g2.ConversionRate = lo.ToPtr(decimal.NewFromInt(2))

		err := svc.handleCreditGrants(ctx, s.testData.sub, []dto.CreateCreditGrantRequest{g1, g2})
		s.Error(err)
	})

	s.Run("nil_and_set_conversion_rate_mix_is_rejected", func() {
		g1 := baseGrantReq("grant_c")
		g2 := baseGrantReq("grant_d")
		g2.ConversionRate = lo.ToPtr(decimal.NewFromInt(2))

		err := svc.handleCreditGrants(ctx, s.testData.sub, []dto.CreateCreditGrantRequest{g1, g2})
		s.Error(err)
	})

	s.Run("mismatched_topup_conversion_rates_are_rejected", func() {
		g1 := baseGrantReq("grant_e")
		g1.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(1))
		g2 := baseGrantReq("grant_f")
		g2.TopupConversionRate = lo.ToPtr(decimal.NewFromInt(3))

		err := svc.handleCreditGrants(ctx, s.testData.sub, []dto.CreateCreditGrantRequest{g1, g2})
		s.Error(err)
	})

	s.Run("creates_subscription_scoped_grant_anchored_at_start_date", func() {
		err := svc.handleCreditGrants(ctx, s.testData.sub, []dto.CreateCreditGrantRequest{baseGrantReq("grant_ok")})
		s.Require().NoError(err)

		filter := types.NewNoLimitCreditGrantFilter()
		filter.SubscriptionIDs = []string{s.testData.sub.ID}
		grants, gerr := s.GetStores().CreditGrantRepo.List(ctx, filter)
		s.Require().NoError(gerr)
		s.Require().Len(grants, 1)
		s.Equal(types.CreditGrantScopeSubscription, grants[0].Scope)
		s.Equal(s.testData.sub.ID, lo.FromPtr(grants[0].SubscriptionID))
		s.True(grants[0].Credits.Equal(decimal.NewFromInt(50)))
		s.Require().NotNil(grants[0].StartDate)
		s.True(grants[0].StartDate.Equal(s.testData.sub.StartDate))
	})

	s.Run("trial_end_shifts_grant_start_date", func() {
		trialEnd := s.testData.now.Add(5 * 24 * time.Hour)
		sub := &subscription.Subscription{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
			PlanID:             s.testData.plan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-5 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-5 * 24 * time.Hour),
			CurrentPeriodEnd:   trialEnd,
			BillingAnchor:      s.testData.now.Add(-5 * 24 * time.Hour),
			TrialStart:         lo.ToPtr(s.testData.now.Add(-5 * 24 * time.Hour)),
			TrialEnd:           &trialEnd,
			Currency:           "usd",
			BillingCycle:       types.BillingCycleAnniversary,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusTrialing,
			SubscriptionType:   types.SubscriptionTypeStandalone,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, nil))

		s.Require().NoError(svc.handleCreditGrants(ctx, sub, []dto.CreateCreditGrantRequest{baseGrantReq("grant_trial")}))

		filter := types.NewNoLimitCreditGrantFilter()
		filter.SubscriptionIDs = []string{sub.ID}
		grants, gerr := s.GetStores().CreditGrantRepo.List(ctx, filter)
		s.Require().NoError(gerr)
		s.Require().Len(grants, 1)
		s.Require().NotNil(grants[0].StartDate)
		s.True(grants[0].StartDate.Equal(trialEnd), "grant must start at trial end, not subscription start")
	})
}

func (s *SubscriptionCreateConfigSuite) TestHandleEntitlementProration() {
	ctx := s.GetContext()
	svc := s.internalService()

	// Plan has a metered entitlement; the subscription starts mid-period so the
	// prorated limit must be strictly below the original.
	m := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "Proration Meter",
		EventName: "proration_event",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))

	f := &feature.Feature{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_FEATURE),
		Name:      "proration_feature",
		LookupKey: "proration_feature",
		Type:      types.FeatureTypeMetered,
		MeterID:   m.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))

	planEnt := &entitlement.Entitlement{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT),
		EntityType:       types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:         s.testData.plan.ID,
		FeatureID:        f.ID,
		FeatureType:      types.FeatureTypeMetered,
		IsEnabled:        true,
		UsageLimit:       lo.ToPtr(int64(1000)),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, planEnt)
	s.Require().NoError(err)

	periodStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	midPeriodStart := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          midPeriodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingAnchor:      periodStart,
		Currency:           "usd",
		Timezone:           "UTC",
		BillingCycle:       types.BillingCycleCalendar,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, nil))

	s.Require().NoError(svc.handleEntitlementProration(ctx, sub))

	// A subscription-scoped prorated entitlement must exist for the current period.
	filter := types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{sub.ID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION)
	ents, err := s.GetStores().EntitlementRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(ents, 1)

	prorated := ents[0]
	s.Equal(f.ID, prorated.FeatureID)
	s.Require().NotNil(prorated.UsageLimit)
	s.Less(lo.FromPtr(prorated.UsageLimit), int64(1000), "mid-period start must reduce the limit")
	s.Greater(lo.FromPtr(prorated.UsageLimit), int64(0))
	s.Require().NotNil(prorated.StartDate)
	s.True(prorated.StartDate.Equal(periodStart))
	s.Require().NotNil(prorated.EndDate)
	s.True(prorated.EndDate.Equal(periodEnd))
}
