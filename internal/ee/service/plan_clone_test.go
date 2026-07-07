package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// PlanCloneServiceSuite covers ClonePlan: cloning a plan with its active
// prices, published entitlements, and published plan-scoped credit grants.
type PlanCloneServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  PlanService
	testData struct {
		sourcePlan  *plan.Plan
		fixedPrice  *price.Price
		usagePrice  *price.Price
		feature     *feature.Feature
		entitlement *entitlement.Entitlement
		grant       *creditgrant.CreditGrant
		now         time.Time
	}
}

func TestPlanCloneService(t *testing.T) {
	suite.Run(t, new(PlanCloneServiceSuite))
}

func (s *PlanCloneServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewPlanService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.setupTestData()
}

func (s *PlanCloneServiceSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	displayOrder := 3
	s.testData.sourcePlan = &plan.Plan{
		ID:           "plan_clone_src",
		Name:         "Source Plan",
		LookupKey:    "clone-source",
		Description:  "Source plan description",
		Metadata:     types.Metadata{"tier": "gold", "region": "us"},
		DisplayOrder: &displayOrder,
		BaseModel:    types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.sourcePlan))

	s.testData.fixedPrice = &price.Price{
		ID:                 "price_clone_fixed",
		Amount:             decimal.NewFromInt(25),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.sourcePlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		LookupKey:          "clone-fixed-price",
		DisplayName:        "Fixed Fee",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.fixedPrice))

	s.testData.usagePrice = &price.Price{
		ID:                 "price_clone_usage",
		Amount:             decimal.NewFromFloat(0.1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.sourcePlan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            "meter_clone",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		DisplayName:        "Usage Fee",
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), s.testData.usagePrice))

	s.testData.feature = &feature.Feature{
		ID:        "feat_clone",
		Name:      "Clone Feature",
		Type:      types.FeatureTypeBoolean,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), s.testData.feature))

	s.testData.entitlement = &entitlement.Entitlement{
		ID:          "ent_clone",
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    s.testData.sourcePlan.ID,
		FeatureID:   s.testData.feature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), s.testData.entitlement)
	s.NoError(err)

	s.testData.grant = &creditgrant.CreditGrant{
		ID:             "cg_clone",
		Name:           "Clone Grant",
		Scope:          types.CreditGrantScopePlan,
		PlanID:         lo.ToPtr(s.testData.sourcePlan.ID),
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
		Priority:       lo.ToPtr(1),
		BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().CreditGrantRepo.Create(s.GetContext(), s.testData.grant)
	s.NoError(err)
}

func (s *PlanCloneServiceSuite) TestClonePlanValidation() {
	testCases := []struct {
		name   string
		id     string
		req    dto.ClonePlanRequest
		verify func(err error)
	}{
		{
			name: "empty_plan_id_returns_validation_error",
			id:   "",
			req:  dto.ClonePlanRequest{Name: "X", LookupKey: "x"},
			verify: func(err error) {
				s.Error(err)
				s.True(ierr.IsValidation(err))
			},
		},
		{
			name: "missing_name_returns_validation_error",
			id:   "plan_clone_src",
			req:  dto.ClonePlanRequest{LookupKey: "cloned-key"},
			verify: func(err error) {
				s.Error(err)
				s.True(ierr.IsValidation(err))
			},
		},
		{
			name: "missing_lookup_key_returns_validation_error",
			id:   "plan_clone_src",
			req:  dto.ClonePlanRequest{Name: "Cloned"},
			verify: func(err error) {
				s.Error(err)
				s.True(ierr.IsValidation(err))
			},
		},
		{
			name: "source_plan_not_found",
			id:   "plan_missing",
			req:  dto.ClonePlanRequest{Name: "Cloned", LookupKey: "cloned-key"},
			verify: func(err error) {
				s.Error(err)
				s.True(ierr.IsNotFound(err))
			},
		},
		{
			name: "lookup_key_already_taken_returns_already_exists",
			id:   "plan_clone_src",
			req:  dto.ClonePlanRequest{Name: "Cloned", LookupKey: "clone-source"},
			verify: func(err error) {
				s.Error(err)
				s.True(ierr.IsAlreadyExists(err))
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			_, err := s.service.ClonePlan(s.GetContext(), tc.id, tc.req)
			tc.verify(err)
		})
	}
}

func (s *PlanCloneServiceSuite) TestClonePlanCopiesPricesEntitlementsAndGrants() {
	resp, err := s.service.ClonePlan(s.GetContext(), s.testData.sourcePlan.ID, dto.ClonePlanRequest{
		Name:      "Cloned Plan",
		LookupKey: "cloned-plan",
	})
	s.NoError(err)
	s.NotNil(resp)

	// New plan identity and inherited fields
	s.NotEqual(s.testData.sourcePlan.ID, resp.Plan.ID)
	s.Equal("Cloned Plan", resp.Plan.Name)
	s.Equal("cloned-plan", resp.Plan.LookupKey)
	s.Equal(s.testData.sourcePlan.Description, resp.Plan.Description)
	s.Equal(s.testData.sourcePlan.DisplayOrder, resp.Plan.DisplayOrder)

	// Metadata merged from source plus source_plan_id marker
	s.Equal("gold", resp.Plan.Metadata["tier"])
	s.Equal("us", resp.Plan.Metadata["region"])
	s.Equal(s.testData.sourcePlan.ID, resp.Plan.Metadata["source_plan_id"])

	// Read back the plan through the repo
	storedPlan, err := s.GetStores().PlanRepo.Get(s.GetContext(), resp.Plan.ID)
	s.NoError(err)
	s.Equal("Cloned Plan", storedPlan.Name)
	s.Equal(types.StatusPublished, storedPlan.Status)

	// Prices: cloned onto the new plan with fresh IDs and cleared lookup keys
	storedPrices, err := s.GetStores().PriceRepo.List(s.GetContext(), types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{resp.Plan.ID}).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN))
	s.NoError(err)
	s.Len(storedPrices, 2)
	s.Len(resp.Prices, 2)
	for _, p := range storedPrices {
		s.NotEqual(s.testData.fixedPrice.ID, p.ID)
		s.NotEqual(s.testData.usagePrice.ID, p.ID)
		s.Equal(resp.Plan.ID, p.EntityID)
		s.Empty(p.LookupKey, "cloned price lookup key must be cleared")
	}
	amounts := lo.Map(storedPrices, func(p *price.Price, _ int) string { return p.Amount.String() })
	s.ElementsMatch([]string{decimal.NewFromInt(25).String(), decimal.NewFromFloat(0.1).String()}, amounts)

	// Entitlements: cloned onto the new plan, feature preserved
	storedEnts, err := s.GetStores().EntitlementRepo.List(s.GetContext(), types.NewNoLimitEntitlementFilter().
		WithPlanIDs([]string{resp.Plan.ID}))
	s.NoError(err)
	s.Len(storedEnts, 1)
	s.Len(resp.Entitlements, 1)
	s.NotEqual(s.testData.entitlement.ID, storedEnts[0].ID)
	s.Equal(resp.Plan.ID, storedEnts[0].EntityID)
	s.Equal(s.testData.feature.ID, storedEnts[0].FeatureID)
	s.True(storedEnts[0].IsEnabled)

	// Credit grants: cloned onto the new plan, credits preserved
	storedGrants, err := s.GetStores().CreditGrantRepo.GetByPlan(s.GetContext(), resp.Plan.ID)
	s.NoError(err)
	s.Len(storedGrants, 1)
	s.Len(resp.CreditGrants, 1)
	s.NotEqual(s.testData.grant.ID, storedGrants[0].ID)
	s.Equal(resp.Plan.ID, lo.FromPtr(storedGrants[0].PlanID))
	s.Equal(types.CreditGrantScopePlan, storedGrants[0].Scope)
	s.True(storedGrants[0].Credits.Equal(decimal.NewFromInt(100)))

	// Source plan untouched
	source, err := s.GetStores().PlanRepo.Get(s.GetContext(), s.testData.sourcePlan.ID)
	s.NoError(err)
	s.Equal("Source Plan", source.Name)
	s.Equal("clone-source", source.LookupKey)
}

func (s *PlanCloneServiceSuite) TestClonePlanAppliesRequestOverrides() {
	newDescription := "Overridden description"
	newDisplayOrder := 9

	resp, err := s.service.ClonePlan(s.GetContext(), s.testData.sourcePlan.ID, dto.ClonePlanRequest{
		Name:         "Overridden Clone",
		LookupKey:    "overridden-clone",
		Description:  &newDescription,
		DisplayOrder: &newDisplayOrder,
		Metadata:     types.Metadata{"tier": "silver", "extra": "yes"},
	})
	s.NoError(err)

	s.Equal(newDescription, resp.Plan.Description)
	s.Equal(&newDisplayOrder, resp.Plan.DisplayOrder)
	// Request metadata overrides overlapping source keys, source-only keys survive
	s.Equal("silver", resp.Plan.Metadata["tier"])
	s.Equal("us", resp.Plan.Metadata["region"])
	s.Equal("yes", resp.Plan.Metadata["extra"])
	s.Equal(s.testData.sourcePlan.ID, resp.Plan.Metadata["source_plan_id"])

	stored, err := s.GetStores().PlanRepo.Get(s.GetContext(), resp.Plan.ID)
	s.NoError(err)
	s.Equal(newDescription, stored.Description)
	s.Equal(9, lo.FromPtr(stored.DisplayOrder))
}

func (s *PlanCloneServiceSuite) TestClonePlanWithNoAssociations() {
	bare := &plan.Plan{
		ID:        "plan_clone_bare",
		Name:      "Bare Plan",
		LookupKey: "clone-bare",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), bare))

	resp, err := s.service.ClonePlan(s.GetContext(), bare.ID, dto.ClonePlanRequest{
		Name:      "Bare Clone",
		LookupKey: "bare-clone",
	})
	s.NoError(err)
	s.Len(resp.Prices, 0)
	s.Len(resp.Entitlements, 0)
	s.Len(resp.CreditGrants, 0)

	stored, err := s.GetStores().PlanRepo.Get(s.GetContext(), resp.Plan.ID)
	s.NoError(err)
	s.Equal("Bare Clone", stored.Name)
}

func (s *PlanCloneServiceSuite) TestClonePlanSkipsArchivedPrices() {
	archived := &price.Price{
		ID:                 "price_clone_archived",
		Amount:             decimal.NewFromInt(99),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.sourcePlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	archived.Status = types.StatusArchived
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), archived))

	resp, err := s.service.ClonePlan(s.GetContext(), s.testData.sourcePlan.ID, dto.ClonePlanRequest{
		Name:      "No Archived Clone",
		LookupKey: "no-archived-clone",
	})
	s.NoError(err)
	// Only the two published prices are cloned, not the archived one
	s.Len(resp.Prices, 2)
	clonedAmounts := lo.Map(resp.Prices, func(p *dto.PriceResponse, _ int) string { return p.Price.Amount.String() })
	s.NotContains(clonedAmounts, decimal.NewFromInt(99).String())
}
