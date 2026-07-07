package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PlanListServiceSuite covers GetPlans expansion branches (prices, meters,
// price units, entitlements, features, credit grants).
type PlanListServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PlanService
	testData struct {
		plan      *plan.Plan
		meter     *meter.Meter
		priceUnit *priceunit.PriceUnit
	}
}

func TestPlanListService(t *testing.T) {
	suite.Run(t, new(PlanListServiceSuite))
}

func (s *PlanListServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPlanService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *PlanListServiceSuite) setupTestData() {
	s.testData.plan = &plan.Plan{
		ID:        "plan_list_1",
		Name:      "List Plan",
		LookupKey: "list-plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))

	s.testData.meter = &meter.Meter{
		ID:        "meter_list",
		Name:      "List Meter",
		EventName: "list_event",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(s.GetContext(), s.testData.meter))

	s.testData.priceUnit = &priceunit.PriceUnit{
		ID:             "pu_list",
		Name:           "List Unit",
		Code:           "lst",
		Symbol:         "L",
		BaseCurrency:   "usd",
		ConversionRate: decimal.NewFromFloat(0.5),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().PriceUnitRepo.Create(s.GetContext(), s.testData.priceUnit)
	s.NoError(err)

	// Usage price with a meter (FIAT)
	usagePrice := &price.Price{
		ID:                 "price_list_usage",
		Amount:             decimal.NewFromFloat(0.02),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            s.testData.meter.ID,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), usagePrice))

	// Fixed price with a CUSTOM price unit
	customPrice := &price.Price{
		ID:                 "price_list_custom",
		Amount:             decimal.NewFromInt(10),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		PriceUnitType:      types.PRICE_UNIT_TYPE_CUSTOM,
		PriceUnitID:        lo.ToPtr(s.testData.priceUnit.ID),
		PriceUnit:          lo.ToPtr(s.testData.priceUnit.Code),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), customPrice))

	feat := &feature.Feature{
		ID:        "feat_list",
		Name:      "List Feature",
		Type:      types.FeatureTypeBoolean,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), feat))

	ent := &entitlement.Entitlement{
		ID:          "ent_list",
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    s.testData.plan.ID,
		FeatureID:   feat.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)

	grant := &creditgrant.CreditGrant{
		ID:             "cg_list",
		Name:           "List Grant",
		Scope:          types.CreditGrantScopePlan,
		PlanID:         lo.ToPtr(s.testData.plan.ID),
		Credits:        decimal.NewFromInt(50),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().CreditGrantRepo.Create(s.GetContext(), grant)
	s.NoError(err)
}

func (s *PlanListServiceSuite) TestGetPlansWithNilFilterUsesDefaults() {
	resp, err := s.service.GetPlans(s.GetContext(), nil)
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Equal(s.testData.plan.ID, resp.Items[0].Plan.ID)
	s.Equal(1, resp.Pagination.Total)
	// No expansion requested → nothing attached
	s.Nil(resp.Items[0].Prices)
	s.Nil(resp.Items[0].Entitlements)
	s.Nil(resp.Items[0].CreditGrants)
}

func (s *PlanListServiceSuite) TestGetPlansFilterByPlanIDs() {
	secondPlan := &plan.Plan{
		ID:        "plan_list_2",
		Name:      "Second List Plan",
		LookupKey: "second-list-plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), secondPlan))

	testCases := []struct {
		name        string
		planIDs     []string
		expectedIDs []string
	}{
		{
			name:        "no_plan_ids_returns_all_plans",
			planIDs:     nil,
			expectedIDs: []string{s.testData.plan.ID, secondPlan.ID},
		},
		{
			name:        "single_plan_id_returns_only_that_plan",
			planIDs:     []string{secondPlan.ID},
			expectedIDs: []string{secondPlan.ID},
		},
		{
			name:        "multiple_plan_ids_return_matching_plans",
			planIDs:     []string{s.testData.plan.ID, secondPlan.ID},
			expectedIDs: []string{s.testData.plan.ID, secondPlan.ID},
		},
		{
			name:        "unknown_plan_id_returns_empty",
			planIDs:     []string{"plan_missing"},
			expectedIDs: []string{},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			filter := types.NewNoLimitPlanFilter()
			filter.PlanIDs = tc.planIDs

			resp, err := s.service.GetPlans(s.GetContext(), filter)
			s.NoError(err)

			gotIDs := lo.Map(resp.Items, func(p *dto.PlanResponse, _ int) string { return p.Plan.ID })
			s.ElementsMatch(tc.expectedIDs, gotIDs)
			s.Equal(len(tc.expectedIDs), resp.Pagination.Total)
		})
	}
}

func (s *PlanListServiceSuite) TestGetPlansInvalidFilterReturnsValidationError() {
	filter := types.NewPlanFilter()
	filter.QueryFilter.Limit = lo.ToPtr(-1)
	_, err := s.service.GetPlans(s.GetContext(), filter)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *PlanListServiceSuite) TestGetPlansEmptyResult() {
	filter := types.NewPlanFilter()
	// No archived plans exist, so this must return an empty result
	filter.QueryFilter.Status = lo.ToPtr(types.StatusArchived)
	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 0)
	s.Equal(0, resp.Pagination.Total)
}

func (s *PlanListServiceSuite) TestGetPlansExpandPricesWithMetersAndPriceUnit() {
	filter := types.NewPlanFilter()
	filter.QueryFilter.Expand = lo.ToPtr("prices,meters,priceunit")

	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 1)

	prices := resp.Items[0].Prices
	s.Len(prices, 2)

	pricesByID := lo.KeyBy(prices, func(p *dto.PriceResponse) string { return p.Price.ID })

	// Usage price carries its expanded meter
	usage, ok := pricesByID["price_list_usage"]
	s.True(ok)
	s.NotNil(usage.Meter)
	s.Equal(s.testData.meter.ID, usage.Meter.ID)
	s.Equal(s.testData.meter.Name, usage.Meter.Name)

	// Custom price carries its expanded pricing unit
	custom, ok := pricesByID["price_list_custom"]
	s.True(ok)
	s.NotNil(custom.PricingUnit)
	s.Equal(s.testData.priceUnit.ID, custom.PricingUnit.PriceUnit.ID)
	s.True(custom.PricingUnit.PriceUnit.ConversionRate.Equal(decimal.NewFromFloat(0.5)))
}

func (s *PlanListServiceSuite) TestGetPlansExpandPricesWithNestedPriceUnit() {
	filter := types.NewPlanFilter()
	// Nested syntax: priceunit expansion scoped under prices
	filter.QueryFilter.Expand = lo.ToPtr("prices.priceunit")

	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Len(resp.Items[0].Prices, 2)

	pricesByID := lo.KeyBy(resp.Items[0].Prices, func(p *dto.PriceResponse) string { return p.Price.ID })
	custom, ok := pricesByID["price_list_custom"]
	s.True(ok)
	s.NotNil(custom.PricingUnit)
	s.Equal(s.testData.priceUnit.ID, custom.PricingUnit.PriceUnit.ID)
}

func (s *PlanListServiceSuite) TestGetPlansExpandEntitlementsWithFeatures() {
	filter := types.NewPlanFilter()
	filter.QueryFilter.Expand = lo.ToPtr("entitlements,features")
	filter.Sort = []*types.SortCondition{{Field: "created_at", Direction: types.SortDirectionDesc}}

	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 1)

	ents := resp.Items[0].Entitlements
	s.Len(ents, 1)
	s.Equal("ent_list", ents[0].Entitlement.ID)
	s.Equal(s.testData.plan.ID, ents[0].Entitlement.EntityID)
	s.NotNil(ents[0].Feature)
	s.Equal("feat_list", ents[0].Feature.ID)
}

func (s *PlanListServiceSuite) TestGetPlansExpandCreditGrants() {
	filter := types.NewPlanFilter()
	filter.QueryFilter.Expand = lo.ToPtr("credit_grant")

	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 1)

	grants := resp.Items[0].CreditGrants
	s.Len(grants, 1)
	s.Equal("cg_list", grants[0].CreditGrant.ID)
	s.True(grants[0].CreditGrant.Credits.Equal(decimal.NewFromInt(50)))
	s.Equal(s.testData.plan.ID, lo.FromPtr(grants[0].CreditGrant.PlanID))
}

func (s *PlanListServiceSuite) TestGetPlansExpandAllTogether() {
	// Second plan with no associations to verify per-plan bucketing
	barePlan := &plan.Plan{
		ID:        "plan_list_bare",
		Name:      "Bare List Plan",
		LookupKey: "list-bare",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), barePlan))

	filter := types.NewPlanFilter()
	filter.QueryFilter.Expand = lo.ToPtr("prices,meters,entitlements,features,credit_grant")

	resp, err := s.service.GetPlans(s.GetContext(), filter)
	s.NoError(err)
	s.Len(resp.Items, 2)

	itemsByID := lo.KeyBy(resp.Items, func(p *dto.PlanResponse) string { return p.Plan.ID })

	full, ok := itemsByID[s.testData.plan.ID]
	s.True(ok)
	s.Len(full.Prices, 2)
	s.Len(full.Entitlements, 1)
	s.Len(full.CreditGrants, 1)

	bare, ok := itemsByID[barePlan.ID]
	s.True(ok)
	s.Nil(bare.Prices)
	s.Nil(bare.Entitlements)
	s.Nil(bare.CreditGrants)
}
