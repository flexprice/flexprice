package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PriceMoreServiceSuite fills coverage gaps in price.go: entity validation,
// list-by-entity helpers, expansions, update/termination branches, custom
// price unit conversion, and tier calculation edge cases.
type PriceMoreServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PriceService
	testData struct {
		plan  *plan.Plan
		meter *meter.Meter
		now   time.Time
	}
}

func TestPriceMoreService(t *testing.T) {
	suite.Run(t, new(PriceMoreServiceSuite))
}

func (s *PriceMoreServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPriceService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *PriceMoreServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	s.testData.plan = &plan.Plan{
		ID:        "plan_pm",
		Name:      "Price More Plan",
		LookupKey: "price-more-plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	s.testData.meter = &meter.Meter{
		ID:        "meter_pm",
		Name:      "Price More Meter",
		EventName: "pm_event",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meter))
}

// fixedPlanPriceRequest returns a valid FLAT_FEE fixed price request for the given entity.
func (s *PriceMoreServiceSuite) fixedPriceRequest(entityType types.PriceEntityType, entityID string) dto.CreatePriceRequest {
	amount := decimal.NewFromInt(10)
	return dto.CreatePriceRequest{
		Amount:             &amount,
		Currency:           "usd",
		EntityType:         entityType,
		EntityID:           entityID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
	}
}

func (s *PriceMoreServiceSuite) TestCreatePriceEntityValidation() {
	// Fixtures for the happy entity paths
	testAddon := &addon.Addon{
		ID:        "addon_pm",
		Name:      "PM Addon",
		LookupKey: "pm-addon",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().AddonRepo.Create(s.GetContext(), testAddon))

	testSub := &subscription.Subscription{
		ID:                 "sub_pm",
		PlanID:             s.testData.plan.ID,
		CustomerID:         "cust_pm",
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		StartDate:          s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(29 * 24 * time.Hour),
		BillingAnchor:      s.testData.now.Add(-24 * time.Hour),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), testSub))

	testCases := []struct {
		name          string
		entityType    types.PriceEntityType
		entityID      string
		expectedError bool
		errCheck      func(err error) bool
	}{
		{name: "plan_exists_creates_price", entityType: types.PRICE_ENTITY_TYPE_PLAN, entityID: s.testData.plan.ID, expectedError: false},
		{name: "plan_not_found_returns_not_found", entityType: types.PRICE_ENTITY_TYPE_PLAN, entityID: "plan_nope", expectedError: true, errCheck: ierr.IsNotFound},
		{name: "addon_exists_creates_price", entityType: types.PRICE_ENTITY_TYPE_ADDON, entityID: testAddon.ID, expectedError: false},
		{name: "addon_not_found_returns_not_found", entityType: types.PRICE_ENTITY_TYPE_ADDON, entityID: "addon_nope", expectedError: true, errCheck: ierr.IsNotFound},
		{name: "subscription_exists_creates_price", entityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, entityID: testSub.ID, expectedError: false},
		{name: "subscription_not_found_returns_not_found", entityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, entityID: "sub_nope", expectedError: true, errCheck: ierr.IsNotFound},
		{name: "unsupported_entity_type_returns_validation_error", entityType: types.PRICE_ENTITY_TYPE_PRICE, entityID: "price_nope", expectedError: true, errCheck: ierr.IsValidation},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreatePrice(s.GetContext(), s.fixedPriceRequest(tc.entityType, tc.entityID))
			if tc.expectedError {
				s.Error(err)
				if tc.errCheck != nil {
					s.True(tc.errCheck(err), "unexpected error kind: %v", err)
				}
				return
			}
			s.NoError(err)
			s.NotNil(resp)

			// Read back and verify entity linkage
			stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
			s.NoError(err)
			s.Equal(tc.entityType, stored.EntityType)
			s.Equal(tc.entityID, stored.EntityID)
			s.True(stored.Amount.Equal(decimal.NewFromInt(10)))
		})
	}
}

func (s *PriceMoreServiceSuite) TestCreatePriceAddonDisplayNameDefaultsToAddonName() {
	testAddon := &addon.Addon{
		ID:        "addon_pm_dn",
		Name:      "Display Addon",
		LookupKey: "pm-addon-dn",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().AddonRepo.Create(s.GetContext(), testAddon))

	resp, err := s.service.CreatePrice(s.GetContext(), s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_ADDON, testAddon.ID))
	s.NoError(err)
	s.Equal("Display Addon", resp.Price.DisplayName)
}

func (s *PriceMoreServiceSuite) TestCreatePriceSkipValidationDefaultsDisplayNames() {
	testCases := []struct {
		name                string
		priceType           types.PriceType
		expectedDisplayName string
	}{
		{name: "fixed_price_defaults_to_recurring", priceType: types.PRICE_TYPE_FIXED, expectedDisplayName: "Recurring"},
		{name: "usage_price_defaults_to_usage", priceType: types.PRICE_TYPE_USAGE, expectedDisplayName: "Usage"},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			req := s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, "plan_ignored_by_skip")
			req.SkipEntityValidation = true
			req.Type = tc.priceType
			if tc.priceType == types.PRICE_TYPE_USAGE {
				req.MeterID = s.testData.meter.ID
				req.InvoiceCadence = types.InvoiceCadenceArrear
			}
			resp, err := s.service.CreatePrice(s.GetContext(), req)
			s.NoError(err)
			s.Equal(tc.expectedDisplayName, resp.Price.DisplayName)
		})
	}
}

func (s *PriceMoreServiceSuite) TestCreatePriceWithGroup() {
	priceGroup := &group.Group{
		ID:         "grp_pm",
		Name:       "PM Group",
		EntityType: types.GroupEntityTypePrice,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().GroupRepo.Create(s.GetContext(), priceGroup))

	s.Run("valid_group_is_assigned", func() {
		req := s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID)
		req.GroupID = priceGroup.ID
		resp, err := s.service.CreatePrice(s.GetContext(), req)
		s.NoError(err)

		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
		s.NoError(err)
		s.Equal(priceGroup.ID, stored.GroupID)
	})

	s.Run("unknown_group_returns_validation_error", func() {
		req := s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID)
		req.GroupID = "grp_missing"
		_, err := s.service.CreatePrice(s.GetContext(), req)
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

func (s *PriceMoreServiceSuite) TestGetPricesBySubscriptionID() {
	s.Run("empty_subscription_id_returns_validation_error", func() {
		_, err := s.service.GetPricesBySubscriptionID(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("returns_subscription_scoped_prices", func() {
		subPrice := &price.Price{
			ID:                 "price_pm_sub",
			Amount:             decimal.NewFromInt(7),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:           "sub_pm_scoped",
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), subPrice))

		resp, err := s.service.GetPricesBySubscriptionID(s.GetContext(), "sub_pm_scoped")
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal(subPrice.ID, resp.Items[0].Price.ID)
		s.Equal(types.PRICE_ENTITY_TYPE_SUBSCRIPTION, resp.Items[0].Price.EntityType)
	})
}

// TestGetPricesExpiredPriceFiltering verifies AllowExpiredPrices end to end:
// by default GetPrices must exclude prices whose EndDate has passed (mirrors
// the real repo's EndDate IS NULL OR EndDate > now predicate), and include
// them when AllowExpiredPrices is set.
func (s *PriceMoreServiceSuite) TestGetPricesExpiredPriceFiltering() {
	newPlanPrice := func(id string, endDate *time.Time) *price.Price {
		return &price.Price{
			ID:                 id,
			Amount:             decimal.NewFromInt(5),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           s.testData.plan.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			EndDate:            endDate,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
	}

	pastEnd := s.testData.now.Add(-24 * time.Hour)
	futureEnd := s.testData.now.Add(24 * time.Hour)
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), newPlanPrice("price_pm_active", nil)))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), newPlanPrice("price_pm_future_end", &futureEnd)))
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), newPlanPrice("price_pm_expired", &pastEnd)))

	testCases := []struct {
		name         string
		allowExpired bool
		expectedIDs  []string
	}{
		{
			name:         "default_filter_excludes_expired_prices",
			allowExpired: false,
			expectedIDs:  []string{"price_pm_active", "price_pm_future_end"},
		},
		{
			name:         "allow_expired_prices_includes_expired_prices",
			allowExpired: true,
			expectedIDs:  []string{"price_pm_active", "price_pm_future_end", "price_pm_expired"},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			filter := types.NewNoLimitPriceFilter().
				WithEntityIDs([]string{s.testData.plan.ID}).
				WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
				WithAllowExpiredPrices(tc.allowExpired)

			resp, err := s.service.GetPrices(s.GetContext(), filter)
			s.NoError(err)

			gotIDs := lo.Map(resp.Items, func(p *dto.PriceResponse, _ int) string { return p.Price.ID })
			s.ElementsMatch(tc.expectedIDs, gotIDs)
			s.Equal(len(tc.expectedIDs), resp.Pagination.Total)
		})
	}
}

func (s *PriceMoreServiceSuite) TestGetPricesByCostsheetID() {
	s.Run("empty_costsheet_id_returns_validation_error", func() {
		_, err := s.service.GetPricesByCostsheetID(s.GetContext(), "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("returns_costsheet_scoped_prices", func() {
		csPrice := &price.Price{
			ID:                 "price_pm_cs",
			Amount:             decimal.NewFromInt(3),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_COSTSHEET,
			EntityID:           "cs_pm_1",
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), csPrice))

		resp, err := s.service.GetPricesByCostsheetID(s.GetContext(), "cs_pm_1")
		s.NoError(err)
		s.Len(resp.Items, 1)
		s.Equal(csPrice.ID, resp.Items[0].Price.ID)
	})
}

func (s *PriceMoreServiceSuite) TestGetPriceWithExpandedRelations() {
	priceGroup := &group.Group{
		ID:         "grp_pm_get",
		Name:       "Get Group",
		EntityType: types.GroupEntityTypePrice,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().GroupRepo.Create(s.GetContext(), priceGroup))

	pu := &priceunit.PriceUnit{
		ID:             "pu_pm_get",
		Name:           "Get Unit",
		Code:           "gtu",
		Symbol:         "G",
		BaseCurrency:   "usd",
		ConversionRate: decimal.NewFromInt(2),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), pu)
	s.NoError(err)

	p := &price.Price{
		ID:                 "price_pm_get",
		Amount:             decimal.NewFromInt(5),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            s.testData.meter.ID,
		GroupID:            priceGroup.ID,
		PriceUnitType:      types.PRICE_UNIT_TYPE_CUSTOM,
		PriceUnitID:        lo.ToPtr(pu.ID),
		PriceUnit:          lo.ToPtr(pu.Code),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))

	resp, err := s.service.GetPrice(s.GetContext(), p.ID)
	s.NoError(err)
	s.NotNil(resp.Meter)
	s.Equal(s.testData.meter.ID, resp.Meter.ID)
	s.NotNil(resp.Group)
	s.Equal(priceGroup.ID, resp.Group.ID)
	s.NotNil(resp.PricingUnit)
	s.Equal(pu.ID, resp.PricingUnit.PriceUnit.ID)
	s.Equal(types.PRICE_ENTITY_TYPE_PLAN, resp.EntityType)
	s.Equal(s.testData.plan.ID, resp.EntityID)
}

func (s *PriceMoreServiceSuite) TestGetPriceToleratesMissingGroup() {
	p := &price.Price{
		ID:                 "price_pm_badgrp",
		Amount:             decimal.NewFromInt(5),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		GroupID:            "grp_gone",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))

	// Group fetch failure must not fail the request
	resp, err := s.service.GetPrice(s.GetContext(), p.ID)
	s.NoError(err)
	s.Nil(resp.Group)
	s.Equal(p.ID, resp.Price.ID)
}

func (s *PriceMoreServiceSuite) TestGetPricesWithExpansions() {
	testAddon := &addon.Addon{
		ID:        "addon_pm_list",
		Name:      "List Addon",
		LookupKey: "pm-addon-list",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().AddonRepo.Create(s.GetContext(), testAddon))

	priceGroup := &group.Group{
		ID:         "grp_pm_list",
		Name:       "List Group",
		EntityType: types.GroupEntityTypePrice,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().GroupRepo.Create(s.GetContext(), priceGroup))

	pu := &priceunit.PriceUnit{
		ID:             "pu_pm_list",
		Name:           "List Unit",
		Code:           "plu",
		Symbol:         "P",
		BaseCurrency:   "usd",
		ConversionRate: decimal.NewFromInt(3),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), pu)
	s.NoError(err)

	planPrice := &price.Price{
		ID:                 "price_pm_list_plan",
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            s.testData.meter.ID,
		GroupID:            priceGroup.ID,
		PriceUnitType:      types.PRICE_UNIT_TYPE_CUSTOM,
		PriceUnitID:        lo.ToPtr(pu.ID),
		PriceUnit:          lo.ToPtr(pu.Code),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), planPrice))

	addonPrice := &price.Price{
		ID:                 "price_pm_list_addon",
		Amount:             decimal.NewFromInt(2),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           testAddon.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), addonPrice))

	filter := types.NewNoLimitPriceFilter().
		WithStatus(types.StatusPublished).
		WithExpand("meters,plan,addons,groups,priceunit")

	resp, err := s.service.GetPrices(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 2)

	itemsByID := lo.KeyBy(resp.Items, func(p *dto.PriceResponse) string { return p.Price.ID })

	planItem := itemsByID[planPrice.ID]
	s.NotNil(planItem.Meter)
	s.Equal(s.testData.meter.ID, planItem.Meter.ID)
	s.NotNil(planItem.Plan)
	s.Equal(s.testData.plan.ID, planItem.Plan.Plan.ID)
	s.NotNil(planItem.Group)
	s.Equal(priceGroup.ID, planItem.Group.ID)
	s.NotNil(planItem.PricingUnit)
	s.Equal(pu.ID, planItem.PricingUnit.PriceUnit.ID)

	addonItem := itemsByID[addonPrice.ID]
	s.NotNil(addonItem.Addon)
	s.Equal(testAddon.ID, addonItem.Addon.Addon.ID)
	s.Nil(addonItem.Meter)
}

func (s *PriceMoreServiceSuite) TestGetPricesRejectsInvalidExpand() {
	filter := types.NewNoLimitPriceFilter().WithExpand("subscription")
	_, err := s.service.GetPrices(s.GetContext(), filter)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

// createStoredFixedPrice inserts a published fixed plan price directly in the repo.
func (s *PriceMoreServiceSuite) createStoredFixedPrice(id string, amount decimal.Decimal, startDate *time.Time) *price.Price {
	p := &price.Price{
		ID:                 id,
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		StartDate:          startDate,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), p))
	return p
}

func (s *PriceMoreServiceSuite) TestUpdatePriceUnitTypeGuards() {
	fiatPrice := s.createStoredFixedPrice("price_pm_fiat", decimal.NewFromInt(10), nil)

	customPrice := &price.Price{
		ID:                 "price_pm_custom",
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_CUSTOM,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), customPrice))

	testCases := []struct {
		name    string
		priceID string
		req     dto.UpdatePriceRequest
	}{
		{
			name:    "fiat_price_rejects_price_unit_amount",
			priceID: fiatPrice.ID,
			req:     dto.UpdatePriceRequest{PriceUnitAmount: lo.ToPtr(decimal.NewFromInt(5))},
		},
		{
			name:    "fiat_price_rejects_price_unit_tiers",
			priceID: fiatPrice.ID,
			req: dto.UpdatePriceRequest{PriceUnitTiers: []dto.CreatePriceTier{
				{UnitAmount: decimal.NewFromInt(1)},
			}},
		},
		{
			name:    "custom_price_rejects_amount",
			priceID: customPrice.ID,
			req:     dto.UpdatePriceRequest{Amount: lo.ToPtr(decimal.NewFromInt(5))},
		},
		{
			name:    "custom_price_rejects_tiers",
			priceID: customPrice.ID,
			req: dto.UpdatePriceRequest{Tiers: []dto.CreatePriceTier{
				{UnitAmount: decimal.NewFromInt(1)},
			}},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_, err := s.service.UpdatePrice(s.GetContext(), tc.priceID, tc.req)
			s.Error(err)
			s.True(ierr.IsValidation(err))
		})
	}
}

func (s *PriceMoreServiceSuite) TestUpdatePriceTerminationGuards() {
	s.Run("terminated_price_rejects_critical_update", func() {
		p := s.createStoredFixedPrice("price_pm_term", decimal.NewFromInt(10), nil)
		endDate := s.testData.now.Add(-time.Hour)
		p.EndDate = &endDate
		s.NoError(s.GetStores().PriceRepo.Update(s.GetContext(), p, false))

		_, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			Amount: lo.ToPtr(decimal.NewFromInt(20)),
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("effective_from_before_price_start_date_returns_error", func() {
		startDate := s.testData.now.Add(-24 * time.Hour)
		p := s.createStoredFixedPrice("price_pm_early", decimal.NewFromInt(10), &startDate)

		before := startDate.Add(-time.Hour)
		_, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			Amount:        lo.ToPtr(decimal.NewFromInt(20)),
			EffectiveFrom: &before,
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}

func (s *PriceMoreServiceSuite) TestUpdatePriceCriticalFieldsTerminatesAndRecreates() {
	startDate := s.testData.now.Add(-30 * 24 * time.Hour)
	old := s.createStoredFixedPrice("price_pm_crit", decimal.NewFromInt(10), &startDate)

	resp, err := s.service.UpdatePrice(s.GetContext(), old.ID, dto.UpdatePriceRequest{
		Amount: lo.ToPtr(decimal.NewFromInt(25)),
	})
	s.NoError(err)
	s.NotEqual(old.ID, resp.Price.ID, "critical update must create a new price")
	s.True(resp.Price.Amount.Equal(decimal.NewFromInt(25)))

	// Old price terminated, sequence bumped
	storedOld, err := s.GetStores().PriceRepo.Get(s.GetContext(), old.ID)
	s.NoError(err)
	s.NotNil(storedOld.EndDate)

	// New price starts exactly when the old one ends
	storedNew, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
	s.NoError(err)
	s.True(storedNew.StartDate.Equal(lo.FromPtr(storedOld.EndDate)))
	s.True(storedNew.Amount.Equal(decimal.NewFromInt(25)))
}

func (s *PriceMoreServiceSuite) TestUpdatePriceSimpleFieldUpdates() {
	p := s.createStoredFixedPrice("price_pm_simple", decimal.NewFromInt(10), nil)

	priceGroup := &group.Group{
		ID:         "grp_pm_upd",
		Name:       "Update Group",
		EntityType: types.GroupEntityTypePrice,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().GroupRepo.Create(s.GetContext(), priceGroup))

	s.Run("non_critical_fields_update_in_place", func() {
		resp, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			LookupKey:   "pm-simple-updated",
			Description: "updated description",
			DisplayName: "Updated Display",
			Metadata:    map[string]string{"k": "v"},
			GroupID:     lo.ToPtr(priceGroup.ID),
		})
		s.NoError(err)
		s.Equal(p.ID, resp.Price.ID, "simple update must not create a new price")

		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.Equal("pm-simple-updated", stored.LookupKey)
		s.Equal("updated description", stored.Description)
		s.Equal("Updated Display", stored.DisplayName)
		s.Equal("v", stored.Metadata["k"])
		s.Equal(priceGroup.ID, stored.GroupID)
	})

	s.Run("clearing_group_with_empty_string", func() {
		_, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			GroupID: lo.ToPtr(""),
		})
		s.NoError(err)
		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.Empty(stored.GroupID)
	})

	s.Run("invalid_group_on_simple_update_returns_error", func() {
		_, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			GroupID: lo.ToPtr("grp_gone"),
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("effective_from_without_critical_field_returns_validation_error", func() {
		// The DTO requires effective_from to be paired with a critical field
		endDate := s.testData.now.Add(24 * time.Hour)
		_, err := s.service.UpdatePrice(s.GetContext(), p.ID, dto.UpdatePriceRequest{
			EffectiveFrom: &endDate,
		})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("effective_from_with_critical_field_terminates_at_given_date", func() {
		startDate := s.testData.now.Add(-30 * 24 * time.Hour)
		terminable := s.createStoredFixedPrice("price_pm_eff", decimal.NewFromInt(10), &startDate)

		effectiveFrom := s.testData.now.Add(24 * time.Hour)
		resp, err := s.service.UpdatePrice(s.GetContext(), terminable.ID, dto.UpdatePriceRequest{
			Amount:        lo.ToPtr(decimal.NewFromInt(15)),
			EffectiveFrom: &effectiveFrom,
		})
		s.NoError(err)
		s.NotEqual(terminable.ID, resp.Price.ID)

		storedOld, err := s.GetStores().PriceRepo.Get(s.GetContext(), terminable.ID)
		s.NoError(err)
		s.NotNil(storedOld.EndDate)
		s.True(storedOld.EndDate.Equal(effectiveFrom), "old price must end at effective_from")

		storedNew, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
		s.NoError(err)
		s.True(storedNew.StartDate.Equal(effectiveFrom), "new price must start at effective_from")
		s.True(storedNew.Amount.Equal(decimal.NewFromInt(15)))
	})
}

func (s *PriceMoreServiceSuite) TestCreatePriceWithCustomPriceUnit() {
	published := &priceunit.PriceUnit{
		ID:             "pu_pm_conv",
		Name:           "Conv Unit",
		Code:           "cnv",
		Symbol:         "C",
		BaseCurrency:   "usd",
		ConversionRate: decimal.NewFromFloat(0.5),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), published)
	s.NoError(err)

	archived := &priceunit.PriceUnit{
		ID:             "pu_pm_arch",
		Name:           "Archived Unit",
		Code:           "arc",
		Symbol:         "A",
		BaseCurrency:   "usd",
		ConversionRate: decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	archived.Status = types.StatusArchived
	_, err = s.GetStores().PriceUnitRepo.Create(s.GetContext(), archived)
	s.NoError(err)

	inrUnit := &priceunit.PriceUnit{
		ID:             "pu_pm_inr",
		Name:           "INR Unit",
		Code:           "inr",
		Symbol:         "₹",
		BaseCurrency:   "inr",
		ConversionRate: decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().PriceUnitRepo.Create(s.GetContext(), inrUnit)
	s.NoError(err)

	customReq := func(code string) dto.CreatePriceRequest {
		req := s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID)
		req.Amount = nil
		req.PriceUnitType = types.PRICE_UNIT_TYPE_CUSTOM
		req.PriceUnitConfig = &dto.PriceUnitConfig{
			Amount:    lo.ToPtr(decimal.NewFromInt(100)),
			PriceUnit: code,
		}
		return req
	}

	s.Run("flat_fee_amount_is_converted_to_fiat", func() {
		resp, err := s.service.CreatePrice(s.GetContext(), customReq("cnv"))
		s.NoError(err)

		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
		s.NoError(err)
		// 100 units * 0.5 = 50 usd
		s.True(stored.Amount.Equal(decimal.NewFromInt(50)), "expected 50, got %s", stored.Amount)
		s.Equal(published.ID, lo.FromPtr(stored.PriceUnitID))
		s.True(lo.FromPtr(stored.ConversionRate).Equal(decimal.NewFromFloat(0.5)))
	})

	s.Run("tiered_price_unit_tiers_are_converted", func() {
		req := s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID)
		req.Amount = nil
		req.PriceUnitType = types.PRICE_UNIT_TYPE_CUSTOM
		req.BillingModel = types.BILLING_MODEL_TIERED
		req.TierMode = types.BILLING_TIER_SLAB
		upTo := uint64(100)
		req.PriceUnitConfig = &dto.PriceUnitConfig{
			PriceUnit: "cnv",
			PriceUnitTiers: []dto.CreatePriceTier{
				{UpTo: &upTo, UnitAmount: decimal.NewFromInt(10), FlatAmount: lo.ToPtr(decimal.NewFromInt(4))},
				{UpTo: nil, UnitAmount: decimal.NewFromInt(2)},
			},
		}

		resp, err := s.service.CreatePrice(s.GetContext(), req)
		s.NoError(err)

		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), resp.Price.ID)
		s.NoError(err)
		s.Len(stored.Tiers, 2)
		// unit amounts converted at 0.5 rate
		s.True(stored.Tiers[0].UnitAmount.Equal(decimal.NewFromInt(5)))
		s.True(lo.FromPtr(stored.Tiers[0].FlatAmount).Equal(decimal.NewFromInt(2)))
		s.True(stored.Tiers[1].UnitAmount.Equal(decimal.NewFromInt(1)))
	})

	s.Run("unpublished_price_unit_returns_validation_error", func() {
		_, err := s.service.CreatePrice(s.GetContext(), customReq("arc"))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("currency_mismatch_returns_validation_error", func() {
		// price is usd but the unit's base currency is inr
		_, err := s.service.CreatePrice(s.GetContext(), customReq("inr"))
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_price_unit_code_returns_not_found", func() {
		_, err := s.service.CreatePrice(s.GetContext(), customReq("zzz"))
		s.Error(err)
	})
}

func (s *PriceMoreServiceSuite) TestCreateBulkPricePlanSyncPathWithRoles() {
	// Roles on the context exercise the role-propagation branch of the
	// post-create sync context construction.
	ctx := context.WithValue(s.GetContext(), types.CtxRoles, []string{"admin"})

	req := dto.CreateBulkPriceRequest{
		Items: []dto.CreatePriceRequest{
			s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID),
			s.fixedPriceRequest(types.PRICE_ENTITY_TYPE_PLAN, s.testData.plan.ID),
		},
	}

	resp, err := s.service.CreateBulkPrice(ctx, req)
	s.NoError(err)
	s.Len(resp.Items, 2)

	// Read back: both prices exist on the plan
	stored, err := s.GetStores().PriceRepo.List(s.GetContext(), types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{s.testData.plan.ID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN))
	s.NoError(err)
	s.Len(stored, 2)
}

// ---- calculation edge cases ------------------------------------------------

func (s *PriceMoreServiceSuite) newCalcPrice(model types.BillingModel, amount decimal.Decimal) *price.Price {
	return &price.Price{
		ID:           "price_calc",
		Amount:       amount,
		Currency:     "usd",
		BillingModel: model,
	}
}

func (s *PriceMoreServiceSuite) TestCalculateCostSheetPrice() {
	p := s.newCalcPrice(types.BILLING_MODEL_FLAT_FEE, decimal.NewFromInt(2))
	got := s.service.CalculateCostSheetPrice(s.GetContext(), p, decimal.NewFromInt(5))
	s.True(got.Equal(decimal.NewFromInt(10)), "expected 10, got %s", got)

	// Zero quantity short-circuits to zero
	zero := s.service.CalculateCostSheetPrice(s.GetContext(), p, decimal.Zero)
	s.True(zero.Equal(decimal.Zero))
}

func (s *PriceMoreServiceSuite) TestCalculateCostPackageEdgeCases() {
	testCases := []struct {
		name     string
		divideBy int
		round    types.RoundType
		quantity decimal.Decimal
		expected decimal.Decimal
	}{
		{
			name:     "package_divide_by_zero_returns_zero",
			divideBy: 0,
			quantity: decimal.NewFromInt(10),
			expected: decimal.Zero,
		},
		{
			name:     "package_rounds_up_by_default",
			divideBy: 100,
			quantity: decimal.NewFromInt(150),
			expected: decimal.NewFromInt(20), // 2 packages * 10
		},
		{
			name:     "package_round_down_floors_partial_packages",
			divideBy: 100,
			round:    types.ROUND_DOWN,
			quantity: decimal.NewFromInt(150),
			expected: decimal.NewFromInt(10), // 1 package * 10
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			p := s.newCalcPrice(types.BILLING_MODEL_PACKAGE, decimal.NewFromInt(10))
			p.TransformQuantity = price.JSONBTransformQuantity{DivideBy: tc.divideBy, Round: tc.round}
			got := s.service.CalculateCost(s.GetContext(), p, tc.quantity)
			s.True(got.Equal(tc.expected), "expected %s, got %s", tc.expected, got)
		})
	}
}

func (s *PriceMoreServiceSuite) TestCalculateTieredCostInvalidModeAndNoTiers() {
	s.Run("no_tiers_returns_zero", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_TIERED, decimal.Zero)
		p.TierMode = types.BILLING_TIER_VOLUME
		got := s.service.CalculateCost(s.GetContext(), p, decimal.NewFromInt(10))
		s.True(got.Equal(decimal.Zero))
	})

	s.Run("invalid_tier_mode_returns_zero", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_TIERED, decimal.Zero)
		p.TierMode = types.BillingTier("BOGUS")
		p.Tiers = []price.PriceTier{{UnitAmount: decimal.NewFromInt(1)}}
		got := s.service.CalculateCost(s.GetContext(), p, decimal.NewFromInt(10))
		s.True(got.Equal(decimal.Zero))
	})
}

func (s *PriceMoreServiceSuite) TestCalculateCostWithBreakupEdgeCases() {
	s.Run("package_zero_quantity_keeps_tier_unit_amount", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_PACKAGE, decimal.NewFromInt(10))
		p.TransformQuantity = price.JSONBTransformQuantity{DivideBy: 100}
		got := s.service.CalculateCostWithBreakup(s.GetContext(), p, decimal.Zero, true)
		s.True(got.FinalCost.Equal(decimal.Zero))
		// 10 per 100-unit package = 0.1 per unit
		s.True(got.TierUnitAmount.Equal(decimal.NewFromFloat(0.1)), "got %s", got.TierUnitAmount)
	})

	s.Run("package_divide_by_zero_returns_empty_breakup", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_PACKAGE, decimal.NewFromInt(10))
		p.TransformQuantity = price.JSONBTransformQuantity{DivideBy: 0}
		got := s.service.CalculateCostWithBreakup(s.GetContext(), p, decimal.NewFromInt(10), true)
		s.True(got.FinalCost.Equal(decimal.Zero))
		s.Equal(-1, got.SelectedTierIndex)
	})

	s.Run("package_round_down_breakup", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_PACKAGE, decimal.NewFromInt(10))
		p.TransformQuantity = price.JSONBTransformQuantity{DivideBy: 100, Round: types.ROUND_DOWN}
		got := s.service.CalculateCostWithBreakup(s.GetContext(), p, decimal.NewFromInt(250), true)
		s.True(got.FinalCost.Equal(decimal.NewFromInt(20)), "got %s", got.FinalCost)
	})

	s.Run("tiered_breakup_with_no_tiers_returns_empty", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_TIERED, decimal.Zero)
		p.TierMode = types.BILLING_TIER_VOLUME
		got := s.service.CalculateCostWithBreakup(s.GetContext(), p, decimal.NewFromInt(10), false)
		s.True(got.FinalCost.Equal(decimal.Zero))
		s.Equal(-1, got.SelectedTierIndex)
	})

	s.Run("tiered_breakup_invalid_mode_returns_empty", func() {
		p := s.newCalcPrice(types.BILLING_MODEL_TIERED, decimal.Zero)
		p.TierMode = types.BillingTier("BOGUS")
		p.Tiers = []price.PriceTier{{UnitAmount: decimal.NewFromInt(1)}}
		got := s.service.CalculateCostWithBreakup(s.GetContext(), p, decimal.NewFromInt(10), false)
		s.True(got.FinalCost.Equal(decimal.Zero))
		s.Equal(-1, got.SelectedTierIndex)
	})
}

func (s *PriceMoreServiceSuite) TestCalculateCostFromUsageResultsNonTiered() {
	p := s.newCalcPrice(types.BILLING_MODEL_FLAT_FEE, decimal.NewFromInt(2))
	results := []events.UsageResult{
		{Value: decimal.NewFromInt(3)},
		{Value: decimal.NewFromInt(4)},
	}
	got := s.service.CalculateCostFromUsageResults(s.GetContext(), p, results)
	// (3 + 4) * 2 = 14
	s.True(got.Equal(decimal.NewFromInt(14)), "expected 14, got %s", got)
}

func (s *PriceMoreServiceSuite) TestDeletePriceEndDateValidation() {
	s.Run("end_date_before_start_date_returns_validation_error", func() {
		startDate := s.testData.now.Add(-time.Hour)
		p := s.createStoredFixedPrice("price_pm_del_guard", decimal.NewFromInt(10), &startDate)

		badEnd := startDate.Add(-24 * time.Hour)
		err := s.service.DeletePrice(s.GetContext(), p.ID, dto.DeletePriceRequest{EndDate: &badEnd})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("explicit_end_date_is_persisted", func() {
		startDate := s.testData.now.Add(-48 * time.Hour)
		p := s.createStoredFixedPrice("price_pm_del_ok", decimal.NewFromInt(10), &startDate)

		endDate := s.testData.now.Add(-time.Hour)
		err := s.service.DeletePrice(s.GetContext(), p.ID, dto.DeletePriceRequest{EndDate: &endDate})
		s.NoError(err)

		stored, err := s.GetStores().PriceRepo.Get(s.GetContext(), p.ID)
		s.NoError(err)
		s.NotNil(stored.EndDate)
		s.True(stored.EndDate.Equal(endDate.UTC()))

		// Idempotency guard: deleting again fails cleanly
		err = s.service.DeletePrice(s.GetContext(), p.ID, dto.DeletePriceRequest{})
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})
}
